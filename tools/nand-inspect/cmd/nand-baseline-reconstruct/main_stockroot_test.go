//go:build nandstockroot

package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestStockRootCLIRejectsOtherTrialApprovalsBeforeAccess(t *testing.T) {
	for _, args := range [][]string{
		{"apply", "--manifest", "/must-not-open", "--capsule", "/must-not-open", "--evidence", "/must-not-create", "--confirm", "APPLY-2134-EARLY-ADB-01"},
		{"qualify-first", "--confirm", "RECONSTRUCT-STOCK-ADB-FIRST-1573"},
		{"complete", "--confirm", "RECONSTRUCT-STOCK-ADB-REMAINING-865"},
		{"apply", "--offset", "0"},
	} {
		var out, err bytes.Buffer
		if run(args, &out, &err) != 2 || strings.Contains(err.String(), "pin input:") {
			t.Fatalf("unexpected access/acceptance: %v %s", args, &err)
		}
	}
	var out, err bytes.Buffer
	if run([]string{"--help"}, &out, &err) != 0 || !strings.Contains(out.String(), "APPLY-STOCKROOT-NATIVE-01") ||
		!strings.Contains(out.String(), "Native ADB is optional") {
		t.Fatal("wrong StockRoot help")
	}
}
