// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import "testing"

func TestGPIOPollRequiresPriorityEdge(t *testing.T) {
	hasEdge, err := gpioPollHasEdge(pollPriority | pollError)
	if err != nil || !hasEdge {
		t.Fatalf("priority edge = %t, error = %v", hasEdge, err)
	}
	hasEdge, err = gpioPollHasEdge(pollError)
	if err == nil || hasEdge {
		t.Fatalf("error-only wakeup = %t, error = %v", hasEdge, err)
	}
	hasEdge, err = gpioPollHasEdge(0)
	if err != nil || hasEdge {
		t.Fatalf("empty wakeup = %t, error = %v", hasEdge, err)
	}
}

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
		{frame: [6]byte{0x04, 0x0b, 0x02, 0, 0, 0}},
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

func TestDecodeRecoveredButtonEvents(t *testing.T) {
	names := []string{
		"action",
		"action-long",
		"bluetooth",
		"bluetooth-long",
		"micmute",
		"micmute-long",
		"reset",
		"reset-long",
	}
	for code, name := range names {
		event, valid := decodeMCUEvent([6]byte{0x04, byte(code)})
		want := inputEvent{Name: name, Topic: "com.harman.vui.keypress"}
		if !valid || event != want {
			t.Fatalf(
				"button code %d = (%#v, %t), want %#v",
				code,
				event,
				valid,
				want,
			)
		}
		topic, args := event.publication()
		if topic != "com.harman.vui.keypress" ||
			len(args) != 1 || args[0] != name {
			t.Fatalf("button publication = %q %#v", topic, args)
		}
	}
}

func TestDisabledCombinedButtonEventIsIgnored(t *testing.T) {
	if event, valid := decodeMCUEvent([6]byte{0x04, 0x0a}); valid {
		t.Fatalf("disabled combined event was decoded as %#v", event)
	}
}
