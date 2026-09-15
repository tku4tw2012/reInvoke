//go:build !nand2134 && !nandstockroot

package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestReconstructionCLIRejectsBeforeAccess(t *testing.T) {
	for _, args := range [][]string{
		{"restore", "--manifest", "/dev/zero", "--capsule", "/dev/zero", "--evidence", "/not-created"},
		{"qualify-first", "--manifest", "/dev/zero", "--capsule", "/dev/zero", "--evidence", "/not-created", "--confirm", "wrong"},
		{"qualify-first", "--manifest", "/dev/zero", "--capsule", "/dev/zero", "--evidence", "/not-created", "--confirm", "RECONSTRUCT-SEPT7-FIRST-1573"},
		{"complete", "--manifest", "/dev/zero", "--capsule", "/dev/zero", "--evidence", "/not-created", "--confirm", "RECONSTRUCT-SEPT7-REMAINING-865"},
		{"complete", "--manifest", "/dev/zero", "--capsule", "/dev/zero", "--evidence", "/not-created", "--confirm", "RECONSTRUCT-SEPT7-FIRST-1573"},
		{"complete", "--manifest", "/dev/zero", "--capsule", "/dev/zero", "--evidence", "/not-created", "--confirm", "RECONSTRUCT-STOCK-ADB-FIRST-1573"},
		{"qualify-first", "--offset", "0"},
		{"plan", "--manifest", "/dev/zero", "--capsule", "/dev/zero", "--evidence", "/not-created"},
	} {
		var out, err bytes.Buffer
		if code := run(args, &out, &err); code != 2 {
			t.Fatalf("args=%v code=%d err=%s", args, code, &err)
		}
		if strings.Contains(err.String(), "pin input:") || strings.Contains(err.String(), "MEMGETINFO") {
			t.Fatal("access before validation")
		}
	}
}

func TestReconstructionCLIPlanRejectsDeviceInput(t *testing.T) {
	var out, err bytes.Buffer
	code := run([]string{"plan", "--manifest", "/dev/zero", "--capsule", "/dev/zero"}, &out, &err)
	if code != 1 || !strings.Contains(err.String(), "regular file") {
		t.Fatalf("code=%d error=%s", code, &err)
	}
}
