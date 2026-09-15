package probewrite

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"os"

	"github.com/tku4tw2012/reinvoke/tools/nand-inspect/internal/regularfile"
)

const (
	End               = Start + Span
	EraseBytes        = 131072
	PageBytes         = 2048
	VisibleOOB        = 32
	Blocks            = int(Span / EraseBytes)
	ImageBytes  int64 = Span
	DeviceBytes int64 = 268435456
)

type Block struct {
	Index          int    `json:"index"`
	AbsoluteOffset int64  `json:"absolute_offset"`
	OriginalSHA256 string `json:"original_sha256"`
	ProbeSHA256    string `json:"probe_sha256"`
	Changed        bool   `json:"changed"`
}

type Plan struct {
	Start            int64   `json:"start_byte"`
	End              int64   `json:"end_exclusive"`
	Span             int64   `json:"extent_bytes"`
	ImageSHA256      string  `json:"image_sha256"`
	OriginalSHA256   string  `json:"original_sha256"`
	ChangedBlocks    int     `json:"changed_blocks"`
	WriteOrder       []int   `json:"write_order"`
	Blocks           []Block `json:"blocks"`
	WriteAuthorized  bool    `json:"write_authorized"`
	Profile          string  `json:"profile,omitempty"`
	FilesystemSHA256 string  `json:"filesystem_sha256,omitempty"`
}

type Bundle struct {
	Plan     Plan
	image    io.ReaderAt
	original io.ReaderAt
	files    []*os.File
}

func digest(data []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

func checkFile(file *os.File, expectedBytes int64, expectedHash string) error {
	stat, err := file.Stat()
	if err != nil {
		return err
	}
	if stat.Size() != expectedBytes {
		return fmt.Errorf("wrong file size for %s: got %d expected %d", file.Name(), stat.Size(), expectedBytes)
	}
	hash := sha256.New()
	n, err := io.CopyN(hash, io.NewSectionReader(file, 0, expectedBytes), expectedBytes)
	if err != nil || n != expectedBytes {
		return fmt.Errorf("hash input %s: bytes=%d error=%v", file.Name(), n, err)
	}
	if fmt.Sprintf("%x", hash.Sum(nil)) != expectedHash {
		return fmt.Errorf("SHA-256 mismatch for %s", file.Name())
	}
	return nil
}

func Load(imagePath, originalPath string) (*Bundle, error) {
	bundle := &Bundle{}
	for _, input := range []struct {
		path string
		size int64
		hash string
	}{{imagePath, ImageBytes, ImageHash}, {originalPath, Span, OriginalHash}} {
		file, err := regularfile.Open(input.path)
		if err != nil {
			bundle.Close()
			return nil, err
		}
		bundle.files = append(bundle.files, file)
		if err := checkFile(file, input.size, input.hash); err != nil {
			bundle.Close()
			return nil, err
		}
	}
	bundle.image, bundle.original = bundle.files[0], bundle.files[1]
	if err := bundle.buildPlan(); err != nil {
		bundle.Close()
		return nil, err
	}
	return bundle, nil
}

func (b *Bundle) Close() error {
	var first error
	for _, file := range b.files {
		if err := file.Close(); err != nil && first == nil {
			first = err
		}
	}
	b.files = nil
	return first
}

func (b *Bundle) block(index int, restore bool) ([]byte, error) {
	if index < 0 || index >= Blocks {
		return nil, fmt.Errorf("block index outside fixed target: %d", index)
	}
	data := make([]byte, EraseBytes)
	offset := int64(index) * EraseBytes
	reader := b.original
	if !restore {
		reader = b.image
	}
	n, err := reader.ReadAt(data, offset)
	if err != nil || n != len(data) {
		return nil, fmt.Errorf("read artifact block %d: bytes=%d error=%v", index, n, err)
	}
	return data, nil
}

func (b *Bundle) verifiedBlock(index int, restore bool) ([]byte, error) {
	data, err := b.block(index, restore)
	if err != nil {
		return nil, err
	}
	expected := b.Plan.Blocks[index].ProbeSHA256
	if restore {
		expected = b.Plan.Blocks[index].OriginalSHA256
	}
	if digest(data) != expected {
		return nil, fmt.Errorf("artifact changed after planning at block %d", index)
	}
	return data, nil
}

func (b *Bundle) buildPlan() error {
	b.Plan = Plan{Start: Start, End: End, Span: Span, Profile: PlanProfileName, FilesystemSHA256: FilesystemHash}
	imageHash := sha256.New()
	originalHash := sha256.New()
	for index := 0; index < Blocks; index++ {
		original, err := b.block(index, true)
		if err != nil {
			return err
		}
		probe, err := b.block(index, false)
		if err != nil {
			return err
		}
		imageHash.Write(probe)
		originalHash.Write(original)
		changed := !bytes.Equal(original, probe)
		b.Plan.Blocks = append(b.Plan.Blocks, Block{
			Index: index, AbsoluteOffset: Start + int64(index)*EraseBytes,
			OriginalSHA256: digest(original), ProbeSHA256: digest(probe), Changed: changed,
		})
		if changed {
			b.Plan.ChangedBlocks++
			if !HeaderLast || index != 0 {
				b.Plan.WriteOrder = append(b.Plan.WriteOrder, index)
			}
		}
	}
	// Header-last ordering for a complete filesystem is not power-loss atomic.
	if HeaderLast && b.Plan.Blocks[0].Changed {
		b.Plan.WriteOrder = append(b.Plan.WriteOrder, 0)
	}
	b.Plan.ImageSHA256 = fmt.Sprintf("%x", imageHash.Sum(nil))
	b.Plan.OriginalSHA256 = fmt.Sprintf("%x", originalHash.Sum(nil))
	if len(b.files) != 0 {
		return validatePlanHashes(b.Plan)
	}
	return nil
}

func validatePlanHashes(plan Plan) error {
	if plan.OriginalSHA256 != OriginalHash {
		return fmt.Errorf("original changed during block planning")
	}
	if plan.ImageSHA256 != ImageHash {
		return fmt.Errorf("candidate fragment changed during block planning")
	}
	return nil
}
