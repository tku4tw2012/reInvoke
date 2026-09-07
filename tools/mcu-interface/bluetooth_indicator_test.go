// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

type recordingBluetoothIndicator struct {
	mu    sync.Mutex
	modes []string
	errs  []error
}

func (indicator *recordingBluetoothIndicator) SetBackIfChanged(
	mode string,
) error {
	indicator.mu.Lock()
	defer indicator.mu.Unlock()
	indicator.modes = append(indicator.modes, mode)
	if len(indicator.errs) == 0 {
		return nil
	}
	err := indicator.errs[0]
	indicator.errs = indicator.errs[1:]
	return err
}

func (indicator *recordingBluetoothIndicator) recorded() []string {
	indicator.mu.Lock()
	defer indicator.mu.Unlock()
	return append([]string(nil), indicator.modes...)
}

func TestDecodeBluetoothIndicatorState(t *testing.T) {
	for _, test := range []struct {
		content string
		readErr error
		mode    string
		wantErr bool
	}{
		{content: "pairing\n", mode: "slow-blink"},
		{content: "connected ", mode: "on"},
		{content: "off", mode: "off"},
		{content: "unknown", mode: "off", wantErr: true},
		{content: " connected", mode: "off", wantErr: true},
		{readErr: errors.New("missing"), mode: "off", wantErr: true},
	} {
		mode, _, err := decodeBluetoothIndicatorState(
			[]byte(test.content),
			test.readErr,
		)
		if mode != test.mode || (err != nil) != test.wantErr {
			t.Errorf(
				"decode(%q, %v) = (%q, %v), want (%q, error=%t)",
				test.content,
				test.readErr,
				mode,
				err,
				test.mode,
				test.wantErr,
			)
		}
	}
}

func TestReadBoundedBluetoothState(t *testing.T) {
	path := t.TempDir() + "/state"
	if err := os.WriteFile(
		path,
		[]byte(strings.Repeat("x", bluetoothStateMaximumSize+1)),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := readBoundedBluetoothState(path); err == nil {
		t.Fatal("oversized state was accepted")
	}
}

func TestBluetoothStateWatcherUsesSafeOffWhenProducerDisappears(t *testing.T) {
	indicator := &recordingBluetoothIndicator{}
	reads := 0
	watcher := bluetoothStateWatcher{
		path:      "/state",
		indicator: indicator,
		readFile: func(string) ([]byte, error) {
			reads++
			if reads == 1 {
				return []byte("connected\n"), nil
			}
			return nil, errors.New("producer disappeared")
		},
		logf: func(string, ...interface{}) {},
	}
	watcher.reconcile()
	watcher.reconcile()
	if got := indicator.recorded(); !reflect.DeepEqual(
		got,
		[]string{"on", "off"},
	) {
		t.Fatalf("modes = %v", got)
	}
}

func TestBluetoothStateWatcherRetriesWriteFailure(t *testing.T) {
	indicator := &recordingBluetoothIndicator{
		errs: []error{errors.New("write failed"), nil},
	}
	watcher := bluetoothStateWatcher{
		path:      "/state",
		indicator: indicator,
		readFile: func(string) ([]byte, error) {
			return []byte("pairing\n"), nil
		},
		logf: func(string, ...interface{}) {},
	}
	watcher.reconcile()
	watcher.reconcile()
	if got := indicator.recorded(); !reflect.DeepEqual(
		got,
		[]string{"slow-blink", "slow-blink"},
	) {
		t.Fatalf("modes = %v", got)
	}
}

func TestBluetoothStateWatcherDeduplicatesConfirmedWrites(t *testing.T) {
	writer := &recordingIndicatorLEDWriter{}
	controller := newIndicatorLEDController(writer)
	watcher := bluetoothStateWatcher{
		path:      "/state",
		indicator: controller,
		readFile: func(string) ([]byte, error) {
			return []byte("connected\n"), nil
		},
		logf: func(string, ...interface{}) {},
	}
	watcher.reconcile()
	watcher.reconcile()
	if frames := writer.recorded(); !reflect.DeepEqual(
		frames,
		[][6]byte{{indicatorLEDCode, 0, 0, 1, 0, 0}},
	) {
		t.Fatalf("frames = %x", frames)
	}
}

func TestFinalizeBluetoothIndicatorWaitsForWatcherThenClears(t *testing.T) {
	indicator := &recordingBluetoothIndicator{}
	watcherDone := make(chan error)
	finalized := make(chan struct{})
	var watcherErr error
	var clearErr error
	go func() {
		watcherErr, clearErr = finalizeBluetoothIndicator(
			watcherDone,
			indicator,
		)
		close(finalized)
	}()

	select {
	case <-finalized:
		t.Fatal("final clear ran before watcher stopped")
	case <-time.After(20 * time.Millisecond):
	}
	watcherDone <- nil
	<-finalized
	if watcherErr != nil || clearErr != nil {
		t.Fatalf("finalize errors = (%v, %v)", watcherErr, clearErr)
	}
	if got := indicator.recorded(); !reflect.DeepEqual(got, []string{"off"}) {
		t.Fatalf("modes = %v", got)
	}
}

func TestFinalizeBluetoothIndicatorOverridesLateWAMPState(t *testing.T) {
	writer := &recordingIndicatorLEDWriter{}
	controller := newIndicatorLEDController(writer)
	if err := controller.SetBackIfChanged("off"); err != nil {
		t.Fatal(err)
	}
	if err := controller.Set("back", "on", "ignored"); err != nil {
		t.Fatal(err)
	}
	watcherDone := make(chan error, 1)
	watcherDone <- nil
	watcherErr, clearErr := finalizeBluetoothIndicator(
		watcherDone,
		controller,
	)
	if watcherErr != nil || clearErr != nil {
		t.Fatalf("finalize errors = (%v, %v)", watcherErr, clearErr)
	}
	wantLast := [6]byte{indicatorLEDCode, 0, 0, 0, 0, 0}
	frames := writer.recorded()
	if frames[len(frames)-1] != wantLast {
		t.Fatalf("last frame = %x, want %x", frames[len(frames)-1], wantLast)
	}
}

func TestBluetoothStateWatcherClearsOnCancellation(t *testing.T) {
	indicator := &recordingBluetoothIndicator{}
	ctx, cancel := context.WithCancel(context.Background())
	watcher := bluetoothStateWatcher{
		path:      "/state",
		indicator: indicator,
		interval:  time.Millisecond,
		readFile: func(string) ([]byte, error) {
			cancel()
			return []byte("pairing\n"), nil
		},
		logf: func(string, ...interface{}) {},
	}
	if err := watcher.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if got := indicator.recorded(); !reflect.DeepEqual(
		got,
		[]string{"slow-blink", "off"},
	) {
		t.Fatalf("modes = %v", got)
	}
}
