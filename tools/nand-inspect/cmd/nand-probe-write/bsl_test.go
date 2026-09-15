//go:build nandbsl || nandbslcompact

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tku4tw2012/reinvoke/tools/nand-inspect/internal/probewrite"
)

func TestBSLCLIRestoreAndResumeUnsupported(t *testing.T) {
	for _, action := range []string{"restore", "resume", "resume-plan", "resume-preflight"} {
		for _, approval := range []string{"", probewrite.InstallAck, probewrite.RestoreAck, probewrite.ResumeAck} {
			var out, errs bytes.Buffer
			if code := run([]string{action, "--image", "nonexistent", "--original", "nonexistent", "--confirm", approval}, &out, &errs); code != 2 ||
				out.Len() != 0 || !strings.Contains(errs.String(), "no device accessed") {
				t.Fatalf("unsupported %s reached inputs/device: code=%d err=%s", action, code, &errs)
			}
		}
	}
}

func TestBSLCLIApprovalsAndHostGuard(t *testing.T) {
	for _, test := range []struct{ action, approval string }{
		{"preflight", ""}, {"install", probewrite.InstallAck}, {"check-map", probewrite.MappingAck},
	} {
		var out, errs bytes.Buffer
		if code := run([]string{test.action, "--image", "nonexistent", "--original", "nonexistent", "--confirm", test.approval}, &out, &errs); code != 1 ||
			out.Len() != 0 || !strings.Contains(errs.String(), "require root on the ARM") {
			t.Fatalf("host hardware action accepted: code=%d err=%s", code, &errs)
		}
	}
	for _, test := range []struct{ action, approval string }{
		{"install", "INSTALL-rootfs-pilot-pty01-02920000-04fa0000"},
		{"install", probewrite.MappingAck},
		{"check-map", "MAP-ONLY-rootfs-pilot-pty01-02920000-04fa0000"},
		{"check-map", probewrite.InstallAck},
	} {
		var out, errs bytes.Buffer
		if code := run([]string{test.action, "--image", "nonexistent", "--original", "nonexistent", "--confirm", test.approval}, &out, &errs); code != 2 ||
			out.Len() != 0 || !strings.Contains(errs.String(), "exact confirmation") {
			t.Fatalf("wrong approval accepted: code=%d err=%s", code, &errs)
		}
	}
}

func TestBSLCLIActualPlan(t *testing.T) {
	dir := os.Getenv("NAND_BSL_ARTIFACT_DIR")
	if dir == "" {
		t.Skip("set NAND_BSL_ARTIFACT_DIR for retained BSL CLI verification")
	}
	var out, errs bytes.Buffer
	if code := run([]string{"plan", "--image", filepath.Join(dir, probewrite.ImageFilename), "--original", filepath.Join(dir, probewrite.OriginalFilename)}, &out, &errs); code != 0 {
		t.Fatalf("plan failed: code=%d err=%s", code, &errs)
	}
	var plan probewrite.Plan
	if err := json.Unmarshal(out.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	if plan.Start != 0x01a20000 || plan.End != 0x01f20000 || plan.Span != 5242880 ||
		len(plan.Blocks) != 40 || plan.ImageSHA256 != probewrite.ImageHash ||
		plan.OriginalSHA256 != probewrite.OriginalHash || plan.Profile != probewrite.PlanProfileName ||
		plan.FilesystemSHA256 != probewrite.FilesystemHash || plan.WriteAuthorized ||
		plan.ChangedBlocks == 0 || plan.WriteOrder[len(plan.WriteOrder)-1] != 0 || errs.Len() != 0 {
		t.Fatalf("incorrect offline BSL plan: %+v", plan)
	}
}
