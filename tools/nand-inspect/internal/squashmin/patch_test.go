package squashmin

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestExactStream(t *testing.T) {
	data := bytes.Repeat([]byte("a diagnostic fragment\n"), 400)
	compressed, err := compress(data, 9)
	if err != nil {
		t.Fatal(err)
	}
	for _, extra := range []int{0, 5, 500} {
		stream, err := exactStream(compressed, len(compressed)+extra)
		if err != nil {
			t.Fatal(err)
		}
		got, err := inflateExact(stream, len(data))
		if err != nil || !bytes.Equal(got, data) {
			t.Fatalf("extra=%d: %v", extra, err)
		}
	}
	if _, err := exactStream(compressed, len(compressed)-1); err == nil {
		t.Fatal("accepted oversize")
	}
	if _, err := exactStream(compressed, len(compressed)+1); err == nil {
		t.Fatal("accepted unrepresentable gap")
	}
	if _, err := inflateExact(append(compressed, 0), len(data)); err == nil {
		t.Fatal("accepted trailing bytes")
	}
	if _, err := inflateExact(compressed[:len(compressed)-1], len(data)); err == nil {
		t.Fatal("accepted truncated stream")
	}
	if _, err := diagnosticInit([]byte("unexpected startup")); err == nil {
		t.Fatal("accepted unexpected payload")
	}
	if _, _, err := Patch(make([]byte, ImageBytes), ""); err == nil {
		t.Fatal("accepted wrong hash")
	}
	if _, _, err := Patch([]byte("hsqs"), ""); err == nil {
		t.Fatal("accepted truncated image")
	}
}

func TestMetadataBoundaries(t *testing.T) {
	compressed, err := compress([]byte("abcd"), 9)
	if err != nil {
		t.Fatal(err)
	}
	image := make([]byte, 2)
	le.PutUint16(image, uint16(len(compressed)))
	image = append(image, compressed...)
	image = append(image, 4, 128, 'e', 'f', 'g', 'h')
	fs := &filesystem{image: image}
	got, err := fs.readMetadata(0, 2, 4, uint64(len(image)))
	if err != nil || string(got) != "cdef" {
		t.Fatalf("cross-block metadata: %q, %v", got, err)
	}
	for _, tc := range []struct{ offset, count, end uint64 }{
		{4, 1, uint64(len(image))},
		{0, 9, uint64(len(image))},
		{0, 1, 2},
		{0, EraseBytes + 1, uint64(len(image))},
	} {
		if _, err := fs.readMetadata(0, tc.offset, tc.count, tc.end); err == nil {
			t.Fatalf("accepted %+v", tc)
		}
	}
	if _, _, err := (&filesystem{image: []byte{0, 0}}).metadata(0, 2); err == nil {
		t.Fatal("accepted zero-length metadata")
	}
	if _, err := region(image, ^uint64(0), 1); err == nil {
		t.Fatal("accepted overflowing region")
	}
	if _, err := inflateExact(compressed, 3); err == nil {
		t.Fatal("accepted oversized expanded block")
	}
}

func TestConflictingDirectoryInode(t *testing.T) {
	image := make([]byte, 512)
	fs := &filesystem{image: image, inodeTable: 96, dirTable: 256, fragmentTable: 400}
	le.PutUint16(image[96:], 0x8000|64)
	root := image[98:130]
	le.PutUint16(root, 1)
	le.PutUint16(root[24:], 30) // 12-byte header + 8-byte entry + seven-byte name + 3
	init := image[130:162]
	le.PutUint16(init, 2)
	le.PutUint32(init[12:], 7)
	le.PutUint16(image[256:], 0x8000|27)
	dir := image[258:]
	le.PutUint32(dir[8:], 9) // Directory says inode 9, inode record says 7.
	le.PutUint16(dir[12:], 32)
	le.PutUint16(dir[16:], 2)
	le.PutUint16(dir[18:], 6)
	copy(dir[20:], "init.rc")
	if _, _, err := fs.locate(); err == nil || !strings.Contains(err.Error(), "conflicting init.rc inode") {
		t.Fatalf("did not reject conflicting inode metadata: %v", err)
	}
}

func TestInstalledImage(t *testing.T) {
	path := os.Getenv("SQUASHMIN_SOURCE")
	if path == "" {
		t.Skip("set SQUASHMIN_SOURCE for private archived image integration test")
	}
	image, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out, result, err := Patch(image, "/usr/bin/gzip")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("result: %+v", result)
	if len(out) != len(image) {
		t.Fatal("changed image length")
	}
	loc := result.Location
	if loc.InodeNumber != 799 || loc.FragmentIndex != 14 || loc.FragmentStart != 3768546 ||
		loc.StoredBytes != 48200 || loc.ExpandedBytes != 122555 || loc.FileOffset != 85888 || loc.FileBytes != 8862 {
		t.Fatalf("unexpected parsed installed location: %+v", loc)
	}
	if !bytes.Equal(out[:loc.FragmentStart], image[:loc.FragmentStart]) ||
		!bytes.Equal(out[loc.FragmentStart+uint64(loc.StoredBytes):], image[loc.FragmentStart+uint64(loc.StoredBytes):]) {
		t.Fatal("changed bytes outside fragment")
	}
	bad := append([]byte(nil), image...)
	le.PutUint64(bad[40:], ImageBytes-1)
	if _, err := parse(bad); err == nil {
		t.Fatal("accepted conflicting size metadata")
	}
	fs, err := parse(out)
	if err != nil {
		t.Fatal(err)
	}
	newLoc, expanded, err := fs.locate()
	if err != nil || newLoc != loc {
		t.Fatalf("candidate metadata changed: %+v %v", newLoc, err)
	}
	originalFS, err := parse(image)
	if err != nil {
		t.Fatal(err)
	}
	_, originalExpanded, err := originalFS.locate()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(image[fs.inodeTable:], out[fs.inodeTable:]) {
		t.Fatal("changed filesystem metadata")
	}
	if !bytes.Equal(expanded[:loc.FileOffset], originalExpanded[:loc.FileOffset]) ||
		!bytes.Equal(expanded[loc.FileOffset+loc.FileBytes:], originalExpanded[loc.FileOffset+loc.FileBytes:]) {
		t.Fatal("changed another file's bytes in the shared fragment")
	}
	newInit := expanded[loc.FileOffset : loc.FileOffset+loc.FileBytes]
	oldInit := originalExpanded[loc.FileOffset : loc.FileOffset+loc.FileBytes]
	oldLines, newLines := strings.Split(string(oldInit), "\n"), strings.Split(string(newInit), "\n")
	if len(oldLines) != len(newLines) {
		t.Fatal("changed line count")
	}
	changedLines, changedBytes := 0, 0
	for i := range oldLines {
		if oldLines[i] != newLines[i] {
			changedLines++
			if i+1 != 85 && i+1 != 87 {
				t.Fatalf("changed unexpected line %d", i+1)
			}
		}
	}
	for i := range oldInit {
		if oldInit[i] != newInit[i] {
			changedBytes++
		}
	}
	if changedLines != 2 || hash(newInit) != "329259c33cdcdba7ad064333e962ebaee3997928eddf23545e3bdea3dc7485e3" {
		t.Fatal("unexpected diagnostic payload")
	}
	t.Logf("exact payload changes: %d bytes, lines 85 and 87 only", changedBytes)
	if _, err := diagnosticInit(newInit); err == nil {
		t.Fatal("accepted already-patched payload")
	}
	if _, _, err := Patch(out, "/usr/bin/gzip"); err == nil {
		t.Fatal("accepted already-patched image")
	}
	for _, mutation := range []func([]byte){
		func(b []byte) { le.PutUint32(b, 0) },
		func(b []byte) { le.PutUint16(b[20:], 4) },
		func(b []byte) { le.PutUint16(b[28:], 3) },
		func(b []byte) { le.PutUint32(b[12:], 4096) },
		func(b []byte) { le.PutUint64(b[64:], uint64(len(b))+1) },
		func(b []byte) { le.PutUint64(b[80:], 0) },
	} {
		copy(bad, image)
		mutation(bad)
		if _, err := parse(bad); err == nil {
			t.Fatal("accepted conflicting superblock")
		}
	}
	copy(bad, image)
	bad[loc.FragmentStart+5] ^= 1
	if _, _, err := Patch(bad, "/usr/bin/gzip"); err == nil {
		t.Fatal("accepted mutated original fragment")
	}
}
