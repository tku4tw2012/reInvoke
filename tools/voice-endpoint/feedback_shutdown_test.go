// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"testing"
)

func TestFeedbackCancellationKeepsCleanupPending(t *testing.T) {
	var output bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	feedback := &phaseFeedback{
		touched: true,
		last:    phaseSpeaking,
		logger:  log.New(&output, "", 0),
		call: func(callContext context.Context, _ string, _ []interface{}, kwargs map[string]interface{}) error {
			calls++
			if kwargs["state"] != "" {
				t.Fatal("cleanup reported a non-idle state")
			}
			if calls == 1 {
				cancel()
				return errors.New("use of closed network connection")
			}
			if callContext.Err() != nil {
				t.Fatal("final cleanup reused the cancelled context")
			}
			return nil
		},
	}
	feedback.apply(ctx, phaseIdle)
	if !feedback.touched || feedback.last != "" {
		t.Fatal("cancelled attempt falsely marked cleanup complete")
	}
	if strings.Contains(output.String(), "failed") {
		t.Fatal("normal cancellation was logged as a voice failure")
	}
	feedback.apply(context.Background(), phaseIdle)
	if feedback.touched || calls != 2 || feedback.last != phaseIdle {
		t.Fatal("background cleanup did not finish the pending reset")
	}
}
