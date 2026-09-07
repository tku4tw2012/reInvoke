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

const microphoneReconcileInterval = 5 * time.Second

type microphonePrivacyController struct {
	mu sync.Mutex

	muted   bool
	desired bool
	unknown bool

	policyMu       sync.Mutex
	requestedMuted bool
	policyVersion  uint64
	appliedVersion uint64

	statePath   string
	controlPath string
	lights      *ledPlayer
	lifetime    context.Context
	reconcile   chan struct{}
	logf        func(string, ...interface{})
	capture     capturePrivacyGate
}

func newMicrophonePrivacyController(
	muted bool,
	statePath string,
	controlPath string,
	lights *ledPlayer,
	logf func(string, ...interface{}),
) *microphonePrivacyController {
	return &microphonePrivacyController{
		muted:          muted,
		desired:        muted,
		unknown:        muted,
		requestedMuted: muted,
		policyVersion:  1,
		appliedVersion: 1,
		statePath:      statePath,
		controlPath:    controlPath,
		lights:         lights,
		lifetime:       context.Background(),
		reconcile:      make(chan struct{}, 1),
		logf:           logf,
	}
}

func (controller *microphonePrivacyController) Apply(
	ctx context.Context,
	event inputEvent,
) error {
	if event.Name != "micmute" {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	muted, version := controller.toggleRequestedPolicy()
	return controller.applyRequestedPolicy(ctx, muted, version)
}

func (controller *microphonePrivacyController) Set(
	ctx context.Context,
	muted bool,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	version := controller.recordRequestedPolicy(muted)
	return controller.applyRequestedPolicy(ctx, muted, version)
}

func (controller *microphonePrivacyController) applyRequestedPolicy(
	ctx context.Context,
	muted bool,
	version uint64,
) error {
	controller.mu.Lock()
	if err := ctx.Err(); err != nil {
		if muted {
			controller.requireMutedPolicy()
			controller.desired = true
			controller.unknown = true
			controller.fenceCaptureLocked(controller.lifetime)
		}
		controller.mu.Unlock()
		if muted {
			controller.RequestReconcile()
		}
		return err
	}
	if controller.policyWasApplied(version) &&
		controller.muted == muted && controller.desired == muted &&
		!controller.unknown {
		controller.mu.Unlock()
		return nil
	}
	err := controller.setLocked(ctx, muted, version)
	if err == nil {
		controller.markPolicyApplied(version)
	}
	controller.mu.Unlock()
	if err != nil {
		controller.RequestReconcile()
	}
	return err
}

func (controller *microphonePrivacyController) Reconcile(
	ctx context.Context,
) error {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	requestedMuted, _ := controller.requestedPolicy()
	if !requestedMuted && !controller.muted &&
		!controller.desired && !controller.unknown {
		return nil
	}
	version := controller.requireMutedPolicy()
	err := controller.setLocked(ctx, true, version)
	if err == nil {
		controller.markPolicyApplied(version)
	}
	return err
}

func (controller *microphonePrivacyController) RequestReconcile() {
	select {
	case controller.reconcile <- struct{}{}:
	default:
	}
}

func (controller *microphonePrivacyController) Run(ctx context.Context) {
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

func (controller *microphonePrivacyController) setLocked(
	ctx context.Context,
	muted bool,
	version uint64,
) error {
	controller.desired = true
	controller.unknown = true
	if err := controller.transitionHardwareLocked(ctx, muted, muted); err != nil {
		if !muted {
			return controller.restoreMuteLocked(controller.lifetime, err)
		}
		return err
	}
	controller.desired = muted
	controller.unknown = false
	if !muted {
		requestedMuted, currentVersion := controller.requestedPolicy()
		if requestedMuted || currentVersion != version {
			if err := controller.transitionHardwareLocked(
				controller.lifetime,
				true,
				true,
			); err != nil {
				controller.desired = true
				controller.unknown = true
				return fmt.Errorf(
					"mute superseded microphone unmute: %w",
					err,
				)
			}
			controller.desired = true
			controller.unknown = false
			if requestedMuted {
				controller.markPolicyApplied(currentVersion)
			}
			return errors.New("microphone unmute was superseded")
		}
		controller.allowCaptureLocked()
	}
	if controller.logf != nil {
		controller.logf("confirmed DSP microphone muted=%t", muted)
	}
	return nil
}

func (controller *microphonePrivacyController) restoreMuteLocked(
	ctx context.Context,
	cause error,
) error {
	restoreErr := controller.transitionHardwareLocked(ctx, true, true)
	if restoreErr == nil {
		controller.desired = true
		controller.unknown = false
		version := controller.requireMutedPolicy()
		controller.markPolicyApplied(version)
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

func (controller *microphonePrivacyController) transitionHardwareLocked(
	ctx context.Context,
	muted bool,
	fence bool,
) error {
	if muted && fence {
		controller.fenceCaptureLocked(ctx)
	}
	if muted {
		if err := persistMicrophoneState(controller.statePath, true); err != nil {
			return fmt.Errorf("persist required microphone mute: %w", err)
		}
	}
	if err := setDSPMicrophone(ctx, controller.controlPath, muted); err != nil {
		return err
	}
	if err := persistMicrophoneState(controller.statePath, muted); err != nil {
		return fmt.Errorf("persist confirmed microphone state: %w", err)
	}
	controller.muted = muted
	if controller.lights != nil {
		if err := controller.lights.SetPrivacyMuted(
			controller.lifetime,
			muted,
		); err != nil {
			return fmt.Errorf("set microphone privacy indicator: %w", err)
		}
	}
	return nil
}

func (controller *microphonePrivacyController) fenceCaptureLocked(
	ctx context.Context,
) {
	gate := controller.capture
	if gate == nil {
		return
	}
	if !gate.Live() {
		controller.capture = nil
		return
	}
	if err := gate.Fence(ctx); err == nil {
		return
	} else if controller.logf != nil {
		controller.logf("fence microphone capture delivery: %v", err)
	}
	if err := gate.TerminateVerified(); err != nil && controller.logf != nil {
		controller.logf("terminate unfenced microphone capture owner: %v", err)
	}
	controller.capture = nil
}

func (controller *microphonePrivacyController) allowCaptureLocked() {
	gate := controller.capture
	if gate == nil || !gate.Live() {
		controller.capture = nil
		return
	}
	if err := gate.Allow(controller.lifetime); err != nil {
		if controller.logf != nil {
			controller.logf("allow microphone capture delivery: %v", err)
		}
		controller.capture = nil
	}
}

func (controller *microphonePrivacyController) Synchronize(
	ctx context.Context,
	gate capturePrivacyGate,
) (bool, error) {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return controller.muted, err
	}
	entryMuted := controller.muted
	entryDesired := controller.desired
	entryUnknown := controller.unknown
	entryRequested, entryVersion := controller.requestedPolicy()
	controller.capture = gate

	if err := gate.Fence(ctx); err != nil {
		var terminateErr error
		if ctx.Err() == nil && controller.lifetime.Err() == nil {
			terminateErr = gate.TerminateVerified()
		}
		controller.capture = nil
		if terminateErr != nil {
			return controller.muted, fmt.Errorf(
				"fence capture owner: %v; terminate verified owner: %w",
				err,
				terminateErr,
			)
		}
		return controller.muted, fmt.Errorf("fence capture owner: %w", err)
	}
	if err := controller.transitionHardwareLocked(ctx, true, false); err != nil {
		controller.capture = nil
		return controller.muted, fmt.Errorf(
			"reassert DSP microphone mute after capture setup: %w",
			err,
		)
	}
	if err := gate.Drain(ctx); err != nil {
		var terminateErr error
		if ctx.Err() == nil && controller.lifetime.Err() == nil {
			terminateErr = gate.TerminateVerified()
		}
		controller.capture = nil
		version := controller.requireMutedPolicy()
		controller.desired = true
		controller.unknown = false
		controller.markPolicyApplied(version)
		if terminateErr != nil {
			return controller.muted, fmt.Errorf(
				"drain capture owner: %v; terminate verified owner: %w",
				err,
				terminateErr,
			)
		}
		return controller.muted, fmt.Errorf("drain capture owner: %w", err)
	}
	controller.desired = entryDesired
	controller.unknown = entryUnknown

	requestedMuted, policyVersion := controller.requestedPolicy()
	restoreUnmuted := !entryMuted && !entryDesired && !entryUnknown &&
		!entryRequested && !requestedMuted && policyVersion == entryVersion
	if restoreUnmuted {
		if err := controller.transitionHardwareLocked(ctx, false, false); err != nil {
			return controller.muted, controller.restoreMuteLocked(
				controller.lifetime,
				fmt.Errorf("restore synchronized microphone unmute: %w", err),
			)
		}
		controller.desired = false
		controller.unknown = false
		requestedMuted, policyVersion = controller.requestedPolicy()
		if requestedMuted || policyVersion != entryVersion {
			if err := controller.transitionHardwareLocked(
				controller.lifetime,
				true,
				false,
			); err != nil {
				controller.desired = true
				controller.unknown = true
				return controller.muted, fmt.Errorf(
					"apply mute requested during synchronized restore: %w",
					err,
				)
			}
			controller.desired = requestedMuted
			controller.unknown = !requestedMuted
			if requestedMuted {
				controller.markPolicyApplied(policyVersion)
			}
			restoreUnmuted = false
		}
	} else if requestedMuted {
		controller.desired = true
		controller.unknown = false
		controller.markPolicyApplied(policyVersion)
	} else if policyVersion != entryVersion {
		controller.desired = false
		controller.unknown = true
	}
	if controller.capture != gate || !gate.Live() {
		controller.capture = nil
		return controller.muted, errors.New(
			"capture privacy authority was lost before synchronization reply",
		)
	}
	if err := gate.State(ctx, controller.muted); err != nil {
		controller.capture = nil
		return controller.muted, fmt.Errorf(
			"send synchronized capture privacy state: %w",
			err,
		)
	}
	if restoreUnmuted {
		requestedMuted, policyVersion = controller.requestedPolicy()
		if requestedMuted || policyVersion != entryVersion {
			if err := controller.transitionHardwareLocked(
				controller.lifetime,
				true,
				false,
			); err != nil {
				controller.desired = true
				controller.unknown = true
				return controller.muted, fmt.Errorf(
					"mute requested before capture allow: %w",
					err,
				)
			}
			controller.desired = requestedMuted
			controller.unknown = !requestedMuted
			if requestedMuted {
				controller.markPolicyApplied(policyVersion)
			}
			if err := gate.State(
				controller.lifetime,
				controller.muted,
			); err != nil {
				controller.capture = nil
				return controller.muted, fmt.Errorf(
					"send revised capture privacy state: %w",
					err,
				)
			}
			restoreUnmuted = false
		}
	}
	if restoreUnmuted {
		if controller.capture != gate || !gate.Live() {
			controller.capture = nil
			return controller.muted, errors.New(
				"capture privacy authority was lost before allow",
			)
		}
		if err := gate.Allow(ctx); err != nil {
			controller.capture = nil
			return controller.muted, fmt.Errorf(
				"allow synchronized capture generation: %w",
				err,
			)
		}
	}
	return controller.muted, nil
}

func (controller *microphonePrivacyController) DetachCaptureAuthority(
	gate capturePrivacyGate,
) {
	controller.mu.Lock()
	if controller.capture == gate {
		controller.capture = nil
	}
	controller.mu.Unlock()
}

func (controller *microphonePrivacyController) recordRequestedPolicy(
	muted bool,
) uint64 {
	controller.policyMu.Lock()
	defer controller.policyMu.Unlock()
	controller.policyVersion++
	controller.requestedMuted = muted
	return controller.policyVersion
}

func (controller *microphonePrivacyController) toggleRequestedPolicy() (
	bool,
	uint64,
) {
	controller.policyMu.Lock()
	defer controller.policyMu.Unlock()
	controller.policyVersion++
	controller.requestedMuted = !controller.requestedMuted
	return controller.requestedMuted, controller.policyVersion
}

func (controller *microphonePrivacyController) requestedPolicy() (bool, uint64) {
	controller.policyMu.Lock()
	defer controller.policyMu.Unlock()
	return controller.requestedMuted, controller.policyVersion
}

func (controller *microphonePrivacyController) requireMutedPolicy() uint64 {
	controller.policyMu.Lock()
	defer controller.policyMu.Unlock()
	if !controller.requestedMuted {
		controller.policyVersion++
		controller.requestedMuted = true
	}
	return controller.policyVersion
}

func (controller *microphonePrivacyController) markPolicyApplied(version uint64) {
	controller.policyMu.Lock()
	if version > controller.appliedVersion {
		controller.appliedVersion = version
	}
	controller.policyMu.Unlock()
}

func (controller *microphonePrivacyController) policyWasApplied(
	version uint64,
) bool {
	controller.policyMu.Lock()
	defer controller.policyMu.Unlock()
	return controller.appliedVersion >= version
}
