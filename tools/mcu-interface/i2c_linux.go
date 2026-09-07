// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const (
	i2cRead          = 0x0001
	i2cRDWR          = 0x0707
	expanderLockPath = "/run/reinvoke/expander.lock"
	i2cRetryCount    = 5
	i2cRetryDelay    = 20 * time.Millisecond
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
	mu       sync.Mutex
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

func (bus *linuxI2C) ReadRegister(
	address,
	register byte,
) (byte, error) {
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
			Flags:   i2cRead,
			Length:  uint16(len(value)),
			Buffer:  uintptr(unsafe.Pointer(&value[0])),
		},
	}
	if err := bus.transfer(messages); err != nil {
		return 0, err
	}
	runtime.KeepAlive(pointer)
	runtime.KeepAlive(value)
	return value[0], nil
}

func (bus *linuxI2C) WriteRegister(
	address,
	register,
	value byte,
) error {
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

func (bus *linuxI2C) ReadMCUEvent() ([6]byte, error) {
	var frame [6]byte
	messages := []i2cMessage{{
		Address: 0x36,
		Flags:   i2cRead,
		Length:  uint16(len(frame)),
		Buffer:  uintptr(unsafe.Pointer(&frame[0])),
	}}
	err := bus.transfer(messages)
	runtime.KeepAlive(frame)
	return frame, err
}

func (bus *linuxI2C) WriteMCUCommand(frame [6]byte) error {
	messages := []i2cMessage{{
		Address: 0x36,
		Length:  uint16(len(frame)),
		Buffer:  uintptr(unsafe.Pointer(&frame[0])),
	}}
	err := bus.transfer(messages)
	runtime.KeepAlive(frame)
	return err
}

func (bus *linuxI2C) WriteMCUData(data []byte) error {
	if len(data) == 0 || len(data) > 392 {
		return fmt.Errorf("MCU data length must be from 1 through 392")
	}
	messages := []i2cMessage{{
		Address: 0x36,
		Length:  uint16(len(data)),
		Buffer:  uintptr(unsafe.Pointer(&data[0])),
	}}
	err := bus.transfer(messages)
	runtime.KeepAlive(data)
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
	bus.mu.Lock()
	defer bus.mu.Unlock()

	for attempt := 0; attempt < i2cRetryCount; attempt++ {
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
		if errno == 0 {
			if result != uintptr(len(messages)) {
				return fmt.Errorf(
					"I2C_RDWR transferred %d of %d messages",
					result,
					len(messages),
				)
			}
			return nil
		}
		if errno != syscall.EAGAIN &&
			errno != syscall.EBUSY &&
			errno != syscall.EREMOTEIO &&
			errno != syscall.ENXIO &&
			errno != syscall.ETIMEDOUT {
			return fmt.Errorf("I2C_RDWR: %w", errno)
		}
		if attempt+1 < i2cRetryCount {
			time.Sleep(i2cRetryDelay)
		}
	}
	return errors.New("I2C_RDWR transient failure retries exhausted")
}
