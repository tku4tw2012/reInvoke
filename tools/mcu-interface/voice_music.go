// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"time"
)

type stereoSoftvol interface {
	ReadChannels() ([2]int, error)
	CompareAndSwapChannels([2]int, [2]int) (bool, error)
	Close() error
}

// voiceMusicDuck wraps only card 0's existing "music" control. Dial writes
// still own the normal trim; during voice they update the restoration level
// instead of fighting the duck. Nothing here calls the master DSP volume.
type voiceMusicDuck struct {
	mu       sync.Mutex
	control  stereoSoftvol
	open     func() (stereoSoftvol, error)
	playing  func() bool
	ownsFile bool
	wanted   bool
	held     bool
	before   [2]int
	after    [2]int
}

func voiceMusicLevel(value int) int {
	// The donor's depths are not established. Reuse the project's named soft
	// ratio as amplitude attenuation on the known 0.2 dB music control.
	steps := int(math.Round(20 * math.Log10(float64(duckScale[duckSoft])/100) *
		softvolMax / softvolRangeDB))
	value += steps
	if value < 0 {
		return 0
	}
	return value
}

func (duck *voiceMusicDuck) SetActive(active bool) error {
	if duck == nil {
		return nil
	}
	duck.mu.Lock()
	defer duck.mu.Unlock()
	duck.wanted = active
	if !active && !duck.held {
		return nil
	}
	if active && !duck.held && (duck.playing == nil || !duck.playing()) {
		return nil
	}
	if duck.control == nil {
		if duck.open == nil {
			return errors.New("music softvol is unavailable")
		}
		control, err := duck.open()
		if err != nil {
			return err
		}
		duck.control, duck.ownsFile = control, true
	}
	current, err := duck.control.ReadChannels()
	if err != nil {
		return err
	}
	if !active {
		if current != duck.after {
			duck.held = false
			return nil
		}
		changed, err := duck.control.CompareAndSwapChannels(current, duck.before)
		if err != nil {
			return err
		}
		duck.held = false
		if !changed {
			return errors.New("music softvol changed during voice restore; leaving it untouched")
		}
		return nil
	}
	if duck.held && current == duck.after {
		return nil
	}
	return duck.applyLocked(current, current)
}

func (duck *voiceMusicDuck) applyLocked(current, base [2]int) error {
	target := [2]int{voiceMusicLevel(base[0]), voiceMusicLevel(base[1])}
	// Keep the receipt even on an ioctl error: a failed write might have
	// reached the control, and cleanup must still compare before restoring.
	duck.before, duck.after, duck.held = base, target, true
	changed, err := duck.control.CompareAndSwapChannels(current, target)
	if err != nil {
		return err
	}
	if !changed {
		duck.held = false
		return errors.New("music softvol changed during voice duck; retrying")
	}
	return nil
}

func (duck *voiceMusicDuck) Read() (int, error) {
	duck.mu.Lock()
	defer duck.mu.Unlock()
	if duck.control == nil {
		return 0, errors.New("music softvol is unavailable")
	}
	current, err := duck.control.ReadChannels()
	if err != nil {
		return 0, err
	}
	if duck.held && current == duck.after {
		return duck.before[0], nil
	}
	duck.held = false
	return current[0], nil
}

func (duck *voiceMusicDuck) Write(value int) error {
	if value < 0 || value > softvolMax {
		return fmt.Errorf("softvol %d is outside 0..%d", value, softvolMax)
	}
	duck.mu.Lock()
	defer duck.mu.Unlock()
	if duck.control == nil {
		return errors.New("music softvol is unavailable")
	}
	current, err := duck.control.ReadChannels()
	if err != nil {
		return err
	}
	target := [2]int{value, value}
	if duck.wanted && duck.playing != nil && duck.playing() {
		return duck.applyLocked(current, target)
	}
	changed, err := duck.control.CompareAndSwapChannels(current, target)
	if err != nil {
		return err
	}
	duck.held = false
	if !changed {
		return errors.New("music softvol changed during dial write")
	}
	return nil
}

func (duck *voiceMusicDuck) Close() error {
	var restoreErr error
	for attempt := 0; attempt < 3; attempt++ {
		restoreErr = duck.SetActive(false)
		if restoreErr == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	duck.mu.Lock()
	defer duck.mu.Unlock()
	if duck.ownsFile && duck.control != nil {
		err := duck.control.Close()
		duck.control = nil
		if restoreErr == nil {
			restoreErr = err
		}
	}
	return restoreErr
}
