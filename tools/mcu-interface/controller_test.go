// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"reflect"
	"testing"
	"time"
)

type hardwareOperation struct {
	kind     string
	address  byte
	register byte
	value    byte
}

type recordingHardware struct {
	registers  map[[2]byte]byte
	operations []hardwareOperation
	failDACAt  int
}

func newRecordingHardware(expander byte) *recordingHardware {
	return &recordingHardware{
		registers: map[[2]byte]byte{
			{expanderAddress, expanderOutput}: expander,
		},
		failDACAt: -1,
	}
}

func (hardware *recordingHardware) ReadRegister(
	address,
	register byte,
) (byte, error) {
	value := hardware.registers[[2]byte{address, register}]
	hardware.operations = append(hardware.operations, hardwareOperation{
		kind:     "read",
		address:  address,
		register: register,
		value:    value,
	})
	return value, nil
}

func (hardware *recordingHardware) WriteRegister(
	address,
	register,
	value byte,
) error {
	hardware.operations = append(hardware.operations, hardwareOperation{
		kind:     "write",
		address:  address,
		register: register,
		value:    value,
	})
	if address == dacAddress && int(register) == hardware.failDACAt {
		return fmt.Errorf("injected failure")
	}
	hardware.registers[[2]byte{address, register}] = value
	return nil
}

func (hardware *recordingHardware) UpdateRegister(
	address,
	register byte,
	update func(byte) byte,
) error {
	current, err := hardware.ReadRegister(address, register)
	if err != nil {
		return err
	}
	return hardware.WriteRegister(address, register, update(current))
}

func TestInitializePreservesMuteFirstCapturedOrder(t *testing.T) {
	hardware := newRecordingHardware(0x00)
	control := newController(hardware, mutePolicy{})
	var slept time.Duration
	control.sleep = func(duration time.Duration) {
		slept += duration
	}

	if err := control.initialize(); err != nil {
		t.Fatal(err)
	}

	var writes []hardwareOperation
	for _, operation := range hardware.operations {
		if operation.kind == "write" {
			writes = append(writes, operation)
		}
	}
	expectedPrefix := []hardwareOperation{
		{kind: "write", address: 0x20, register: 0x03, value: 0x00},
		{kind: "write", address: 0x20, register: 0x01, value: 0x02},
		{kind: "write", address: 0x20, register: 0x01, value: 0x02},
		{kind: "write", address: 0x20, register: 0x01, value: 0x03},
		{kind: "write", address: 0x20, register: 0x01, value: 0x13},
		{kind: "write", address: 0x20, register: 0x01, value: 0x1b},
	}
	if !reflect.DeepEqual(writes[:len(expectedPrefix)], expectedPrefix) {
		t.Fatalf("startup writes = %#v, want prefix %#v", writes, expectedPrefix)
	}
	for index, setting := range dacInitialization {
		operation := writes[len(expectedPrefix)+index]
		expected := hardwareOperation{
			kind:     "write",
			address:  dacAddress,
			register: setting[0],
			value:    setting[1],
		}
		if operation != expected {
			t.Fatalf("DAC write %d = %#v, want %#v", index, operation, expected)
		}
	}
	if slept != 2*time.Second {
		t.Fatalf("settle delay = %v, want 2s", slept)
	}
}

func TestLiveExpanderValueIsPreservedByInitialization(t *testing.T) {
	hardware := newRecordingHardware(0xfb)
	control := newController(hardware, mutePolicy{})
	control.sleep = func(time.Duration) {}

	if err := control.initialize(); err != nil {
		t.Fatal(err)
	}

	for _, operation := range hardware.operations {
		if operation.kind == "write" &&
			operation.address == expanderAddress &&
			operation.register == expanderOutput &&
			operation.value != 0xfb {
			t.Fatalf("captured expander value changed: %#v", operation)
		}
	}
}

func TestUnmuteRequiresPolicyAndDACFirst(t *testing.T) {
	hardware := newRecordingHardware(0x00)
	control := newController(hardware, mutePolicy{})
	control.sleep = func(time.Duration) {}
	if err := control.initialize(); err != nil {
		t.Fatal(err)
	}

	if err := control.setDACMute(false); err == nil {
		t.Fatal("unmute succeeded without local policy")
	}
	control.policy.AllowUnmute = true
	if err := control.setAmpMute(false); err == nil {
		t.Fatal("amplifier unmuted before DAC")
	}
	if err := control.setDACMute(false); err != nil {
		t.Fatalf("DAC unmute: %v", err)
	}
	if err := control.setAmpMute(false); err != nil {
		t.Fatalf("amplifier unmute: %v", err)
	}

	value := hardware.registers[[2]byte{expanderAddress, expanderOutput}]
	if value != 0x1d {
		t.Fatalf("unmuted expander value = 0x%02x, want 0x1d", value)
	}
}

func TestShutdownMutesAmplifierBeforeDAC(t *testing.T) {
	hardware := newRecordingHardware(0x00)
	control := newController(hardware, mutePolicy{AllowUnmute: true})
	control.sleep = func(time.Duration) {}
	if err := control.initialize(); err != nil {
		t.Fatal(err)
	}
	if err := control.setDACMute(false); err != nil {
		t.Fatal(err)
	}
	if err := control.setAmpMute(false); err != nil {
		t.Fatal(err)
	}
	start := len(hardware.operations)

	if err := control.muteAll(); err != nil {
		t.Fatal(err)
	}

	var writes []hardwareOperation
	for _, operation := range hardware.operations[start:] {
		if operation.kind == "write" {
			writes = append(writes, operation)
		}
	}
	if len(writes) != 2 ||
		writes[0].value != 0x1f ||
		writes[1].value != 0x1b {
		t.Fatalf("shutdown writes = %#v", writes)
	}
}

func TestPlaybackPolicyOwnsOrderedUnmuteAndRemute(t *testing.T) {
	hardware := newRecordingHardware(0x00)
	control := newController(hardware, mutePolicy{
		AllowPlaybackUnmute: true,
	})
	control.sleep = func(time.Duration) {}
	if err := control.initialize(); err != nil {
		t.Fatal(err)
	}
	start := len(hardware.operations)

	if err := control.setPlaybackActive(true); err != nil {
		t.Fatal(err)
	}
	if err := control.setPlaybackActive(false); err != nil {
		t.Fatal(err)
	}

	var writes []hardwareOperation
	for _, operation := range hardware.operations[start:] {
		if operation.kind == "write" {
			writes = append(writes, operation)
		}
	}
	want := []byte{0x1f, 0x1d, 0x1f, 0x1b}
	if len(writes) != len(want) {
		t.Fatalf("playback writes = %#v", writes)
	}
	for index, value := range want {
		if writes[index].value != value {
			t.Fatalf("playback writes = %#v, want values %x", writes, want)
		}
	}
}

func TestPlaybackUnmuteIsDeniedWithoutLocalMonitor(t *testing.T) {
	hardware := newRecordingHardware(0x00)
	control := newController(hardware, mutePolicy{})
	control.sleep = func(time.Duration) {}
	if err := control.initialize(); err != nil {
		t.Fatal(err)
	}
	if err := control.setPlaybackActive(true); err == nil {
		t.Fatal("playback unmute succeeded without local monitor policy")
	}
}

func TestDACFailureReassertsBothMutes(t *testing.T) {
	hardware := newRecordingHardware(0x00)
	hardware.failDACAt = 0x25
	control := newController(hardware, mutePolicy{})
	control.sleep = func(time.Duration) {}

	if err := control.initialize(); err == nil {
		t.Fatal("initialize succeeded despite DAC failure")
	}
	if !control.ampMuted || !control.dacMuted || control.initialized {
		t.Fatalf(
			"unsafe state: ampMuted=%t dacMuted=%t initialized=%t",
			control.ampMuted,
			control.dacMuted,
			control.initialized,
		)
	}
}
