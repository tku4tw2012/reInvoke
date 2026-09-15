package main

import (
	"bytes"
	"hash/crc32"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func syntheticSquashFS() []byte {
	data := make([]byte, squashfsHeaderSize)
	littleEndian.PutUint32(data, squashfsMagic)
	littleEndian.PutUint32(data[4:], 7)
	littleEndian.PutUint32(data[12:], 131072)
	littleEndian.PutUint16(data[20:], 1)
	littleEndian.PutUint16(data[28:], 4)
	littleEndian.PutUint64(data[40:], uint64(len(data)))
	return data
}

func syntheticImage() []byte {
	payloads := [][]byte{syntheticSquashFS(), []byte("OOB!")}
	data := make([]byte, fixedHeaderSize+len(payloads)*recordSize)
	littleEndian.PutUint32(data, imageMagic)
	littleEndian.PutUint32(data[4:], 1549)
	littleEndian.PutUint32(data[12:], invokePageSize)
	littleEndian.PutUint32(data[20:], invokeEraseSize/invokePageSize)
	littleEndian.PutUint32(data[24:], 16)
	littleEndian.PutUint32(data[28:], uint32(len(payloads)))
	data[32] = 0x23
	copy(data[33:], "A0")
	for index, name := range []string{"rootfs", "app"} {
		record := data[fixedHeaderSize+index*recordSize : fixedHeaderSize+(index+1)*recordSize]
		copy(record, name)
		littleEndian.PutUint64(record[16:], uint64(len(payloads[index])))
		littleEndian.PutUint32(record[24:], crc32.ChecksumIEEE(payloads[index]))
		littleEndian.PutUint32(record[28:], 123)
		if index == 0 {
			littleEndian.PutUint32(record[40:], 9)
			littleEndian.PutUint32(record[44:], 2)
		} else {
			littleEndian.PutUint32(record[40:], 11)
			littleEndian.PutUint32(record[44:], 3)
			littleEndian.PutUint32(record[48:], 1)
		}
	}
	for _, payload := range payloads {
		data = append(data, payload...)
	}
	return append(data, 'P', 'K', 3, 4, 0, 0)
}

func tableCRC(table []byte) {
	littleEndian.PutUint32(table[len(table)-4:], ^crc32.ChecksumIEEE(table[:len(table)-4]))
}

func syntheticTable() []byte {
	table := make([]byte, versionHeaderSize+2*recordSize+4)
	littleEndian.PutUint32(table, imageMagic)
	littleEndian.PutUint32(table[4:], 0x55504544)
	littleEndian.PutUint32(table[8:], 2)
	for index, name := range []string{"rootfs", "app"} {
		record := table[versionHeaderSize+index*recordSize : versionHeaderSize+(index+1)*recordSize]
		copy(record, name)
		littleEndian.PutUint32(record[16:], 123)
		start, blocks := uint32(9), uint32(2)
		if index == 1 {
			start, blocks = 11, 3
		}
		littleEndian.PutUint32(record[24:], start)
		littleEndian.PutUint32(record[28:], blocks)
		littleEndian.PutUint32(record[40:], start)
		littleEndian.PutUint32(record[44:], blocks)
		copy(record[48:], "MLC")
	}
	tableCRC(table)
	return table
}

func syntheticCapture() []byte {
	capture := make([]byte, 16*invokeEraseSize)
	table := syntheticTable()
	for block := 1; block < 9; block++ {
		copy(capture[(block+1)*invokeEraseSize-versionWindowSize:], table)
	}
	copy(capture[9*invokeEraseSize:], syntheticSquashFS())
	return capture
}

func TestImageStructureAndCRC(t *testing.T) {
	data := syntheticImage()
	image, err := inspectImage(bytes.NewReader(data), uint64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if len(image.Records) != 2 || image.Geometry.Blocks != 16 ||
		image.Geometry.EraseBytes != invokeEraseSize || image.DDRType != 3 || image.DDRChannels != 2 {
		t.Fatalf("unexpected image header: %+v", image)
	}
	rootfs, app := image.Records[0], image.Records[1]
	if rootfs.PayloadOffset != 192 || rootfs.Allocation.StartByte != 9*invokeEraseSize ||
		rootfs.Allocation.Bytes != 2*invokeEraseSize || rootfs.SquashFS == nil ||
		rootfs.SquashFS.BytesUsed != squashfsHeaderSize {
		t.Fatalf("unexpected rootfs metadata: %+v", rootfs)
	}
	if app.DataType != 1 || app.PayloadOffset != 192+squashfsHeaderSize {
		t.Fatalf("OOB descriptor or concatenation incorrect: %+v", app)
	}
	if image.TrailerBytes != 6 || !image.TrailerIsZIP {
		t.Fatalf("trailer was lost or included in last payload: %+v", image)
	}
}

func TestImageRejectsShortReaderDespitePrefixCRC(t *testing.T) {
	data := syntheticImage()[:192+squashfsHeaderSize+4]
	littleEndian.PutUint64(data[128+16:], 8)
	// The stored CRC still matches the readable four-byte prefix.
	_, err := inspectImage(bytes.NewReader(data), uint64(len(data)+4))
	if err == nil || !strings.Contains(err.Error(), "checksum payload") {
		t.Fatalf("accepted early EOF with a matching prefix CRC: %v", err)
	}
}

func TestImageRejectsMalformedData(t *testing.T) {
	tests := []struct {
		name   string
		mutate func([]byte) []byte
		want   string
	}{
		{"short header", func(b []byte) []byte { return b[:12] }, "outside input"},
		{"bad magic", func(b []byte) []byte { b[0] ^= 1; return b }, "bad magic"},
		{"zero dimension", func(b []byte) []byte { littleEndian.PutUint32(b[12:], 0); return b }, "zero geometry"},
		{"overflow geometry", func(b []byte) []byte {
			littleEndian.PutUint32(b[12:], math.MaxUint32)
			littleEndian.PutUint32(b[20:], math.MaxUint32)
			littleEndian.PutUint32(b[24:], math.MaxUint32)
			return b
		}, "overflows"},
		{"zero count", func(b []byte) []byte { littleEndian.PutUint32(b[28:], 0); return b }, "descriptor count"},
		{"huge count", func(b []byte) []byte { littleEndian.PutUint32(b[28:], math.MaxUint32); return b }, "descriptor count"},
		{"short table", func(b []byte) []byte { return b[:100] }, "outside input"},
		{"empty payload", func(b []byte) []byte { littleEndian.PutUint64(b[80:], 0); return b }, "empty"},
		{"oversized payload", func(b []byte) []byte { littleEndian.PutUint64(b[80:], math.MaxUint64); return b }, "exceeds input"},
		{"truncated payload", func(b []byte) []byte { return b[:200] }, "exceeds input"},
		{"payload CRC", func(b []byte) []byte { b[200] ^= 1; return b }, "CRC mismatch"},
		{"outside NAND", func(b []byte) []byte { littleEndian.PutUint32(b[104:], 16); return b }, "allocation outside"},
		{"allocation overflow", func(b []byte) []byte { littleEndian.PutUint32(b[108:], math.MaxUint32); return b }, "allocation outside"},
		{"unterminated name", func(b []byte) []byte { copy(b[64:80], "abcdefghijklmnop"); return b }, "fixed-width string"},
		{"nonprintable name", func(b []byte) []byte { b[64] = 1; return b }, "non-printable"},
		{"name padding", func(b []byte) []byte { b[79] = 1; return b }, "string padding"},
		{"duplicate name", func(b []byte) []byte { copy(b[128:144], b[64:80]); return b }, "duplicate"},
		{"unknown data type", func(b []byte) []byte { littleEndian.PutUint32(b[112:], 3); return b }, "unsupported descriptor"},
		{"unknown partition type", func(b []byte) []byte { littleEndian.PutUint32(b[116:], 3); return b }, "unsupported descriptor"},
		{"reserved header", func(b []byte) []byte { b[63] = 1; return b }, "reserved header"},
		{"reserved descriptor", func(b []byte) []byte { b[127] = 1; return b }, "unsupported descriptor"},
		{"invalid reserved blocks", func(b []byte) []byte { littleEndian.PutUint32(b[100:], 3); return b }, "unsupported descriptor"},
		{"oversized SquashFS", func(b []byte) []byte {
			littleEndian.PutUint64(b[192+40:], 1000)
			littleEndian.PutUint32(b[88:], crc32.ChecksumIEEE(b[192:192+squashfsHeaderSize]))
			return b
		}, "SquashFS"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data := test.mutate(syntheticImage())
			_, err := inspectImage(bytes.NewReader(data), uint64(len(data)))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("got %v, wanted error containing %q", err, test.want)
			}
		})
	}
}

func TestCaptureIndependentTables(t *testing.T) {
	data := syntheticCapture()
	capture, err := inspectCapture(bytes.NewReader(data), uint64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if len(capture.TableOffsets) != 8 || capture.TableOffsets[0] != 0x3f000 ||
		capture.TableOffsets[7] != 0x11f000 || capture.Status != 0x55504544 {
		t.Fatalf("wrong tables: %+v", capture)
	}
	rootfs := capture.Records[0]
	if rootfs.Name != "rootfs" || rootfs.Part1.Allocation != rootfs.Part2.Allocation ||
		rootfs.Part1.Version.Minor != 123 || rootfs.Part2.Version.Minor != 0 ||
		rootfs.SquashFS1 == nil || rootfs.SquashFS1.BytesUsed != squashfsHeaderSize {
		t.Fatalf("wrong rootfs record: %+v", rootfs)
	}
}

func TestCaptureRejectsMalformedMetadata(t *testing.T) {
	tests := []struct {
		name   string
		mutate func([]byte) []byte
		want   string
	}{
		{"short capture", func(b []byte) []byte { return b[:invokeEraseSize] }, "nine complete"},
		{"nonaligned capture", func(b []byte) []byte { return b[:len(b)-1] }, "complete"},
		{"no tables", func(b []byte) []byte { clearTables(b); return b }, "no version tables"},
		{"corrupt mirror", func(b []byte) []byte { b[0x5f000+20] ^= 1; return b }, "CRC mismatch"},
		{"invalid count", func(b []byte) []byte { littleEndian.PutUint32(b[0x3f000+8:], math.MaxUint32); return b }, "count"},
		{"valid conflicting mirror", func(b []byte) []byte {
			table := b[0x5f000 : 0x5f000+len(syntheticTable())]
			littleEndian.PutUint32(table[4:], 1)
			tableCRC(table)
			return b
		}, "CRC-valid version tables disagree"},
		{"allocation outside", func(b []byte) []byte {
			table := b[0x3f000 : 0x3f000+len(syntheticTable())]
			littleEndian.PutUint32(table[versionHeaderSize+24:], 16)
			tableCRC(table)
			return b
		}, "allocation outside"},
		{"duplicate record", func(b []byte) []byte {
			table := b[0x3f000 : 0x3f000+len(syntheticTable())]
			copy(table[versionHeaderSize+recordSize:versionHeaderSize+recordSize+16],
				table[versionHeaderSize:versionHeaderSize+16])
			tableCRC(table)
			return b
		}, "duplicate version"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data := test.mutate(syntheticCapture())
			_, err := inspectCapture(bytes.NewReader(data), uint64(len(data)))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("got %v, wanted error containing %q", err, test.want)
			}
		})
	}
}

func clearTables(data []byte) {
	for block := 1; block < 9; block++ {
		littleEndian.PutUint32(data[(block+1)*invokeEraseSize-versionWindowSize:], 0)
	}
}

func TestMissingTableIsReported(t *testing.T) {
	data := syntheticCapture()
	littleEndian.PutUint32(data[0x3f000:], 0)
	capture, err := inspectCapture(bytes.NewReader(data), uint64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if len(capture.TableOffsets) != 7 || len(capture.MissingOffsets) != 1 ||
		capture.MissingOffsets[0] != 0x3f000 {
		t.Fatalf("missing table was silently ignored: %+v", capture)
	}
}

func TestComparisonChecksAddressesNotNames(t *testing.T) {
	imageData, captureData := syntheticImage(), syntheticCapture()
	image, err := inspectImage(bytes.NewReader(imageData), uint64(len(imageData)))
	if err != nil {
		t.Fatal(err)
	}
	capture, err := inspectCapture(bytes.NewReader(captureData), uint64(len(captureData)))
	if err != nil {
		t.Fatal(err)
	}
	comparison, err := compareAllocations(image, capture)
	if err != nil || len(comparison.MatchingAllocations) != 2 {
		t.Fatalf("comparison failed: %+v %v", comparison, err)
	}
	capture.Records[0].Part2.Allocation.StartByte++
	if _, err := compareAllocations(image, capture); err == nil {
		t.Fatal("accepted a different second-copy address with the same name")
	}
	capture.Geometry.DataBytes++
	if _, err := compareAllocations(image, capture); err == nil {
		t.Fatal("accepted different geometry")
	}
}

func TestCLIRejectsCorruptionWithoutSuccessOutput(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "image")
	data := syntheticImage()
	data[200] ^= 1
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	status := run([]string{"container", path}, &stdout, &stderr)
	if status != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "CRC mismatch") {
		t.Fatalf("status=%d stdout=%s stderr=%s", status, &stdout, &stderr)
	}
}

func TestCLICompare(t *testing.T) {
	dir := t.TempDir()
	imagePath, capturePath := filepath.Join(dir, "image"), filepath.Join(dir, "capture")
	if err := os.WriteFile(imagePath, syntheticImage(), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(capturePath, syntheticCapture(), 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if status := run([]string{"compare", imagePath, capturePath}, &stdout, &stderr); status != 0 {
		t.Fatalf("status=%d stderr=%s", status, &stderr)
	}
	for _, want := range []string{`"inspection_only": true`, `"matching_named_allocations"`, `"sha256"`, `"rootfs"`} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("missing %s in report", want)
		}
	}
}

func TestCLIUsageAndNonFiles(t *testing.T) {
	for _, args := range [][]string{{}, {"flash", "image"}, {"compare", "image"}, {"capture", "a", "b"}} {
		var stdout, stderr bytes.Buffer
		if status := run(args, &stdout, &stderr); status != 2 {
			t.Fatalf("args=%v status=%d", args, status)
		}
	}
	var stdout, stderr bytes.Buffer
	if status := run([]string{"container", "/dev/null"}, &stdout, &stderr); status != 1 || stdout.Len() != 0 {
		t.Fatalf("device node accepted: status=%d", status)
	}
	stdout.Reset()
	stderr.Reset()
	if status := run([]string{"--help"}, &stdout, &stderr); status != 0 || stdout.Len() == 0 {
		t.Fatalf("help failed: status=%d", status)
	}
}

func TestBoundsArithmetic(t *testing.T) {
	if _, err := multiply(math.MaxUint64, 2); err == nil {
		t.Fatal("multiplication overflow accepted")
	}
	if _, err := readBytes(bytes.NewReader(nil), 5, math.MaxUint64, 1); err == nil {
		t.Fatal("offset overflow accepted")
	}
	if _, err := readBytes(bytes.NewReader(nil), 5, 1, math.MaxUint64); err == nil {
		t.Fatal("length overflow accepted")
	}
}
