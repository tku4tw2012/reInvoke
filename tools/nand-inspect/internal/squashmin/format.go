// Package squashmin implements a deliberately narrow, offline-only patch of the
// hash-pinned installed SquashFS. It is not a general-purpose filesystem writer.
package squashmin

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"io"
)

const (
	ImageBytes       = 48831891
	ImageSHA256      = "717041d874bba6a16cda6578101ab1b7e1ff7737ade1b0921b77c9e4e65f6170"
	InitSHA256       = "2b2a189751d3a2d7c9c5dcfba55da2ab5374cc3d0d1bbc03e809db1289442d14"
	CaptureSHA256    = "2fac4159fe23aa25581c29f6c90033af3a1126a02593db0bd47e2c10d2c09f19"
	RootfsOffset     = 0x02920000
	RootfsAllocation = 90 * 1024 * 1024
	EraseBytes       = 128 * 1024
	metadataBytes    = 8192
)

var le = binary.LittleEndian

// Disk definitions are from the archived Invoke-kernel/fs/squashfs:
// squashfs_fs.h, inode.c, dir.c, fragment.c, and block.c. In particular the
// "gzip" compressor uses zlib framing, not a .gz header, and directory sizes
// include three synthetic bytes for "." and "..".
type filesystem struct {
	image                                     []byte
	inodes, fragments                         uint32
	inodeTable, dirTable, fragmentTable, root uint64
}

type Location struct {
	InodeReference uint64 `json:"inode_reference"`
	InodeNumber    uint32 `json:"inode_number"`
	FragmentIndex  uint32 `json:"fragment_index"`
	FragmentStart  uint64 `json:"fragment_start"`
	StoredBytes    uint32 `json:"stored_bytes"`
	ExpandedBytes  int    `json:"expanded_bytes"`
	FileOffset     uint32 `json:"file_offset"`
	FileBytes      uint32 `json:"file_bytes"`
}

func region(b []byte, off, n uint64) ([]byte, error) {
	if off > uint64(len(b)) || n > uint64(len(b))-off {
		return nil, fmt.Errorf("truncated region at %#x, length %d", off, n)
	}
	return b[off : off+n], nil
}

// ByteReader prevents read-ahead hiding trailing bytes. The target kernel's
// zlib_wrapper.c checks that all input buffers were consumed at Z_STREAM_END.
func inflateExact(b []byte, limit int) ([]byte, error) {
	input := bytes.NewReader(b)
	r, err := zlib.NewReader(input)
	if err != nil {
		return nil, err
	}
	out, err := io.ReadAll(io.LimitReader(r, int64(limit)+1))
	closeErr := r.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if len(out) > limit {
		return nil, fmt.Errorf("expanded block exceeds %d bytes", limit)
	}
	if input.Len() != 0 {
		return nil, fmt.Errorf("zlib stream has %d trailing bytes", input.Len())
	}
	return out, nil
}

func parse(image []byte) (*filesystem, error) {
	h, err := region(image, 0, 96)
	if err != nil {
		return nil, err
	}
	if le.Uint32(h) != 0x73717368 || le.Uint16(h[28:]) != 4 || le.Uint16(h[30:]) != 0 ||
		le.Uint16(h[20:]) != 1 || le.Uint32(h[12:]) != EraseBytes || le.Uint16(h[22:]) != 17 {
		return nil, fmt.Errorf("unsupported SquashFS superblock")
	}
	if le.Uint64(h[40:]) != uint64(len(image)) || len(image) != ImageBytes {
		return nil, fmt.Errorf("conflicting filesystem size")
	}
	fs := &filesystem{image: image, inodes: le.Uint32(h[4:]), fragments: le.Uint32(h[16:]),
		inodeTable: le.Uint64(h[64:]), dirTable: le.Uint64(h[72:]),
		fragmentTable: le.Uint64(h[80:]), root: le.Uint64(h[32:])}
	if fs.inodes != 3886 || fs.fragments != 281 || fs.inodeTable < 96 ||
		fs.dirTable <= fs.inodeTable || fs.fragmentTable <= fs.dirTable ||
		fs.fragmentTable >= uint64(len(image)) {
		return nil, fmt.Errorf("conflicting table metadata")
	}
	return fs, nil
}

func (f *filesystem) metadata(pos, end uint64) ([]byte, uint64, error) {
	h, err := region(f.image, pos, 2)
	if err != nil {
		return nil, 0, err
	}
	size := uint64(le.Uint16(h) & 0x7fff)
	if size == 0 || size > metadataBytes || pos >= end || size+2 > end-pos {
		return nil, 0, fmt.Errorf("invalid metadata length at %#x", pos)
	}
	data, err := region(f.image, pos+2, size)
	if err != nil {
		return nil, 0, err
	}
	if le.Uint16(h)&0x8000 == 0 {
		data, err = inflateExact(data, metadataBytes)
		if err != nil {
			return nil, 0, fmt.Errorf("metadata at %#x: %w", pos, err)
		}
	}
	return data, pos + 2 + size, nil
}

func (f *filesystem) readMetadata(pos, offset, count, end uint64) ([]byte, error) {
	if count > EraseBytes {
		return nil, fmt.Errorf("metadata request too large")
	}
	out := make([]byte, 0, count)
	for uint64(len(out)) < count {
		block, next, err := f.metadata(pos, end)
		if err != nil {
			return nil, err
		}
		if offset >= uint64(len(block)) {
			return nil, fmt.Errorf("invalid metadata offset")
		}
		n := count - uint64(len(out))
		if n > uint64(len(block))-offset {
			n = uint64(len(block)) - offset
		}
		out = append(out, block[offset:offset+n]...)
		pos, offset = next, 0
	}
	return out, nil
}

func (f *filesystem) inode(ref uint64, size uint64) ([]byte, error) {
	return f.readMetadata(f.inodeTable+(ref>>16), ref&0xffff, size, f.dirTable)
}

func (f *filesystem) locate() (Location, []byte, error) {
	var loc Location
	root, err := f.inode(f.root, 32)
	if err != nil {
		return loc, nil, err
	}
	var start, size, offset uint64
	switch le.Uint16(root) {
	case 1:
		start, size, offset = uint64(le.Uint32(root[16:])), uint64(le.Uint16(root[24:])), uint64(le.Uint16(root[26:]))
	case 8:
		root, err = f.inode(f.root, 40)
		if err != nil {
			return loc, nil, err
		}
		start, size, offset = uint64(le.Uint32(root[24:])), uint64(le.Uint32(root[20:])), uint64(le.Uint16(root[34:]))
	default:
		return loc, nil, fmt.Errorf("root is not a directory")
	}
	if size < 3 {
		return loc, nil, fmt.Errorf("invalid directory size")
	}
	dir, err := f.readMetadata(f.dirTable+start, offset, size-3, f.fragmentTable)
	if err != nil {
		return loc, nil, err
	}
	found := false
	names := make(map[string]bool)
	var expectedInode uint32
	for len(dir) != 0 {
		if len(dir) < 12 {
			return loc, nil, fmt.Errorf("truncated directory header")
		}
		count, block, base := le.Uint32(dir)+1, le.Uint32(dir[4:]), le.Uint32(dir[8:])
		if count < 1 || count > 256 {
			return loc, nil, fmt.Errorf("invalid directory count")
		}
		dir = dir[12:]
		for i := uint32(0); i < count; i++ {
			if len(dir) < 8 {
				return loc, nil, fmt.Errorf("truncated directory entry")
			}
			n := int(le.Uint16(dir[6:])) + 1
			if n > 256 || n+8 > len(dir) {
				return loc, nil, fmt.Errorf("invalid directory name length")
			}
			name := string(dir[8 : 8+n])
			if names[name] || bytes.ContainsAny([]byte(name), "/\x00") || name == "." || name == ".." {
				return loc, nil, fmt.Errorf("conflicting directory entry")
			}
			names[name] = true
			if name == "init.rc" {
				if le.Uint16(dir[4:]) != 2 {
					return loc, nil, fmt.Errorf("init.rc is not a regular file")
				}
				found = true
				loc.InodeReference = uint64(block)<<16 | uint64(le.Uint16(dir))
				expectedInode = uint32(int64(base) + int64(int16(le.Uint16(dir[2:]))))
			}
			dir = dir[8+n:]
		}
	}
	if !found {
		return loc, nil, fmt.Errorf("missing init.rc")
	}
	inode, err := f.inode(loc.InodeReference, 32)
	if err != nil {
		return loc, nil, err
	}
	if le.Uint16(inode) != 2 || le.Uint32(inode[12:]) != expectedInode {
		return loc, nil, fmt.Errorf("conflicting init.rc inode")
	}
	loc.InodeNumber, loc.FragmentIndex = expectedInode, le.Uint32(inode[20:])
	loc.FileOffset, loc.FileBytes = le.Uint32(inode[24:]), le.Uint32(inode[28:])
	if loc.FileBytes == 0 || loc.FileBytes >= EraseBytes || loc.FragmentIndex >= f.fragments {
		return loc, nil, fmt.Errorf("init.rc is not a single fragment")
	}
	index, err := region(f.image, f.fragmentTable+uint64(loc.FragmentIndex/512)*8, 8)
	if err != nil {
		return loc, nil, err
	}
	table := le.Uint64(index)
	if table < f.dirTable || table >= f.fragmentTable {
		return loc, nil, fmt.Errorf("invalid fragment table pointer")
	}
	entry, err := f.readMetadata(table, uint64(loc.FragmentIndex%512)*16, 16, f.fragmentTable)
	if err != nil {
		return loc, nil, err
	}
	loc.FragmentStart, loc.StoredBytes = le.Uint64(entry), le.Uint32(entry[8:])
	if loc.StoredBytes == 0 || loc.StoredBytes > EraseBytes || loc.FragmentStart < 96 ||
		loc.FragmentStart >= f.inodeTable || uint64(loc.StoredBytes) > f.inodeTable-loc.FragmentStart ||
		le.Uint32(entry[12:]) != 0 {
		return loc, nil, fmt.Errorf("invalid compressed fragment extent")
	}
	stored, err := region(f.image, loc.FragmentStart, uint64(loc.StoredBytes))
	if err != nil {
		return loc, nil, err
	}
	expanded, err := inflateExact(stored, EraseBytes)
	if err != nil {
		return loc, nil, err
	}
	loc.ExpandedBytes = len(expanded)
	if uint64(loc.FileOffset)+uint64(loc.FileBytes) > uint64(len(expanded)) {
		return loc, nil, fmt.Errorf("file exceeds fragment")
	}
	return loc, expanded, nil
}
