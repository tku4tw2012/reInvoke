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

func TestRunWithReconnectRetriesWithoutCancelingLifecycle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var attempts int32
	done := make(chan error, 1)

	go func() {
		done <- runWithReconnect(
			ctx,
			time.Millisecond,
			func(ctx context.Context) error {
				attempt := atomic.AddInt32(&attempts, 1)
				if attempt < 3 {
					return errors.New("router unavailable")
				}
				cancel()
				return nil
			},
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
	)
	if err == nil {
		t.Fatal("invalid reconnect delay was accepted")
	}
}
