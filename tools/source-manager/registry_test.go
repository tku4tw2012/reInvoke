// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import "testing"

// These mirror the donor's own MusicSourceStart.pm cases rather than this
// implementation's shape, so the behaviour under test is Harman's contract.

// test1: nothing is active before any source starts.
func TestNoActiveSourceInitially(t *testing.T) {
	if got := newRegistry().Active(); got != "" {
		t.Errorf("initial active = %q, want empty", got)
	}
}

// test2: an unregistered source cannot become active.
func TestStartRefusesUnregisteredSource(t *testing.T) {
	r := newRegistry()
	if _, err := r.Start("a.b.c"); err == nil {
		t.Error("expected starting an unregistered source to fail")
	}
	if got := r.Active(); got != "" {
		t.Errorf("active = %q after a refused start, want empty", got)
	}
}

// test3: a registered source becomes active.
func TestRegisteredSourceBecomesActive(t *testing.T) {
	r := newRegistry()
	if err := r.Register("a.b.c"); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, err := r.Start("a.b.c"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got := r.Active(); got != "a.b.c" {
		t.Errorf("active = %q, want a.b.c", got)
	}
}

// test4: starting a second source displaces the first, and the caller is told
// which one so it can be stopped. Without that the displaced source keeps
// rendering into the same DAC.
func TestStartingASecondSourceDisplacesTheFirst(t *testing.T) {
	r := newRegistry()
	for _, uri := range []string{"a.b.c", "d.f.e"} {
		if err := r.Register(uri); err != nil {
			t.Fatalf("Register(%s): %v", uri, err)
		}
	}
	if _, err := r.Start("a.b.c"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	displaced, err := r.Start("d.f.e")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if displaced != "a.b.c" {
		t.Errorf("displaced = %q, want a.b.c", displaced)
	}
	if got := r.Active(); got != "d.f.e" {
		t.Errorf("active = %q, want d.f.e", got)
	}
}

// Restarting the active source must not report a displacement, or the service
// would stop the very source it just started.
func TestRestartingActiveSourceDisplacesNothing(t *testing.T) {
	r := newRegistry()
	if err := r.Register("a.b.c"); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, err := r.Start("a.b.c"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	displaced, err := r.Start("a.b.c")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if displaced != "" {
		t.Errorf("displaced = %q, want empty", displaced)
	}
}

// get-registered answers in registration order and survives duplicates.
func TestRegisteredListing(t *testing.T) {
	r := newRegistry()
	for _, uri := range []string{"one", "two", "one"} {
		if err := r.Register(uri); err != nil {
			t.Fatalf("Register(%s): %v", uri, err)
		}
	}
	got := r.Registered()
	if len(got) != 2 || got[0] != "one" || got[1] != "two" {
		t.Errorf("registered = %v, want [one two]", got)
	}
}

func TestFlushClearsEverything(t *testing.T) {
	r := newRegistry()
	if err := r.Register("a.b.c"); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, err := r.Start("a.b.c"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	r.Flush()
	if got := r.Active(); got != "" {
		t.Errorf("active = %q after flush, want empty", got)
	}
	if got := r.Registered(); len(got) != 0 {
		t.Errorf("registered = %v after flush, want empty", got)
	}
}

// TestPerSourceVolumeIsKeptApart proves one source's level does not become
// another's. The donor kept these separate so switching sources and back
// returned to the level that source was last set to.
func TestPerSourceVolumeIsKeptApart(t *testing.T) {
	r := newRegistry()
	for _, uri := range []string{"com.harman.bluetooth", "com.harman.linein"} {
		if err := r.Register(uri); err != nil {
			t.Fatalf("register %s: %v", uri, err)
		}
	}
	if err := r.SetVolume("com.harman.bluetooth", 70); err != nil {
		t.Fatalf("set bluetooth volume: %v", err)
	}
	if err := r.SetVolume("com.harman.linein", 20); err != nil {
		t.Fatalf("set linein volume: %v", err)
	}
	if level, known := r.Volume("com.harman.bluetooth"); !known || level != 70 {
		t.Fatalf("bluetooth volume is %d (known=%v), expected 70", level, known)
	}
	if level, known := r.Volume("com.harman.linein"); !known || level != 20 {
		t.Fatalf("linein volume is %d (known=%v), expected 20", level, known)
	}
	if _, known := r.Volume("com.harman.never"); known {
		t.Fatal("an unset source reported a volume")
	}
}

// TestSetVolumeRefusesUnknownSourceAndRange proves the registry does not accept
// a level for a source that never registered, or one outside the scale.
func TestSetVolumeRefusesUnknownSourceAndRange(t *testing.T) {
	r := newRegistry()
	if err := r.SetVolume("com.harman.bluetooth", 50); err == nil {
		t.Fatal("accepted a volume for an unregistered source")
	}
	if err := r.Register("com.harman.bluetooth"); err != nil {
		t.Fatalf("register: %v", err)
	}
	for _, level := range []int{-1, 101} {
		if err := r.SetVolume("com.harman.bluetooth", level); err == nil {
			t.Fatalf("accepted out-of-range volume %d", level)
		}
	}
	if err := r.SetVolume("", 50); err == nil {
		t.Fatal("accepted an empty source")
	}
}

// TestTrackPositionAcceptsDonorShapes proves the relay reads the payloads the
// donor actually sent, including the form with a leading source URI, and
// rejects the ones it never sent.
func TestTrackPositionAcceptsDonorShapes(t *testing.T) {
	position, duration, err := trackPosition([]interface{}{int64(30), int64(210)})
	if err != nil || position != 30 || duration != 210 {
		t.Fatalf("bare pair gave %d,%d,%v", position, duration, err)
	}
	position, duration, err = trackPosition(
		[]interface{}{"com.harman.bluetooth", int64(5), int64(9)})
	if err != nil || position != 5 || duration != 9 {
		t.Fatalf("leading URI gave %d,%d,%v", position, duration, err)
	}
	if _, _, err := trackPosition([]interface{}{int64(7)}); err != nil {
		t.Fatalf("position without duration was rejected: %v", err)
	}
	for _, args := range [][]interface{}{
		{},
		{int64(-1)},
		{"com.harman.bluetooth"},
		{int64(1), int64(-2)},
		{int64(1), int64(2), int64(3), int64(4)},
	} {
		if _, _, err := trackPosition(args); err == nil {
			t.Fatalf("trackPosition accepted %v", args)
		}
	}
}

// TestSourceVolumeArguments proves the volumeSet payload reader accepts both
// the addressed and the implicit form and refuses anything else.
func TestSourceVolumeArguments(t *testing.T) {
	uri, level, err := sourceVolume([]interface{}{"com.harman.bluetooth", int64(40)})
	if err != nil || uri != "com.harman.bluetooth" || level != 40 {
		t.Fatalf("addressed form gave %q,%d,%v", uri, level, err)
	}
	uri, level, err = sourceVolume([]interface{}{int64(40)})
	if err != nil || uri != "" || level != 40 {
		t.Fatalf("implicit form gave %q,%d,%v", uri, level, err)
	}
	for _, args := range [][]interface{}{
		{},
		{int64(101)},
		{int64(-1)},
		{"com.harman.bluetooth", "loud"},
		{int64(1), int64(2), int64(3)},
	} {
		if _, _, err := sourceVolume(args); err == nil {
			t.Fatalf("sourceVolume accepted %v", args)
		}
	}
}

// TestShutdownIsIdempotent proves a repeated shutdown request does not panic on
// a closed channel. podium.conf stopped services by name and could repeat.
func TestShutdownIsIdempotent(t *testing.T) {
	s := &service{sources: newRegistry(), shutdown: make(chan struct{}), logf: func(string, ...interface{}) {}}
	s.requestShutdown()
	s.requestShutdown()
	select {
	case <-s.shutdown:
	default:
		t.Fatal("shutdown was not signalled")
	}
}
