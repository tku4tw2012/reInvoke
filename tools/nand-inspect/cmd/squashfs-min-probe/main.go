// squashfs-min-probe creates offline candidate artifacts, never device writes.
package main

import (
	"bytes"
	"compress/zlib"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/tku4tw2012/reinvoke/tools/nand-inspect/internal/regularfile"
	"github.com/tku4tw2012/reinvoke/tools/nand-inspect/internal/squashmin"
)

func digest(b []byte) string { return fmt.Sprintf("%x", sha256.Sum256(b)) }

type eraseBlock struct {
	RootfsIndex     int    `json:"rootfs_index"`
	CaptureIndex    int    `json:"capture_index"`
	CaptureStart    int    `json:"capture_start"`
	ChangedBytes    int    `json:"changed_bytes"`
	FirstDifference int    `json:"first_difference_in_block"`
	LastDifference  int    `json:"last_difference_in_block"`
	OriginalSHA256  string `json:"original_sha256"`
	CandidateSHA256 string `json:"candidate_sha256"`
}

type proposal struct {
	Purpose                   string           `json:"purpose"`
	WriteApproved             bool             `json:"write_approved"`
	RuntimeVerified           bool             `json:"runtime_verified"`
	SignatureStatus           string           `json:"signature_status"`
	ImageBytes                int              `json:"image_bytes"`
	RootfsOffset              int              `json:"rootfs_offset"`
	AllocationBytes           int              `json:"allocation_bytes"`
	EraseBytes                int              `json:"erase_bytes"`
	CaptureSHA256             string           `json:"capture_sha256"`
	AllocationOriginalSHA256  string           `json:"allocation_original_sha256"`
	AllocationCandidateSHA256 string           `json:"allocation_candidate_sha256"`
	AllocationTailSHA256      string           `json:"unchanged_allocation_tail_sha256"`
	ChangedBytes              int              `json:"changed_bytes"`
	Patch                     squashmin.Result `json:"patch"`
	Blocks                    []eraseBlock     `json:"blocks"`
}

func readImage(path string) ([]byte, error) {
	f, err := regularfile.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if st.Size() != squashmin.ImageBytes {
		return nil, fmt.Errorf("wrong image length")
	}
	return io.ReadAll(io.LimitReader(f, squashmin.ImageBytes+1))
}

func readAllocation(path string, image []byte) ([]byte, error) {
	f, err := regularfile.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if st.Size() != 256*1024*1024 {
		return nil, fmt.Errorf("wrong capture length")
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, err
	}
	if fmt.Sprintf("%x", h.Sum(nil)) != squashmin.CaptureSHA256 {
		return nil, fmt.Errorf("wrong capture SHA-256")
	}
	allocation := make([]byte, squashmin.RootfsAllocation)
	if _, err := f.ReadAt(allocation, squashmin.RootfsOffset); err != nil {
		return nil, err
	}
	if !bytes.Equal(allocation[:len(image)], image) {
		return nil, fmt.Errorf("capture and installed filesystem disagree")
	}
	return allocation, nil
}

func outsideRepository(out string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	for dir := cwd; ; dir = filepath.Dir(dir) {
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			rel, err := filepath.Rel(dir, out)
			if err != nil {
				return err
			}
			if rel == "." || (rel != ".." && !bytes.HasPrefix([]byte(rel), []byte(".."+string(filepath.Separator)))) {
				return fmt.Errorf("private artifacts must be outside the repository")
			}
			return nil
		}
		if filepath.Dir(dir) == dir {
			return fmt.Errorf("run from within the repository to enforce artifact isolation")
		}
	}
}

func writeNew(dir, name string, data []byte) error {
	f, err := os.OpenFile(filepath.Join(dir, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func expand(b []byte) ([]byte, error) {
	r, err := zlib.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(io.LimitReader(r, squashmin.EraseBytes+1))
}

func run() error {
	source := flag.String("source", "", "exact installed-rootfs.squashfs (regular file)")
	capture := flag.String("capture", "", "hash-pinned data-only NAND capture (regular file)")
	output := flag.String("out", "", "NEW external archive directory; parent must exist")
	gzipPath := flag.String("gzip", "/usr/bin/gzip", "existing host GNU gzip; empty means Go-only (may not fit)")
	stockADB := flag.Bool("stock-adb", false, "enable original on-boot adbd and remove its disabled flag")
	flag.Parse()
	if *source == "" || *capture == "" || *output == "" || flag.NArg() != 0 {
		return fmt.Errorf("required: -source FILE -capture FILE -out NEW_EXTERNAL_DIRECTORY")
	}
	out, err := filepath.Abs(*output)
	if err != nil {
		return err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(out))
	if err != nil {
		return err
	}
	out = filepath.Join(parent, filepath.Base(out))
	if err := outsideRepository(out); err != nil {
		return err
	}
	if _, err := os.Lstat(out); !os.IsNotExist(err) {
		return fmt.Errorf("output already exists or cannot be inspected")
	}
	image, err := readImage(*source)
	if err != nil {
		return err
	}
	allocation, err := readAllocation(*capture, image)
	if err != nil {
		return err
	}
	patch := squashmin.Patch
	if *stockADB {
		patch = squashmin.PatchStockADB
	}
	candidate, result, err := patch(image, *gzipPath)
	if err != nil {
		return err
	}
	purpose, err := squashmin.Purpose(result)
	if err != nil {
		return err
	}
	p := proposal{Purpose: purpose,
		SignatureStatus: "unknown; no boot/signature acceptance claim",
		ImageBytes:      len(candidate), RootfsOffset: squashmin.RootfsOffset, AllocationBytes: len(allocation),
		EraseBytes: squashmin.EraseBytes, CaptureSHA256: squashmin.CaptureSHA256,
		AllocationOriginalSHA256: digest(allocation),
		AllocationTailSHA256:     digest(allocation[len(image):]), Patch: result}
	var oldBlocks, newBlocks []byte
	for start := 0; start < len(allocation); start += squashmin.EraseBytes {
		before := allocation[start : start+squashmin.EraseBytes]
		after := append([]byte(nil), before...)
		for i := 0; i < len(after) && start+i < len(candidate); i++ {
			after[i] = candidate[start+i]
		}
		block := eraseBlock{RootfsIndex: start / squashmin.EraseBytes,
			CaptureIndex: (squashmin.RootfsOffset + start) / squashmin.EraseBytes,
			CaptureStart: squashmin.RootfsOffset + start, FirstDifference: -1}
		for i := range before {
			if before[i] != after[i] {
				block.ChangedBytes++
				if block.FirstDifference < 0 {
					block.FirstDifference = i
				}
				block.LastDifference = i
			}
		}
		if block.ChangedBytes > 0 {
			block.OriginalSHA256, block.CandidateSHA256 = digest(before), digest(after)
			p.Blocks = append(p.Blocks, block)
			p.ChangedBytes += block.ChangedBytes
			oldBlocks, newBlocks = append(oldBlocks, before...), append(newBlocks, after...)
		}
	}
	copy(allocation, candidate)
	p.AllocationCandidateSHA256 = digest(allocation)
	if digest(allocation[len(image):]) != p.AllocationTailSHA256 {
		return fmt.Errorf("allocation padding changed")
	}
	loc := result.Location
	start, end := loc.FragmentStart, loc.FragmentStart+uint64(loc.StoredBytes)
	originalExpanded, err := expand(image[start:end])
	if err != nil {
		return err
	}
	candidateExpanded, err := expand(candidate[start:end])
	if err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	if err := os.Mkdir(out, 0700); err != nil {
		return err
	}
	artifacts := map[string][]byte{
		"minimal-probe.squashfs":       candidate,
		"changed-blocks-original.bin":  oldBlocks,
		"changed-blocks-candidate.bin": newBlocks,
		"fragment-original.zlib":       image[start:end],
		"fragment-candidate.zlib":      candidate[start:end],
		"fragment-original.expanded":   originalExpanded,
		"fragment-candidate.expanded":  candidateExpanded,
		"original-init.rc":             originalExpanded[loc.FileOffset : loc.FileOffset+loc.FileBytes],
		"candidate-init.rc":            candidateExpanded[loc.FileOffset : loc.FileOffset+loc.FileBytes],
		"PROPOSAL.json":                append(encoded, '\n'),
	}
	for name, data := range artifacts {
		if err := writeNew(out, name, data); err != nil {
			return err
		}
	}
	fmt.Printf("%s\n%d changed bytes in %d erase blocks; offline only, write_approved=false\n", out, p.ChangedBytes, len(p.Blocks))
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
