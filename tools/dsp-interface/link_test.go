// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"os"
	"testing"
	"time"
)

type orderedGPIO struct {
	*memoryGPIO
	operations *[]string
}

func (lines *orderedGPIO) Read(pin int) (bool, error) {
	*lines.operations = append(*lines.operations, "gpio-read")
	return lines.memoryGPIO.Read(pin)
}

type orderedI2C struct {
	*memoryI2C
	operations *[]string
}

func (bus *orderedI2C) ReadRegister(address, register byte) (byte, error) {
	*bus.operations = append(*bus.operations, "i2c-read")
	return bus.memoryI2C.ReadRegister(address, register)
}

func (bus *orderedI2C) UpdateRegister(
	address,
	register byte,
	update func(byte) byte,
) error {
	*bus.operations = append(*bus.operations, "i2c-update")
	return bus.memoryI2C.UpdateRegister(address, register, update)
}

func TestBootPreparesGPIOBeforeReleasingDSPReset(t *testing.T) {
	var operations []string
	gpio := &orderedGPIO{
		memoryGPIO: newMemoryGPIO(),
		operations: &operations,
	}
	i2c := &orderedI2C{
		memoryI2C:  newMemoryI2C(),
		operations: &operations,
	}
	link := newLink(newMemorySPI(), gpio, i2c, linkOptions{
		Pins:  defaultPinout(),
		Sleep: func(time.Duration) {},
	})
	if err := link.Boot(bootImage{Stream: []byte{0, 0, 0, 0}}); err != nil {
		t.Fatal(err)
	}
	firstI2C := -1
	gpioReads := 0
	for index, operation := range operations {
		if operation == "gpio-read" {
			gpioReads++
		}
		if operation == "i2c-update" && firstI2C == -1 {
			firstI2C = index
		}
	}
	if firstI2C < 3 || gpioReads < 3 {
		t.Fatalf("GPIO was not sampled before reset: %v", operations)
	}
}

func TestBootStreamsRecoveredImageExactly(t *testing.T) {
	path := os.Getenv("REINVOKE_DSP_IMAGE")
	if path == "" {
		t.Skip("REINVOKE_DSP_IMAGE is not set")
	}
	image, err := loadBootImage(path)
	if err != nil {
		t.Fatal(err)
	}
	spi := newMemorySPI()
	gpio := newMemoryGPIO()
	link := newLink(spi, gpio, newMemoryI2C(), linkOptions{
		Pins:  defaultPinout(),
		Sleep: func(time.Duration) {},
	})
	if err := link.Boot(image); err != nil {
		t.Fatal(err)
	}
	transfers, transferred, chunked, serial := spi.Counters()
	if transfers != recordedImageTransfers ||
		transferred != recordedImageBytes ||
		chunked != recordedImageTransfers ||
		serial != 0 {
		t.Fatalf(
			"unexpected transfer counts: %d %d %d %d",
			transfers,
			transferred,
			chunked,
			serial,
		)
	}
	if got := spi.StreamSHA256(); got != recordedStreamSHA256 {
		t.Fatalf("wire SHA-256 = %s, want %s", got, recordedStreamSHA256)
	}
	operations := gpio.Operations()
	wantPrefix := []string{
		"export 4",
		"export 13",
		"export 12",
		"export 15",
		"out 4",
		"in 13",
		"in 12",
		"in 15",
		"write 4=1",
		"read 13",
		"read 12",
		"read 15",
	}
	if len(operations) < len(wantPrefix) {
		t.Fatalf("GPIO operations too short: %v", operations)
	}
	for index, want := range wantPrefix {
		if operations[index] != want {
			t.Fatalf(
				"GPIO operation %d = %q, want %q",
				index,
				operations[index],
				want,
			)
		}
	}
}

func TestPollDecodesCapturedBootEvent(t *testing.T) {
	spi := newMemorySPI()
	spi.KeepTransfers = true
	spi.Queue([]byte{0x00, 0x01, 0x00, 0x01, 0x06, 0x04, 0x00, 0x00})
	gpio := newMemoryGPIO()
	gpio.OnRead = func(pin int, current bool) bool {
		if pin == defaultPinout().Ready {
			return false
		}
		return current
	}
	link := newLink(spi, gpio, newMemoryI2C(), linkOptions{
		Pins:  defaultPinout(),
		Sleep: func(time.Duration) {},
	})
	if err := link.prepareHandshake(); err != nil {
		t.Fatal(err)
	}
	link.booted = true

	event, worked, err := link.Poll()
	if err != nil {
		t.Fatal(err)
	}
	if !worked || event == nil ||
		event.ID != messageIDBoot ||
		!bytes.Equal(event.Payload, []byte{0x04}) {
		t.Fatalf("unexpected poll result: worked=%t event=%#v", worked, event)
	}
	for _, transfer := range spi.Recorded {
		if len(transfer.TX) != 1 || transfer.TX[0] != 0 {
			t.Fatalf("receive transfer did not transmit zero: %x", transfer.TX)
		}
	}
}

func TestPollResynchronizesObservedLeadingZero(t *testing.T) {
	spi := newMemorySPI()
	spi.Queue([]byte{0x00, 0x00, 0x01, 0x00, 0x01, 0x06, 0x04, 0x00, 0x00})
	gpio := newMemoryGPIO()
	gpio.OnRead = func(pin int, current bool) bool {
		if pin == defaultPinout().Ready {
			return false
		}
		return current
	}
	link := newLink(spi, gpio, newMemoryI2C(), linkOptions{
		Pins:  defaultPinout(),
		Sleep: func(time.Duration) {},
	})
	if err := link.prepareHandshake(); err != nil {
		t.Fatal(err)
	}
	link.booted = true

	event, worked, err := link.Poll()
	if err != nil {
		t.Fatal(err)
	}
	if !worked || event == nil ||
		event.ID != messageIDBoot ||
		!bytes.Equal(event.Payload, []byte{0x04}) {
		t.Fatalf("unexpected poll result: worked=%t event=%#v", worked, event)
	}
	if got := link.Stats().FrameResyncs; got != 1 {
		t.Fatalf("frame resyncs = %d, want 1", got)
	}
}

func TestTransmitUsesRecoveredMicrosecondHandshake(t *testing.T) {
	spi := newMemorySPI()
	spi.KeepTransfers = true
	spi.Queue([]byte{0x00, 0x01, 0x00, 0x01, 0x06, 0x04, 0x00, 0x00})
	gpio := newMemoryGPIO()
	gpio.OnRead = func(pin int, current bool) bool {
		return false
	}

	var sleeps []time.Duration
	link := newLink(spi, gpio, newMemoryI2C(), linkOptions{
		Pins: defaultPinout(),
		Sleep: func(delay time.Duration) {
			sleeps = append(sleeps, delay)
		},
	})
	link.booted = true
	if err := link.Enqueue(messageIDControl, []byte{0x08}); err != nil {
		t.Fatal(err)
	}

	event, worked, err := link.Poll()
	if err != nil {
		t.Fatal(err)
	}
	if !worked || event == nil || event.ID != messageIDBoot {
		t.Fatalf("unexpected transmit result: worked=%t event=%#v", worked, event)
	}
	want := []time.Duration{
		time.Microsecond,
		time.Microsecond,
		time.Microsecond,
		time.Microsecond,
		2 * time.Microsecond,
	}
	if len(sleeps) != len(want) {
		t.Fatalf("sleep sequence = %v, want %v", sleeps, want)
	}
	for index := range want {
		if sleeps[index] != want[index] {
			t.Fatalf("sleep sequence = %v, want %v", sleeps, want)
		}
	}

	commandLength := frameLength(1)
	responseLength := frameLength(1)
	if len(spi.Recorded) != commandLength+responseLength {
		t.Fatalf("transfer count = %d", len(spi.Recorded))
	}
	for index, transfer := range spi.Recorded {
		hasTransmit := len(transfer.TX) != 0
		if index < commandLength && !hasTransmit {
			t.Fatalf("command transfer %d was receive-only", index)
		}
		if index >= commandLength &&
			(!hasTransmit || len(transfer.TX) != 1 || transfer.TX[0] != 0) {
			t.Fatalf("response transfer %d transmitted %x", index, transfer.TX)
		}
	}
}

func TestTransmitRetriesRejectedResponse(t *testing.T) {
	spi := newMemorySPI()
	spi.Queue([]byte{
		0xff, 0, 0, 0, 0, 0, 0, 0, 0,
		0x00, 0x01, 0x00, 0x01, 0x06, 0x04, 0x00, 0x00,
	})
	gpio := newMemoryGPIO()
	gpio.OnRead = func(pin int, current bool) bool { return false }
	var retryDelayCount int
	link := newLink(spi, gpio, newMemoryI2C(), linkOptions{
		Pins: defaultPinout(),
		Sleep: func(delay time.Duration) {
			if delay == receiveRetryDelay {
				retryDelayCount++
			}
		},
	})
	link.booted = true
	if err := link.Enqueue(messageIDControl, []byte{0x08}); err != nil {
		t.Fatal(err)
	}
	event, worked, err := link.Poll()
	if err != nil {
		t.Fatal(err)
	}
	if !worked || event == nil || event.ID != messageIDBoot {
		t.Fatalf("unexpected retry result: worked=%t event=%#v", worked, event)
	}
	if retryDelayCount != 1 {
		t.Fatalf("retry delays = %d, want 1", retryDelayCount)
	}
}
