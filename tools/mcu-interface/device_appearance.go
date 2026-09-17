// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

// Device colour, LED brightness and MCU power mode.
//
// The opcodes here were recovered from the donor mcu-interface binary by
// disassembly rather than guessed; docs/mcu-command-map.md records how, and
// why guessing in this command space is not acceptable. Each frame is six
// bytes to I2C address 0x36, which is the transport the donor's own writer
// uses at 0x7dbf0.

package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	// setPowerModeCode is the donor's setmcupowermode. Byte 1 selects the mode.
	setPowerModeCode byte = 0x02
	// setBrightnessCode is SetRGBLEDBrightness. Byte 1 is 0 to 100, which the
	// donor validates with "cmp r0, #100" before sending.
	setBrightnessCode byte = 0x0A
	// setDeviceColorCode is setDeviceColor. Byte 1 selects black or white.
	setDeviceColorCode byte = 0x0B
	// getDeviceColorCode requests the colour; the answer arrives on the event
	// channel rather than as a reply, so it is not read back inline.
	getDeviceColorCode byte = 0x0C

	// Vendor defaults from caldata/FENV.bin: LED_INTENSITY and LED_WHITE are
	// both 50, and LED_RGB is 000000, which is black.
	defaultLEDBrightness = 50
	defaultDeviceColor   = "black"
)

// deviceColorValue maps the donor's two accepted colour names. The donor
// compared the argument against exactly these strings and sent 0 or 1; it did
// not accept an RGB triple on this opcode.
func deviceColorValue(name string) (byte, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "black", "off":
		return 0, nil
	case "white", "on":
		return 1, nil
	}
	return 0, fmt.Errorf("unknown device colour %q: the MCU accepts black or white", name)
}

func deviceColorName(value byte) string {
	if value == 0 {
		return "black"
	}
	return "white"
}

// powerModeValue maps the donor's two mode names.
func powerModeValue(name string) (byte, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "standby", "standby mode":
		return 0, nil
	case "operational", "operational mode":
		return 1, nil
	}
	return 0, fmt.Errorf(
		"unknown power mode %q: the MCU accepts standby or operational", name)
}

// deviceAppearanceController owns the colour and brightness of the ring.
//
// Brightness and colour are held together because both are read back by
// getDeviceColor and because a caller that sets one usually wants the other to
// stay put.
type deviceAppearanceController struct {
	writer heartbeatWriter
	logf   func(string, ...interface{})

	mu         sync.Mutex
	brightness byte
	color      byte
	applied    bool
	identity   *hardwareIdentity
}

func newDeviceAppearanceController(
	writer heartbeatWriter,
	logf func(string, ...interface{}),
) *deviceAppearanceController {
	color, _ := deviceColorValue(defaultDeviceColor)
	return &deviceAppearanceController{
		writer:     writer,
		logf:       logf,
		brightness: defaultLEDBrightness,
		color:      color,
	}
}

// SetBrightness sends the vendor's 0 to 100 brightness.
//
// The range is rejected here as well as by the controller because the donor
// rejected it before sending, and a value above 100 on this opcode has never
// been observed on this unit.
func (controller *deviceAppearanceController) SetBrightness(level int) error {
	if level < 0 || level > 100 {
		return fmt.Errorf("brightness %d is outside 0 to 100", level)
	}
	controller.mu.Lock()
	defer controller.mu.Unlock()
	// Bytes 2 to 5 are zero. The donor left them as whatever the stack held,
	// so the controller ignores them; zero is within what it already accepts.
	frame := [6]byte{setBrightnessCode, byte(level), 0, 0, 0, 0}
	if err := controller.writer.WriteMCUCommand(frame); err != nil {
		return fmt.Errorf("set LED brightness: %w", err)
	}
	controller.brightness = byte(level)
	controller.applied = true
	return nil
}

// SetColor sends the device colour.
func (controller *deviceAppearanceController) SetColor(name string) error {
	value, err := deviceColorValue(name)
	if err != nil {
		return err
	}
	controller.mu.Lock()
	defer controller.mu.Unlock()
	frame := [6]byte{setDeviceColorCode, value, 0, 0, 0, 0}
	if err := controller.writer.WriteMCUCommand(frame); err != nil {
		return fmt.Errorf("set device colour: %w", err)
	}
	controller.color = value
	controller.applied = true
	return nil
}

// Color reports the colour this service last set.
//
// The donor's getDeviceColor sends a request and the answer arrives on the
// event channel, not as a reply. Nothing on this unit consumes that event, so
// this reports what was last written rather than pretending to read the
// hardware. A caller is told which it is getting.
func (controller *deviceAppearanceController) Color() (string, bool) {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	return deviceColorName(controller.color), controller.applied
}

func (controller *deviceAppearanceController) Brightness() int {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	return int(controller.brightness)
}

// RequestColor sends the getDeviceColor request frame.
func (controller *deviceAppearanceController) RequestColor() error {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	frame := [6]byte{getDeviceColorCode, 0, 0, 0, 0, 0}
	if err := controller.writer.WriteMCUCommand(frame); err != nil {
		return fmt.Errorf("request device colour: %w", err)
	}
	return nil
}

// SetPowerMode selects standby or operational.
func (controller *deviceAppearanceController) SetPowerMode(name string) error {
	value, err := powerModeValue(name)
	if err != nil {
		return err
	}
	controller.mu.Lock()
	defer controller.mu.Unlock()
	frame := [6]byte{setPowerModeCode, value, 0, 0, 0, 0}
	if err := controller.writer.WriteMCUCommand(frame); err != nil {
		return fmt.Errorf("set MCU power mode: %w", err)
	}
	return nil
}

// ApplyDefaults sets the vendor's own startup appearance.
//
// Failure is reported rather than fatal: the speaker plays audio without its
// ring being right, and refusing to start over an LED would be worse than the
// fault it is reporting.
func (controller *deviceAppearanceController) ApplyDefaults() {
	if err := controller.SetBrightness(defaultLEDBrightness); err != nil {
		if controller.logf != nil {
			controller.logf("APPEARANCE_BRIGHTNESS_FAILED: %v", err)
		}
		return
	}
	if err := controller.SetColor(defaultDeviceColor); err != nil && controller.logf != nil {
		controller.logf("APPEARANCE_COLOR_FAILED: %v", err)
	}
}

// appearanceSource says whether a reported colour was actually written by this
// service or is only the default it assumes.
func appearanceSource(applied bool) string {
	if applied {
		return "last-written"
	}
	return "assumed-default"
}

// Hardware identity, read from the microcontroller.
//
// GetHWID is a request-and-wait exchange, not a register read. The donor sent
// opcode 0x07 and then polled a field that its event reader filled in, ten
// milliseconds at a time, up to 101 times. The reply arrives on the same event
// channel as button presses and carries the same opcode.
//
// Reply layout, from the donor's event dispatch at 0xd2cec:
//
//	byte 0  0x07
//	byte 1  board revision: 0 is DV1, 1 is DV2, anything else is an error
//	byte 2  version field, printed as two digits
//	byte 3  version field, printed as two digits
//	byte 4  version field, printed as two digits
const (
	// getHWIDCode requests the hardware identity. It is a read: the donor sent
	// it on every GetHWID call and it returns data rather than changing state.
	getHWIDCode byte = 0x07

	// hwidPollInterval and hwidPollAttempts reproduce the donor's own wait,
	// which is 101 attempts ten milliseconds apart.
	hwidPollInterval = 10 * time.Millisecond
	hwidPollAttempts = 101
)

// hardwareIdentity is what the MCU reports about the board it is soldered to.
type hardwareIdentity struct {
	Revision string
	Version  string
}

// decodeHWIDFrame reads a reply. It reports whether the frame is one.
//
// A revision byte outside the two the donor accepted is refused rather than
// guessed: the donor logged "HW ID Error!" for exactly this case, so a third
// value means something this project has not seen and must not name.
func decodeHWIDFrame(frame [6]byte) (hardwareIdentity, error) {
	if frame[0] != getHWIDCode {
		return hardwareIdentity{}, errNotHWIDFrame
	}
	var revision string
	switch frame[1] {
	case 0:
		revision = "DV1"
	case 1:
		revision = "DV2"
	default:
		return hardwareIdentity{}, fmt.Errorf(
			"MCU reported unknown board revision %d", frame[1])
	}
	return hardwareIdentity{
		Revision: revision,
		Version:  fmt.Sprintf("%02d%02d%02d", frame[2], frame[3], frame[4]),
	}, nil
}

// errNotHWIDFrame marks a frame that is simply not a reply to this request,
// which is expected: button presses share the channel.
var errNotHWIDFrame = errors.New("not a hardware identity frame")

// RequestHWID sends the request. The reply is delivered by the event reader.
func (controller *deviceAppearanceController) RequestHWID() error {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	frame := [6]byte{getHWIDCode, 0, 0, 0, 0, 0}
	if err := controller.writer.WriteMCUCommand(frame); err != nil {
		return fmt.Errorf("request hardware identity: %w", err)
	}
	return nil
}

// OfferFrame gives the controller a frame read from the MCU. It reports
// whether the frame was a hardware identity reply, so the caller can go on
// handling the frame itself if it was not.
func (controller *deviceAppearanceController) OfferFrame(frame [6]byte) bool {
	identity, err := decodeHWIDFrame(frame)
	if err != nil {
		if err != errNotHWIDFrame && controller.logf != nil {
			controller.logf("HWID_DECODE_FAILED: %v", err)
		}
		return err != errNotHWIDFrame
	}
	controller.mu.Lock()
	controller.identity = &identity
	controller.mu.Unlock()
	return true
}

// HWID reports the identity if the MCU has answered.
func (controller *deviceAppearanceController) HWID() (hardwareIdentity, bool) {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	if controller.identity == nil {
		return hardwareIdentity{}, false
	}
	return *controller.identity, true
}

// ReadHWID requests the identity and waits for the event reader to deliver it.
//
// The wait reproduces the donor's: ten milliseconds at a time, up to 101
// attempts. A cached answer is returned immediately, because the identity of
// the board this is soldered to does not change while the service runs.
func (controller *deviceAppearanceController) ReadHWID(
	ctx context.Context,
) (hardwareIdentity, error) {
	if identity, known := controller.HWID(); known {
		return identity, nil
	}
	if err := controller.RequestHWID(); err != nil {
		return hardwareIdentity{}, err
	}
	for attempt := 0; attempt < hwidPollAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return hardwareIdentity{}, ctx.Err()
		case <-time.After(hwidPollInterval):
		}
		if identity, known := controller.HWID(); known {
			return identity, nil
		}
	}
	// The donor logged "get HW ID timerout" here and returned nothing.
	return hardwareIdentity{}, errors.New("timed out waiting for the MCU to report its hardware identity")
}
