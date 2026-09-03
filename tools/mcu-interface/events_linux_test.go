// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import "testing"

func TestDecodeVerifiedRotaryEvents(t *testing.T) {
	tests := []struct {
		frame [6]byte
		event inputEvent
		valid bool
	}{
		{
			frame: [6]byte{0x04, 0x08, 0x02, 0, 0, 0},
			event: inputEvent{Name: "volumeup", Step: "2"},
			valid: true,
		},
		{
			frame: [6]byte{0x04, 0x09, 0x05, 0, 0, 0},
			event: inputEvent{Name: "volumedown", Step: "5"},
			valid: true,
		},
		{frame: [6]byte{0x04, 0x08, 0x00, 0, 0, 0}},
		{frame: [6]byte{0x04, 0x08, 0x06, 0, 0, 0}},
		{frame: [6]byte{0x04, 0x07, 0x02, 0, 0, 0}},
		{frame: [6]byte{0x03, 0x08, 0x02, 0, 0, 0}},
	}

	for _, test := range tests {
		event, valid := decodeMCUEvent(test.frame)
		if valid != test.valid || event != test.event {
			t.Errorf(
				"decodeMCUEvent(%x) = (%#v, %t), want (%#v, %t)",
				test.frame,
				event,
				valid,
				test.event,
				test.valid,
			)
		}
	}
}
