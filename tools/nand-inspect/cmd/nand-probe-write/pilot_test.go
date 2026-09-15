//go:build nandpilot

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/tku4tw2012/reinvoke/tools/nand-inspect/internal/probewrite"
)

func TestPilotCLIRejectsNonPilotApprovalAndOverrides(t *testing.T) {
	for _, test := range []struct{ action, approval string }{
		{"install", "INSTALL-minimal-probe-02ca0000-02ce0000"},
		{"restore", "RESTORE-original-two-blocks-02ca0000-02ce0000"},
		{"check-map", "MAP-ONLY-two-blocks-02ca0000-02ce0000"},
	} {
		var out, errs bytes.Buffer
		if code := run([]string{test.action, "--image", "/dev/null", "--original", "/dev/null", "--confirm", test.approval}, &out, &errs); code != 2 || out.Len() != 0 || !strings.Contains(errs.String(), "exact confirmation") {
			t.Fatalf("non-pilot approval accepted: code=%d stderr=%s", code, &errs)
		}
	}
	var out, errs bytes.Buffer
	if run([]string{"plan", "--image", "/dev/null", "--original", "/dev/null", "--offset", "0"}, &out, &errs) != 2 {
		t.Fatal("offset override accepted")
	}
}

func TestPilotCLIHelpAndActualPlan(t *testing.T) {
	var out, errs bytes.Buffer
	if run([]string{"--help"}, &out, &errs) != 0 || strings.Contains(out.String(), "two-block") ||
		!strings.Contains(out.String(), probewrite.ImageFilename) ||
		!strings.Contains(out.String(), "0x02920000") {
		t.Fatalf("pilot help is wrong or legacy: %s", &out)
	}
	payload, original := os.Getenv("NAND_PILOT_PAYLOAD"), os.Getenv("NAND_PILOT_ORIGINAL")
	if payload == "" || original == "" {
		t.Fatal("NAND_PILOT_PAYLOAD and NAND_PILOT_ORIGINAL are required")
	}
	out.Reset()
	errs.Reset()
	if code := run([]string{"plan", "--image", payload, "--original", original}, &out, &errs); code != 0 {
		t.Fatalf("pilot plan failed: code=%d stderr=%s", code, &errs)
	}
	var plan probewrite.Plan
	if err := json.Unmarshal(out.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	if plan.Start != probewrite.Start || plan.End != probewrite.End || plan.Span != probewrite.Span ||
		plan.Profile != probewrite.PlanProfileName || plan.FilesystemSHA256 != probewrite.FilesystemHash ||
		plan.ImageSHA256 != probewrite.ImageHash || plan.OriginalSHA256 != probewrite.OriginalHash ||
		len(plan.Blocks) != probewrite.Blocks || plan.ChangedBlocks == 0 || plan.WriteAuthorized || errs.Len() != 0 {
		t.Fatalf("wrong pilot plan: %+v stderr=%s", plan, &errs)
	}
	if plan.Blocks[0].Changed && plan.WriteOrder[len(plan.WriteOrder)-1] != 0 {
		t.Fatal("changed filesystem header is not last")
	}
}
