// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

type recordingPublisher struct {
	topics chan string
	fail   error
}

func (r *recordingPublisher) publish(topic string, _ []interface{}) error {
	if r.fail != nil {
		return r.fail
	}
	select {
	case r.topics <- topic:
	default:
	}
	return nil
}

func TestReadyUsesTheDonorTopic(t *testing.T) {
	p := &recordingPublisher{topics: make(chan string, 1)}
	if err := announceReady(p, "mcu-interface"); err != nil {
		t.Fatalf("announceReady: %v", err)
	}
	if got := <-p.topics; got != "com.harman.ready.mcu-interface" {
		t.Errorf("topic = %q", got)
	}
}

func TestHeartbeatRepeatsUntilCancelled(t *testing.T) {
	p := &recordingPublisher{topics: make(chan string, 4)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go runHeartbeat(ctx, p, "audio-ui", 10*time.Millisecond)
	for i := 0; i < 2; i++ {
		select {
		case got := <-p.topics:
			if got != "com.harman.heartbeat.audio-ui" {
				t.Fatalf("topic = %q", got)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("heartbeat did not repeat")
		}
	}
}

// A failed heartbeat must surface: it is the signal a watchdog acts on, and
// swallowing it would recreate the "looks up but is not" failure exactly.
func TestHeartbeatReportsFailure(t *testing.T) {
	p := &recordingPublisher{topics: make(chan string, 1), fail: errors.New("router gone")}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- runHeartbeat(ctx, p, "x", 10*time.Millisecond) }()
	select {
	case err := <-done:
		if err == nil {
			t.Error("expected the publish failure to be reported")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("heartbeat swallowed the failure")
	}
}
