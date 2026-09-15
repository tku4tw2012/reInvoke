//go:build nand2134

package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/tku4tw2012/reinvoke/tools/nand-inspect/internal/probewrite"
)

func TestReconstruction2134CLIUnsealedDoesNotAccessInputs(t *testing.T) {
	if probewrite.CheckReconstructionAction("apply", "APPLY-2134-EARLY-ADB-01") == nil {
		t.Skip("profile has been sealed")
	}
	for _, action := range []string{"plan", "preflight", "apply", "verify-target"} {
		args := []string{action, "--manifest", "/must-not-open-manifest", "--capsule", "/must-not-open-capsule"}
		if action != "plan" {
			args = append(args, "--evidence", "/must-not-create-evidence")
		}
		if action == "apply" {
			args = append(args, "--confirm", "APPLY-2134-EARLY-ADB-01")
		}
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "unsealed") ||
			strings.Contains(stderr.String(), "pin input:") || stdout.Len() != 0 {
			t.Fatalf("action=%s unexpectedly accessed input/device: code=%d err=%s", action, code, &stderr)
		}
	}
}

func TestReconstruction2134CLIHelpAndWrongPhase(t *testing.T) {
	var out, err bytes.Buffer
	if run([]string{"--help"}, &out, &err) != 0 || !strings.Contains(out.String(), "apply") ||
		!strings.Contains(out.String(), "0x08320000") || !strings.Contains(out.String(), "APPLY-2134-EARLY-ADB-01") ||
		strings.Contains(out.String(), "qualify-first") {
		t.Fatalf("wrong profile help: %s", &out)
	}
	for _, args := range [][]string{
		{"qualify-first", "--confirm", "RECONSTRUCT-STOCK-ADB-FIRST-1573"},
		{"complete", "--confirm", "RECONSTRUCT-STOCK-ADB-REMAINING-865"},
		{"apply", "--confirm", "RECONSTRUCT-STOCK-ADB-REMAINING-865"},
		{"apply", "--offset", "0"},
	} {
		out.Reset()
		err.Reset()
		if run(args, &out, &err) != 2 {
			t.Fatalf("unsupported action/ack accepted: %v", args)
		}
	}
}
