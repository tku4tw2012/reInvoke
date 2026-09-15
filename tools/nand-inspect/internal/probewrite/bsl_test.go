//go:build nandbsl || nandbslcompact

package probewrite

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestBSLFixedGeometryPinsAndBounds(t *testing.T) {
	wantBytes := int64(5111808)
	wantImage := "91a9dde15fea2959232fc9a0cdafdb739e08a7498c4e8b57d867889c8f358163"
	wantCurrent := "238187a1d490d43dc9e44c658f88735900d3b6dfc7ed312313b9d267156dd92c"
	wantFilesystem := "16921ec74f3f88f19bba741319f9a60c2adefbf6da2e655b418f0ebc564d3194"
	wantInstall := "INSTALL-reinvoke-bsl-v2-01a20000-01f20000"
	wantMapping := "MAP-ONLY-reinvoke-bsl-v2-01a20000-01f20000"
	switch PlanProfileName {
	case "reinvoke-bsl-v2-20260911-slim02":
	case "reinvoke-bsl-compact-20260911":
		wantBytes = 2445312
		wantImage = "606923f27236a1bf86807c19354c772dea65ddefca73a0ba075a59f68c4ffec1"
		wantCurrent = "91a9dde15fea2959232fc9a0cdafdb739e08a7498c4e8b57d867889c8f358163"
		wantFilesystem = "d23e1844df71cf58b5f1313038116bc1055db9432cfb726a074e43de52bfee88"
		wantInstall = "INSTALL-reinvoke-bsl-compact-01a20000-01f20000"
		wantMapping = "MAP-ONLY-reinvoke-bsl-compact-01a20000-01f20000"
	default:
		t.Fatal("unknown BSL profile")
	}
	if Start != 0x01a20000 || Span != 0x00500000 || End != 0x01f20000 ||
		Blocks != 40 || EraseBytes != 131072 || PageBytes != 2048 ||
		ImageBytes != 5242880 || FilesystemBytes != wantBytes ||
		!HeaderLast || ResumeSupported || RestoreSupported {
		t.Fatal("BSL geometry or forward-only policy changed")
	}
	if Span-FilesystemBytes < EraseBytes {
		t.Fatal("BSL payload must retain at least its final erased padding block")
	}
	for name, pins := range map[string][2]string{
		"image":      {ImageHash, wantImage},
		"current":    {OriginalHash, wantCurrent},
		"filesystem": {FilesystemHash, wantFilesystem},
		"before":     {BeforeHash, "b5a41c3758763bbec72769fab4a2533bf2db0b6312d93d25a695f9e4b9e02260"},
		"after":      {AfterHash, "75c62f48adf907f820d0568cc1966ca5dfa157633851e6556f9adbb42064e6bc"},
		"table":      {TableHash, "e25ca94fac7c1fac5df425246b9ea147751a6fb1be252e322e547c807f737083"},
		"install":    {InstallAck, wantInstall},
		"mapping":    {MappingAck, wantMapping},
	} {
		if pins[0] != pins[1] {
			t.Fatalf("%s pin changed", name)
		}
	}
	for _, offset := range []int64{-EraseBytes, Span, math.MaxInt64 - PageBytes + 1} {
		if bounds(offset, EraseBytes, PageBytes) == nil {
			t.Fatalf("out-of-range/overflow offset accepted: %#x", offset)
		}
	}
	if bounds(Span-EraseBytes, EraseBytes, EraseBytes) != nil ||
		bounds(Span-PageBytes, PageBytes, PageBytes) != nil {
		t.Fatal("last BSL block/page rejected")
	}
	bundle, _ := pilotFixture(t, true)
	if _, err := bundle.verifiedBlock(39, false); err != nil {
		t.Fatal(err)
	}
	for _, index := range []int{-1, 40, math.MaxInt32} {
		if _, err := bundle.block(index, false); err == nil {
			t.Fatalf("out-of-range artifact block accepted: %d", index)
		}
	}
}

func TestBSLFullSpanHeaderLastAndPreservation(t *testing.T) {
	for _, headerChanged := range []bool{false, true} {
		bundle, device := pilotFixture(t, headerChanged)
		want := []int{39}
		if headerChanged {
			want = append(want, 0)
		}
		if !reflect.DeepEqual(bundle.Plan.WriteOrder, want) {
			t.Fatalf("wrong header-last order: %v", bundle.Plan.WriteOrder)
		}
		log := &pilotJournal{}
		var oob pilotByteCount
		if err := Execute(context.Background(), device, bundle, false, InstallAck, &oob, log, func() error { return nil }); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(device.erases, want) || device.writes != len(want) ||
			device.data[EraseBytes] != 0x12 || device.data[39*EraseBytes] != 0x44 ||
			log.preflightBlocks != 40 || int64(oob) != 81920 || log.last != "extent-verified-no-reboot" {
			t.Fatalf("wrong extent operation: erases=%v pages=%d preflight=%d", device.erases, device.writes, log.preflightBlocks)
		}
		for index := 0; index < Blocks; index++ {
			actual := make([]byte, EraseBytes)
			if _, err := device.ReadAt(actual, int64(index)*EraseBytes); err != nil {
				t.Fatal(err)
			}
			if digest(actual) != bundle.Plan.Blocks[index].ProbeSHA256 {
				t.Fatalf("block %d not preserved/programmed exactly", index)
			}
		}
	}
}

func TestBSLUnchangedExtentDoesNotErase(t *testing.T) {
	bundle, device := pilotFixture(t, false)
	bundle.image = bundle.original
	if err := bundle.buildPlan(); err != nil {
		t.Fatal(err)
	}
	if err := Execute(context.Background(), device, bundle, false, InstallAck, io.Discard, &pilotJournal{}, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if bundle.Plan.ChangedBlocks != 0 || len(bundle.Plan.WriteOrder) != 0 || len(device.erases) != 0 || device.writes != 0 {
		t.Fatal("unchanged extent caused mutation")
	}
}

func TestBSLRestoreAndWrongApprovalFailBeforeDeviceAccess(t *testing.T) {
	for _, approval := range []string{"", RestoreAck, InstallAck, MappingAck,
		"INSTALL-reinvoke-bsl-v2-01a20000-01f20000",
		"INSTALL-reinvoke-bsl-compact-01a20000-01f20000",
		"INSTALL-rootfs-pilot-pty01-02920000-04fa0000",
		"RESTORE-original-rootfs-pilot-pty01-02920000-04fa0000"} {
		if err := Execute(context.Background(), nil, nil, true, approval, nil, nil, nil); err == nil ||
			!strings.Contains(err.Error(), "restore is unsupported") {
			t.Fatalf("restoration was not rejected before access: %v", err)
		}
		if approval != InstallAck {
			if err := Execute(context.Background(), nil, nil, false, approval, nil, nil, nil); err == nil {
				t.Fatalf("wrong install approval accepted: %q", approval)
			}
		}
	}
	if _, err := preflight(context.Background(), nil, nil, true, nil, nil); err == nil {
		t.Fatal("internal restore preflight accepted")
	}
	if err := executePrepared(context.Background(), nil, nil, nil, true, nil, nil); err == nil {
		t.Fatal("prepared restoration accepted")
	}
}

func TestBSLRetainedPayloadAndCurrentState(t *testing.T) {
	dir := os.Getenv("NAND_BSL_ARTIFACT_DIR")
	if dir == "" {
		t.Skip("set NAND_BSL_ARTIFACT_DIR to verify the retained BSL artifacts")
	}
	image, original := filepath.Join(dir, ImageFilename), filepath.Join(dir, OriginalFilename)
	bundle, err := Load(image, original)
	if err != nil {
		t.Fatal(err)
	}
	defer bundle.Close()
	fs, err := os.Open(filepath.Join(dir, FilesystemName))
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	if err := checkFile(fs, FilesystemBytes, FilesystemHash); err != nil {
		t.Fatal(err)
	}
	hash := sha256.New()
	if _, err := io.CopyN(hash, io.NewSectionReader(bundle.image, 0, FilesystemBytes), FilesystemBytes); err != nil {
		t.Fatal(err)
	}
	padding := make([]byte, Span-FilesystemBytes)
	if _, err := bundle.image.ReadAt(padding, FilesystemBytes); err != nil || !allFF(padding) ||
		fmt.Sprintf("%x", hash.Sum(nil)) != FilesystemHash {
		t.Fatalf("filesystem/payload padding mismatch: %v", err)
	}
	firstChanged, changedBlocks := 1, 39
	if PlanProfileName == "reinvoke-bsl-compact-20260911" {
		firstChanged, changedBlocks = 8, 32
		for index := 1; index < firstChanged; index++ {
			if bundle.Plan.Blocks[index].Changed {
				t.Fatalf("unchanged compact-image block %d would be written", index)
			}
		}
	}
	if bundle.Plan.Profile != PlanProfileName || bundle.Plan.FilesystemSHA256 != FilesystemHash ||
		len(bundle.Plan.Blocks) != 40 || bundle.Plan.WriteAuthorized ||
		bundle.Plan.ChangedBlocks != changedBlocks || bundle.Plan.Blocks[39].Changed {
		t.Fatalf("incorrect pinned BSL plan: changed=%d order=%v",
			bundle.Plan.ChangedBlocks, bundle.Plan.WriteOrder)
	}
	var wantOrder []int
	for index := firstChanged; index <= 38; index++ {
		wantOrder = append(wantOrder, index)
	}
	wantOrder = append(wantOrder, 0)
	if !reflect.DeepEqual(bundle.Plan.WriteOrder, wantOrder) {
		t.Fatalf("pinned BSL write order changed: %v", bundle.Plan.WriteOrder)
	}
	for _, paths := range [][2]string{{original, image}, {image, image}, {fs.Name(), original}, {image, fs.Name()}} {
		if accepted, err := Load(paths[0], paths[1]); err == nil {
			accepted.Close()
			t.Fatalf("wrong/swapped artifact accepted: %v", paths)
		}
	}
	bundle.image = bytes.NewReader(make([]byte, Span))
	if _, err := bundle.verifiedBlock(0, false); err == nil {
		t.Fatal("candidate mutation after load accepted")
	}
}
