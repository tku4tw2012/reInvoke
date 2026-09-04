// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"time"
)

func runWithReconnect(
	ctx context.Context,
	delay time.Duration,
	run func(context.Context) error,
	shouldReconnect func(error) bool,
	logf func(string, ...interface{}),
) error {
	if delay <= 0 {
		return errors.New("reconnect delay must be positive")
	}
	for {
		err := run(ctx)
		if ctx.Err() != nil {
			return nil
		}
		if shouldReconnect != nil && !shouldReconnect(err) {
			return err
		}
		if logf != nil {
			logf("WAMP session ended: %v; reconnecting in %s", err, delay)
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil
		case <-timer.C:
		}
	}
}
