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
