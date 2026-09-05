// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMicrophoneMuteIsNotRegisteredOnWAMP(t *testing.T) {
	for _, procedure := range procedures {
		if procedure.Opcode == 0x09 {
			t.Fatalf("microphone mute exposed as %q", procedure.Name)
		}
	}
}

func TestSafePolicyAllowsOnlyMicrophoneMute(t *testing.T) {
	if !isSafeMicrophoneMute(micMuteSpec, []interface{}{uint64(1)}) {
		t.Fatal("safe microphone mute was rejected")
	}
	if isSafeMicrophoneMute(micMuteSpec, []interface{}{uint64(0)}) {
		t.Fatal("microphone unmute was accepted as safe")
	}
}

func TestDispatchStopsWhenPumpContextEnds(t *testing.T) {
	dsp := newLink(
		newMemorySPI(),
		newMemoryGPIO(),
		newMemoryI2C(),
		linkOptions{Pins: defaultPinout()},
	)
	dsp.booted = true
	service := &wampService{
		link:           dsp,
		commandTimeout: time.Second,
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- service.dispatch(ctx, procedures[5], nil)
	}()

	deadline := time.Now().Add(time.Second)
	for {
		dsp.mu.Lock()
		queued := len(dsp.queue)
		dsp.mu.Unlock()
		if queued == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("dispatch did not queue DSP command")
		}
		time.Sleep(time.Millisecond)
	}

	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("dispatch error = %v, want context cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("dispatch did not stop after pump cancellation")
	}
}

func TestDispatchWaitsForServiceReady(t *testing.T) {
	dsp := newLink(
		newMemorySPI(),
		newMemoryGPIO(),
		newMemoryI2C(),
		linkOptions{Pins: defaultPinout()},
	)
	dsp.booted = true
	ready := make(chan struct{})
	service := &wampService{
		link:           dsp,
		commandTimeout: time.Second,
		ready:          ready,
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- service.dispatch(ctx, procedures[5], nil)
	}()

	time.Sleep(20 * time.Millisecond)
	dsp.mu.Lock()
	queuedBeforeReady := len(dsp.queue)
	dsp.mu.Unlock()
	if queuedBeforeReady != 0 {
		t.Fatalf("queued %d commands before service readiness", queuedBeforeReady)
	}

	close(ready)
	deadline := time.Now().Add(time.Second)
	for {
		dsp.mu.Lock()
		queued := len(dsp.queue)
		dsp.mu.Unlock()
		if queued == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("dispatch did not queue after service readiness")
		}
		time.Sleep(time.Millisecond)
	}

	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("dispatch error = %v, want context cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("dispatch did not stop after cancellation")
	}
}

func TestDispatchReadyWaitHonorsCancellation(t *testing.T) {
	service := &wampService{ready: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := service.dispatch(ctx, procedures[5], nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("dispatch error = %v, want context cancellation", err)
	}
}

func TestDispatchTimeoutDropsQueuedCommand(t *testing.T) {
	dsp := newLink(
		newMemorySPI(),
		newMemoryGPIO(),
		newMemoryI2C(),
		linkOptions{Pins: defaultPinout()},
	)
	dsp.booted = true
	service := &wampService{
		link:           dsp,
		commandTimeout: 5 * time.Millisecond,
	}
	err := service.dispatch(context.Background(), procedures[5], nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("dispatch error = %v, want deadline exceeded", err)
	}
	received, worked, pollErr := dsp.Poll()
	if pollErr != nil || !worked || received != nil {
		t.Fatalf(
			"drop poll: received=%v worked=%t error=%v",
			received,
			worked,
			pollErr,
		)
	}
	dsp.mu.Lock()
	queued := len(dsp.queue)
	dsp.mu.Unlock()
	if queued != 0 {
		t.Fatalf("expired command remained queued: %d", queued)
	}
}

// The DSP link is the authority for the boot event. Recording readiness must
// not depend on the frame also reaching a live WAMP session, because that
// delivery is best effort and was observed to be lost on service restart.
func TestPumpRecordsBootStateWithoutWAMPDelivery(t *testing.T) {
	spi := newMemorySPI()
	spi.Queue([]byte{0x00, 0x01, 0x00, 0x01, 0x06, 0x04, 0x00, 0x00})
	gpio := newMemoryGPIO()
	gpio.OnRead = func(pin int, current bool) bool {
		if pin == defaultPinout().Ready {
			return false
		}
		return current
	}
	dsp := newLink(spi, gpio, newMemoryI2C(), linkOptions{
		Pins:  defaultPinout(),
		Sleep: func(time.Duration) {},
	})
	if err := dsp.prepareHandshake(); err != nil {
		t.Fatal(err)
	}
	dsp.booted = true

	statePath := filepath.Join(t.TempDir(), "dsp-booted")
	bootEvents := make(chan struct{}, 1)
	service := &wampService{
		link:       dsp,
		policy:     mutePolicy{BootStatePath: statePath},
		bootEvents: bootEvents,
		idle:       time.Millisecond,
		logf:       func(string, ...interface{}) {},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// An unbuffered channel with no reader models the window where no WAMP
	// session is consuming device frames.
	done := make(chan error, 1)
	go func() {
		done <- service.pump(ctx, make(chan frame))
	}()

	select {
	case <-bootEvents:
	case <-time.After(2 * time.Second):
		t.Fatal("pump did not report the DSP boot event")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("pump did not stop")
	}

	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("DSP boot state was not recorded: %v", err)
	}
}

// The DSP client must also survive a router that interleaves traffic with its
// setup replies, because it registers seven procedures and one subscription
// before its session loop starts.
func TestSetupResponseQueuesInterleavedMessages(t *testing.T) {
	clientConnection, routerConnection := net.Pipe()
	defer clientConnection.Close()
	defer routerConnection.Close()
	client := &wampConnection{connection: clientConnection, nextID: 1}
	router := &wampConnection{connection: routerConnection, nextID: 1}

	requestID := client.requestID()
	routerDone := make(chan error, 1)
	go func() {
		for _, message := range [][]interface{}{
			{wampEvent, uint64(400), uint64(3), map[string]interface{}{}},
			{wampSubscribed, requestID, uint64(11)},
		} {
			if err := router.writeFrame(message); err != nil {
				routerDone <- err
				return
			}
		}
		routerDone <- nil
	}()

	var deferred [][]interface{}
	response, err := client.awaitSetupResponse(
		wampSubscribed,
		requestID,
		"subscription",
		&deferred,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-routerDone; err != nil {
		t.Fatal(err)
	}
	subscription, ok := unsigned(response[2])
	if !ok || subscription != 11 {
		t.Fatalf("subscription response = %v, want subscription 11", response)
	}
	if len(deferred) != 1 || messageType(deferred[0]) != wampEvent {
		t.Fatalf("queued the wrong messages: %v", deferred)
	}
}
