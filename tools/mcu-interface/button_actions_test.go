// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestVoiceButtonRouting(t *testing.T) {
	running := func(string) ([]byte, error) { return []byte("state: RUNNING\n"), nil }
	closed := func(string) ([]byte, error) { return []byte("closed\n"), nil }
	for _, test := range []struct {
		name   string
		active bool
		read   func(string) ([]byte, error)
		want   string
	}{
		{"action", true, running, actionVoiceCancel},
		{"action", true, closed, actionVoiceCancel},
		{"action", false, running, actionMusicPause},
		{"action", false, closed, actionVoiceCancel},
		{"action-long", true, running, actionVoiceTrigger},
		{"action-long", false, running, actionVoiceTrigger},
		{"bluetooth", true, running, actionBluetoothPair},
		{"volumeup", true, running, actionVolumeUp},
		{"unrelated", true, running, ""},
	} {
		if got := resolveButtonAction(inputEvent{Name: test.name}, "status", test.read, test.active); got != test.want {
			t.Fatalf("%s active=%t = %q, want %q", test.name, test.active, got, test.want)
		}
	}
	if got := resolveButtonAction(inputEvent{Name: "action"}, "status",
		func(string) ([]byte, error) { return nil, errors.New("missing") }, false); got != actionVoiceCancel {
		t.Fatal("unknown playback caused an unjustified music pause")
	}
}

func TestLocalPauseYieldsToVoice(t *testing.T) {
	active := true
	calls := 0
	controller := &mediaActionController{
		playbackStatus: "status",
		readFile:       func(string) ([]byte, error) { return []byte("state: RUNNING\n"), nil },
		voiceActive:    func() bool { return active },
		requests:       make(chan struct{}, 1),
		call: func(_ context.Context, procedure string) error {
			calls++
			if procedure != "com.harman.music.pause" {
				t.Fatal("local action changed the existing pause owner")
			}
			return nil
		},
	}
	ctx := context.Background()
	if err := controller.Apply(ctx, inputEvent{Name: "action"}); err != nil {
		t.Fatal(err)
	}
	if len(controller.requests) != 0 {
		t.Fatal("voice-active top tap queued a music pause")
	}
	active = false
	if err := controller.Apply(ctx, inputEvent{Name: "action", Action: actionVoiceCancel}); err != nil {
		t.Fatal(err)
	}
	if len(controller.requests) != 0 {
		t.Fatal("local action re-resolved an already captured voice cancellation")
	}
	if err := controller.Apply(ctx, inputEvent{Name: "action"}); err != nil || len(controller.requests) != 1 {
		t.Fatal("normal idle music pause changed")
	}
	<-controller.requests
	active = true
	if err := controller.pause(ctx); err != nil || calls != 0 {
		t.Fatal("queued music pause ran after voice became active")
	}
	active = false
	if err := controller.pause(ctx); err != nil || calls != 1 {
		t.Fatal("idle music no longer uses the normal pause procedure")
	}
	if err := controller.Apply(ctx, inputEvent{Name: "action-long"}); err != nil || len(controller.requests) != 0 {
		t.Fatal("long press also queued a local music action")
	}
}

func TestRelaySharesOneResolvedVoiceAction(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	input := make(chan inputEvent, 3)
	publications := make(chan inputEvent, 3)
	applied := make(chan inputEvent, 3)
	done := make(chan struct{})
	resolutions := 0
	go func() {
		defer close(done)
		runEventRelay(ctx, input, recordingInputController{events: applied}, publications, nil,
			func(event inputEvent) string {
				resolutions++
				return resolveButtonAction(event, "", nil, true)
			})
	}()
	first := time.Now()
	input <- inputEvent{Name: "action-long", OccurredAt: first}
	select {
	case event := <-applied:
		if event.Action != actionVoiceTrigger {
			t.Fatal("local worker did not receive the resolved action")
		}
	case <-time.After(time.Second):
		t.Fatal("local worker did not run")
	}
	input <- inputEvent{Name: "action-long", OccurredAt: first.Add(time.Millisecond)}
	input <- inputEvent{Name: "action", OccurredAt: first.Add(time.Second)}
	for _, want := range []string{actionVoiceTrigger, actionVoiceCancel} {
		select {
		case event := <-publications:
			if event.Action != want {
				t.Fatalf("publication changed captured decision: %q", event.Action)
			}
		case <-time.After(time.Second):
			t.Fatal("resolved action was not published")
		}
	}
	cancel()
	<-done
	if resolutions != 2 || len(publications) != 0 {
		t.Fatal("raw/repeated long-press frame was dispatched twice")
	}
}
