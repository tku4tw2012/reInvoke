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

var errMicrophoneRequestSuperseded = errors.New(
	"microphone privacy request was superseded",
)

type microphonePrivacyController struct {
	mu sync.Mutex

	muted   bool
	desired bool
	unknown bool

	policyMu       sync.Mutex
	requestedMuted bool
	baseMuted      bool
	policyRevision uint64
	nextVersion    uint64
	appliedVersion uint64
	pendingPolicy  map[uint64]bool

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
		baseMuted:      muted,
		policyRevision: 1,
		nextVersion:    1,
		appliedVersion: 1,
		pendingPolicy:  make(map[uint64]bool),
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
			// A mute request remains fail-closed even if its caller
			// disappears while waiting for the transition mutex.
			controller.desired = true
			controller.unknown = true
			controller.fenceCaptureLocked(controller.lifetime)
			controller.mu.Unlock()
			controller.RequestReconcile()
			return err
		}
		effectiveMuted, removed := controller.cancelRequestedPolicy(version)
		needsReconcile := removed &&
			(effectiveMuted || controller.unknown ||
				controller.muted != effectiveMuted ||
				controller.desired != effectiveMuted)
		if needsReconcile {
			controller.desired = true
			controller.unknown = true
			controller.fenceCaptureLocked(controller.lifetime)
		}
		controller.mu.Unlock()
		if needsReconcile {
			controller.RequestReconcile()
		}
		return err
	}
	requestState := controller.requestState(version, muted)
	if requestState == policyRequestSuperseded {
		effectiveMuted, removed := controller.discardSupersededUnmute(
			version,
			muted,
		)
		if removed && effectiveMuted {
			controller.desired = true
			controller.unknown = true
			controller.fenceCaptureLocked(controller.lifetime)
		}
		controller.mu.Unlock()
		if removed && effectiveMuted {
			controller.RequestReconcile()
		}
		return errMicrophoneRequestSuperseded
	}
	if requestState == policyRequestApplied {
		controller.mu.Unlock()
		return nil
	}
	err := controller.setLocked(ctx, muted)
	if err == nil {
		err = controller.finalizeRequestedPolicyLocked(version, muted)
	}
	controller.mu.Unlock()
	if err != nil && !errors.Is(err, errMicrophoneRequestSuperseded) {
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
	err := controller.setLocked(ctx, true)
	if err == nil {
		if !controller.commitPolicy(version, true) {
			effectiveMuted, _, _ := controller.requestedPolicyState()
			controller.desired = effectiveMuted
			controller.unknown = controller.muted != effectiveMuted
		}
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
		controller.commitPolicy(version, true)
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
	entryRequested, entryRevision, _ := controller.requestedPolicyState()
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
		controller.commitPolicy(version, true)
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

	requestedMuted, policyRevision, effectiveVersion :=
		controller.requestedPolicyState()
	restoreUnmuted := !entryMuted && !entryDesired && !entryUnknown &&
		!entryRequested && !requestedMuted && policyRevision == entryRevision
	if restoreUnmuted {
		if err := controller.transitionHardwareLocked(ctx, false, false); err != nil {
			return controller.muted, controller.restoreMuteLocked(
				controller.lifetime,
				fmt.Errorf("restore synchronized microphone unmute: %w", err),
			)
		}
		controller.desired = false
		controller.unknown = false
		requestedMuted, policyRevision, effectiveVersion =
			controller.requestedPolicyState()
		if requestedMuted || policyRevision != entryRevision {
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
				controller.commitPolicy(effectiveVersion, true)
			}
			restoreUnmuted = false
		}
	} else if requestedMuted {
		controller.desired = true
		controller.unknown = false
		controller.commitPolicy(effectiveVersion, true)
	} else if policyRevision != entryRevision {
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
		controller.policyMu.Lock()
		requestedMuted, effectiveVersion =
			controller.effectivePolicyLocked()
		policyRevision = controller.policyRevision
		if requestedMuted || policyRevision != entryRevision {
			controller.policyMu.Unlock()
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
				controller.commitPolicy(effectiveVersion, true)
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
		} else {
			// Keep the policy lock through ALLOW so a newer mute request
			// cannot land between the final policy check and authorization.
			if !controller.commitPolicyLocked(effectiveVersion, false) {
				controller.policyMu.Unlock()
				return controller.muted, errMicrophoneRequestSuperseded
			}
			if controller.capture != gate || !gate.Live() {
				controller.policyMu.Unlock()
				controller.capture = nil
				return controller.muted, errors.New(
					"capture privacy authority was lost before allow",
				)
			}
			if err := gate.Allow(ctx); err != nil {
				controller.policyMu.Unlock()
				controller.capture = nil
				return controller.muted, fmt.Errorf(
					"allow synchronized capture generation: %w",
					err,
				)
			}
			controller.policyMu.Unlock()
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

type policyRequestState uint8

const (
	policyRequestPending policyRequestState = iota
	policyRequestApplied
	policyRequestSuperseded
)

func (controller *microphonePrivacyController) effectivePolicyLocked() (
	bool,
	uint64,
) {
	version := controller.appliedVersion
	muted := controller.baseMuted
	for pendingVersion, pendingMuted := range controller.pendingPolicy {
		if pendingVersion > version {
			version = pendingVersion
			muted = pendingMuted
		}
	}
	controller.requestedMuted = muted
	return muted, version
}

func (controller *microphonePrivacyController) recordRequestedPolicy(
	muted bool,
) uint64 {
	controller.policyMu.Lock()
	defer controller.policyMu.Unlock()
	controller.nextVersion++
	version := controller.nextVersion
	controller.pendingPolicy[version] = muted
	controller.policyRevision++
	controller.effectivePolicyLocked()
	return version
}

func (controller *microphonePrivacyController) toggleRequestedPolicy() (
	bool,
	uint64,
) {
	controller.policyMu.Lock()
	defer controller.policyMu.Unlock()
	muted, _ := controller.effectivePolicyLocked()
	controller.nextVersion++
	version := controller.nextVersion
	controller.pendingPolicy[version] = !muted
	controller.policyRevision++
	controller.effectivePolicyLocked()
	return !muted, version
}

func (controller *microphonePrivacyController) cancelRequestedPolicy(
	version uint64,
) (bool, bool) {
	controller.policyMu.Lock()
	defer controller.policyMu.Unlock()
	if _, ok := controller.pendingPolicy[version]; !ok {
		muted, _ := controller.effectivePolicyLocked()
		return muted, false
	}
	delete(controller.pendingPolicy, version)
	controller.policyRevision++
	muted, _ := controller.effectivePolicyLocked()
	return muted, true
}

func (controller *microphonePrivacyController) requestedPolicy() (bool, uint64) {
	controller.policyMu.Lock()
	defer controller.policyMu.Unlock()
	muted, _ := controller.effectivePolicyLocked()
	return muted, controller.policyRevision
}

func (controller *microphonePrivacyController) requestedPolicyState() (
	bool,
	uint64,
	uint64,
) {
	controller.policyMu.Lock()
	defer controller.policyMu.Unlock()
	muted, version := controller.effectivePolicyLocked()
	return muted, controller.policyRevision, version
}

func (controller *microphonePrivacyController) requireMutedPolicy() uint64 {
	controller.policyMu.Lock()
	defer controller.policyMu.Unlock()
	muted, version := controller.effectivePolicyLocked()
	if muted {
		return version
	}
	controller.nextVersion++
	version = controller.nextVersion
	controller.pendingPolicy[version] = true
	controller.policyRevision++
	controller.effectivePolicyLocked()
	return version
}

func (controller *microphonePrivacyController) requestState(
	version uint64,
	muted bool,
) policyRequestState {
	controller.policyMu.Lock()
	defer controller.policyMu.Unlock()
	return controller.requestStateLocked(version, muted)
}

func (controller *microphonePrivacyController) requestStateLocked(
	version uint64,
	muted bool,
) policyRequestState {
	pendingMuted, pending := controller.pendingPolicy[version]
	effectiveMuted, effectiveVersion := controller.effectivePolicyLocked()
	if pending && pendingMuted == muted &&
		effectiveVersion == version && effectiveMuted == muted {
		return policyRequestPending
	}
	if version <= controller.appliedVersion && controller.baseMuted == muted {
		return policyRequestApplied
	}
	return policyRequestSuperseded
}

func (controller *microphonePrivacyController) commitPolicyLocked(
	version uint64,
	muted bool,
) bool {
	if controller.requestStateLocked(version, muted) == policyRequestApplied {
		return true
	}
	if controller.requestStateLocked(version, muted) != policyRequestPending {
		return false
	}
	controller.baseMuted = muted
	controller.appliedVersion = version
	for pendingVersion := range controller.pendingPolicy {
		if pendingVersion <= version {
			delete(controller.pendingPolicy, pendingVersion)
		}
	}
	controller.policyRevision++
	controller.effectivePolicyLocked()
	return true
}

func (controller *microphonePrivacyController) commitPolicy(
	version uint64,
	muted bool,
) bool {
	controller.policyMu.Lock()
	defer controller.policyMu.Unlock()
	return controller.commitPolicyLocked(version, muted)
}

func (controller *microphonePrivacyController) finalizeRequestedPolicyLocked(
	version uint64,
	muted bool,
) error {
	controller.policyMu.Lock()
	state := controller.requestStateLocked(version, muted)
	if state == policyRequestPending || state == policyRequestApplied {
		if !controller.commitPolicyLocked(version, muted) {
			controller.policyMu.Unlock()
			return errMicrophoneRequestSuperseded
		}
		if !muted {
			controller.allowCaptureLocked()
		}
		controller.policyMu.Unlock()
		return nil
	}
	if !muted {
		if pendingMuted, ok := controller.pendingPolicy[version]; ok &&
			!pendingMuted {
			delete(controller.pendingPolicy, version)
			controller.policyRevision++
		}
	}
	effectiveMuted, effectiveVersion := controller.effectivePolicyLocked()
	controller.policyMu.Unlock()

	if !muted {
		if err := controller.transitionHardwareLocked(
			controller.lifetime,
			true,
			true,
		); err != nil {
			controller.desired = true
			controller.unknown = true
			return fmt.Errorf("mute superseded microphone unmute: %w", err)
		}

	}
	controller.desired = effectiveMuted
	controller.unknown = controller.muted != effectiveMuted
	if effectiveMuted && controller.muted {
		controller.commitPolicy(effectiveVersion, true)
		controller.desired = true
		controller.unknown = false
	}
	return errMicrophoneRequestSuperseded
}

func (controller *microphonePrivacyController) discardSupersededUnmute(
	version uint64,
	muted bool,
) (bool, bool) {
	if muted {
		requestedMuted, _ := controller.requestedPolicy()
		return requestedMuted, false
	}
	controller.policyMu.Lock()
	defer controller.policyMu.Unlock()
	pendingMuted, ok := controller.pendingPolicy[version]
	if !ok || pendingMuted {
		effectiveMuted, _ := controller.effectivePolicyLocked()
		return effectiveMuted, false
	}
	delete(controller.pendingPolicy, version)
	controller.policyRevision++
	effectiveMuted, _ := controller.effectivePolicyLocked()
	return effectiveMuted, true
}
