// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"sync"
	"syscall"
	"time"
)

const (
	microphoneStatePath = "/run/reinvoke/microphone-state"
	maxPendingPhases    = 16
	phaseRefresh        = 2 * time.Second
)

type voicePhase string

const (
	phaseListening voicePhase = "listening"
	phaseThinking  voicePhase = "thinking"
	phaseSpeaking  voicePhase = "speaking"
	phaseIdle      voicePhase = "idle"
)

func loggedPhase(line string) (voicePhase, bool) {
	switch line {
	case "UNIT_LINK PHASE listening":
		return phaseListening, true
	case "UNIT_LINK PHASE thinking":
		return phaseThinking, true
	case "UNIT_LINK PHASE speaking":
		return phaseSpeaking, true
	case "UNIT_LINK PHASE idle":
		return phaseIdle, true
	}
	return "", false
}

type phaseCaller func(context.Context, string, []interface{}, map[string]interface{}) error

type phaseFeedback struct {
	ctx       context.Context
	cancel    context.CancelFunc
	done      chan struct{}
	wake      chan struct{}
	mu        sync.Mutex
	pending   []voicePhase
	statePath string
	call      phaseCaller
	logger    *log.Logger
	last      voicePhase
	touched   bool
	desired   voicePhase
}

func newFeedback(ctx context.Context, statePath string, logger *log.Logger, call phaseCaller) *phaseFeedback {
	ctx, cancel := context.WithCancel(ctx)
	feedback := &phaseFeedback{
		ctx: ctx, cancel: cancel, done: make(chan struct{}), wake: make(chan struct{}, 1),
		statePath: statePath, call: call, logger: logger,
	}
	go feedback.run()
	return feedback
}

func (feedback *phaseFeedback) publish(phase voicePhase) {
	if feedback.ctx.Err() != nil {
		return
	}
	feedback.mu.Lock()
	if len(feedback.pending) > 0 && feedback.pending[len(feedback.pending)-1] == phase {
		feedback.mu.Unlock()
		return
	}
	overflow := len(feedback.pending) == maxPendingPhases
	if overflow {
		// Keep the newest state, especially idle, rather than replaying stale
		// turns after an unresponsive router. Never block the audio pipes.
		feedback.pending = feedback.pending[:0]
	}
	feedback.pending = append(feedback.pending, phase)
	feedback.mu.Unlock()
	if overflow {
		feedback.logger.Print("voice phase queue full; coalescing pending feedback")
	}
	select {
	case feedback.wake <- struct{}{}:
	default:
	}
}

func (feedback *phaseFeedback) close() {
	feedback.cancel()
	<-feedback.done
}

func (feedback *phaseFeedback) run() {
	defer close(feedback.done)
	ticker := time.NewTicker(phaseRefresh)
	defer ticker.Stop()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), controlTimeout)
		defer cancel()
		feedback.apply(ctx, phaseIdle)
	}()
	for {
		select {
		case <-feedback.ctx.Done():
			return
		case <-ticker.C:
			if feedback.desired != "" && (feedback.touched || feedback.desired != phaseIdle) {
				// A repeated state renews the MCU lease; it must not restart
				// cues or animations there.
				feedback.last = ""
				feedback.apply(feedback.ctx, feedback.desired)
			}
		case <-feedback.wake:
		}
		for feedback.ctx.Err() == nil {
			feedback.mu.Lock()
			if len(feedback.pending) == 0 {
				feedback.mu.Unlock()
				break
			}
			phase := feedback.pending[0]
			feedback.pending = feedback.pending[1:]
			feedback.mu.Unlock()
			feedback.apply(feedback.ctx, phase)
		}
	}
}

func (feedback *phaseFeedback) apply(ctx context.Context, phase voicePhase) {
	feedback.desired = phase
	if phase == phaseIdle && !feedback.touched {
		return
	}
	state := string(phase)
	switch phase {
	case phaseListening, phaseThinking, phaseSpeaking:
		unmuted, err := microphoneUnmuted(feedback.statePath)
		if err != nil {
			feedback.logger.Printf("voice feedback suppressed: %v", err)
		}
		if !unmuted {
			// Cleanup still reaches the MCU while muted; only that owner can
			// restore music and stop voice cues without clearing muted red.
			if feedback.touched {
				feedback.apply(ctx, phaseIdle)
			}
			return
		}
	case phaseIdle:
		state = ""
	default:
		feedback.logger.Print("voice feedback rejected an invalid phase")
		return
	}
	if phase == feedback.last {
		return
	}
	if phase != phaseIdle {
		// A timed-out call might still have applied. Always attempt cleanup.
		feedback.touched = true
	}
	kwargs := map[string]interface{}{"state": state}
	if err := feedback.call(ctx, "com.harman.extStateUpdate", []interface{}{"voice"}, kwargs); err != nil {
		feedback.last = ""
		// Shutdown cancels an in-flight call; run's background cleanup still
		// owns the pending reset and reports any actual cleanup failure.
		if ctx.Err() != nil {
			return
		}
		feedback.logger.Printf("voice %s failed: %v", phase, err)
		return
	}
	feedback.last = phase
	if phase == phaseIdle {
		feedback.touched = false
	}
}

func microphoneUnmuted(path string) (bool, error) {
	file, err := openRegular(path, syscall.O_RDONLY, 0)
	if err != nil {
		return false, fmt.Errorf("microphone-state unreadable: %w", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 33))
	if err != nil {
		return false, fmt.Errorf("microphone-state unreadable: %w", err)
	}
	switch string(data) {
	case "unmuted", "unmuted\n":
		return true, nil
	case "muted", "muted\n":
		return false, nil
	default:
		return false, errors.New("microphone-state invalid")
	}
}
