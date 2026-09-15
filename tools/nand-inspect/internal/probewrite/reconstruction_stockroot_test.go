//go:build nandstockroot

package probewrite

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"

	"github.com/tku4tw2012/reinvoke/tools/nand-inspect/internal/regularfile"
)

func TestReconstructionStockRootSealedInputsAndProtectedBytes(t *testing.T) {
	stockrootFixture := privateArchiveFixture(
		t, "build", "artifacts", "community-stockroot-native-01-20260911",
	)
	b, err := LoadReconstruction(stockrootFixture+"/UPDATE-PLAN.json", stockrootFixture+"/target-blocks-data-oob32.bin")
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if !reconstructionProfileSealed || !ReconstructionSinglePhase || reconstructionCount != 737 ||
		b.plan.Diagnostic.ChangedInitBytePositions != 0 || len(b.plan.Diagnostic.ChangedInitLines) != 0 ||
		len(b.plan.Diagnostic.ChangedSourceBlocks) != 0 || b.plan.WriteAuthorization {
		t.Fatal("StockRoot must be sealed, unmodified-source and separately approved")
	}
	current, err := regularfile.Open(privateArchiveFixture(
		t, "build", "artifacts", "2134-early-adb-update-01-20260911",
		"target-main-data.bin",
	))
	if err != nil {
		t.Fatal(err)
	}
	defer current.Close()
	target, err := regularfile.Open(stockrootFixture + "/target-main-data.bin")
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	spares, err := regularfile.Open(privateArchiveFixture(
		t, "evidence", "nand-restored-ram-inspection-20260909",
		"restored-visible-oob32.bin",
	))
	if err != nil {
		t.Fatal(err)
	}
	defer spares.Close()
	currentHash, targetHash, oobHash := sha256.New(), sha256.New(), sha256.New()
	a, z, oob := make([]byte, EraseBytes), make([]byte, EraseBytes), make([]byte, 2048)
	counts := map[string]int{}
	for block := 0; block < int(DeviceBytes/EraseBytes); block++ {
		for _, read := range []struct {
			file   *os.File
			buffer []byte
			offset int64
		}{
			{current, a, int64(block) * EraseBytes}, {target, z, int64(block) * EraseBytes}, {spares, oob, int64(block) * 2048},
		} {
			n, err := read.file.ReadAt(read.buffer, read.offset)
			if err != nil || n != len(read.buffer) {
				t.Fatalf("read block %d: %d %v", block, n, err)
			}
		}
		currentHash.Write(a)
		targetHash.Write(z)
		oobHash.Write(oob)
		record, changed := b.records[block]
		if !changed {
			if !bytes.Equal(a, z) {
				t.Fatalf("non-whitelisted/protected block %d changed", block)
			}
			continue
		}
		if bytes.Equal(a, z) || !reconstructionTargetBlock(block) ||
			digest(a) != record.CurrentDataSHA256 || digest(z) != record.TargetDataSHA256 ||
			!allFF(oob) || digest(oob) != record.CurrentOOBSHA256 || record.CurrentOOBSHA256 != record.TargetOOBSHA256 {
			t.Fatalf("changed-record current/target/OOB mismatch at %d", block)
		}
		counts[record.Region]++
	}
	if fmt.Sprintf("%x", currentHash.Sum(nil)) != reconstructionStockRootCurrentHash ||
		fmt.Sprintf("%x", targetHash.Sum(nil)) != reconstructionTargetHash ||
		fmt.Sprintf("%x", oobHash.Sum(nil)) != reconstructionStockRootOOBHash {
		t.Fatal("whole-image/OOB hash mismatch")
	}
	want := map[string]int{"pre-bootloader": 8, "post-bootloader": 2, "bootimgs": 40, "bootimgs_B": 40, "rootfs": 647}
	if !reflect.DeepEqual(counts, want) {
		t.Fatalf("unexpected write scope: %v", counts)
	}
	order, err := b.phaseOrder("apply")
	if err != nil || len(order) != 737 || !reflect.DeepEqual(order, b.plan.WriteOrder) ||
		!reflect.DeepEqual(order[len(order)-8:], []int{8, 7, 6, 5, 4, 3, 2, 1}) {
		t.Fatal("wrong single-apply order")
	}
	for i := 1; i < len(order)-8; i++ {
		if order[i] <= order[i-1] {
			t.Fatal("non-preboot order is not ascending")
		}
	}
	var out bytes.Buffer
	if err := b.PrintPlan(&out); err != nil {
		t.Fatal(err)
	}
	var view map[string]json.RawMessage
	if err := json.Unmarshal(out.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if string(view["applyConfirmation"]) != `"APPLY-STOCKROOT-NATIVE-01"` ||
		view["qualifyFirstConfirmation"] != nil || view["completeConfirmation"] != nil {
		t.Fatal("wrong approval protocol")
	}
}

func TestReconstructionStockRootActionAndScopeIsolation(t *testing.T) {
	if err := CheckReconstructionAction("apply", "APPLY-STOCKROOT-NATIVE-01"); err != nil {
		t.Fatal(err)
	}
	for _, ack := range []string{"", "APPLY-2134-EARLY-ADB-01", "RECONSTRUCT-STOCK-ADB-REMAINING-865"} {
		if CheckReconstructionAction("apply", ack) == nil {
			t.Fatal("obsolete/wrong approval accepted")
		}
	}
	for _, action := range []string{"complete", "qualify-first", "restore", "resume"} {
		if CheckReconstructionAction(action, ReconstructionApplyAck) == nil {
			t.Fatal("unsupported action accepted")
		}
	}
	for _, block := range []int{0, 1049, 1536, 1537, 1573, 2033, 2044, 2047} {
		if reconstructionTargetBlock(block) {
			t.Fatalf("protected/outside block accepted: %d", block)
		}
	}
	for _, match := range []bool{true, false} {
		if state, err := reconstructionRequiredState("preflight", match); err != nil || state != "current" {
			t.Fatal("unexpected fallback state")
		}
	}
	if _, err := decodeReconstructionManifest([]byte("{}")); err == nil {
		t.Fatal("unsealed manifest accepted")
	}
}
