package squashmin

import (
	"bytes"
	"os"
	"testing"
)

func TestStockADBRejectsUnknownInputAndVariant(t *testing.T) {
	if _, err := stockADBInit([]byte("service adbd /sbin/adbd\n")); err == nil {
		t.Fatal("unrecognized original init accepted")
	}
	if _, _, err := PatchStockADB(make([]byte, ImageBytes), ""); err == nil {
		t.Fatal("unrecognized source filesystem accepted")
	}
	if _, err := Purpose(Result{Variant: "unreviewed"}); err == nil {
		t.Fatal("unknown diagnostic variant accepted")
	}
	if purpose, err := Purpose(Result{Variant: "stock-adb"}); err != nil || purpose != StockADBPurpose {
		t.Fatal("stock ADB purpose not explicit")
	}
}

func TestStockADBInstalledImage(t *testing.T) {
	source := os.Getenv("SQUASHMIN_SOURCE")
	if source == "" {
		t.Skip("set SQUASHMIN_SOURCE to the pinned original filesystem")
	}
	image, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	candidate, result, err := PatchStockADB(image, "/usr/bin/gzip")
	if err != nil {
		t.Fatal(err)
	}
	if result.Variant != "stock-adb" || len(candidate) != len(image) ||
		result.SourceSHA256 != ImageSHA256 || result.OriginalInitSHA256 != InitSHA256 ||
		result.CandidateSHA256 == ImageSHA256 {
		t.Fatal("incorrect stock diagnostic identity")
	}
	oldFS, err := parse(image)
	if err != nil {
		t.Fatal(err)
	}
	newFS, err := parse(candidate)
	if err != nil {
		t.Fatal(err)
	}
	oldLoc, oldBlock, err := oldFS.locate()
	if err != nil {
		t.Fatal(err)
	}
	newLoc, newBlock, err := newFS.locate()
	if err != nil {
		t.Fatal(err)
	}
	if oldLoc != newLoc || oldLoc != result.Location {
		t.Fatal("filesystem metadata changed")
	}
	start, end := oldLoc.FileOffset, oldLoc.FileOffset+oldLoc.FileBytes
	before, after := oldBlock[start:end], newBlock[start:end]
	want, err := stockADBInit(before)
	if err != nil || !bytes.Equal(after, want) {
		t.Fatal("extracted init differs from exact intended edits")
	}
	if !bytes.Equal(oldBlock[:start], newBlock[:start]) ||
		!bytes.Equal(oldBlock[end:], newBlock[end:]) {
		t.Fatal("another file sharing the fragment changed")
	}
	oldLines, newLines := bytes.Split(before, []byte{'\n'}), bytes.Split(after, []byte{'\n'})
	if len(oldLines) != len(newLines) {
		t.Fatal("init line count changed")
	}
	var changed []int
	for index := range oldLines {
		if !bytes.Equal(oldLines[index], newLines[index]) {
			changed = append(changed, index+1)
		}
	}
	if len(changed) != 3 || changed[0] != 85 || changed[1] != 135 || changed[2] != 235 {
		t.Fatalf("unexpected changed startup lines: %v", changed)
	}
	if !bytes.Equal(newLines[134], []byte("    start adbd ")) ||
		bytes.Contains(after, []byte("service adbd /sbin/adbd\n    disabled\n")) ||
		!bytes.Contains(after, []byte(`iProduct "reInvoke-ADB"`)) {
		t.Fatal("ADB enablement or diagnostic product missing")
	}
	fragmentStart := oldLoc.FragmentStart
	fragmentEnd := fragmentStart + uint64(oldLoc.StoredBytes)
	if !bytes.Equal(image[:fragmentStart], candidate[:fragmentStart]) ||
		!bytes.Equal(image[fragmentEnd:], candidate[fragmentEnd:]) {
		t.Fatal("bytes outside the single fragment changed")
	}
	image[0] ^= 1
	if _, _, err := PatchStockADB(image, "/usr/bin/gzip"); err == nil {
		t.Fatal("source corruption accepted")
	}
}
