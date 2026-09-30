// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

const (
	voiceSubsystem    = "voice"
	voiceLease        = 10 * time.Second
	voiceThinkingWait = 1350 * time.Millisecond
	voiceTickInterval = 50 * time.Millisecond
	voiceRetryWait    = time.Second
)

type voiceLights interface {
	StartVoice(context.Context, string) error
	StopVoice(context.Context) error
}

type voiceCues interface {
	Play(context.Context, string) error
}

type voiceFeedbackController struct {
	mu       sync.Mutex
	lifetime context.Context
	lights   voiceLights
	cues     voiceCues
	music    *voiceMusicDuck
	blocked  func() bool
	logf     func(string, ...interface{})
	now      func() time.Time

	state       string
	expires     time.Time
	thinkingDue time.Time
	retryAt     time.Time
	ledState    string
	cleanup     bool
	cueCancel   context.CancelFunc
	cueDone     chan struct{}
}

func voiceStateReport(args []interface{}, kwargs map[string]interface{}) (string, bool, error) {
	if len(args) != 1 {
		return "", false, errors.New("invalid argument format")
	}
	subsystem, ok := args[0].(string)
	if !ok {
		return "", false, errors.New("invalid argument format")
	}
	if subsystem != voiceSubsystem {
		return "", false, nil
	}
	state, ok := kwargs["state"].(string)
	if !ok {
		return "", true, errors.New("voice state is missing or is not a string")
	}
	switch state {
	case "", "listening", "thinking", "speaking":
		return state, true, nil
	default:
		return "", true, fmt.Errorf("unsupported voice state %q", state)
	}
}

func (voice *voiceFeedbackController) currentTime() time.Time {
	if voice.now != nil {
		return voice.now()
	}
	return time.Now()
}

func (voice *voiceFeedbackController) Active() bool {
	if voice == nil {
		return false
	}
	voice.mu.Lock()
	defer voice.mu.Unlock()
	return voice.state != "" && voice.currentTime().Before(voice.expires)
}

func (voice *voiceFeedbackController) State() string {
	voice.mu.Lock()
	defer voice.mu.Unlock()
	return voice.state
}

func (voice *voiceFeedbackController) Update(ctx context.Context, state string) error {
	voice.mu.Lock()
	defer voice.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	switch state {
	case "", "listening", "thinking", "speaking":
	default:
		return errors.New("unsupported voice state")
	}
	if state != "" && (voice.blocked == nil || voice.blocked()) {
		if err := voice.clearLocked(context.Background()); err != nil {
			return err
		}
		return errors.New("voice feedback suppressed while microphone state is muted or unknown")
	}
	now := voice.currentTime()
	if state == "" {
		return voice.clearLocked(ctx)
	}
	voice.expires = now.Add(voiceLease)
	changed := voice.state != state
	if changed {
		voice.stopCueLocked()
		voice.state, voice.cleanup = state, true
		voice.thinkingDue = time.Time{}
		if state == "thinking" {
			voice.thinkingDue = now.Add(voiceThinkingWait)
		}
	}
	err := voice.applyLocked(ctx, now)
	if changed && state == "listening" {
		if voice.blocked != nil && !voice.blocked() {
			voice.startCueLocked("listening")
		} else {
			return voice.clearLocked(context.Background())
		}
	}
	return err
}

func (voice *voiceFeedbackController) applyLocked(ctx context.Context, now time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var firstError error
	if err := voice.music.SetActive(true); err != nil {
		firstError = fmt.Errorf("duck voice music: %w", err)
	}
	if voice.ledState != voice.state {
		name := map[string]string{
			"listening": "L_101_c_listening",
			"thinking":  "L_104_c_thinking",
			"speaking":  "L_105_c_cortanaspeaking",
		}[voice.state]
		if voice.lights == nil {
			if firstError == nil {
				firstError = errors.New("voice LED player is unavailable")
			}
		} else if err := voice.lights.StartVoice(voice.context(), name); err != nil {
			if firstError == nil {
				firstError = err
			}
		} else {
			voice.ledState = voice.state
		}
	}
	voice.retryAt = now.Add(voiceRetryWait)
	return firstError
}

func (voice *voiceFeedbackController) context() context.Context {
	if voice.lifetime != nil {
		return voice.lifetime
	}
	return context.Background()
}

func (voice *voiceFeedbackController) startCueLocked(name string) {
	if voice.cues == nil {
		return
	}
	voice.stopCueLocked()
	ctx, cancel := context.WithCancel(voice.context())
	done := make(chan struct{})
	voice.cueCancel, voice.cueDone = cancel, done
	go func() {
		defer close(done)
		err := voice.cues.Play(ctx, name)
		if err != nil && ctx.Err() == nil && voice.logf != nil {
			voice.logf("VOICE_CUE_FAILED %s: %v", name, err)
		}
	}()
}

func (voice *voiceFeedbackController) stopCueLocked() {
	if voice.cueCancel != nil {
		voice.cueCancel()
		<-voice.cueDone
		voice.cueCancel, voice.cueDone = nil, nil
	}
}

func (voice *voiceFeedbackController) clearLocked(ctx context.Context) error {
	voice.state = ""
	voice.expires, voice.thinkingDue = time.Time{}, time.Time{}
	voice.stopCueLocked()
	if !voice.cleanup {
		return nil
	}
	var firstError error
	if voice.lights != nil {
		firstError = voice.lights.StopVoice(ctx)
	}
	if err := voice.music.SetActive(false); err != nil && firstError == nil {
		firstError = fmt.Errorf("restore voice music: %w", err)
	}
	if firstError == nil {
		voice.cleanup, voice.ledState = false, ""
	}
	voice.retryAt = voice.currentTime().Add(voiceRetryWait)
	return firstError
}

func (voice *voiceFeedbackController) Reset() error {
	if voice == nil {
		return nil
	}
	voice.mu.Lock()
	defer voice.mu.Unlock()
	return voice.clearLocked(context.Background())
}

func (voice *voiceFeedbackController) tick(ctx context.Context) error {
	voice.mu.Lock()
	defer voice.mu.Unlock()
	if !voice.cleanup {
		return nil
	}
	now := voice.currentTime()
	if voice.state != "" && (!now.Before(voice.expires) || voice.blocked == nil || voice.blocked()) {
		return voice.clearLocked(ctx)
	}
	if voice.state == "thinking" && !voice.thinkingDue.IsZero() && !now.Before(voice.thinkingDue) {
		voice.thinkingDue = time.Time{}
		voice.startCueLocked("processing")
	}
	if now.Before(voice.retryAt) {
		return nil
	}
	if voice.state == "" {
		return voice.clearLocked(ctx)
	}
	return voice.applyLocked(ctx, now)
}

func (voice *voiceFeedbackController) Run(ctx context.Context) {
	ticker := time.NewTicker(voiceTickInterval)
	defer ticker.Stop()
	defer func() {
		if err := voice.Reset(); err != nil && voice.logf != nil {
			voice.logf("VOICE_CLEANUP_FAILED: %v", err)
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := voice.tick(ctx); err != nil && voice.logf != nil {
				voice.logf("VOICE_FEEDBACK_FAILED: %v", err)
			}
		}
	}
}
