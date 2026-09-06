// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"sync"
)

const indicatorLEDCode = byte(0x09)

type indicatorLEDWriter interface {
	WriteMCUCommand([6]byte) error
}

type indicatorLEDState struct {
	amber byte
	white byte
	back  byte
}

type indicatorLEDController struct {
	writer indicatorLEDWriter

	mu        sync.Mutex
	state     indicatorLEDState
	confirmed bool
}

func newIndicatorLEDController(
	writer indicatorLEDWriter,
) *indicatorLEDController {
	return &indicatorLEDController{writer: writer}
}

func (controller *indicatorLEDController) Set(
	target,
	mode,
	color string,
) error {
	controller.mu.Lock()
	defer controller.mu.Unlock()

	candidate := controller.state
	value := indicatorLEDMode(mode)
	switch target {
	case "front":
		switch color {
		case "white":
			candidate.white = value
			candidate.amber = 0
		case "amber":
			candidate.amber = value
			candidate.white = 0
		}
	case "back":
		candidate.back = value
	}
	return controller.writeLocked(candidate)
}

func (controller *indicatorLEDController) SetBackIfChanged(mode string) error {
	controller.mu.Lock()
	defer controller.mu.Unlock()

	value := indicatorLEDMode(mode)
	if controller.confirmed && controller.state.back == value {
		return nil
	}
	candidate := controller.state
	candidate.back = value
	return controller.writeLocked(candidate)
}

func (controller *indicatorLEDController) writeLocked(
	candidate indicatorLEDState,
) error {
	frame := [6]byte{
		indicatorLEDCode,
		candidate.amber,
		candidate.white,
		candidate.back,
		0,
		0,
	}
	if err := controller.writer.WriteMCUCommand(frame); err != nil {
		return fmt.Errorf("set indicator LEDs: %w", err)
	}
	controller.state = candidate
	controller.confirmed = true
	return nil
}

func indicatorLEDMode(mode string) byte {
	switch mode {
	case "", "off":
		return 0
	case "on":
		return 1
	case "dim":
		return 2
	case "slow-blink":
		return 3
	case "fast-blink":
		return 4
	default:
		return 0
	}
}
