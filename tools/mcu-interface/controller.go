// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"fmt"
	"log"
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

type controller struct {
	hardware hardware
	sleep    func(time.Duration)
	logf     func(string, ...interface{})

	mu          sync.Mutex
	initialized bool
	ampMuted    bool
	dacMuted    bool
}

func newController(hw hardware) *controller {
	return &controller{
		hardware: hw,
		sleep:    time.Sleep,
		logf:     log.Printf,
		ampMuted: true,
		dacMuted: true,
	}
}

func (c *controller) initialize() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.hardware.UpdateRegister(
		expanderAddress,
		expanderConfig,
		func(byte) byte {
			// The donor drives every expander pin as an output before it
			// initializes the MCU, DSP rail, and DAC controls. Preserving the
			// other directions here left GPIO3 asserted after cold boot.
			return 0
		},
	); err != nil {
		return fmt.Errorf("configure IO expander: %w", err)
	}
	if err := c.setAmpMuteLocked(true); err != nil {
		return fmt.Errorf("mute amplifier: %w", err)
	}
	if err := c.setDACMuteLocked(true); err != nil {
		return fmt.Errorf("mute DAC: %w", err)
	}

	// The donor also sets output bit 0 here. In reInvoke that bit belongs
	// exclusively to dsp-interface, whose reset pulse spans multiple locked
	// updates; an MCU restart must not release reset between them.
	for _, mask := range []byte{0x10, 0x08} {
		if err := c.updateExpanderLocked(func(value byte) byte {
			return value | mask
		}); err != nil {
			return fmt.Errorf("configure DSP power rails: %w", err)
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

	// The outputs stay muted here, as the donor leaves them.
	//
	// The donor's own log line at this point reads "MCU init io expander.
	// mute amp and dac!!!", and its initialisation contains no unmute: the
	// only UnMuting AMP/DAC sites in that binary are the muteampcontrol and
	// mutedaccontrol WAMP handlers and a power path. Its system-manager is
	// what calls muteampcontrol, so the DSP powers up behind a muted
	// amplifier.
	//
	// An earlier revision of this comment claimed the donor "does not keep
	// them muted". That was asserted, not checked, and it is wrong. Opening
	// the outputs here put the amplifier live for DSP bootup and for the
	// first gain change, which was audible on this unit as more than one pop
	// during startup. OpenOutputs is called once the DSP has accepted a
	// level instead.
	c.initialized = true
	return nil
}

// OpenOutputs unmutes the DAC and then the amplifier, once.
//
// This runtime ships no system-manager, so nothing else would ever open them.
// It is a single ordered step at a known-quiet moment, not a policy: nothing
// polls, nothing re-mutes, and muteampcontrol/mutedaccontrol remain the only
// other things that touch these bits.
// outputSettleDelay is how long the outputs stay muted after whatever last
// disturbed the audio path.
//
// Taken from the donor, not chosen. Its unmute path calls usleep with
// r0 = 0x000F4240, which is 1,000,000 microseconds, immediately before the two
// expander writes that clear the DAC and amplifier mute bits. Nothing else
// separates those writes: the delay is in front of the pair.
//
// This runtime had no delay and opened the outputs as soon as the DSP accepted
// a level, which is a gain change, which is a transient. The listener reported
// a loud pop at every start, and the original speaker did not do that.
const outputSettleDelay = time.Second

func (c *controller) OpenOutputs() error {
	// Outside the lock: nothing else touches these bits at startup, and
	// holding the bus for a second would block the heartbeat.
	c.sleep(outputSettleDelay)

	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.setDACMuteLocked(false); err != nil {
		return fmt.Errorf("unmute DAC: %w", err)
	}
	if c.logf != nil {
		c.logf("OUTPUTS_DAC_OPEN")
	}
	if err := c.setAmpMuteLocked(false); err != nil {
		return fmt.Errorf("unmute amplifier: %w", err)
	}
	// Logged either side of the amplifier, not once at the end. The listener
	// reports two sounds at startup, a click then a pop, and a single line
	// cannot tell which bit produced which.
	if c.logf != nil {
		c.logf("OUTPUTS_AMP_OPEN")
	}
	return nil
}

func (c *controller) setAmpMute(muted bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.setAmpMuteLocked(muted)
}

func (c *controller) setAmpMuteContext(
	ctx context.Context,
	muted bool,
) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	return c.setAmpMuteLocked(muted)
}

func (c *controller) setDACMute(muted bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.setDACMuteLocked(muted)
}

func (c *controller) setDACMuteContext(
	ctx context.Context,
	muted bool,
) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
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
	// Ordering is a hardware requirement: energising the amplifier while the
	// DAC output is still muted thumps the speaker. Satisfy it here rather
	// than refusing, so no caller has to know the order. Candidate 05.8.3
	// returned an error instead, which made a bare amplifier unmute look like
	// a failure when it was only out of sequence.
	if !muted && c.dacMuted {
		if err := c.writeDACMuteLocked(false); err != nil {
			return fmt.Errorf("unmute DAC before amplifier: %w", err)
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
	// The mirror of the unmute order: silence the amplifier before the DAC
	// stops driving it, for the same reason.
	if muted && !c.ampMuted {
		if err := c.writeAmpMuteLocked(true); err != nil {
			return fmt.Errorf("mute amplifier before DAC: %w", err)
		}
	}

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

func (c *controller) unmuteAllowedLocked() error {
	if !c.initialized {
		return errors.New("audio path is not initialized")
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
