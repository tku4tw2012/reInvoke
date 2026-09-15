package squashmin

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"hash/adler32"
	"io"
	"os/exec"
)

func hash(b []byte) string { return fmt.Sprintf("%x", sha256.Sum256(b)) }

// Only these two reviewed lines may change; both replacements preserve length.
// adbd is started after selecting adb and before enabling the USB gadget.
func diagnosticInit(original []byte) ([]byte, error) {
	if hash(original) != InitSHA256 {
		return nil, fmt.Errorf("unexpected init.rc payload")
	}
	oldProduct := []byte("    write /sys/class/android_usb/android0/iProduct \"MRVL USB SDK\"\n")
	newProduct := []byte("    write /sys/class/android_usb/android0/iProduct \"reInvoke-min\"\n")
	oldComment := []byte("    # write /sys/class/android_usb/android0/f_acm/instances \"1\"\n")
	newStart := append([]byte("    start adbd"), bytes.Repeat([]byte(" "), len(oldComment)-len("    start adbd")-1)...)
	newStart = append(newStart, '\n')
	if bytes.Count(original, oldProduct) != 1 || bytes.Count(original, oldComment) != 1 {
		return nil, fmt.Errorf("unexpected diagnostic anchors")
	}
	out := bytes.Replace(original, oldProduct, newProduct, 1)
	out = bytes.Replace(out, oldComment, newStart, 1)
	if len(out) != len(original) {
		return nil, fmt.Errorf("diagnostic changed file length")
	}
	return out, nil
}

func stockADBInit(original []byte) ([]byte, error) {
	if hash(original) != InitSHA256 {
		return nil, fmt.Errorf("unexpected init.rc payload")
	}
	edits := [][2]string{
		{`    write /sys/class/android_usb/android0/iProduct "MRVL USB SDK"` + "\n",
			`    write /sys/class/android_usb/android0/iProduct "reInvoke-ADB"` + "\n"},
		{"    #start adbd\n", "    start adbd \n"},
		{"service adbd /sbin/adbd\n    disabled\n",
			"service adbd /sbin/adbd\n    # adb on\n"},
	}
	out := append([]byte(nil), original...)
	for _, edit := range edits {
		if len(edit[0]) != len(edit[1]) || bytes.Count(out, []byte(edit[0])) != 1 {
			return nil, fmt.Errorf("unexpected stock ADB anchor or replacement length")
		}
		out = bytes.Replace(out, []byte(edit[0]), []byte(edit[1]), 1)
	}
	if len(out) != len(original) {
		return nil, fmt.Errorf("stock ADB edit changed file length")
	}
	return out, nil
}

const (
	LegacyPurpose   = "offline early-adbd + reInvoke-min USB product diagnostic; not standalone reInvoke"
	StockADBPurpose = "offline original firmware with on-boot adbd enabled and reInvoke-ADB USB product; not standalone reInvoke"
)

func Purpose(result Result) (string, error) {
	switch result.Variant {
	case "":
		return LegacyPurpose, nil
	case "stock-adb":
		return StockADBPurpose, nil
	default:
		return "", fmt.Errorf("unknown diagnostic variant %q", result.Variant)
	}
}

func compress(data []byte, level int) ([]byte, error) {
	var b bytes.Buffer
	w, err := zlib.NewWriterLevel(&b, level)
	if err != nil {
		return nil, err
	}
	if _, err = w.Write(data); err != nil {
		return nil, err
	}
	if err = w.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// The installed fragment compresses less well with Go 1.18 than with GNU gzip.
// This optional host-only fallback uses an explicitly supplied existing gzip;
// it never runs anything extracted from the image. RFC 1952's fixed -n header
// and CRC/ISIZE are validated before replacing the framing with RFC 1950.
func compressGzip(data []byte, level int, path string) ([]byte, error) {
	cmd := exec.Command(path, "-n", fmt.Sprintf("-%d", level), "-c")
	cmd.Stdin = bytes.NewReader(data)
	gz, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("host gzip: %w", err)
	}
	if len(gz) < 18 || !bytes.Equal(gz[:8], []byte{31, 139, 8, 0, 0, 0, 0, 0}) {
		return nil, fmt.Errorf("unexpected gzip header")
	}
	input := bytes.NewReader(gz)
	r, err := gzip.NewReader(input)
	if err != nil {
		return nil, err
	}
	r.Multistream(false)
	expanded, err := io.ReadAll(io.LimitReader(r, int64(len(data))+1))
	closeErr := r.Close()
	if err != nil || closeErr != nil || input.Len() != 0 || !bytes.Equal(expanded, data) {
		return nil, fmt.Errorf("host gzip roundtrip failed")
	}
	out := append([]byte{0x78, 0xda}, gz[10:len(gz)-8]...)
	var checksum [4]byte
	binary.BigEndian.PutUint32(checksum[:], adler32.Checksum(data))
	out = append(out, checksum[:]...)
	return out, nil
}

// exactStream adds valid, empty, non-final DEFLATE stored blocks immediately
// after the zlib header, inside the stream. Each consumes five bytes and emits
// nothing. Nothing follows the original Adler-32 trailer. This deliberately
// rejects lengths it cannot represent instead of leaving unused trailing bytes.
func exactStream(stream []byte, size int) ([]byte, error) {
	if len(stream) < 6 || stream[1]&0x20 != 0 {
		return nil, fmt.Errorf("unsupported zlib header")
	}
	gap := size - len(stream)
	if gap < 0 {
		return nil, fmt.Errorf("oversize compression: %d exceeds slot %d", len(stream), size)
	}
	if gap%5 != 0 {
		return nil, fmt.Errorf("cannot fill %d bytes with empty stored blocks", gap)
	}
	out := make([]byte, 0, size)
	out = append(out, stream[:2]...)
	for i := 0; i < gap; i += 5 {
		out = append(out, 0, 0, 0, 255, 255)
	}
	out = append(out, stream[2:]...)
	return out, nil
}

type Result struct {
	Variant               string   `json:"variant,omitempty"`
	Location              Location `json:"location"`
	SourceSHA256          string   `json:"source_sha256"`
	CandidateSHA256       string   `json:"candidate_sha256"`
	OriginalInitSHA256    string   `json:"original_init_sha256"`
	CandidateInitSHA256   string   `json:"candidate_init_sha256"`
	CompressedStreamBytes int      `json:"compressed_stream_bytes"`
	EmptyBlockBytes       int      `json:"empty_deflate_block_bytes"`
	CompressionLevel      int      `json:"compression_level"`
	Compressor            string   `json:"compressor"`
}

// Patch accepts only the exact installed image. An empty gzipPath restricts
// compression to Go; otherwise GNU gzip is tried only if Go cannot fit.
func Patch(image []byte, gzipPath string) ([]byte, Result, error) {
	return patch(image, gzipPath, false)
}

// PatchStockADB enables the original on-boot start and removes only adbd's
// disabled flag, while retaining the file length and all filesystem metadata.
func PatchStockADB(image []byte, gzipPath string) ([]byte, Result, error) {
	return patch(image, gzipPath, true)
}

func patch(image []byte, gzipPath string, stockADB bool) ([]byte, Result, error) {
	var result Result
	if len(image) != ImageBytes || hash(image) != ImageSHA256 {
		return nil, result, fmt.Errorf("wrong installed filesystem size or SHA-256")
	}
	fs, err := parse(image)
	if err != nil {
		return nil, result, err
	}
	loc, block, err := fs.locate()
	if err != nil {
		return nil, result, err
	}
	original := block[loc.FileOffset : loc.FileOffset+loc.FileBytes]
	edit := diagnosticInit
	if stockADB {
		edit = stockADBInit
		result.Variant = "stock-adb"
	}
	replacement, err := edit(original)
	if err != nil {
		return nil, result, err
	}
	changed := append([]byte(nil), block...)
	copy(changed[loc.FileOffset:], replacement)
	var packed []byte
	var sizes []int
	for level := 9; level >= 1; level-- {
		compressed, err := compress(changed, level)
		if err != nil {
			return nil, result, err
		}
		sizes = append(sizes, len(compressed))
		packed, err = exactStream(compressed, int(loc.StoredBytes))
		if err == nil {
			result.CompressionLevel, result.CompressedStreamBytes = level, len(compressed)
			result.Compressor = "Go compress/zlib"
			break
		}
	}
	if packed == nil && gzipPath != "" {
		for level := 9; level >= 1; level-- {
			compressed, err := compressGzip(changed, level, gzipPath)
			if err != nil {
				return nil, result, err
			}
			sizes = append(sizes, len(compressed))
			packed, err = exactStream(compressed, int(loc.StoredBytes))
			if err == nil {
				result.CompressionLevel, result.CompressedStreamBytes = level, len(compressed)
				result.Compressor = "GNU gzip (validated RFC 1952 to RFC 1950 framing)"
				break
			}
		}
	}
	if packed == nil {
		return nil, result, fmt.Errorf("no exact fit: location %+v; compressed sizes levels 9..1 %v", loc, sizes)
	}
	roundtrip, err := inflateExact(packed, EraseBytes)
	if err != nil || !bytes.Equal(roundtrip, changed) {
		return nil, result, fmt.Errorf("compressed roundtrip failed: %v", err)
	}
	out := append([]byte(nil), image...)
	copy(out[loc.FragmentStart:], packed)
	result.Location, result.SourceSHA256, result.CandidateSHA256 = loc, hash(image), hash(out)
	result.OriginalInitSHA256, result.CandidateInitSHA256 = hash(original), hash(replacement)
	result.EmptyBlockBytes = len(packed) - result.CompressedStreamBytes
	return out, result, nil
}
