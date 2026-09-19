// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
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

func TestInitializeMutesBeforeConfiguringDSPPowerRails(t *testing.T) {
	hardware := newRecordingHardware(0x00)
	control := newController(hardware)
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
		{kind: "write", address: 0x20, register: 0x01, value: 0x12},
		{kind: "write", address: 0x20, register: 0x01, value: 0x1a},
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
	control := newController(hardware)
	control.sleep = func(time.Duration) {}

	if err := control.initialize(); err != nil {
		t.Fatal(err)
	}

	// Initialization owns the amplifier and DAC mute bits and nothing else.
	// Every other bit of the live expander value must survive untouched.
	const owned = ampMuteMask | dacMuteMask
	var last byte
	var sawWrite bool
	for _, operation := range hardware.operations {
		if operation.kind != "write" ||
			operation.address != expanderAddress ||
			operation.register != expanderOutput {
			continue
		}
		sawWrite = true
		last = operation.value
		if operation.value&^owned != 0xfb&^owned {
			t.Fatalf("initialization changed a bit it does not own: %#v",
				operation)
		}
	}
	if !sawWrite {
		t.Fatal("initialization never wrote the expander output")
	}
	// Initialization leaves them muted, as the donor does. OpenOutputs is
	// what opens them, once the DSP is ready.
	if last&ampMuteMask == 0 {
		t.Fatalf("initialization left the amplifier open: expander = %#x", last)
	}
}

func TestInitializationRestoresDonorExpanderDirections(t *testing.T) {
	hardware := newRecordingHardware(0)
	hardware.registers[[2]byte{expanderAddress, expanderConfig}] = 0xff
	control := newController(hardware)
	control.sleep = func(time.Duration) {}

	if err := control.initialize(); err != nil {
		t.Fatal(err)
	}
	value := hardware.registers[[2]byte{expanderAddress, expanderConfig}]
	if value != 0x00 {
		t.Fatalf("expander configuration = 0x%02x, want donor value 0x00", value)
	}
}

func TestInitializationNeverReleasesDSPReset(t *testing.T) {
	hardware := newRecordingHardware(0x00)
	control := newController(hardware)
	control.sleep = func(time.Duration) {}

	if err := control.initialize(); err != nil {
		t.Fatal(err)
	}

	for _, operation := range hardware.operations {
		if operation.kind == "write" &&
			operation.address == expanderAddress &&
			operation.register == expanderOutput &&
			operation.value&0x01 != 0 {
			t.Fatalf("MCU initialization released DSP reset: %#v", operation)
		}
	}
}

func TestUnmuteAppliesDACFirst(t *testing.T) {
	hardware := newRecordingHardware(0x00)
	control := newController(hardware)
	control.sleep = func(time.Duration) {}
	if err := control.initialize(); err != nil {
		t.Fatal(err)
	}

	// Ordering is a hardware requirement the controller satisfies itself:
	// unmuting the amplifier first must bring the DAC up with it, not fail.
	if err := control.setAmpMute(false); err != nil {
		t.Fatalf("amplifier unmute: %v", err)
	}
	if control.dacMuted {
		t.Fatal("amplifier was unmuted while the DAC stayed muted")
	}

	value := hardware.registers[[2]byte{expanderAddress, expanderOutput}]
	if value != 0x1c {
		t.Fatalf("unmuted expander value = 0x%02x, want 0x1c", value)
	}
}

func TestShutdownMutesAmplifierBeforeDAC(t *testing.T) {
	hardware := newRecordingHardware(0x00)
	control := newController(hardware)
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
		writes[0].value != 0x1e ||
		writes[1].value != 0x1a {
		t.Fatalf("shutdown writes = %#v", writes)
	}
}

// TestInitializationLeavesOutputsMuted proves the amplifier is not live while
// the DSP powers up.
//
// The donor's initialisation mutes both and never unmutes: the only
// UnMuting AMP/DAC sites in that binary are its muteampcontrol and
// mutedaccontrol handlers and a power path, and its system-manager is what
// calls muteampcontrol afterwards. Opening them during initialisation put the
// amplifier live for DSP bootup and for the first gain change, which was
// audible on this unit as more than one pop during startup.
func TestInitializationLeavesOutputsMuted(t *testing.T) {
	hardware := newRecordingHardware(0x00)
	control := newController(hardware)
	control.sleep = func(time.Duration) {}

	if err := control.initialize(); err != nil {
		t.Fatal(err)
	}

	if !control.ampMuted {
		t.Fatal("initialization left the amplifier open")
	}
	if !control.dacMuted {
		t.Fatal("initialization left the DAC open")
	}
}

// TestOpenOutputsUnmutesDACBeforeAmplifier proves the speaker becomes audible
// on demand, and in the order the hardware requires.
func TestOpenOutputsUnmutesDACBeforeAmplifier(t *testing.T) {
	hardware := newRecordingHardware(0x00)
	control := newController(hardware)
	control.sleep = func(time.Duration) {}
	if err := control.initialize(); err != nil {
		t.Fatal(err)
	}
	start := len(hardware.operations)

	if err := control.OpenOutputs(); err != nil {
		t.Fatal(err)
	}

	var last byte
	var sawWrite bool
	for _, operation := range hardware.operations[start:] {
		if operation.kind == "write" &&
			operation.address == expanderAddress &&
			operation.register == expanderOutput {
			last = operation.value
			sawWrite = true
		}
	}
	if !sawWrite {
		t.Fatal("OpenOutputs never wrote the expander output")
	}
	if last&ampMuteMask != 0 {
		t.Fatalf("amplifier still muted: expander = %#x", last)
	}
	// The DAC bit is an enable line, the inverse of the amplifier bit.
	if last&dacMuteMask == 0 {
		t.Fatalf("DAC still muted: expander = %#x", last)
	}
	if control.ampMuted || control.dacMuted {
		t.Fatalf("controller still reports muted: amp=%v dac=%v",
			control.ampMuted, control.dacMuted)
	}
}

func TestDACFailureReassertsBothMutes(t *testing.T) {
	hardware := newRecordingHardware(0x00)
	hardware.failDACAt = 0x25
	control := newController(hardware)
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

func TestCancelledSessionCannotResumeQueuedMuteWrite(t *testing.T) {
	for _, test := range []struct {
		name string
		run  func(*controller, context.Context) error
	}{
		{
			name: "amplifier",
			run: func(control *controller, ctx context.Context) error {
				return control.setAmpMuteContext(ctx, false)
			},
		},
		{
			name: "DAC",
			run: func(control *controller, ctx context.Context) error {
				return control.setDACMuteContext(ctx, false)
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			hardware := newRecordingHardware(0)
			control := newController(hardware)
			control.initialized = true
			start := len(hardware.operations)
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			control.mu.Lock()
			go func() { done <- test.run(control, ctx) }()
			cancel()
			control.mu.Unlock()
			if err := <-done; !errors.Is(err, context.Canceled) {
				t.Fatalf("queued mute error = %v, want cancellation", err)
			}
			if len(hardware.operations) != start {
				t.Fatalf(
					"cancelled session wrote hardware: %#v",
					hardware.operations[start:],
				)
			}
		})
	}
}
