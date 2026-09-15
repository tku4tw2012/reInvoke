//go:build !nand2134 && !nandstockroot

package probewrite

import (
	"bytes"
	"encoding/json"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/tku4tw2012/reinvoke/tools/nand-inspect/internal/regularfile"
)

func TestReconstructionStockADBAcknowledgments(t *testing.T) {
	for _, test := range []struct {
		action, ack string
		allowed     bool
	}{
		{"qualify-first", "RECONSTRUCT-STOCK-ADB-FIRST-1573", true},
		{"complete", "RECONSTRUCT-STOCK-ADB-REMAINING-865", true},
		{"qualify-first", "RECONSTRUCT-SEPT7-FIRST-1573", false},
		{"complete", "RECONSTRUCT-SEPT7-REMAINING-865", false},
		{"qualify-first", "RECONSTRUCT-STOCK-ADB-REMAINING-865", false},
		{"complete", "RECONSTRUCT-STOCK-ADB-FIRST-1573", false},
	} {
		if err := CheckReconstructionAction(test.action, test.ack); (err == nil) != test.allowed {
			t.Fatalf("%s acknowledgment %q: %v", test.action, test.ack, err)
		}
	}
}

func TestReconstructionStockADBRealTargetAndSupersededInputs(t *testing.T) {
	realReconstructionDirectory := privateArchiveFixture(
		t, "build", "artifacts", "september7-stock-adb-reconstruction-20260911",
	)
	supersededReconstructionDirectory := privateArchiveFixture(
		t, "build", "artifacts", "nand-september7-reconstruction-20260911",
	)
	b, err := LoadReconstruction(realReconstructionDirectory+"/RESTORE-PLAN.json", realReconstructionDirectory+"/target-blocks-data-oob32.bin")
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if b.plan.SourceMainSHA256 != reconstructionSourceHash || b.plan.TargetMainSHA256 != reconstructionTargetHash ||
		b.plan.Diagnostic.FilesystemSHA256 != reconstructionFSHash || b.plan.Diagnostic.InitSHA256 != reconstructionInitHash ||
		!reflect.DeepEqual(b.plan.Diagnostic.ChangedSourceBlocks, []int{357, 358}) ||
		!reflect.DeepEqual(b.plan.Diagnostic.ChangedInitLines, []int{85, 135, 235}) ||
		b.plan.Diagnostic.ChangedInitBytePositions != 29 || b.plan.Diagnostic.USBProduct != "reInvoke-ADB" {
		t.Fatal("stock-ADB diagnostic identity mismatch")
	}
	wrongTarget := &ReconstructionBundle{plan: b.plan}
	wrongTarget.plan.TargetMainSHA256 = reconstructionSourceHash
	if err := wrongTarget.validate(); err == nil {
		t.Fatal("unmodified original accepted as the execution target")
	}

	// This 256 MiB reference is host-test-only and streamed; it is not a writer input.
	target, err := regularfile.Open(realReconstructionDirectory + "/target-main-data.bin")
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	if err := checkFile(target, DeviceBytes, reconstructionTargetHash); err != nil {
		t.Fatal(err)
	}
	main := make([]byte, EraseBytes)
	for _, record := range b.plan.Records {
		n, err := target.ReadAt(main, record.Start)
		if err != nil || n != len(main) || digest(main) != record.TargetDataSHA256 {
			t.Fatalf("capsule record differs from sealed whole target at block %d: bytes=%d error=%v", record.Block, n, err)
		}
	}

	oldFile, err := regularfile.Open(supersededReconstructionDirectory + "/RESTORE-PLAN.json")
	if err != nil {
		t.Fatal(err)
	}
	defer oldFile.Close()
	if err := checkFile(oldFile, 557846, "ee484d7eec4359a6ee701df595fbd9cc54facb31e4937c94fa372908d54f1866"); err != nil {
		t.Fatal(err)
	}
	oldBytes, err := io.ReadAll(io.NewSectionReader(oldFile, 0, 557846))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeReconstructionManifest(oldBytes); err == nil {
		t.Fatal("superseded manifest accepted by the stock-ADB decoder")
	}
	var old reconstructionManifest
	if err := json.Unmarshal(oldBytes, &old); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(old.WriteOrder, b.plan.WriteOrder) ||
		!reflect.DeepEqual(old.ProtectedBlocks, b.plan.ProtectedBlocks) ||
		!reflect.DeepEqual(old.KnownBadBlocks, b.plan.KnownBadBlocks) ||
		old.FirstBlock != b.plan.FirstBlock || old.CurrentMainSHA256 != b.plan.CurrentMainSHA256 ||
		old.CurrentOOBSHA256 != b.plan.CurrentOOBSHA256 || old.CurrentViewSHA256 != b.plan.CurrentViewSHA256 ||
		old.AfterFirstMainSHA256 != b.plan.AfterFirstMainSHA256 || old.AfterFirstOOBSHA256 != b.plan.AfterFirstOOBSHA256 ||
		old.AfterFirstViewSHA256 != b.plan.AfterFirstViewSHA256 || old.TargetOOBSHA256 != b.plan.TargetOOBSHA256 ||
		len(old.Records) != len(b.plan.Records) {
		t.Fatal("write method, order or initial/after-first state changed")
	}
	var changed []int
	for index, record := range b.plan.Records {
		previous := old.Records[index]
		if previous.TargetDataSHA256 != record.TargetDataSHA256 {
			changed = append(changed, record.Block)
			previous.TargetDataSHA256 = record.TargetDataSHA256
		}
		if previous != record {
			t.Fatalf("unexpected additional record change at block %d", record.Block)
		}
	}
	if !reflect.DeepEqual(changed, []int{357, 358}) {
		t.Fatalf("changed target records: %v", changed)
	}
	for _, inputs := range [][2]string{
		{supersededReconstructionDirectory + "/RESTORE-PLAN.json", realReconstructionDirectory + "/target-blocks-data-oob32.bin"},
		{realReconstructionDirectory + "/RESTORE-PLAN.json", supersededReconstructionDirectory + "/target-blocks-data-oob32.bin"},
	} {
		stale, err := LoadReconstruction(inputs[0], inputs[1])
		if err == nil {
			stale.Close()
			t.Fatal("mixed superseded/new inputs accepted")
		}
	}
	var output bytes.Buffer
	if err := b.PrintPlan(&output); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{ReconstructionManifestHash, ReconstructionPayloadHash, reconstructionTargetHash,
		"RECONSTRUCT-STOCK-ADB-FIRST-1573", "RECONSTRUCT-STOCK-ADB-REMAINING-865", `"writeAuthorization":false`} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("stock-ADB plan output is missing %s", want)
		}
	}
	if strings.Contains(output.String(), "RECONSTRUCT-SEPT7-") {
		t.Fatal("plan advertises obsolete acknowledgments")
	}
}
