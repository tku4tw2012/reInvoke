// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"os"
	"testing"
	"time"
)

type recordingMCUEventBus struct {
	frame [6]byte
	reads int
}

func (bus *recordingMCUEventBus) ReadMCUEvent() ([6]byte, error) {
	bus.reads++
	return bus.frame, nil
}

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

func TestDrainReadsLatchedEventAfterInterruptReturnsHigh(t *testing.T) {
	value, err := os.CreateTemp(t.TempDir(), "gpio-value")
	if err != nil {
		t.Fatal(err)
	}
	defer value.Close()
	if _, err := value.WriteString("1"); err != nil {
		t.Fatal(err)
	}

	bus := &recordingMCUEventBus{frame: [6]byte{0x04, 0x04}}
	source := gpioEventSource{value: value, bus: bus}
	events := make(chan inputEvent, 1)
	if err := source.drainPendingEvents(
		context.Background(),
		make([]byte, 8),
		events,
		true,
	); err != nil {
		t.Fatal(err)
	}
	if bus.reads != 1 {
		t.Fatalf("MCU reads = %d, want 1", bus.reads)
	}
	select {
	case event := <-events:
		if event.Name != "micmute" {
			t.Fatalf("event = %#v, want micmute", event)
		}
	default:
		t.Fatal("latched MCU event was not published")
	}
}

func TestDrainDoesNotReadWithoutEdgeWhenInterruptIsHigh(t *testing.T) {
	value, err := os.CreateTemp(t.TempDir(), "gpio-value")
	if err != nil {
		t.Fatal(err)
	}
	defer value.Close()
	if _, err := value.WriteString("1"); err != nil {
		t.Fatal(err)
	}

	bus := &recordingMCUEventBus{frame: [6]byte{0x04, 0x04}}
	source := gpioEventSource{value: value, bus: bus}
	if err := source.drainPendingEvents(
		context.Background(),
		make([]byte, 8),
		nil,
		false,
	); err != nil {
		t.Fatal(err)
	}
	if bus.reads != 0 {
		t.Fatalf("MCU reads = %d, want 0", bus.reads)
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

// A wedged MCU never releases its interrupt. Suppression after a drain limit
// must therefore expire, or physical input stays dead for the whole boot.
func TestRecoverySuppressionExpires(t *testing.T) {
	base := time.Unix(1700000000, 0)
	current := base
	logged := 0
	source := &gpioEventSource{
		logError: func(error) { logged++ },
		now:      func() time.Time { return current },
	}

	deadline := source.suppressRecovery()
	if logged != 1 {
		t.Fatalf("suppression must report degraded input, logged = %d", logged)
	}
	if !deadline.After(current) {
		t.Fatal("suppression deadline must be in the future")
	}
	if current.Before(deadline) != true {
		t.Fatal("recovery must be suppressed immediately after the limit")
	}

	current = base.Add(mcuRecoveryRetryInterval - time.Second)
	if !current.Before(deadline) {
		t.Fatal("recovery must stay suppressed before the retry interval")
	}

	current = base.Add(mcuRecoveryRetryInterval)
	if current.Before(deadline) {
		t.Fatal("recovery must resume once the retry interval has elapsed")
	}
}

func TestCurrentTimeFallsBackToWallClock(t *testing.T) {
	source := &gpioEventSource{}
	if source.currentTime().IsZero() {
		t.Fatal("current time must fall back to the wall clock")
	}
}
