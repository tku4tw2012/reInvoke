// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

type recordingStartupBus struct {
	writes    [][6]byte
	responses [][6]byte
}

func (bus *recordingStartupBus) WriteMCUCommand(frame [6]byte) error {
	bus.writes = append(bus.writes, frame)
	return nil
}

func (bus *recordingStartupBus) ReadMCUEvent() ([6]byte, error) {
	response := bus.responses[0]
	bus.responses = bus.responses[1:]
	return response, nil
}

func TestInitializeMCUProtocolMatchesCapturedExchange(t *testing.T) {
	bus := &recordingStartupBus{responses: [][6]byte{
		{0x01, 0x01, 0x00, 0x01, 0x10, 0x00},
		{0x06, 0x00, 0x0d, 0x00, 0xda, 0xcb},
		{0x23, 0x00, 0x03, 0x00, 0x6a, 0xde},
		{0x26, 0x00, 0xb4, 0x25, 0xb2, 0x25},
	}}
	if err := initializeMCUProtocol(bus); err != nil {
		t.Fatal(err)
	}
	want := [][6]byte{
		{0x01, 0xa6, 0x14},
		{0x23, 0x00, 0x00, 0x00, 0x2c, 0xb8},
		{0x25, 0xa6, 0x14},
		{0x26},
	}
	if !reflect.DeepEqual(bus.writes, want) {
		t.Fatalf("MCU startup writes = %x, want %x", bus.writes, want)
	}
}

func TestInitializeMCUProtocolAcceptsReorderedAndStaleResponses(t *testing.T) {
	bus := &recordingStartupBus{responses: [][6]byte{
		{0x23, 0x00, 0x03, 0x00, 0x6a, 0xde},
		{},
		{0x06, 0x00, 0x0d, 0x00, 0xda, 0xcb},
		{0x01, 0x01, 0x00, 0x01, 0x10, 0x00},
		{},
		{0x26, 0x00, 0xb4, 0x25, 0xb2, 0x25},
	}}
	if err := initializeMCUProtocol(bus); err != nil {
		t.Fatal(err)
	}
}

func TestInitializeMCUProtocolRunsOncePerRAMBoot(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "mcu-ready")
	bus := &recordingStartupBus{responses: [][6]byte{
		{0x01, 0x01, 0x00, 0x01, 0x10, 0x00},
		{0x06},
		{0x23, 0x00, 0x03},
		{0x26},
	}}
	if err := initializeMCUProtocolOnce(bus, statePath); err != nil {
		t.Fatal(err)
	}
	writes := len(bus.writes)
	if err := initializeMCUProtocolOnce(bus, statePath); err != nil {
		t.Fatal(err)
	}
	if len(bus.writes) != writes {
		t.Fatal("MCU startup exchange repeated after state marker")
	}
	info, err := os.Stat(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("state mode = %o, want 600", info.Mode().Perm())
	}
}
