// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

//go:build linux

package main

// Raw I2C_RDWR backend for the IO expander that carries the DSP reset bit.
// Only register 0x01 bit 0 is ever touched, always read-modify-write, so the
// amplifier and DAC mute bits owned by the MCU service are preserved.

import (
	"fmt"
	"os"
	"runtime"
	"syscall"
	"unsafe"
)

const (
	i2cReadFlag      = 0x0001
	i2cRDWR          = 0x0707
	expanderLockPath = "/run/reinvoke/expander.lock"
)

type i2cMessage struct {
	Address uint16
	Flags   uint16
	Length  uint16
	Buffer  uintptr
}

type i2cTransfer struct {
	Messages *i2cMessage
	Count    uint32
}

type linuxI2C struct {
	file     *os.File
	lockFile *os.File
}

func openLinuxI2C(path string) (*linuxI2C, error) {
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("open I2C bus: %w", err)
	}
	lockFile, err := os.OpenFile(
		expanderLockPath,
		os.O_CREATE|os.O_RDWR,
		0o600,
	)
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("open expander lock: %w", err)
	}
	return &linuxI2C{file: file, lockFile: lockFile}, nil
}

func (bus *linuxI2C) Close() error {
	lockErr := bus.lockFile.Close()
	busErr := bus.file.Close()
	if lockErr != nil {
		return lockErr
	}
	return busErr
}

func (bus *linuxI2C) ReadRegister(address, register byte) (byte, error) {
	pointer := []byte{register}
	value := []byte{0}
	messages := []i2cMessage{
		{
			Address: uint16(address),
			Length:  uint16(len(pointer)),
			Buffer:  uintptr(unsafe.Pointer(&pointer[0])),
		},
		{
			Address: uint16(address),
			Flags:   i2cReadFlag,
			Length:  uint16(len(value)),
			Buffer:  uintptr(unsafe.Pointer(&value[0])),
		},
	}
	err := bus.transfer(messages)
	runtime.KeepAlive(pointer)
	runtime.KeepAlive(value)
	if err != nil {
		return 0, err
	}
	return value[0], nil
}

func (bus *linuxI2C) WriteRegister(address, register, value byte) error {
	buffer := []byte{register, value}
	messages := []i2cMessage{{
		Address: uint16(address),
		Length:  uint16(len(buffer)),
		Buffer:  uintptr(unsafe.Pointer(&buffer[0])),
	}}
	err := bus.transfer(messages)
	runtime.KeepAlive(buffer)
	return err
}

func (bus *linuxI2C) UpdateRegister(
	address,
	register byte,
	update func(byte) byte,
) error {
	if err := syscall.Flock(int(bus.lockFile.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("lock expander: %w", err)
	}
	defer syscall.Flock(int(bus.lockFile.Fd()), syscall.LOCK_UN)

	current, err := bus.ReadRegister(address, register)
	if err != nil {
		return err
	}
	return bus.WriteRegister(address, register, update(current))
}

func (bus *linuxI2C) transfer(messages []i2cMessage) error {
	transfer := i2cTransfer{
		Messages: &messages[0],
		Count:    uint32(len(messages)),
	}
	result, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		bus.file.Fd(),
		i2cRDWR,
		uintptr(unsafe.Pointer(&transfer)),
	)
	runtime.KeepAlive(messages)
	if errno != 0 {
		return fmt.Errorf("I2C_RDWR: %w", errno)
	}
	if result != uintptr(len(messages)) {
		return fmt.Errorf(
			"I2C_RDWR transferred %d of %d messages",
			result,
			len(messages),
		)
	}
	return nil
}
