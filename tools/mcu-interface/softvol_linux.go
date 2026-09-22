// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"sync"
	"syscall"
	"unsafe"
)

// In the donor, the speaker's user volume was an ALSA softvol control rather
// than DSP gain. It drove that control from aui::VolumeManager through
// add_softvol/fade_step and a periodic softvol_fading_tick, which is why its
// volume changes were smooth: a softvol write is an ioctl on an already-open
// descriptor, so a fade costs nothing. Candidate 05.8.9 attenuated with DSP
// gain instead and forked a process per rotary detent, which stuttered during
// playback. This runtime carries the level on the DSP byte; see
// dspByteForPercent in volume.go.
//
// The control is created by ALSA when the named PCM is first opened, so it
// exists only once the donor stack has opened "music". Verified on hardware:
// numid 97 on card 0, two channels, range 0..255 in 0.2 dB steps, and stepping
// it down during playback was audibly quieter in stages.
const (
	// snd_ctl_elem_value on 32-bit ARM: a 64 byte id, a bitfield padded to the
	// union's 8 byte alignment, a 512 byte union, a timespec and reserved tail.
	ctlIDSize      = 64
	ctlValueOffset = 72
	ctlStructSize  = 712

	ctlDirRead  = 2
	ctlDirWrite = 1
	// _IOWR('U', nr, struct snd_ctl_elem_value). The size is encoded rather
	// than pasted: a wrong one returns ENOTTY instead of a wrong answer.
	ctlElemRead  = (ctlDirRead|ctlDirWrite)<<30 | ctlStructSize<<16 | 'U'<<8 | 0x12
	ctlElemWrite = (ctlDirRead|ctlDirWrite)<<30 | ctlStructSize<<16 | 'U'<<8 | 0x13

	softvolMax = 255

	// The plugin's own range, confirmed against this hardware at numid 97 in
	// 0.2 dB steps: 255 steps of 0.2 dB is 51 dB.
	softvolRangeDB = 51.0

	// snd_ctl_elem_id field offsets. Addressing by name rather than numeric id
	// matters here: the control does not exist until the donor stack first
	// opens the named PCM, so its numid is assigned at runtime and is not a
	// stable thing to hard-code.
	ctlIDIfaceOffset = 4
	ctlIDNameOffset  = 16
	ctlIDNameMax     = 44
	ctlIfaceMixer    = 2
)

// softvolControl writes one ALSA mixer control addressed by name.
type softvolControl struct {
	mu   sync.Mutex
	file *os.File
	name string
}

func openSoftvol(card int, name string) (*softvolControl, error) {
	if name == "" || len(name) >= ctlIDNameMax {
		return nil, fmt.Errorf("softvol name must be 1..%d bytes", ctlIDNameMax-1)
	}
	path := fmt.Sprintf("/dev/snd/controlC%d", card)
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	return &softvolControl{file: file, name: name}, nil
}

func (c *softvolControl) Close() error { return c.file.Close() }

func (c *softvolControl) elem(buffer []byte, request uintptr) error {
	// numid stays zero so the kernel resolves the element by interface, name
	// and index instead.
	binary.LittleEndian.PutUint32(buffer[ctlIDIfaceOffset:], ctlIfaceMixer)
	copy(buffer[ctlIDNameOffset:ctlIDNameOffset+ctlIDNameMax], c.name)
	_, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		c.file.Fd(),
		request,
		uintptr(unsafe.Pointer(&buffer[0])),
	)
	if errno != 0 {
		return errno
	}
	return nil
}

// Read reports the first channel's value.
func (c *softvolControl) Read() (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	buffer := make([]byte, ctlStructSize)
	if err := c.elem(buffer, ctlElemRead); err != nil {
		return 0, fmt.Errorf("read softvol: %w", err)
	}
	return int(binary.LittleEndian.Uint32(buffer[ctlValueOffset:])), nil
}

// Write sets every channel to the same value.
func (c *softvolControl) Write(value int) error {
	if value < 0 || value > softvolMax {
		return fmt.Errorf("softvol %d is outside 0..%d", value, softvolMax)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	buffer := make([]byte, ctlStructSize)
	// Read first so channels this build does not set keep their value.
	if err := c.elem(buffer, ctlElemRead); err != nil {
		return fmt.Errorf("read softvol before write: %w", err)
	}
	binary.LittleEndian.PutUint32(buffer[ctlValueOffset:], uint32(value))
	binary.LittleEndian.PutUint32(buffer[ctlValueOffset+4:], uint32(value))
	if err := c.elem(buffer, ctlElemWrite); err != nil {
		return fmt.Errorf("write softvol: %w", err)
	}
	return nil
}

// softvolForPercent fills in the gap between two DSP gain bytes.
//
// It does not carry the user's level. The donor's design did, mapping percent
// straight onto this control, and that cannot work here: the level measured
// as comfortable on this unit is DSP gain 5 with this control at 255, so
// putting the dial on the control would need the DSP to make up 33.6 dB at
// the default position, which is gain 239 against a measured loud point of
// 90. The arithmetic is in docs/current-product-contract.md.
//
// What this control is good for is resolution. The DSP byte is coarse where
// it is actually used: 5 to 6 is 1.58 dB and 4 to 5 is 1.94 dB, so a single
// step is a clearly audible jump. This control moves in 0.2 dB, about eight
// steps for one DSP step.
//
// So the DSP byte is chosen to be at or just above the level the dial asks
// for, and this attenuates the small remainder. At the default dial the two
// agree exactly and this returns 255, which is what was measured.
func softvolForPercent(percent int) int {
	if percent <= 0 {
		return 0
	}
	wanted := dialAmplitude(percent) * dspMaxByte
	byteSent := float64(dspByteForPercent(percent))
	if byteSent <= 0 || wanted <= 0 {
		return softvolMax
	}
	// The DSP rounds, so it can land slightly below what was asked for. There
	// is no headroom above 0 dB here, so in that case leave the control open
	// and accept the DSP's own value.
	if byteSent <= wanted {
		return softvolMax
	}
	trimDB := 20 * math.Log10(wanted/byteSent)
	if trimDB < -softvolRangeDB {
		trimDB = -softvolRangeDB
	}
	value := int(math.Round((softvolRangeDB + trimDB) * softvolMax / softvolRangeDB))
	if value < 0 {
		value = 0
	}
	if value > softvolMax {
		value = softvolMax
	}
	return value
}
