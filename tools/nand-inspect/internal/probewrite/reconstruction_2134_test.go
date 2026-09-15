//go:build nand2134

package probewrite

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"unsafe"
)

func TestReconstruction2134UnsealedBeforeAccess(t *testing.T) {
	if reconstructionProfileSealed {
		t.Skip("final compiled profile is sealed; unsealed guard test no longer applicable")
	}
	check := func(err error) {
		t.Helper()
		if err == nil || !strings.Contains(err.Error(), "unsealed") {
			t.Fatalf("unsealed profile did not fail before access: %v", err)
		}
	}
	for _, action := range []string{"plan", "preflight", "apply", "verify-target"} {
		ack := ""
		if action == "apply" {
			ack = "APPLY-2134-EARLY-ADB-01"
		}
		check(CheckReconstructionAction(action, ack))
		check(ExecuteReconstruction(context.Background(), nil, nil, action, ack, nil))
	}
	_, err := LoadReconstruction("/must-not-open-manifest", "/must-not-open-capsule")
	check(err)
	_, err = decodeReconstructionManifest([]byte("{}"))
	check(err)
	_, err = OpenReconstructionMTD("/must-not-inspect-evidence")
	check(err)
	check(CheckReconstructionRuntime())
	check((&ReconstructionBundle{}).PrintPlan(&bytes.Buffer{}))
	d := &ReconstructionMTD{}
	check(d.EraseBlock(1))
	check(d.ProgramPage(1, 0, nil, nil))
}

func TestReconstruction2134ExclusiveApplyAndFullOrder(t *testing.T) {
	if !ReconstructionSinglePhase || ReconstructionApplyAck != "APPLY-2134-EARLY-ADB-01" ||
		ReconstructionFirstAck != "" || ReconstructionCompleteAck != "" {
		t.Fatal("2134 mode/acknowledgment isolation failed")
	}
	for _, test := range []struct {
		action, ack string
		allowed     bool
	}{
		{"apply", "APPLY-2134-EARLY-ADB-01", true},
		{"apply", "", false},
		{"apply", "RECONSTRUCT-STOCK-ADB-REMAINING-865", false},
		{"qualify-first", "RECONSTRUCT-STOCK-ADB-FIRST-1573", false},
		{"qualify-first", "", false},
		{"complete", "", false},
		{"complete", "RECONSTRUCT-STOCK-ADB-REMAINING-865", false},
		{"preflight", "", true},
		{"verify-target", "", true},
	} {
		if err := checkReconstructionActionMode(test.action, test.ack); (err == nil) != test.allowed {
			t.Fatalf("action=%s ack=%q err=%v", test.action, test.ack, err)
		}
	}
	expected := []int{330, 331, 329, 106, 105, 8, 7, 6, 5, 4, 3, 2, 1}
	for _, order := range [][]int{expected, {330, 3, 1}, {330}} {
		if err := validate2134PrebootOrder(order); err != nil {
			t.Fatalf("changed-only preboot order rejected: %v", err)
		}
	}
	for _, order := range [][]int{{1, 3}, {3, 330}} {
		if validate2134PrebootOrder(order) == nil {
			t.Fatal("non-descending or non-final preboot order accepted")
		}
	}
	b := &ReconstructionBundle{plan: reconstructionManifest{WriteOrder: append([]int(nil), expected...)}}
	order, err := b.phaseOrder("apply")
	if err != nil || !reflect.DeepEqual(order, expected) {
		t.Fatalf("apply must write the complete list without skipping its first entry: %v %v", order, err)
	}
	order[0] = 0
	if !reflect.DeepEqual(b.plan.WriteOrder, expected) {
		t.Fatal("returned order aliases the sealed manifest")
	}
	for _, action := range []string{"qualify-first", "complete", "resume"} {
		if _, err := b.phaseOrder(action); err == nil {
			t.Fatalf("2134 accepted %s", action)
		}
	}
}

func TestReconstruction2134CurrentOnlyStateAndNoFictionalPhase(t *testing.T) {
	for _, matchesCurrent := range []bool{false, true} {
		for _, action := range []string{"apply", "preflight"} {
			state, err := reconstructionRequiredState(action, matchesCurrent)
			if err != nil || state != "current" {
				t.Fatalf("%s must require current even when current mismatches: %s %v", action, state, err)
			}
		}
	}
	if state, err := reconstructionRequiredState("verify-target", false); err != nil || state != "target" {
		t.Fatal("verify-target does not require final state")
	}
	b := &ReconstructionBundle{plan: reconstructionManifest{
		CurrentMainSHA256: "current", CurrentOOBSHA256: "oob", CurrentViewSHA256: "view",
		AfterFirstMainSHA256: "after", AfterFirstOOBSHA256: "oob", AfterFirstViewSHA256: "view",
	}}
	s := &reconstructionSnapshot{main: "after", oob: "oob", view: "view", blocks: make([]reconstructionBlockHash, 2048)}
	if b.matchSnapshot(s, "current") == nil || b.matchSnapshot(s, "after-first") == nil {
		t.Fatal("2134 accepted an after-first fallback")
	}
	data, err := json.Marshal(reconstructionManifest{WriteOrder: []int{330, 8, 7, 6, 5, 4, 3, 2, 1}})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("afterFirst")) || bytes.Contains(data, []byte("firstBlock")) {
		t.Fatal("single-phase manifest encoding invents a qualification phase")
	}
}

func TestReconstruction2134BoundsABIAndUnchangedOOB(t *testing.T) {
	if reconstructionStart != 0x20000 || reconstructionEnd != 0x8320000 ||
		reconstructionFirst != -1 || reconstruction2134CurrentMainHash != "33f16be0dcbc42b92221471ab5685d84868ebff6f6ce7dabf3b66adcd638489f" {
		t.Fatal("2134 fixed view/current pin incorrect")
	}
	for _, block := range []int{0, 1049, 1050, 1536, 1537, 1573, 2033, 2034, 2044, 2045, 2046, 2047} {
		if reconstructionTargetBlock(block) {
			t.Fatalf("protected or outside block accepted: %d", block)
		}
	}
	if !reconstructionTargetBlock(1) || !reconstructionTargetBlock(1048) {
		t.Fatal("legal view endpoint rejected")
	}
	_, arg := partitionRangeArg(1, "2134-test", 0, reconstructionStart, reconstructionSpan)
	if binary.LittleEndian.Uint64(arg[0:8]) != 0x20000 ||
		binary.LittleEndian.Uint64(arg[0:8])+binary.LittleEndian.Uint64(arg[8:16]) != 0x8320000 {
		t.Fatal("2134 BLKPG view is not exact")
	}
	var abi reconstructionWriteRequest
	if unsafe.Sizeof(abi) != 48 || unsafe.Offsetof(abi.Mode) != 40 {
		t.Fatal("MEMWRITE ABI changed")
	}
	oob := bytes.Repeat([]byte{255}, VisibleOOB)
	if _, err := reconstructionPageRequest(0, make([]byte, PageBytes), oob); err != nil {
		t.Fatal(err)
	}
	oob[2] = 0
	if _, err := reconstructionPageRequest(0, make([]byte, PageBytes), oob); err == nil {
		t.Fatal("2134 accepted an OOB tag")
	}
	oobHash := digest(bytes.Repeat([]byte{255}, EraseBytes/PageBytes*VisibleOOB))
	record := reconstructionRecord{Block: 330, CurrentOOBSHA256: oobHash, TargetOOBSHA256: oobHash, TargetOOBAllFF: true}
	if err := validateReconstructionRecordOOB(record); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*reconstructionRecord){
		func(r *reconstructionRecord) { r.OOBDiff = true },
		func(r *reconstructionRecord) { r.TargetOOBAllFF = false },
		func(r *reconstructionRecord) { r.CurrentOOBSHA256 = "changed" },
		func(r *reconstructionRecord) { r.CurrentOOBSHA256 = "non-FF"; r.TargetOOBSHA256 = "non-FF" },
	} {
		bad := record
		mutate(&bad)
		if validateReconstructionRecordOOB(bad) == nil {
			t.Fatal("OOB change accepted")
		}
	}
}

func TestReconstruction2134AllowsExplicitBlankRootfsRecords(t *testing.T) {
	blank := bytes.Repeat([]byte{255}, reconstructionRecordBytes)
	record := reconstructionRecord{Block: 330, Start: 330 * EraseBytes, PayloadBytes: reconstructionRecordBytes,
		TargetDataAllFF: true, TargetOOBAllFF: true, TargetDataSHA256: digest(blank[:EraseBytes]),
		TargetOOBSHA256: digest(blank[EraseBytes:]), CurrentOOBSHA256: digest(blank[EraseBytes:])}
	b := &ReconstructionBundle{payload: bytes.NewReader(blank), records: map[int]reconstructionRecord{330: record}}
	if err := b.readTarget(330, make([]byte, reconstructionRecordBytes)); err != nil {
		t.Fatalf("explicit all-FF target was rejected: %v", err)
	}
	if _, err := b.phaseOrder("qualify-first"); err == nil {
		t.Fatal("app qualification became possible")
	}
}
