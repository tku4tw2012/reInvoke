//go:build !nandpilot

package probewrite

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPinnedArtifactChecks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "input")
	if err := os.WriteFile(path, []byte("wrong"), 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := checkFile(file, 6, digest([]byte("wrong"))); err == nil {
		t.Fatal("wrong size accepted")
	}
	if err := checkFile(file, 5, ImageHash); err == nil {
		t.Fatal("wrong hash accepted")
	}
	if err := checkFile(file, 5, digest([]byte("wrong"))); err != nil {
		t.Fatal(err)
	}
	if bundle, err := Load(path, path); err == nil {
		bundle.Close()
		t.Fatal("unapproved fixture accepted by production loader")
	}
	if bundle, err := Load("/dev/null", path); err == nil {
		bundle.Close()
		t.Fatal("device input accepted")
	}
}

func TestFixedExtentArithmetic(t *testing.T) {
	if !HeaderLast && (Start != 0x02ca0000 || End != 0x02ce0000 || Span != 262144 ||
		ImageBytes != Span || Blocks != 2 || Start/EraseBytes != 357) {
		t.Fatal("fixed proposal geometry changed")
	}
	var bundle Bundle
	if _, err := bundle.block(-1, false); err == nil {
		t.Fatal("negative artifact block accepted")
	}
	if _, err := bundle.block(Blocks, false); err == nil {
		t.Fatal("past-end artifact block accepted")
	}
}

func TestOriginalPlanHashCannotDrift(t *testing.T) {
	plan := Plan{OriginalSHA256: OriginalHash, ImageSHA256: ImageHash}
	if err := validatePlanHashes(plan); err != nil {
		t.Fatal(err)
	}
	plan.OriginalSHA256 = digest([]byte("modified between initial hash and planning"))
	if err := validatePlanHashes(plan); err == nil {
		t.Fatal("original block hashes accepted without verifying their aggregate")
	}
	plan.OriginalSHA256 = OriginalHash
	plan.ImageSHA256 = digest(nil)
	if err := validatePlanHashes(plan); err == nil {
		t.Fatal("modified candidate fragment accepted")
	}
}

func TestPlanUsesNeutralFragmentOrder(t *testing.T) {
	bundle, _ := fixture(t)
	data, err := json.Marshal(bundle.Plan)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	if _, present := fields["write_order_superblock_last"]; present {
		t.Fatal("fragment order incorrectly claims a superblock is written last")
	}
	wantOrder := "[0,1]"
	if HeaderLast {
		wantOrder = "[1,0]"
	}
	if string(fields["write_order"]) != wantOrder || bundle.Plan.WriteAuthorized {
		t.Fatalf("unexpected offline plan: %s", data)
	}
}

func TestLoadRejectsLegacySizesAndUnpinnedFragments(t *testing.T) {
	for _, size := range []int64{83 * EraseBytes, 99 * EraseBytes, 48832512, 373 * EraseBytes, DeviceBytes, Span - 1, Span + 1, Span} {
		file, err := os.Create(filepath.Join(t.TempDir(), "unapproved.bin"))
		if err != nil {
			t.Fatal(err)
		}
		if err := file.Truncate(size); err != nil {
			file.Close()
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		bundle, err := Load(file.Name(), file.Name())
		if err == nil {
			bundle.Close()
			t.Fatalf("accepted unapproved artifact of size %d", size)
		}
		if size == Span && !strings.Contains(err.Error(), "SHA-256 mismatch") {
			t.Fatalf("same-size unapproved fragment did not reach hash check: %v", err)
		}
	}
}

func TestShortFragmentIsNotPadded(t *testing.T) {
	bundle, _ := fixture(t)
	bundle.image = bytes.NewReader(make([]byte, Span-1))
	if err := bundle.buildPlan(); err == nil {
		t.Fatal("truncated candidate fragment was silently padded")
	}
}

func TestRetainedTwoBlockArtifacts(t *testing.T) {
	if HeaderLast {
		t.Skip("retained two-block artifacts belong only to the default profile")
	}
	dir := os.Getenv("NAND_TWO_BLOCK_CANDIDATE_DIR")
	if dir == "" {
		t.Skip("set NAND_TWO_BLOCK_CANDIDATE_DIR, NAND_TWO_BLOCK_CAPTURE and NAND_TWO_BLOCK_SOURCE for retained-artifact verification")
	}
	imagePath := filepath.Join(dir, "changed-blocks-candidate.bin")
	originalPath := filepath.Join(dir, "changed-blocks-original.bin")
	bundle, err := Load(imagePath, originalPath)
	if err != nil {
		t.Fatal(err)
	}
	defer bundle.Close()
	if bundle.Plan.Start != 0x02ca0000 || bundle.Plan.End != 0x02ce0000 ||
		bundle.Plan.Span != 262144 || bundle.Plan.ChangedBlocks != 2 ||
		!reflect.DeepEqual(bundle.Plan.WriteOrder, []int{0, 1}) || bundle.Plan.WriteAuthorized {
		t.Fatalf("wrong retained plan: %+v", bundle.Plan)
	}
	wantBlocks := []Block{
		{0, 0x02ca0000, "214e8410c83512506009451d6cc12df7484673e148aae9e42c14a215a05b127e", "8a08aba2eaed2c8f8965121aef0569e34072e8e3b928832a860d221cd05bb5ca", true},
		{1, 0x02cc0000, "40d7f5281c300706c35e0055c9bd6ec396d037b7037452b90c72813c48bf5744", "c167fc59585f9bd326b4d7968f32e6a51afa8633d62ad6d0e523e93671672b91", true},
	}
	if !reflect.DeepEqual(bundle.Plan.Blocks, wantBlocks) {
		t.Fatalf("retained block pins differ: %+v", bundle.Plan.Blocks)
	}
	if err := validatePlanHashes(bundle.Plan); err != nil {
		t.Fatal(err)
	}
	openPinned := func(path string, size int64, hash string) *os.File {
		t.Helper()
		file, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { file.Close() })
		if err := checkFile(file, size, hash); err != nil {
			t.Fatal(err)
		}
		return file
	}
	capture := openPinned(os.Getenv("NAND_TWO_BLOCK_CAPTURE"), DeviceBytes, "2fac4159fe23aa25581c29f6c90033af3a1126a02593db0bd47e2c10d2c09f19")
	source := openPinned(os.Getenv("NAND_TWO_BLOCK_SOURCE"), 48831891, "717041d874bba6a16cda6578101ab1b7e1ff7737ade1b0921b77c9e4e65f6170")
	fullImage := openPinned(filepath.Join(dir, "minimal-probe.squashfs"), 48831891, "1d7b26012a9feed017439b030c1965315e39464d2044e98c50aa3ef9017d3594")
	read := func(file io.ReaderAt, offset int64, length int) []byte {
		t.Helper()
		data := make([]byte, length)
		if n, err := file.ReadAt(data, offset); err != nil || n != len(data) {
			t.Fatalf("read retained bytes at %#x: n=%d err=%v", offset, n, err)
		}
		return data
	}
	for index := 0; index < Blocks; index++ {
		probe, err := bundle.verifiedBlock(index, false)
		if err != nil {
			t.Fatal(err)
		}
		original, err := bundle.verifiedBlock(index, true)
		if err != nil {
			t.Fatal(err)
		}
		rootfsOffset := int64(28+index) * EraseBytes
		if !bytes.Equal(probe, read(fullImage, rootfsOffset, EraseBytes)) ||
			!bytes.Equal(original, read(source, rootfsOffset, EraseBytes)) ||
			!bytes.Equal(original, read(capture, Start+int64(index)*EraseBytes, EraseBytes)) {
			t.Fatalf("fragment %d differs from retained full artifacts", index)
		}
	}
	if digest(read(capture, Start-EraseBytes, EraseBytes)) != BeforeHash ||
		digest(read(capture, End, EraseBytes)) != AfterHash {
		t.Fatal("adjacent-block pins differ from retained capture")
	}
	for block := 1; block < 9; block++ {
		if digest(read(capture, int64((block+1)*EraseBytes-4096), 848)) != TableHash {
			t.Fatalf("version-table copy %d differs from retained capture", block)
		}
	}
	for _, paths := range [][2]string{
		{fullImage.Name(), originalPath}, {source.Name(), originalPath},
		{capture.Name(), originalPath}, {originalPath, imagePath},
		{imagePath, imagePath}, {imagePath, capture.Name()},
	} {
		if rejected, err := Load(paths[0], paths[1]); err == nil {
			rejected.Close()
			t.Fatalf("accepted non-fragment or swapped input: %v", paths)
		}
	}
	corrupt := read(bundle.image, 0, int(Span))
	corrupt[EraseBytes] ^= 1
	corruptPath := filepath.Join(t.TempDir(), "corrupt-candidate.bin")
	if err := os.WriteFile(corruptPath, corrupt, 0600); err != nil {
		t.Fatal(err)
	}
	if rejected, err := Load(corruptPath, originalPath); err == nil {
		rejected.Close()
		t.Fatal("accepted corrupted candidate fragment")
	}
}

type failedWriter struct{}

func (failedWriter) Write([]byte) (int, error) { return 0, os.ErrPermission }

func TestOOBBackupFailurePreventsWrite(t *testing.T) {
	bundle, device := fixture(t)
	_, err := Prepare(context.Background(), device, bundle, failedWriter{}, &mockJournal{})
	if err == nil || device.enabled || len(device.erases) > 0 {
		t.Fatal("OOB evidence failure accepted")
	}
}
