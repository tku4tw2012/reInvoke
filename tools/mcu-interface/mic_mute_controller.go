// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"fmt"
	"sync"
	"time"
)

const microphoneReconcileInterval = 5 * time.Second

type microphoneMuteController struct {
	mu sync.Mutex

	muted   bool
	desired bool
	unknown bool

	statePath   string
	controlPath string
	lights      *ledPlayer
	lifetime    context.Context
	reconcile   chan struct{}
	logf        func(string, ...interface{})
}

func newMicrophoneMuteController(
	muted bool,
	statePath string,
	controlPath string,
	lights *ledPlayer,
	logf func(string, ...interface{}),
) *microphoneMuteController {
	return &microphoneMuteController{
		muted:       muted,
		desired:     muted,
		unknown:     muted,
		statePath:   statePath,
		controlPath: controlPath,
		lights:      lights,
		lifetime:    context.Background(),
		reconcile:   make(chan struct{}, 1),
		logf:        logf,
	}
}

func (controller *microphoneMuteController) Apply(
	ctx context.Context,
	event inputEvent,
) error {
	if event.Name != "micmute" {
		return nil
	}
	controller.mu.Lock()
	if err := ctx.Err(); err != nil {
		controller.mu.Unlock()
		return err
	}
	err := controller.setLocked(ctx, !controller.muted)
	controller.mu.Unlock()
	if err != nil {
		controller.RequestReconcile()
	}
	return err
}

func (controller *microphoneMuteController) Set(
	ctx context.Context,
	muted bool,
) error {
	controller.mu.Lock()
	if err := ctx.Err(); err != nil {
		controller.mu.Unlock()
		return err
	}
	err := controller.setLocked(ctx, muted)
	controller.mu.Unlock()
	if err != nil {
		controller.RequestReconcile()
	}
	return err
}

func (controller *microphoneMuteController) Reconcile(
	ctx context.Context,
) error {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if !controller.muted && !controller.desired && !controller.unknown {
		return nil
	}
	return controller.setLocked(ctx, true)
}

func (controller *microphoneMuteController) RequestReconcile() {
	select {
	case controller.reconcile <- struct{}{}:
	default:
	}
}

func (controller *microphoneMuteController) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-controller.reconcile:
		}
		for {
			if err := controller.Reconcile(ctx); err == nil {
				break
			} else if controller.logf != nil {
				controller.logf("reconcile DSP microphone mute: %v", err)
			}
			timer := time.NewTimer(microphoneReconcileInterval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}
}

func (controller *microphoneMuteController) setLocked(
	ctx context.Context,
	muted bool,
) error {
	controller.desired = true
	controller.unknown = true
	if !muted && controller.lights != nil {
		if err := controller.lights.SetMicrophoneMuted(
			controller.lifetime,
			false,
		); err != nil {
			return fmt.Errorf("clear mic-mute indicator before unmute: %w", err)
		}
	}
	if muted {
		if err := persistMicrophoneState(controller.statePath, true); err != nil {
			return fmt.Errorf("persist required microphone mute: %w", err)
		}
	}
	if err := setDSPMicrophone(ctx, controller.controlPath, muted); err != nil {
		if !muted {
			return controller.restoreMuteLocked(controller.lifetime, err)
		}
		return err
	}
	if err := persistMicrophoneState(controller.statePath, muted); err != nil {
		if !muted {
			return controller.restoreMuteLocked(controller.lifetime, err)
		}
		return fmt.Errorf("persist confirmed microphone state: %w", err)
	}
	controller.muted = muted
	controller.desired = muted
	controller.unknown = false
	if controller.lights != nil {
		if err := controller.lights.SetMicrophoneMuted(
			controller.lifetime,
			muted,
		); err != nil {
			return fmt.Errorf("set microphone mute indicator: %w", err)
		}
	}
	if controller.logf != nil {
		controller.logf("confirmed DSP microphone muted=%t", muted)
	}
	return nil
}

func (controller *microphoneMuteController) restoreMuteLocked(
	ctx context.Context,
	cause error,
) error {
	restoreErr := setDSPMicrophone(ctx, controller.controlPath, true)
	if restoreErr == nil {
		restoreErr = persistMicrophoneState(controller.statePath, true)
	}
	if restoreErr == nil {
		controller.muted = true
		controller.desired = true
		controller.unknown = false
		if controller.lights != nil {
			restoreErr = controller.lights.SetMicrophoneMuted(
				controller.lifetime,
				true,
			)
		}
	}
	if restoreErr != nil {
		return fmt.Errorf(
			"microphone unmute outcome unknown: %v; restore mute: %w",
			cause,
			restoreErr,
		)
	}
	return fmt.Errorf("microphone unmute was reverted: %w", cause)
}
