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

func TestMessagePackRejectsHostileDeclaredLengths(t *testing.T) {
	payloads := [][]byte{
		{0xdb, 0xff, 0xff, 0xff, 0xff},
		{0xdd, 0xff, 0xff, 0xff, 0xff},
		{0xdf, 0xff, 0xff, 0xff, 0xff},
	}
	for _, payload := range payloads {
		if _, err := decodeMessagePack(payload); err == nil {
			t.Fatalf("hostile MessagePack length was accepted: %x", payload)
		}
	}
}

func TestMessagePackRejectsExcessiveNesting(t *testing.T) {
	payload := make([]byte, maxMessagePackDepth+2)
	for index := range payload {
		payload[index] = 0x91
	}
	payload = append(payload, 0xc0)
	if _, err := decodeMessagePack(payload); err == nil {
		t.Fatal("excessively nested MessagePack was accepted")
	}
}
