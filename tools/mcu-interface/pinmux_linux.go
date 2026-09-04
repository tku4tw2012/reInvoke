// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"
	"runtime"
	"syscall"
	"unsafe"
)

const (
	mcuPinmuxRegister  = int64(0xf7ea8008)
	mcuPinmuxGPIO3Mask = uint32(0x00200000)
)

func applyRegisterMasks(before, setMask, clearMask uint32) uint32 {
	return (before | setMask) &^ clearMask
}

func updateMappedRegister32(
	file *os.File,
	offset int64,
	setMask,
	clearMask uint32,
) error {
	pageSize := int64(os.Getpagesize())
	pageOffset := offset &^ (pageSize - 1)
	registerOffset := int(offset - pageOffset)
	mapping, err := syscall.Mmap(
		int(file.Fd()),
		pageOffset,
		int(pageSize),
		syscall.PROT_READ|syscall.PROT_WRITE,
		syscall.MAP_SHARED,
	)
	if err != nil {
		return fmt.Errorf("map register %#x: %w", offset, err)
	}
	register := (*uint32)(unsafe.Pointer(&mapping[registerOffset]))
	before := *register
	after := applyRegisterMasks(before, setMask, clearMask)
	if after != before {
		*register = after
	}
	actual := *register
	runtime.KeepAlive(mapping)
	if err := syscall.Munmap(mapping); err != nil {
		return fmt.Errorf("unmap register %#x: %w", offset, err)
	}
	if actual != after {
		return fmt.Errorf(
			"verify register %#x: got %#08x, want %#08x",
			offset,
			actual,
			after,
		)
	}
	return nil
}

func configureMCUInterruptPin(path string) error {
	file, err := os.OpenFile(path, os.O_RDWR|syscall.O_SYNC, 0)
	if err != nil {
		return fmt.Errorf("open physical register device: %w", err)
	}
	if err := updateMappedRegister32(
		file,
		mcuPinmuxRegister,
		mcuPinmuxGPIO3Mask,
		0,
	); err != nil {
		_ = file.Close()
		return fmt.Errorf("configure MCU GPIO3 pinmux: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close physical register device: %w", err)
	}
	return nil
}
