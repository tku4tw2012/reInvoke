// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"reflect"
	"testing"
)

func TestMessagePackPreservesCapturedWAMPID(t *testing.T) {
	const capturedID = uint64(2245403379414270)
	payload, err := encodeMessagePack([]interface{}{uint64(68), capturedID})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeMessagePack(payload)
	if err != nil {
		t.Fatal(err)
	}
	want := []interface{}{uint64(68), capturedID}
	if !reflect.DeepEqual(decoded, want) {
		t.Fatalf("decoded value = %#v, want %#v", decoded, want)
	}
}

func TestMessagePackRejectsOversizedCollectionLength(t *testing.T) {
	payload := []byte{0xdd, 0xff, 0xff, 0xff, 0xff}
	if _, err := decodeMessagePack(payload); err == nil {
		t.Fatal("oversized array length was accepted")
	}
}
