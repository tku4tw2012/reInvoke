// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"time"
)

// mediaActionController performs the one media action the retail speaker
// reached from the top panel: pausing what is playing. The recovered audio-ui
// table dispatched the short tap to music-pause while a renderer was running
// and never to a resume, so resuming is deliberately not offered here.
type mediaActionController struct {
	caller, host, realm, playbackStatus string
	port                                int
	requests                            chan struct{}
	readFile                            func(string) ([]byte, error)
	call                                func(context.Context, string) error
	logf                                func(string, ...interface{})
}

func (controller *mediaActionController) Apply(ctx context.Context, event inputEvent) error {
	if event.Name != "action" {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// Nothing playing means the tap was not a pause; the retail speaker used
	// it to cancel speech instead, which this runtime has no target for.
	if !playbackRunning(controller.playbackStatus, controller.readFile) {
		return nil
	}
	select {
	case controller.requests <- struct{}{}:
		return nil
	default:
		return errors.New("Bluetooth media action already queued")
	}
}

func (controller *mediaActionController) pause(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// Re-check rather than trust the queued request: the renderer may have
	// stopped while this was waiting, and pausing a stopped renderer would
	// report a success the speaker cannot justify.
	if !playbackRunning(controller.playbackStatus, controller.readFile) {
		return errors.New("playback is no longer running")
	}
	const procedure = "com.harman.bluetooth.pause"
	if controller.call != nil {
		return controller.call(ctx, procedure)
	}
	command := exec.CommandContext(ctx, controller.caller,
		"--router-host", controller.host,
		"--router-port", strconv.Itoa(controller.port),
		"--realm", controller.realm,
		"--call", procedure)
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %w: %s", procedure, err, output)
	}
	return nil
}

func (controller *mediaActionController) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-controller.requests:
			if ctx.Err() != nil {
				return
			}
			// Network replies must not hold up the physical Mic-Mute worker.
			request, cancel := context.WithTimeout(ctx, 3*time.Second)
			err := controller.pause(request)
			cancel()
			if err != nil && ctx.Err() == nil && controller.logf != nil {
				controller.logf("Bluetooth media action failed: %v", err)
			}
		}
	}
}
