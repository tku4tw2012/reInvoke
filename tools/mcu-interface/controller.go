// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

const (
	expanderAddress = 0x20
	expanderOutput  = 0x01
	expanderConfig  = 0x03
	dacAddress      = 0x4c

	ampMuteMask = byte(0x02)
	dacMuteMask = byte(0x04)
)

var dacInitialization = [][2]byte{
	{0x00, 0x00},
	{0x01, 0x11},
	{0x0d, 0x10},
	{0x25, 0x08},
	{0x41, 0x04},
	{0x41, 0x07},
	{0x08, 0x3f},
	{0x28, 0x00},
	{0x3d, 0x30},
	{0x3e, 0x30},
}

type hardware interface {
	ReadRegister(address, register byte) (byte, error)
	WriteRegister(address, register, value byte) error
	UpdateRegister(address, register byte, update func(byte) byte) error
}

type mutePolicy struct {
	AllowUnmute         bool
	AllowPlaybackUnmute bool
}

type controller struct {
	hardware hardware
	policy   mutePolicy
	sleep    func(time.Duration)

	mu          sync.Mutex
	initialized bool
	ampMuted    bool
	dacMuted    bool
}

func newController(hw hardware, policy mutePolicy) *controller {
	return &controller{
		hardware: hw,
		policy:   policy,
		sleep:    time.Sleep,
		ampMuted: true,
		dacMuted: true,
	}
}

func (c *controller) initialize() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.hardware.WriteRegister(
		expanderAddress,
		expanderConfig,
		0x00,
	); err != nil {
		return fmt.Errorf("configure IO expander: %w", err)
	}
	if err := c.setAmpMuteLocked(true); err != nil {
		return fmt.Errorf("mute amplifier: %w", err)
	}
	if err := c.setDACMuteLocked(true); err != nil {
		return fmt.Errorf("mute DAC: %w", err)
	}

	for _, mask := range []byte{0x01, 0x10, 0x08} {
		if err := c.updateExpanderLocked(func(value byte) byte {
			return value | mask
		}); err != nil {
			return fmt.Errorf("power DSP: %w", err)
		}
	}
	for _, setting := range dacInitialization {
		if err := c.hardware.WriteRegister(
			dacAddress,
			setting[0],
			setting[1],
		); err != nil {
			_ = c.setAmpMuteLocked(true)
			_ = c.setDACMuteLocked(true)
			return fmt.Errorf("initialize DAC register 0x%02x: %w", setting[0], err)
		}
	}

	c.sleep(2 * time.Second)
	c.initialized = true
	return nil
}

func (c *controller) setAmpMute(muted bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.setAmpMuteLocked(muted)
}

func (c *controller) setDACMute(muted bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.setDACMuteLocked(muted)
}

func (c *controller) muteAll() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	ampErr := c.setAmpMuteLocked(true)
	dacErr := c.setDACMuteLocked(true)
	if ampErr != nil {
		return fmt.Errorf("mute amplifier: %w", ampErr)
	}
	if dacErr != nil {
		return fmt.Errorf("mute DAC: %w", dacErr)
	}
	return nil
}

func (c *controller) setAmpMuteLocked(muted bool) error {
	if !muted {
		if err := c.unmuteAllowedLocked(); err != nil {
			return err
		}
	}
	return c.writeAmpMuteLocked(muted)
}

func (c *controller) writeAmpMuteLocked(muted bool) error {
	if !muted {
		if c.dacMuted {
			return errors.New("DAC must be unmuted before amplifier")
		}
	}

	err := c.updateExpanderLocked(func(value byte) byte {
		if muted {
			return value | ampMuteMask
		}
		return value &^ ampMuteMask
	})
	if err == nil {
		c.ampMuted = muted
	}
	return err
}

func (c *controller) setDACMuteLocked(muted bool) error {
	if !muted {
		if err := c.unmuteAllowedLocked(); err != nil {
			return err
		}
	}
	return c.writeDACMuteLocked(muted)
}

func (c *controller) writeDACMuteLocked(muted bool) error {
	err := c.updateExpanderLocked(func(value byte) byte {
		if muted {
			return value &^ dacMuteMask
		}
		return value | dacMuteMask
	})
	if err == nil {
		c.dacMuted = muted
	}
	return err
}

func (c *controller) setPlaybackActive(active bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !active {
		ampErr := c.writeAmpMuteLocked(true)
		dacErr := c.writeDACMuteLocked(true)
		if ampErr != nil {
			return fmt.Errorf("mute amplifier: %w", ampErr)
		}
		if dacErr != nil {
			return fmt.Errorf("mute DAC: %w", dacErr)
		}
		return nil
	}
	if !c.initialized {
		return errors.New("audio path is not initialized")
	}
	if !c.policy.AllowPlaybackUnmute {
		return errors.New("playback unmute denied by local policy")
	}
	if err := c.writeDACMuteLocked(false); err != nil {
		return fmt.Errorf("unmute DAC: %w", err)
	}
	if err := c.writeAmpMuteLocked(false); err != nil {
		_ = c.writeDACMuteLocked(true)
		return fmt.Errorf("unmute amplifier: %w", err)
	}
	return nil
}

func (c *controller) unmuteAllowedLocked() error {
	if !c.initialized {
		return errors.New("audio path is not initialized")
	}
	if !c.policy.AllowUnmute {
		return errors.New("unmute denied by local policy")
	}
	return nil
}

func (c *controller) updateExpanderLocked(update func(byte) byte) error {
	return c.hardware.UpdateRegister(
		expanderAddress,
		expanderOutput,
		update,
	)
}
