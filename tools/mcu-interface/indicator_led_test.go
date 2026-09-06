// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"
)

type recordingIndicatorLEDWriter struct {
	mu     sync.Mutex
	frames [][6]byte
	errs   []error
}

func (writer *recordingIndicatorLEDWriter) WriteMCUCommand(
	frame [6]byte,
) error {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	writer.frames = append(writer.frames, frame)
	if len(writer.errs) == 0 {
		return nil
	}
	err := writer.errs[0]
	writer.errs = writer.errs[1:]
	return err
}

func (writer *recordingIndicatorLEDWriter) recorded() [][6]byte {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	return append([][6]byte(nil), writer.frames...)
}

func TestIndicatorLEDModes(t *testing.T) {
	for _, test := range []struct {
		mode string
		want byte
	}{
		{mode: "off", want: 0},
		{mode: "on", want: 1},
		{mode: "dim", want: 2},
		{mode: "slow-blink", want: 3},
		{mode: "fast-blink", want: 4},
		{mode: "", want: 0},
		{mode: "unknown", want: 0},
	} {
		t.Run(test.mode, func(t *testing.T) {
			writer := &recordingIndicatorLEDWriter{}
			controller := newIndicatorLEDController(writer)
			if err := controller.Set("back", test.mode, "ignored"); err != nil {
				t.Fatal(err)
			}
			want := [6]byte{indicatorLEDCode, 0, 0, test.want, 0, 0}
			if frames := writer.recorded(); !reflect.DeepEqual(
				frames,
				[][6]byte{want},
			) {
				t.Fatalf("frames = %x, want %x", frames, want)
			}
		})
	}
}

func TestIndicatorLEDFrontColorsAreMutuallyExclusive(t *testing.T) {
	writer := &recordingIndicatorLEDWriter{}
	controller := newIndicatorLEDController(writer)
	if err := controller.Set("front", "slow-blink", "amber"); err != nil {
		t.Fatal(err)
	}
	if err := controller.Set("front", "fast-blink", "white"); err != nil {
		t.Fatal(err)
	}
	want := [][6]byte{
		{indicatorLEDCode, 3, 0, 0, 0, 0},
		{indicatorLEDCode, 0, 4, 0, 0, 0},
	}
	if frames := writer.recorded(); !reflect.DeepEqual(frames, want) {
		t.Fatalf("frames = %x, want %x", frames, want)
	}
}

func TestIndicatorLEDBackIgnoresColor(t *testing.T) {
	writer := &recordingIndicatorLEDWriter{}
	controller := newIndicatorLEDController(writer)
	if err := controller.Set("back", "dim", "not-a-color"); err != nil {
		t.Fatal(err)
	}
	want := [6]byte{indicatorLEDCode, 0, 0, 2, 0, 0}
	if frames := writer.recorded(); !reflect.DeepEqual(
		frames,
		[][6]byte{want},
	) {
		t.Fatalf("frames = %x, want %x", frames, want)
	}
}

func TestIndicatorLEDUnknownTargetAndFrontColorSendUnchanged(t *testing.T) {
	writer := &recordingIndicatorLEDWriter{}
	controller := newIndicatorLEDController(writer)
	if err := controller.Set("front", "on", "white"); err != nil {
		t.Fatal(err)
	}
	if err := controller.Set("unknown", "fast-blink", "amber"); err != nil {
		t.Fatal(err)
	}
	if err := controller.Set("front", "fast-blink", "green"); err != nil {
		t.Fatal(err)
	}
	want := [][6]byte{
		{indicatorLEDCode, 0, 1, 0, 0, 0},
		{indicatorLEDCode, 0, 1, 0, 0, 0},
		{indicatorLEDCode, 0, 1, 0, 0, 0},
	}
	if frames := writer.recorded(); !reflect.DeepEqual(frames, want) {
		t.Fatalf("frames = %x, want %x", frames, want)
	}
}

func TestIndicatorLEDWriteFailureRollsBackState(t *testing.T) {
	writer := &recordingIndicatorLEDWriter{
		errs: []error{errors.New("injected failure"), nil},
	}
	controller := newIndicatorLEDController(writer)
	if err := controller.Set("front", "slow-blink", "white"); err == nil {
		t.Fatal("failed write succeeded")
	}
	if err := controller.Set("back", "on", "ignored"); err != nil {
		t.Fatal(err)
	}
	want := [][6]byte{
		{indicatorLEDCode, 0, 3, 0, 0, 0},
		{indicatorLEDCode, 0, 0, 1, 0, 0},
	}
	if frames := writer.recorded(); !reflect.DeepEqual(frames, want) {
		t.Fatalf("frames = %x, want %x", frames, want)
	}
}

func TestIndicatorLEDBackDeduplicatesConfirmedState(t *testing.T) {
	writer := &recordingIndicatorLEDWriter{}
	controller := newIndicatorLEDController(writer)
	if err := controller.SetBackIfChanged("off"); err != nil {
		t.Fatal(err)
	}
	if err := controller.SetBackIfChanged("off"); err != nil {
		t.Fatal(err)
	}
	if frames := writer.recorded(); !reflect.DeepEqual(
		frames,
		[][6]byte{{indicatorLEDCode, 0, 0, 0, 0, 0}},
	) {
		t.Fatalf("frames = %x", frames)
	}
}

func TestIndicatorLEDBackRetriesFailedState(t *testing.T) {
	writer := &recordingIndicatorLEDWriter{
		errs: []error{errors.New("injected failure"), nil},
	}
	controller := newIndicatorLEDController(writer)
	if err := controller.SetBackIfChanged("on"); err == nil {
		t.Fatal("failed write succeeded")
	}
	if err := controller.SetBackIfChanged("on"); err != nil {
		t.Fatal(err)
	}
	want := [6]byte{indicatorLEDCode, 0, 0, 1, 0, 0}
	if frames := writer.recorded(); !reflect.DeepEqual(
		frames,
		[][6]byte{want, want},
	) {
		t.Fatalf("frames = %x", frames)
	}
}

func TestIndicatorLEDBackWatcherReassertsAfterWAMPSet(t *testing.T) {
	writer := &recordingIndicatorLEDWriter{}
	controller := newIndicatorLEDController(writer)
	if err := controller.SetBackIfChanged("on"); err != nil {
		t.Fatal(err)
	}
	if err := controller.Set("back", "off", "ignored"); err != nil {
		t.Fatal(err)
	}
	if err := controller.SetBackIfChanged("on"); err != nil {
		t.Fatal(err)
	}
	want := [][6]byte{
		{indicatorLEDCode, 0, 0, 1, 0, 0},
		{indicatorLEDCode, 0, 0, 0, 0, 0},
		{indicatorLEDCode, 0, 0, 1, 0, 0},
	}
	if frames := writer.recorded(); !reflect.DeepEqual(frames, want) {
		t.Fatalf("frames = %x, want %x", frames, want)
	}
}

type blockingIndicatorLEDWriter struct {
	entered chan [6]byte
	release chan error
}

func (writer *blockingIndicatorLEDWriter) WriteMCUCommand(
	frame [6]byte,
) error {
	writer.entered <- frame
	return <-writer.release
}

func TestIndicatorLEDCallsSerializeStateAndWrites(t *testing.T) {
	writer := &blockingIndicatorLEDWriter{
		entered: make(chan [6]byte),
		release: make(chan error),
	}
	controller := newIndicatorLEDController(writer)
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- controller.Set("front", "slow-blink", "white")
	}()
	if frame := <-writer.entered; frame != ([6]byte{
		indicatorLEDCode, 0, 3, 0, 0, 0,
	}) {
		t.Fatalf("first frame = %x", frame)
	}

	secondStarted := make(chan struct{})
	secondDone := make(chan error, 1)
	go func() {
		close(secondStarted)
		secondDone <- controller.SetBackIfChanged("fast-blink")
	}()
	<-secondStarted
	select {
	case frame := <-writer.entered:
		t.Fatalf("second write entered before first completed: %x", frame)
	case <-time.After(50 * time.Millisecond):
	}

	writer.release <- nil
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	if frame := <-writer.entered; frame != ([6]byte{
		indicatorLEDCode, 0, 3, 4, 0, 0,
	}) {
		t.Fatalf("second frame = %x", frame)
	}
	writer.release <- nil
	if err := <-secondDone; err != nil {
		t.Fatal(err)
	}
}
