// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

// Recovered wire framing of the donor dsp-client SPI link.
//
// Host to DSP, five header bytes followed by a zero padded payload:
//
//	byte 0  message id, high byte
//	byte 1  message id, low byte
//	byte 2  payload length, high byte
//	byte 3  payload length, low byte
//	byte 4  checksum
//	byte 5+ payload
//
// The recorded EVENT_DSP_BOOTUP frame 00 01 00 01 06 04 00 00 and the recorded
// getVer frame 00 00 00 01 09 08 00 00 both satisfy the rules below.

import (
	"errors"
	"fmt"
)

// Message ids used by the recovered command table.
const (
	messageIDControl = 0
	messageIDBoot    = 1
	messageIDTest    = 2
)

// The donor never queues a payload longer than three bytes, but the staging
// buffer bounds every frame it can build.
const maxPayloadBytes = 614400 - 5

type frame struct {
	ID      uint16
	Payload []byte
}

// Code is the event code of a device-to-host frame, which the donor reads as
// the first payload byte.
func (f frame) Code() (byte, bool) {
	if len(f.Payload) == 0 {
		return 0, false
	}
	return f.Payload[0], true
}

// frameLength rounds the five byte header plus payload up to a multiple of
// four. Both directions use the same rule.
func frameLength(payload int) int {
	return ((payload + 5 + 3) / 4) * 4
}

// devicePayloadRead is how many bytes the donor reads after the header. It
// clamps to three so that an id and code tuple always arrives.
func devicePayloadRead(payload int) int {
	if payload <= 3 {
		return 3
	}
	return frameLength(payload) - 5
}

func checksum(id uint16, payload []byte) byte {
	sum := int(id>>8) + int(id&0xff) + (len(payload) >> 8) +
		(len(payload) & 0xff)
	for _, value := range payload {
		sum += int(value)
	}
	return byte(sum)
}

func buildFrame(id uint16, payload []byte) ([]byte, error) {
	if len(payload) > maxPayloadBytes {
		return nil, fmt.Errorf("payload of %d bytes is too long", len(payload))
	}
	out := make([]byte, frameLength(len(payload)))
	out[0] = byte(id >> 8)
	out[1] = byte(id)
	out[2] = byte(len(payload) >> 8)
	out[3] = byte(len(payload))
	out[4] = checksum(id, payload)
	copy(out[5:], payload)
	return out, nil
}

var (
	errFrameRejected = errors.New("device frame rejected by header check")
	errFrameChecksum = errors.New("device frame checksum mismatch")
)

// parseDeviceHeader applies the donor's header check: a frame whose first two
// bytes are 0xFF, or which declares no payload, is discarded.
func parseDeviceHeader(header []byte) (id uint16, length int, err error) {
	if len(header) < 5 {
		return 0, 0, errors.New("device header needs five bytes")
	}
	if header[0] == 0xff || header[1] == 0xff {
		return 0, 0, errFrameRejected
	}
	id = uint16(header[0])<<8 | uint16(header[1])
	length = int(header[2])<<8 | int(header[3])
	if length == 0 {
		return 0, 0, errFrameRejected
	}
	return id, length, nil
}

// decodeDeviceFrame verifies the checksum over the four header bytes and every
// byte actually read, padding included, then returns the declared payload.
func decodeDeviceFrame(header, read []byte) (frame, error) {
	id, length, err := parseDeviceHeader(header)
	if err != nil {
		return frame{}, err
	}
	if len(read) != devicePayloadRead(length) {
		return frame{}, fmt.Errorf(
			"device frame read %d bytes, expected %d",
			len(read),
			devicePayloadRead(length),
		)
	}
	sum := int(header[0]) + int(header[1]) + int(header[2]) + int(header[3])
	for _, value := range read {
		sum += int(value)
	}
	if byte(sum) != header[4] {
		return frame{}, errFrameChecksum
	}
	if length > len(read) {
		length = len(read)
	}
	payload := make([]byte, length)
	copy(payload, read[:length])
	return frame{ID: id, Payload: payload}, nil
}

// eventName returns the recovered name of a device-to-host event, or an empty
// string for the codes the donor routes to its fallback branch.
func eventName(id uint16, code byte) string {
	switch id {
	case messageIDControl:
		switch code {
		case 0x04:
			return "EVENT_NEW_DAC_GAIN"
		case 0x05:
			return "EVENT_EXPECT_SPEECH"
		case 0x06:
			return "EVENT_CANCEL_TRIGGER"
		case 0x07:
			return "EVENT_SW_UPGRADE"
		case 0x08:
			return "EVENT_DSP_VERSION"
		case 0x09:
			return "EVENT_MIC_MUTE"
		case 0x0b:
			return "EVENT_CORTANA_SKYPE"
		case 0x0c:
			return "DSP_MEMORY_DUMP"
		case 0xff:
			return "EVENT_ERR"
		}
	case messageIDBoot:
		switch code {
		case 0x00:
			return "EVENT_TRIGGER_FOUND"
		case 0x01:
			return "EVENT_PAYLOAD_DEGIN"
		case 0x02:
			return "EVENT_PAYLOAD_END"
		case 0x03:
			return "EVENT_PAYLOAD_TIMEOUT"
		case 0x04:
			return "EVENT_DSP_BOOTUP"
		case 0xff:
			return "EVENT_WRITE_ERR"
		}
	case messageIDTest:
		switch code {
		case 0x00:
			return "EVENT_MIC_TEST_SINGLE"
		case 0x01:
			return "EVENT_MIC_TEST_PAIR"
		case 0x02:
			return "EVENT_MIC_NORMAL"
		case 0x03:
			return "EVENT_HW_PERFORM_TEST"
		case 0xff:
			return "EVENT_TEST_ERR"
		}
	}
	return ""
}

// decodeVersion packs the four EVENT_DSP_VERSION payload bytes big endian into
// one integer. The captured 00 00 64 58 packs to 25688, which is the value the
// donor published.
func decodeVersion(payload []byte) (text string, packed uint32, ok bool) {
	if len(payload) < 5 {
		return "", 0, false
	}
	bytes := payload[1:5]
	packed = uint32(bytes[0])<<24 | uint32(bytes[1])<<16 |
		uint32(bytes[2])<<8 | uint32(bytes[3])
	return fmt.Sprintf(
		"%X.%X.%X.%X",
		bytes[0],
		bytes[1],
		bytes[2],
		bytes[3],
	), packed, true
}
