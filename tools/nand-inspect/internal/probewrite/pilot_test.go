//go:build nandpilot

package probewrite

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"testing"
)

func TestPilotFixedGeometryAndApprovalIsolation(t *testing.T) {
	if Start != 0x02920000 || Span != 0x02680000 || End != 0x04fa0000 || Blocks != 308 ||
		FilesystemBytes != 40267776 || Span > 90*1024*1024 || End > 0x08320000 ||
		ImageBytes != Span || !HeaderLast || PlanProfileName == "" || FilesystemHash == "" {
		t.Fatal("invalid fixed complete-rootfs profile")
	}
	if Span != ((FilesystemBytes+EraseBytes-1)/EraseBytes)*EraseBytes {
		t.Fatal("pilot extent is not the erase-rounded filesystem size")
	}
	_, data := partitionArg(1, "pilot", 0)
	if binary.LittleEndian.Uint64(data[:8]) != uint64(Start) ||
		binary.LittleEndian.Uint64(data[8:16]) != uint64(Span) {
		t.Fatal("kernel mapping does not match the pilot bounds")
	}
	for _, extent := range []struct {
		offset int64
		length int
	}{
		{-EraseBytes, EraseBytes}, {Span, EraseBytes}, {Span - PageBytes, PageBytes + 1},
		{0, int(DeviceBytes)}, {0, 90*1024*1024 + EraseBytes},
	} {
		if bounds(extent.offset, extent.length, PageBytes) == nil {
			t.Fatalf("out-of-profile operation accepted: %+v", extent)
		}
	}
	if bounds(Span-EraseBytes, EraseBytes, EraseBytes) != nil {
		t.Fatal("last pilot block rejected")
	}
	device := &pilotDevice{}
	for _, test := range []struct {
		restore  bool
		approval string
	}{
		{false, "INSTALL-minimal-probe-02ca0000-02ce0000"},
		{true, "RESTORE-original-two-blocks-02ca0000-02ce0000"},
		{false, "INSTALL-rootfs-probe-02920000-057c0000"},
	} {
		if err := Execute(context.Background(), device, nil, test.restore, test.approval, io.Discard, &pilotJournal{}, func() error { return nil }); err == nil {
			t.Fatal("non-pilot approval accepted")
		}
	}
	if device.identities != 0 || device.enabled {
		t.Fatal("wrong approval reached the device")
	}
}

func TestPilotSparseInstallRestoreHeaderLastAndPreservation(t *testing.T) {
	bundle, device := pilotFixture(t, true)
	want := []int{Blocks - 1, 0}
	if bundle.Plan.ChangedBlocks != 2 || !reflect.DeepEqual(bundle.Plan.WriteOrder, want) {
		t.Fatalf("header-last order wrong: %v", bundle.Plan.WriteOrder)
	}
	for _, restore := range []bool{false, true} {
		device.erases, device.writes, device.enabled = nil, 0, false
		approval := InstallAck
		if restore {
			approval = RestoreAck
		}
		journal := &pilotJournal{}
		var oob pilotByteCount
		if err := Execute(context.Background(), device, bundle, restore, approval, &oob, journal, func() error { return nil }); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(device.erases, want) || device.writes != 2 ||
			device.data[EraseBytes] != 0x12 || journal.preflightBlocks != Blocks ||
			int64(oob) != Span/PageBytes*VisibleOOB || journal.last != "extent-verified-no-reboot" {
			t.Fatalf("wrong full-span operation: restore=%v erases=%v pages=%d preflight=%d", restore, device.erases, device.writes, journal.preflightBlocks)
		}
	}
	if device.data[0] != 0x11 || device.data[int64(Blocks-1)*EraseBytes] != 0x22 {
		t.Fatal("explicit restore did not restore original changed blocks")
	}
}

func TestPilotUnchangedHeaderIsNotWritten(t *testing.T) {
	bundle, _ := pilotFixture(t, false)
	if bundle.Plan.Blocks[0].Changed || bundle.Plan.ChangedBlocks != 1 ||
		!reflect.DeepEqual(bundle.Plan.WriteOrder, []int{Blocks - 1}) {
		t.Fatalf("unchanged header scheduled for writing: %v", bundle.Plan.WriteOrder)
	}
}

func TestPilotFullPreflightECCAndOOB(t *testing.T) {
	bundle, device := pilotFixture(t, true)
	device.correctedBlock = 0
	journal := &pilotJournal{}
	var oob pilotByteCount
	snapshot, err := Prepare(context.Background(), device, bundle, &oob, journal)
	if err != nil || snapshot.Corrected != 1 || snapshot.Failed != 0 ||
		journal.preflightBlocks != Blocks || int64(oob) != Span/PageBytes*VisibleOOB || device.enabled {
		t.Fatalf("full preflight failed: snapshot=%+v error=%v", snapshot, err)
	}
	device.correctedBlock, device.eccBlock = -1, 1
	if err := Execute(context.Background(), device, bundle, false, InstallAck, io.Discard, &pilotJournal{}, func() error { return nil }); err == nil || device.enabled || len(device.erases) != 0 {
		t.Fatal("pilot install accepted failed ECC in a preserved block")
	}
}

func TestPilotRetainedPayloadCaptureAndPlan(t *testing.T) {
	required := func(name string) string {
		t.Helper()
		path := os.Getenv(name)
		if path == "" {
			t.Fatalf("%s is required for the explicit nandpilot profile tests", name)
		}
		return path
	}
	bundle, err := Load(required("NAND_PILOT_PAYLOAD"), required("NAND_PILOT_ORIGINAL"))
	if err != nil {
		t.Fatal(err)
	}
	defer bundle.Close()
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
	capture := openPinned(required("NAND_PILOT_CAPTURE"), DeviceBytes, "2fac4159fe23aa25581c29f6c90033af3a1126a02593db0bd47e2c10d2c09f19")
	squashfs := openPinned(required("NAND_PILOT_SQUASHFS"), FilesystemBytes, FilesystemHash)
	type artifact struct {
		File   string
		Bytes  int64
		SHA256 string
	}
	var proposal struct {
		Extent struct {
			Start        int64
			EndExclusive int64
			Bytes        int64
			Blocks       int
		}
		Image, Payload, Rollback artifact
		ChangedBlocks            []struct {
			Offset          int64
			Bytes           int
			Changed         bool
			OriginalSHA256  string
			CandidateSHA256 string
		}
	}
	proposalData, err := os.ReadFile(required("NAND_PILOT_PROPOSAL"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(proposalData, &proposal); err != nil {
		t.Fatal(err)
	}
	if proposal.Extent.Start != Start || proposal.Extent.EndExclusive != End ||
		proposal.Extent.Bytes != Span || proposal.Extent.Blocks != Blocks ||
		proposal.Image != (artifact{FilesystemName, FilesystemBytes, FilesystemHash}) ||
		proposal.Payload != (artifact{ImageFilename, Span, ImageHash}) ||
		proposal.Rollback != (artifact{OriginalFilename, Span, OriginalHash}) ||
		len(proposal.ChangedBlocks) != Blocks {
		t.Fatal("proposal extent or artifact metadata does not match fixed profile")
	}
	sectionHash := func(reader io.ReaderAt, offset, length int64) string {
		t.Helper()
		hash := sha256.New()
		if _, err := io.CopyN(hash, io.NewSectionReader(reader, offset, length), length); err != nil {
			t.Fatal(err)
		}
		return fmt.Sprintf("%x", hash.Sum(nil))
	}
	if sectionHash(capture, Start, Span) != OriginalHash ||
		sectionHash(bundle.image, 0, FilesystemBytes) != FilesystemHash ||
		sectionHash(capture, Start-EraseBytes, EraseBytes) != BeforeHash ||
		sectionHash(capture, End, EraseBytes) != AfterHash {
		t.Fatal("payload/source/boundary pins do not match retained files")
	}
	padding := make([]byte, Span-FilesystemBytes)
	if _, err := bundle.image.ReadAt(padding, FilesystemBytes); err != nil || !allFF(padding) {
		t.Fatalf("payload padding is not exact erased bytes: %v", err)
	}
	changed := 0
	var wantOrder []int
	for index := 0; index < Blocks; index++ {
		original := make([]byte, EraseBytes)
		if _, err := capture.ReadAt(original, Start+int64(index)*EraseBytes); err != nil {
			t.Fatal(err)
		}
		probe, err := bundle.verifiedBlock(index, false)
		if err != nil {
			t.Fatal(err)
		}
		want := Block{index, Start + int64(index)*EraseBytes, digest(original), digest(probe), !bytes.Equal(original, probe)}
		if bundle.Plan.Blocks[index] != want {
			t.Fatalf("block %d does not correspond to original capture/payload", index)
		}
		proposed := proposal.ChangedBlocks[index]
		if proposed.Offset != want.AbsoluteOffset || proposed.Bytes != EraseBytes ||
			proposed.Changed != want.Changed || proposed.OriginalSHA256 != want.OriginalSHA256 ||
			proposed.CandidateSHA256 != want.ProbeSHA256 {
			t.Fatalf("proposal block %d does not correspond to original capture/payload", index)
		}
		if want.Changed {
			changed++
			if index != 0 {
				wantOrder = append(wantOrder, index)
			}
		}
	}
	if bundle.Plan.Blocks[0].Changed {
		wantOrder = append(wantOrder, 0)
	}
	if changed != 308 || bundle.Plan.ChangedBlocks != changed || !reflect.DeepEqual(bundle.Plan.WriteOrder, wantOrder) ||
		bundle.Plan.Profile != PlanProfileName || bundle.Plan.FilesystemSHA256 != FilesystemHash || bundle.Plan.WriteAuthorized {
		t.Fatal("pilot plan metadata or changed-block order mismatch")
	}
	for block := 1; block < 9; block++ {
		if sectionHash(capture, int64((block+1)*EraseBytes-4096), 848) != TableHash {
			t.Fatalf("version table %d mismatch", block)
		}
	}
	for _, paths := range [][2]string{
		{squashfs.Name(), required("NAND_PILOT_ORIGINAL")},
		{required("NAND_PILOT_ORIGINAL"), required("NAND_PILOT_PAYLOAD")},
		{required("NAND_PILOT_PAYLOAD"), required("NAND_PILOT_PAYLOAD")},
		{required("NAND_PILOT_PAYLOAD"), capture.Name()},
		{required("NAND_TWO_BLOCK_PAYLOAD"), required("NAND_TWO_BLOCK_ORIGINAL")},
		{required("NAND_PILOT_SUPERSEDED_PAYLOAD"), required("NAND_PILOT_ORIGINAL")},
	} {
		if rejected, err := Load(paths[0], paths[1]); err == nil {
			rejected.Close()
			t.Fatalf("wrong artifact combination accepted: %v", paths)
		}
	}
}
