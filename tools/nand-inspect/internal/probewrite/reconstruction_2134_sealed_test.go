//go:build nand2134

package probewrite

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/tku4tw2012/reinvoke/tools/nand-inspect/internal/regularfile"
)

func TestReconstruction2134FinalSealAndApproval(t *testing.T) {
	if !reconstructionProfileSealed || reconstructionCount != 763 ||
		reconstructionManifestSize != 498997 || reconstructionPayloadSize != 101570560 ||
		ReconstructionManifestHash != "d142d5d1b0bb795da17dae4aac3162c6b955c3797603bdf23a47dec75fc912b8" ||
		ReconstructionPayloadHash != "c8f80dc4e9ae8e86472e032c021ff049364c71d02b4d89f2854d23ab7a788adf" ||
		reconstructionTargetHash != "02c36e6675656cee07c5fdaf8aedf91c8e6586c3729baf734672722b8891c6cc" {
		t.Fatal("2134 final compiled seal mismatch")
	}
	if err := CheckReconstructionAction("apply", "APPLY-2134-EARLY-ADB-01"); err != nil {
		t.Fatal(err)
	}
	for _, ack := range []string{"", "RECONSTRUCT-STOCK-ADB-FIRST-1573", "RECONSTRUCT-STOCK-ADB-REMAINING-865"} {
		if CheckReconstructionAction("apply", ack) == nil {
			t.Fatal("sealing removed the separate apply approval gate")
		}
	}
}

func TestReconstruction2134ActualManifestCapsuleAndTarget(t *testing.T) {
	reconstruction2134Fixture := privateArchiveFixture(
		t, "build", "artifacts", "2134-early-adb-update-01-20260911",
	)
	b, err := LoadReconstruction(reconstruction2134Fixture+"/UPDATE-PLAN.json",
		reconstruction2134Fixture+"/target-blocks-data-oob32.bin")
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	order, err := b.phaseOrder("apply")
	if err != nil || len(order) != 763 || !reflect.DeepEqual(order, b.plan.WriteOrder) ||
		!reflect.DeepEqual(order[len(order)-8:], []int{8, 7, 6, 5, 4, 3, 2, 1}) {
		t.Fatalf("actual one-phase write order mismatch: %v", err)
	}
	if b.plan.WriteAuthorization || b.plan.FirstBlock != 0 || b.plan.AfterFirstMainSHA256 != "" ||
		b.plan.AfterFirstOOBSHA256 != "" || b.plan.AfterFirstViewSHA256 != "" ||
		b.plan.CurrentOOBSHA256 != b.plan.TargetOOBSHA256 ||
		b.plan.Diagnostic.FilesystemSHA256 != reconstruction2134FSHash ||
		b.plan.Diagnostic.InitSHA256 != reconstruction2134InitHash ||
		b.plan.Diagnostic.USBProduct != "RI2134-ADB01" {
		t.Fatal("approval, phase or early-ADB diagnostic metadata mismatch")
	}
	target, err := regularfile.Open(reconstruction2134Fixture + "/target-main-data.bin")
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	if err := checkFile(target, DeviceBytes, reconstructionTargetHash); err != nil {
		t.Fatal(err)
	}
	regions := make(map[string]int)
	blankRootfs := 0
	main := make([]byte, EraseBytes)
	for _, record := range b.plan.Records {
		if !reconstructionTargetBlock(record.Block) || record.OOBDiff || !record.TargetOOBAllFF ||
			record.CurrentOOBSHA256 != record.TargetOOBSHA256 {
			t.Fatalf("unexpected target scope/OOB change at block %d", record.Block)
		}
		n, err := target.ReadAt(main, record.Start)
		if err != nil || n != len(main) || digest(main) != record.TargetDataSHA256 {
			t.Fatalf("capsule differs from target main reference at block %d: bytes=%d error=%v", record.Block, n, err)
		}
		regions[record.Region]++
		if record.Region == "rootfs" && record.TargetDataAllFF {
			blankRootfs++
		}
	}
	wantRegions := map[string]int{
		"pre-bootloader": 8, "post-bootloader": 2, "tz_en": 9,
		"bootimgs": 40, "bootimgs_B": 67, "bsl": 2, "rootfs": 635,
	}
	if !reflect.DeepEqual(regions, wantRegions) || blankRootfs == 0 {
		t.Fatalf("actual changed-region/blank-tail plan mismatch: regions=%v blank-rootfs=%d", regions, blankRootfs)
	}
	var output bytes.Buffer
	if err := b.PrintPlan(&output); err != nil {
		t.Fatal(err)
	}
	var rendered map[string]json.RawMessage
	if err := json.Unmarshal(output.Bytes(), &rendered); err != nil {
		t.Fatal(err)
	}
	if string(rendered["applyConfirmation"]) != `"APPLY-2134-EARLY-ADB-01"` ||
		rendered["qualifyFirstConfirmation"] != nil || rendered["completeConfirmation"] != nil {
		t.Fatal("single-phase plan advertises the wrong approval protocol")
	}
}
