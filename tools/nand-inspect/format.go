package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"math"
)

const (
	imageMagic         = 0xd2ada3f1
	fixedHeaderSize    = 64
	recordSize         = 64
	versionHeaderSize  = 12
	versionWindowSize  = 4096
	invokePageSize     = 2048
	invokeEraseSize    = 131072
	squashfsMagic      = 0x73717368
	squashfsHeaderSize = 96
)

var littleEndian = binary.LittleEndian

type Geometry struct {
	PageBytes     uint64 `json:"page_bytes"`
	OOBBytes      uint64 `json:"declared_oob_bytes"`
	PagesPerBlock uint64 `json:"pages_per_block"`
	Blocks        uint64 `json:"blocks"`
	EraseBytes    uint64 `json:"erase_bytes"`
	DataBytes     uint64 `json:"data_bytes"`
}

type Version struct {
	Minor uint32 `json:"minor"`
	Major uint32 `json:"major"`
}

type Allocation struct {
	StartBlock   uint32 `json:"start_block"`
	Blocks       uint32 `json:"blocks"`
	StartByte    uint64 `json:"start_byte"`
	EndExclusive uint64 `json:"end_exclusive"`
	Bytes        uint64 `json:"bytes"`
}

type SquashFS struct {
	BytesUsed   uint64 `json:"bytes_used"`
	Inodes      uint32 `json:"inodes"`
	BlockBytes  uint32 `json:"block_bytes"`
	Compression uint16 `json:"compression_id"`
	Major       uint16 `json:"major"`
	Minor       uint16 `json:"minor"`
}

type ImageRecord struct {
	Name           string     `json:"name"`
	PayloadOffset  uint64     `json:"payload_offset"`
	StoredBytes    uint64     `json:"stored_bytes"`
	CRC32          uint32     `json:"crc32"`
	Version        Version    `json:"version"`
	ReservedBlocks uint32     `json:"reserved_blocks"`
	DataType       uint32     `json:"data_type"`
	PartitionType  uint32     `json:"partition_type"`
	Allocation     Allocation `json:"allocation"`
	SquashFS       *SquashFS  `json:"squashfs,omitempty"`
}

type Image struct {
	Geometry     Geometry      `json:"geometry"`
	Version      Version       `json:"version"`
	DDRType      uint8         `json:"ddr_type"`
	DDRChannels  uint8         `json:"ddr_channels"`
	CPUType      string        `json:"cpu_type"`
	Records      []ImageRecord `json:"records"`
	PayloadEnd   uint64        `json:"payload_end"`
	TrailerBytes uint64        `json:"trailer_bytes"`
	TrailerIsZIP bool          `json:"trailer_starts_with_zip"`
}

type TablePart struct {
	Version    Version    `json:"version"`
	Allocation Allocation `json:"allocation"`
}

type TableRecord struct {
	Name      string    `json:"name"`
	Part1     TablePart `json:"part1"`
	Part2     TablePart `json:"part2"`
	PartType  string    `json:"part_type"`
	SquashFS1 *SquashFS `json:"part1_squashfs,omitempty"`
}

type Capture struct {
	GeometryAssumption string        `json:"geometry_assumption"`
	Geometry           Geometry      `json:"geometry"`
	TableOffsets       []uint64      `json:"identical_crc_valid_table_offsets"`
	MissingOffsets     []uint64      `json:"table_magic_absent_offsets,omitempty"`
	Status             uint32        `json:"table_status"`
	Records            []TableRecord `json:"records"`
}

type Comparison struct {
	MatchingAllocations []string `json:"matching_named_allocations"`
	CaptureOnly         []string `json:"capture_only_records"`
	Limit               string   `json:"evidence_limit"`
}

func readBytes(reader io.ReaderAt, size, offset, length uint64) ([]byte, error) {
	if offset > size || length > size-offset || length > math.MaxInt {
		return nil, fmt.Errorf("read outside input: offset=%#x length=%#x size=%#x", offset, length, size)
	}
	data := make([]byte, int(length))
	if _, err := io.ReadFull(io.NewSectionReader(reader, int64(offset), int64(length)), data); err != nil {
		return nil, fmt.Errorf("read at %#x: %w", offset, err)
	}
	return data, nil
}

func multiply(a, b uint64) (uint64, error) {
	if b != 0 && a > math.MaxUint64/b {
		return 0, fmt.Errorf("geometry multiplication overflows: %d * %d", a, b)
	}
	return a * b, nil
}

func allocation(start, blocks uint32, geometry Geometry) (Allocation, error) {
	if blocks == 0 || uint64(start) > geometry.Blocks || uint64(blocks) > geometry.Blocks-uint64(start) {
		return Allocation{}, fmt.Errorf("allocation outside device: block=%d count=%d device_blocks=%d", start, blocks, geometry.Blocks)
	}
	offset := uint64(start) * geometry.EraseBytes
	length := uint64(blocks) * geometry.EraseBytes
	return Allocation{start, blocks, offset, offset + length, length}, nil
}

func cString(data []byte, allowEmpty bool) (string, error) {
	end := bytes.IndexByte(data, 0)
	if end < 0 || (!allowEmpty && end == 0) {
		return "", fmt.Errorf("invalid fixed-width string")
	}
	for _, value := range data[:end] {
		if value < 0x20 || value > 0x7e {
			return "", fmt.Errorf("non-printable fixed-width string")
		}
	}
	if !allZero(data[end:]) {
		return "", fmt.Errorf("nonzero string padding")
	}
	return string(data[:end]), nil
}

func allZero(data []byte) bool {
	for _, value := range data {
		if value != 0 {
			return false
		}
	}
	return true
}

func parseVersion(data []byte) Version {
	return Version{littleEndian.Uint32(data), littleEndian.Uint32(data[4:])}
}

func inspectSquashFS(reader io.ReaderAt, size, offset, limit uint64) (*SquashFS, error) {
	if limit < squashfsHeaderSize {
		return nil, nil
	}
	header, err := readBytes(reader, size, offset, squashfsHeaderSize)
	if err != nil {
		return nil, err
	}
	if littleEndian.Uint32(header) != squashfsMagic {
		return nil, nil
	}
	fs := &SquashFS{
		Inodes:      littleEndian.Uint32(header[4:]),
		BlockBytes:  littleEndian.Uint32(header[12:]),
		Compression: littleEndian.Uint16(header[20:]),
		Major:       littleEndian.Uint16(header[28:]),
		Minor:       littleEndian.Uint16(header[30:]),
		BytesUsed:   littleEndian.Uint64(header[40:]),
	}
	if fs.Major != 4 || fs.Minor != 0 || fs.BytesUsed < squashfsHeaderSize ||
		fs.BytesUsed > limit || fs.BlockBytes < 4096 || fs.BlockBytes > 1048576 ||
		fs.BlockBytes&(fs.BlockBytes-1) != 0 {
		return nil, fmt.Errorf("invalid or unsupported SquashFS superblock at %#x", offset)
	}
	return fs, nil
}

func inspectImage(reader io.ReaderAt, size uint64) (*Image, error) {
	if size > math.MaxInt64 {
		return nil, fmt.Errorf("input exceeds supported size")
	}
	header, err := readBytes(reader, size, 0, fixedHeaderSize)
	if err != nil {
		return nil, err
	}
	if littleEndian.Uint32(header) != imageMagic {
		return nil, fmt.Errorf("not a supported Marvell image: bad magic")
	}
	if !allZero(header[35:]) {
		return nil, fmt.Errorf("unsupported nonzero reserved header fields")
	}
	geometry := Geometry{
		PageBytes:     uint64(littleEndian.Uint32(header[12:])),
		OOBBytes:      uint64(littleEndian.Uint32(header[16:])),
		PagesPerBlock: uint64(littleEndian.Uint32(header[20:])),
		Blocks:        uint64(littleEndian.Uint32(header[24:])),
	}
	if geometry.PageBytes == 0 || geometry.PagesPerBlock == 0 || geometry.Blocks == 0 {
		return nil, fmt.Errorf("zero geometry dimension")
	}
	geometry.EraseBytes, err = multiply(geometry.PageBytes, geometry.PagesPerBlock)
	if err != nil {
		return nil, err
	}
	geometry.DataBytes, err = multiply(geometry.EraseBytes, geometry.Blocks)
	if err != nil {
		return nil, err
	}
	count := uint64(littleEndian.Uint32(header[28:]))
	if count == 0 || count > 1024 {
		return nil, fmt.Errorf("unsupported descriptor count %d", count)
	}
	table, err := readBytes(reader, size, fixedHeaderSize, count*recordSize)
	if err != nil {
		return nil, err
	}
	image := &Image{
		Geometry:    geometry,
		Version:     parseVersion(header[4:12]),
		DDRType:     header[32] & 0xf,
		DDRChannels: header[32] >> 4,
		CPUType:     fmt.Sprintf("%02x%02x", header[33], header[34]),
	}
	cursor := uint64(fixedHeaderSize) + count*recordSize
	names := make(map[string]bool)
	for index := uint64(0); index < count; index++ {
		record := table[index*recordSize : (index+1)*recordSize]
		name, err := cString(record[:16], false)
		if err != nil {
			return nil, fmt.Errorf("record %d: %w", index, err)
		}
		if names[name] {
			return nil, fmt.Errorf("duplicate image record %q", name)
		}
		names[name] = true
		stored := littleEndian.Uint64(record[16:])
		if stored == 0 || cursor > size || stored > size-cursor {
			return nil, fmt.Errorf("payload %q exceeds input or is empty", name)
		}
		region, err := allocation(littleEndian.Uint32(record[40:]), littleEndian.Uint32(record[44:]), geometry)
		if err != nil {
			return nil, fmt.Errorf("record %q: %w", name, err)
		}
		dataType := littleEndian.Uint32(record[48:])
		partitionType := littleEndian.Uint32(record[52:])
		reservedBlocks := littleEndian.Uint32(record[36:])
		if dataType > 2 || partitionType > 2 || !allZero(record[56:]) ||
			reservedBlocks > region.Blocks {
			return nil, fmt.Errorf("unsupported descriptor fields for %q", name)
		}
		if dataType == 0 && stored > region.Bytes {
			return nil, fmt.Errorf("normal payload %q is larger than its allocation", name)
		}
		hash := crc32.NewIEEE()
		if _, err := io.CopyN(hash, io.NewSectionReader(reader, int64(cursor), int64(stored)), int64(stored)); err != nil {
			return nil, fmt.Errorf("checksum payload %q: %w", name, err)
		}
		expectedCRC := littleEndian.Uint32(record[24:])
		if hash.Sum32() != expectedCRC {
			return nil, fmt.Errorf("payload %q CRC mismatch: computed=%08x expected=%08x", name, hash.Sum32(), expectedCRC)
		}
		fs, err := inspectSquashFS(reader, size, cursor, stored)
		if err != nil {
			return nil, err
		}
		image.Records = append(image.Records, ImageRecord{
			Name: name, PayloadOffset: cursor, StoredBytes: stored,
			CRC32: expectedCRC, Version: parseVersion(record[28:36]),
			ReservedBlocks: reservedBlocks, DataType: dataType,
			PartitionType: partitionType, Allocation: region, SquashFS: fs,
		})
		cursor += stored
	}
	image.PayloadEnd = cursor
	image.TrailerBytes = size - cursor
	if image.TrailerBytes >= 4 {
		signature, err := readBytes(reader, size, cursor, 4)
		if err != nil {
			return nil, err
		}
		image.TrailerIsZIP = bytes.Equal(signature, []byte{'P', 'K', 3, 4})
	}
	return image, nil
}

func inspectCapture(reader io.ReaderAt, size uint64) (*Capture, error) {
	if size > math.MaxInt64 || size < 9*invokeEraseSize || size%invokeEraseSize != 0 {
		return nil, fmt.Errorf("capture must contain at least nine complete 128 KiB erase blocks")
	}
	geometry := Geometry{
		PageBytes: invokePageSize, PagesPerBlock: invokeEraseSize / invokePageSize,
		Blocks: size / invokeEraseSize, EraseBytes: invokeEraseSize, DataBytes: size,
	}
	capture := &Capture{
		GeometryAssumption: "Invoke data-area profile: 2048-byte pages, 128-KiB erase blocks; OOB absent",
		Geometry:           geometry,
	}
	var firstTable []byte
	for block := uint64(1); block < 9; block++ {
		offset := (block+1)*invokeEraseSize - versionWindowSize
		window, err := readBytes(reader, size, offset, versionWindowSize)
		if err != nil {
			return nil, err
		}
		if littleEndian.Uint32(window) != imageMagic {
			capture.MissingOffsets = append(capture.MissingOffsets, offset)
			continue
		}
		count := uint64(littleEndian.Uint32(window[8:]))
		if count == 0 || count > (versionWindowSize-versionHeaderSize-4)/recordSize {
			return nil, fmt.Errorf("invalid version-table count at %#x", offset)
		}
		length := uint64(versionHeaderSize) + count*recordSize + 4
		table := window[:length]
		if crc32.ChecksumIEEE(table) != 0xffffffff {
			return nil, fmt.Errorf("version-table CRC mismatch at %#x", offset)
		}
		if firstTable != nil {
			if !bytes.Equal(firstTable, table) {
				return nil, fmt.Errorf("CRC-valid version tables disagree at %#x; no layout selected", offset)
			}
			capture.TableOffsets = append(capture.TableOffsets, offset)
			continue
		}
		firstTable = table
		capture.Status = littleEndian.Uint32(table[4:])
		names := make(map[string]bool)
		for index := uint64(0); index < count; index++ {
			record := table[versionHeaderSize+index*recordSize : versionHeaderSize+(index+1)*recordSize]
			name, err := cString(record[:16], false)
			if err != nil {
				return nil, fmt.Errorf("version record %d: %w", index, err)
			}
			if names[name] {
				return nil, fmt.Errorf("duplicate version record %q", name)
			}
			names[name] = true
			partType, err := cString(record[48:], true)
			if err != nil {
				return nil, fmt.Errorf("version record %q type: %w", name, err)
			}
			first, err := allocation(littleEndian.Uint32(record[24:]), littleEndian.Uint32(record[28:]), geometry)
			if err != nil {
				return nil, fmt.Errorf("version record %q part1: %w", name, err)
			}
			second, err := allocation(littleEndian.Uint32(record[40:]), littleEndian.Uint32(record[44:]), geometry)
			if err != nil {
				return nil, fmt.Errorf("version record %q part2: %w", name, err)
			}
			fs, err := inspectSquashFS(reader, size, first.StartByte, first.Bytes)
			if err != nil {
				return nil, err
			}
			capture.Records = append(capture.Records, TableRecord{
				Name: name, PartType: partType,
				Part1:     TablePart{parseVersion(record[16:24]), first},
				Part2:     TablePart{parseVersion(record[32:40]), second},
				SquashFS1: fs,
			})
		}
		capture.TableOffsets = append(capture.TableOffsets, offset)
	}
	if firstTable == nil {
		return nil, fmt.Errorf("no version tables found at the source-defined Invoke offsets")
	}
	return capture, nil
}

func compareAllocations(image *Image, capture *Capture) (*Comparison, error) {
	if image.Geometry.DataBytes != capture.Geometry.DataBytes ||
		image.Geometry.EraseBytes != capture.Geometry.EraseBytes ||
		image.Geometry.PageBytes != capture.Geometry.PageBytes {
		return nil, fmt.Errorf("container and capture geometries differ")
	}
	records := make(map[string]TableRecord)
	for _, record := range capture.Records {
		records[record.Name] = record
	}
	comparison := &Comparison{
		MatchingAllocations: []string{},
		CaptureOnly:         []string{},
		Limit:               "Allocation agreement only; not writer erase scope, active-slot selection, signature acceptance, or write approval.",
	}
	matched := make(map[string]bool)
	for _, record := range image.Records {
		tableRecord, found := records[record.Name]
		if !found || record.Allocation != tableRecord.Part1.Allocation ||
			record.Allocation != tableRecord.Part2.Allocation {
			return nil, fmt.Errorf("allocation mismatch or ambiguous version-table copies for %q", record.Name)
		}
		comparison.MatchingAllocations = append(comparison.MatchingAllocations, record.Name)
		matched[record.Name] = true
	}
	for _, record := range capture.Records {
		if !matched[record.Name] {
			comparison.CaptureOnly = append(comparison.CaptureOnly, record.Name)
		}
	}
	return comparison, nil
}
