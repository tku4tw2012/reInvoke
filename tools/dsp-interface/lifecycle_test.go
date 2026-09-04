// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunWithReconnectRetriesUntilCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var attempts int32
	done := make(chan error, 1)

	go func() {
		done <- runWithReconnect(
			ctx,
			time.Millisecond,
			func(context.Context) error {
				attempt := atomic.AddInt32(&attempts, 1)
				if attempt < 3 {
					return errors.New("router unavailable")
				}
				cancel()
				return nil
			},
			nil,
			nil,
		)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("reconnect loop did not stop")
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
}

func TestRunWithReconnectRejectsInvalidDelay(t *testing.T) {
	err := runWithReconnect(
		context.Background(),
		0,
		func(context.Context) error { return nil },
		nil,
		nil,
	)
	if err == nil {
		t.Fatal("invalid reconnect delay was accepted")
	}
}

func TestRunWithReconnectReturnsNonRetryableError(t *testing.T) {
	want := errors.New("DSP link failed")
	err := runWithReconnect(
		context.Background(),
		time.Millisecond,
		func(context.Context) error { return want },
		func(error) bool { return false },
		nil,
	)
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
}
