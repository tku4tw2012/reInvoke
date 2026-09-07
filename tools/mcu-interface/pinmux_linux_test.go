// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"testing"
)

func TestApplyRegisterMasksPreservesUnrelatedBits(t *testing.T) {
	actual := applyRegisterMasks(0x0018d249, 0x00200000, 0x00000008)
	if actual != 0x0038d241 {
		t.Fatalf("register = %#08x, want %#08x", actual, uint32(0x0038d241))
	}
}
