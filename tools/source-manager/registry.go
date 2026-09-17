// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"sync"
)

// registry is the donor's music-source-manager contract.
//
// Semantics come from Harman's own test suite rather than from reasoning about
// the names: MusicSourceRegister.pm and MusicSourceStart.pm assert that a
// source must register before it can start, that starting an unregistered one
// fails and leaves the active source unchanged, that get-active answers with a
// positional URI and an empty string when nothing is active, and that starting
// a second source calls "<previous-uri>.stop" on the one it displaces.
//
// This runtime previously answered three of these from a fixed table in the
// identifiers service, with get-active returning a keyword map where the donor
// returns a positional string. One source hid the difference; a second would
// not have.
type registry struct {
	mu         sync.Mutex
	registered map[string]bool
	order      []string
	active     string
}

func newRegistry() *registry {
	return &registry{registered: map[string]bool{}}
}

// Register admits a source. Re-registering an existing source is not an error;
// the donor's tests register the same URI across cases without resetting.
func (r *registry) Register(uri string) error {
	if uri == "" {
		return errors.New("source uri must not be empty")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.registered[uri] {
		r.registered[uri] = true
		r.order = append(r.order, uri)
	}
	return nil
}

// Start makes a registered source active and reports whichever source it
// displaced, so the caller can stop that one. An unregistered source is
// refused and the active source is left alone.
func (r *registry) Start(uri string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.registered[uri] {
		return "", errors.New("source is not registered")
	}
	if r.active == uri {
		return "", nil
	}
	displaced := r.active
	r.active = uri
	return displaced, nil
}

// Active is the source that owns the speaker, or empty.
func (r *registry) Active() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.active
}

// Registered lists every admitted source in registration order.
func (r *registry) Registered() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.order))
	copy(out, r.order)
	return out
}

// Flush clears the registry, including the active source.
func (r *registry) Flush() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.registered = map[string]bool{}
	r.order = nil
	r.active = ""
}
