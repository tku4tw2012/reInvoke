// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const mediaControlTimeout = 3 * time.Second

type blueZMediaController struct {
	command    string
	peer       string
	statusPath string
	run        commandRunner
}

func (controller *blueZMediaController) Apply(
	ctx context.Context,
	event inputEvent,
) error {
	if event.Name != "action" {
		return nil
	}
	status, err := os.ReadFile(controller.statusPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read playback status: %w", err)
	}
	action := "play"
	if playbackIsRunning(status) {
		action = "pause"
	}
	_, err = controller.run(ctx, controller.command, controller.peer, action)
	return err
}

func runMediaControlCommand(
	ctx context.Context,
	args ...string,
) ([]byte, error) {
	if len(args) == 0 {
		return nil, errors.New("media-control command is required")
	}
	commandContext, cancel := context.WithTimeout(ctx, mediaControlTimeout)
	defer cancel()
	command := exec.CommandContext(commandContext, args[0], args[1:]...)
	command.Env = os.Environ()
	output, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf(
			"media-control %s: %w: %s",
			args[len(args)-1],
			err,
			strings.TrimSpace(string(output)),
		)
	}
	return output, nil
}
