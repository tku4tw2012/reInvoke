// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"fmt"
	"testing"
)

// recordingMCU captures the frames written to the microcontroller.
type recordingMCU struct {
	frames [][6]byte
	err    error
}

func (r *recordingMCU) WriteMCUCommand(frame [6]byte) error {
	if r.err != nil {
		return r.err
	}
	r.frames = append(r.frames, frame)
	return nil
}

// TestBrightnessFrameMatchesTheDonor asserts the exact frame recovered from the
// donor binary at 0xb499c: opcode 0x0A in byte 0, the level in byte 1.
//
// This is spelled out as literal bytes rather than built from the constants it
// is checking, so that changing an opcode makes the test fail instead of
// quietly agreeing with itself.
func TestBrightnessFrameMatchesTheDonor(t *testing.T) {
	mcu := &recordingMCU{}
	controller := newDeviceAppearanceController(mcu, nil)
	if err := controller.SetBrightness(50); err != nil {
		t.Fatalf("set brightness: %v", err)
	}
	want := [6]byte{0x0A, 50, 0, 0, 0, 0}
	if len(mcu.frames) != 1 || mcu.frames[0] != want {
		t.Fatalf("frame = %v, want %v", mcu.frames, want)
	}
}

// TestBrightnessRejectsOutOfRange proves the 0 to 100 bound the donor enforced
// with "cmp r0, #100" is enforced here, and that a rejected value sends nothing.
func TestBrightnessRejectsOutOfRange(t *testing.T) {
	mcu := &recordingMCU{}
	controller := newDeviceAppearanceController(mcu, nil)
	for _, level := range []int{-1, 101, 255, 1000} {
		if err := controller.SetBrightness(level); err == nil {
			t.Fatalf("accepted brightness %d", level)
		}
	}
	if len(mcu.frames) != 0 {
		t.Fatalf("a rejected brightness still wrote %v", mcu.frames)
	}
	for _, level := range []int{0, 1, 50, 99, 100} {
		if err := controller.SetBrightness(level); err != nil {
			t.Fatalf("rejected valid brightness %d: %v", level, err)
		}
	}
}

// TestDeviceColorFrameMatchesTheDonor asserts opcode 0x0B with the donor's own
// two values: black is 0 and white is 1, as compared at 0xb41c8 and 0xb421c.
func TestDeviceColorFrameMatchesTheDonor(t *testing.T) {
	for _, testCase := range []struct {
		name string
		want [6]byte
	}{
		{"black", [6]byte{0x0B, 0, 0, 0, 0, 0}},
		{"white", [6]byte{0x0B, 1, 0, 0, 0, 0}},
	} {
		mcu := &recordingMCU{}
		controller := newDeviceAppearanceController(mcu, nil)
		if err := controller.SetColor(testCase.name); err != nil {
			t.Fatalf("set %s: %v", testCase.name, err)
		}
		if len(mcu.frames) != 1 || mcu.frames[0] != testCase.want {
			t.Fatalf("%s frame = %v, want %v", testCase.name, mcu.frames, testCase.want)
		}
	}
}

// TestDeviceColorRefusesUnknownNames proves an unsupported colour sends nothing
// rather than a guessed value. The donor accepted only two names on this
// opcode; inventing a third would put an unverified byte on the bus.
func TestDeviceColorRefusesUnknownNames(t *testing.T) {
	mcu := &recordingMCU{}
	controller := newDeviceAppearanceController(mcu, nil)
	for _, name := range []string{"red", "blue", "00ff00", "", "amber"} {
		if err := controller.SetColor(name); err == nil {
			t.Fatalf("accepted colour %q", name)
		}
	}
	if len(mcu.frames) != 0 {
		t.Fatalf("a refused colour still wrote %v", mcu.frames)
	}
}

// TestPowerModeFrameMatchesTheDonor asserts opcode 0x02 with standby 0 and
// operational 1, the two strings compared at 0xb4118.
func TestPowerModeFrameMatchesTheDonor(t *testing.T) {
	for _, testCase := range []struct {
		name string
		want [6]byte
	}{
		{"standby", [6]byte{0x02, 0, 0, 0, 0, 0}},
		{"standby mode", [6]byte{0x02, 0, 0, 0, 0, 0}},
		{"operational", [6]byte{0x02, 1, 0, 0, 0, 0}},
		{"operational mode", [6]byte{0x02, 1, 0, 0, 0, 0}},
	} {
		mcu := &recordingMCU{}
		controller := newDeviceAppearanceController(mcu, nil)
		if err := controller.SetPowerMode(testCase.name); err != nil {
			t.Fatalf("set %s: %v", testCase.name, err)
		}
		if len(mcu.frames) != 1 || mcu.frames[0] != testCase.want {
			t.Fatalf("%s frame = %v, want %v", testCase.name, mcu.frames, testCase.want)
		}
	}
	mcu := &recordingMCU{}
	controller := newDeviceAppearanceController(mcu, nil)
	if err := controller.SetPowerMode("sleep"); err == nil {
		t.Fatal("accepted an unknown power mode")
	}
	if len(mcu.frames) != 0 {
		t.Fatalf("a refused power mode still wrote %v", mcu.frames)
	}
}

// TestGetDeviceColorRequestFrame asserts the request opcode 0x0C.
func TestGetDeviceColorRequestFrame(t *testing.T) {
	mcu := &recordingMCU{}
	controller := newDeviceAppearanceController(mcu, nil)
	if err := controller.RequestColor(); err != nil {
		t.Fatalf("request colour: %v", err)
	}
	want := [6]byte{0x0C, 0, 0, 0, 0, 0}
	if len(mcu.frames) != 1 || mcu.frames[0] != want {
		t.Fatalf("frame = %v, want %v", mcu.frames, want)
	}
}

// TestAppearanceDefaultsAreTheVendorValues proves startup sends the values from
// the vendor's own settings store rather than anything chosen here:
// LED_INTENSITY 50 and LED_RGB 000000, which is black.
func TestAppearanceDefaultsAreTheVendorValues(t *testing.T) {
	mcu := &recordingMCU{}
	controller := newDeviceAppearanceController(mcu, nil)
	controller.ApplyDefaults()
	want := [][6]byte{
		{0x0A, 50, 0, 0, 0, 0},
		{0x0B, 0, 0, 0, 0, 0},
	}
	if len(mcu.frames) != len(want) {
		t.Fatalf("frames = %v, want %v", mcu.frames, want)
	}
	for i := range want {
		if mcu.frames[i] != want[i] {
			t.Fatalf("frame %d = %v, want %v", i, mcu.frames[i], want[i])
		}
	}
}

// TestColorReportsWhetherItWasWritten proves the getter distinguishes a value
// this service actually sent from the default it merely assumes. The donor read
// the real colour from an event this runtime does not consume, so reporting the
// assumption as a reading would be a quiet lie.
func TestColorReportsWhetherItWasWritten(t *testing.T) {
	mcu := &recordingMCU{}
	controller := newDeviceAppearanceController(mcu, nil)
	if colour, applied := controller.Color(); applied {
		t.Fatalf("a fresh controller claimed %q was written", colour)
	}
	if err := controller.SetColor("white"); err != nil {
		t.Fatalf("set colour: %v", err)
	}
	colour, applied := controller.Color()
	if !applied || colour != "white" {
		t.Fatalf("after writing: colour=%q applied=%v", colour, applied)
	}
	if appearanceSource(false) == appearanceSource(true) {
		t.Fatal("the two cases are not distinguishable to a caller")
	}
}

// TestFailedWriteDoesNotRecordState proves a frame the MCU refused does not
// leave this service believing it succeeded.
func TestFailedWriteDoesNotRecordState(t *testing.T) {
	mcu := &recordingMCU{err: errBusRefused}
	controller := newDeviceAppearanceController(mcu, nil)
	if err := controller.SetColor("white"); err == nil {
		t.Fatal("a refused write reported success")
	}
	if _, applied := controller.Color(); applied {
		t.Fatal("a refused write was recorded as applied")
	}
}

// errBusRefused stands in for an I2C failure.
var errBusRefused = fmt.Errorf("bus refused")

// TestHWIDRequestFrame asserts the request opcode 0x07.
func TestHWIDRequestFrame(t *testing.T) {
	mcu := &recordingMCU{}
	controller := newDeviceAppearanceController(mcu, nil)
	if err := controller.RequestHWID(); err != nil {
		t.Fatalf("request: %v", err)
	}
	want := [6]byte{0x07, 0, 0, 0, 0, 0}
	if len(mcu.frames) != 1 || mcu.frames[0] != want {
		t.Fatalf("frame = %v, want %v", mcu.frames, want)
	}
}

// TestHWIDDecodeMatchesTheDonor asserts the reply layout read out of the
// donor's event dispatch: byte 1 selects DV1 or DV2, and bytes 2 to 4 are
// printed as three two-digit fields.
func TestHWIDDecodeMatchesTheDonor(t *testing.T) {
	for _, testCase := range []struct {
		frame        [6]byte
		wantRevision string
		wantVersion  string
	}{
		{[6]byte{0x07, 0, 1, 2, 3, 0}, "DV1", "010203"},
		{[6]byte{0x07, 1, 12, 34, 56, 0}, "DV2", "123456"},
		{[6]byte{0x07, 0, 0, 0, 0, 0}, "DV1", "000000"},
	} {
		identity, err := decodeHWIDFrame(testCase.frame)
		if err != nil {
			t.Fatalf("decode %v: %v", testCase.frame, err)
		}
		if identity.Raw == "" {
			t.Fatalf("decode %v dropped the raw bytes", testCase.frame)
		}
		if identity.Revision != testCase.wantRevision || identity.Version != testCase.wantVersion {
			t.Fatalf("decode %v = %q/%q, want %q/%q",
				testCase.frame, identity.Revision, identity.Version,
				testCase.wantRevision, testCase.wantVersion)
		}
	}
}

// TestHWIDRefusesUnknownRevision proves a revision byte the donor did not
// recognise is refused rather than named. The donor logged "HW ID Error!" for
// this case; inventing a third board name would be a fabricated reading.
func TestHWIDRefusesUnknownRevision(t *testing.T) {
	for _, revision := range []byte{2, 3, 0xff} {
		if _, err := decodeHWIDFrame([6]byte{0x07, revision, 0, 0, 0, 0}); err == nil {
			t.Fatalf("accepted revision byte %d", revision)
		}
	}
}

// TestHWIDIgnoresOtherFrames proves button presses sharing the event channel
// are not mistaken for a reply, and are left for the button decoder.
func TestHWIDIgnoresOtherFrames(t *testing.T) {
	controller := newDeviceAppearanceController(&recordingMCU{}, nil)
	for _, frame := range [][6]byte{
		{0x24, 0, 0, 0, 0, 0},
		{0x09, 1, 0, 1, 0, 0},
		{0x00, 0, 0, 0, 0, 0},
		{0x0C, 1, 0, 0, 0, 0},
	} {
		if controller.OfferFrame(frame) {
			t.Fatalf("claimed frame %v as a hardware identity", frame)
		}
		if _, known := controller.HWID(); known {
			t.Fatalf("frame %v populated the identity", frame)
		}
	}
	if !controller.OfferFrame([6]byte{0x07, 1, 11, 22, 33, 0}) {
		t.Fatal("did not claim a real reply")
	}
	identity, known := controller.HWID()
	if !known || identity.Revision != "DV2" || identity.Version != "112233" {
		t.Fatalf("identity = %+v known=%v", identity, known)
	}
}

// TestReadHWIDReturnsCachedIdentity proves a second read does not re-request:
// the board this is soldered to does not change while the service runs.
func TestReadHWIDReturnsCachedIdentity(t *testing.T) {
	mcu := &recordingMCU{}
	controller := newDeviceAppearanceController(mcu, nil)
	controller.OfferFrame([6]byte{0x07, 0, 1, 2, 3, 0})
	identity, err := controller.ReadHWID(context.Background())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if identity.Revision != "DV1" {
		t.Fatalf("identity = %+v", identity)
	}
	if len(mcu.frames) != 0 {
		t.Fatalf("a cached read still sent %v", mcu.frames)
	}
}

// TestReadHWIDGivesUp proves an MCU that never answers produces an error
// rather than a hang or an invented identity.
func TestReadHWIDGivesUp(t *testing.T) {
	mcu := &recordingMCU{}
	controller := newDeviceAppearanceController(mcu, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := controller.ReadHWID(ctx); err == nil {
		t.Fatal("a silent MCU produced an identity")
	}
	if len(mcu.frames) != 1 {
		t.Fatalf("expected one request, got %v", mcu.frames)
	}
}

// TestBrightnessTakesABareInteger pins the argument shape. Brightness was
// wired to the volume reader, which demands [value, "music"], so every
// well-formed call was refused on hardware until this was corrected.
func TestBrightnessTakesABareInteger(t *testing.T) {
	level, err := singleIntegerArgument([]interface{}{uint64(50)})
	if err != nil || level != 50 {
		t.Fatalf("bare integer gave %d, %v", level, err)
	}
	for _, args := range [][]interface{}{
		{},
		{uint64(50), "music"},
		{"50"},
		{uint64(1), uint64(2)},
	} {
		if _, err := singleIntegerArgument(args); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}
