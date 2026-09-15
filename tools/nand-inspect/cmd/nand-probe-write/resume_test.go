package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/tku4tw2012/reinvoke/tools/nand-inspect/internal/probewrite"
)

func TestResumeCLIGates(t *testing.T) {
	for _, action := range []string{"resume", "resume-preflight", "resume-plan"} {
		for _, override := range []string{"--offset", "--resume-index", "--corrected-limit"} {
			var out, errs bytes.Buffer
			args := []string{action, "--image", "/dev/null", "--original", "/dev/null", override, "1"}
			if code := run(args, &out, &errs); code != 2 || out.Len() != 0 {
				t.Fatalf("override accepted: code=%d stderr=%s", code, &errs)
			}
		}
		if !probewrite.ResumeSupported {
			var out, errs bytes.Buffer
			if code := run([]string{action}, &out, &errs); code != 2 || !strings.Contains(errs.String(), "no device accessed") {
				t.Fatalf("default profile resume reached device: code=%d %s", code, &errs)
			}
		}
	}
	if !probewrite.ResumeSupported {
		return
	}
	for _, action := range []string{"resume", "resume-preflight"} {
		for _, ack := range []string{"", probewrite.InstallAck, probewrite.RestoreAck, probewrite.MappingAck,
			"RESUME-rootfs-pilot-pty01-02920000-04fa0000-block136-page62",
			"READ-ONLY-RESUME-rootfs-pilot-pty01-02920000-04fa0000-block136-page62"} {
			var out, errs bytes.Buffer
			if code := run([]string{action, "--image", "/dev/null", "--original", "/dev/null", "--confirm", ack}, &out, &errs); code != 2 || !strings.Contains(errs.String(), "exact confirmation") {
				t.Fatalf("wrong approval accepted: code=%d %s", code, &errs)
			}
		}
	}
}

func TestResumeOfflineActualPlan(t *testing.T) {
	if !probewrite.ResumeSupported {
		return
	}
	var out, errs bytes.Buffer
	if code := run([]string{"resume-plan", "--image", os.Getenv("NAND_PILOT_PAYLOAD"), "--original", os.Getenv("NAND_PILOT_ORIGINAL")}, &out, &errs); code != 0 {
		t.Fatalf("resume-plan failed: code=%d %s", code, &errs)
	}
	var plan probewrite.ResumePlan
	if err := json.Unmarshal(out.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	if plan.KnownAbsolutePage != 29822 || plan.MainCorrectionLimit != 1 || plan.OOBCorrectionLimit != 1 || !strings.HasPrefix(plan.CorrectionScope, "every candidate page") ||
		len(plan.WriteOrder) != 172 || plan.WriteOrder[0] != 137 || plan.WriteOrder[171] != 0 ||
		plan.Plan.WriteAuthorized || plan.Plan.ImageSHA256 != probewrite.ImageHash {
		t.Fatalf("incorrect fixed resume plan: %+v", plan)
	}
}

func TestHardwareDispatchSelectsOnlyRequestedEngine(t *testing.T) {
	for _, action := range []string{"preflight", "check-map", "resume-preflight", "resume", "install", "restore", "unknown"} {
		t.Run(action, func(t *testing.T) {
			selected := ""
			calls := 0
			engineResult := errors.New("selected engine result")
			record := func(name string) error {
				selected = name
				calls++
				return engineResult
			}
			err := dispatchHardware(action,
				func() error { return record("readonly") },
				func(restore bool) error {
					if restore {
						return record("restore")
					}
					return record("install")
				},
				func() error { return record("resume") })
			want := action
			switch action {
			case "preflight", "check-map", "resume-preflight":
				want = "readonly"
			case "unknown":
				want = ""
			}
			if !probewrite.ResumeSupported && (action == "resume" || action == "resume-preflight") {
				want = ""
			}
			if !probewrite.RestoreSupported && action == "restore" {
				want = ""
			}
			if selected != want {
				t.Fatalf("action %q dispatched to %q, want %q", action, selected, want)
			}
			if want == "" {
				if err == nil || calls != 0 {
					t.Fatal("rejected action reached an engine")
				}
			} else if calls != 1 || !errors.Is(err, engineResult) {
				t.Fatalf("engine count=%d; engine error not propagated: %v", calls, err)
			}
		})
	}
}
