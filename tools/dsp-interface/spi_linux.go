// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

//go:build linux

package main

// spidev backend. It programs the same mode, word size, and speed the donor's
// dspopen programs, and runs one SPI_IOC_MESSAGE(1) per transfer.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

const (
	spiIOCWRMode      = 0x40016b01
	spiIOCRDMode      = 0x80016b01
	spiIOCWRBits      = 0x40016b03
	spiIOCRDBits      = 0x80016b03
	spiIOCWRSpeed     = 0x40046b04
	spiIOCRDSpeed     = 0x80046b04
	spiIOCWriteDirect = 0x40000000
	spiIOCMagic       = 'k'
	dspLockPath       = "/run/reinvoke/dsp-interface.lock"
)

// spiIOCTransfer mirrors struct spi_ioc_transfer, which is 32 bytes.
type spiIOCTransfer struct {
	TXBuffer       uint64
	RXBuffer       uint64
	Length         uint32
	SpeedHz        uint32
	DelayUsecs     uint16
	BitsPerWord    uint8
	CSChange       uint8
	TXNBits        uint8
	RXNBits        uint8
	WordDelayUsecs uint16
}

// spiIOCMessage builds SPI_IOC_MESSAGE(count) the way the _IOW macro does.
func spiIOCMessage(count int) uintptr {
	size := uintptr(count) * unsafe.Sizeof(spiIOCTransfer{})
	return spiIOCWriteDirect | size<<16 | uintptr(spiIOCMagic)<<8
}

type linuxSPI struct {
	file     *os.File
	lockFile *os.File
}

func openLinuxSPI(path string) (*linuxSPI, error) {
	lockFile, err := os.OpenFile(dspLockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open DSP interface lock: %w", err)
	}
	if err := syscall.Flock(
		int(lockFile.Fd()),
		syscall.LOCK_EX|syscall.LOCK_NB,
	); err != nil {
		_ = lockFile.Close()
		return nil, fmt.Errorf("lock DSP interface: %w", err)
	}
	if err := ensureDeviceUnused(path); err != nil {
		_ = lockFile.Close()
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		_ = lockFile.Close()
		return nil, fmt.Errorf("open SPI device: %w", err)
	}
	return &linuxSPI{file: file, lockFile: lockFile}, nil
}

func (bus *linuxSPI) Close() error {
	spiErr := bus.file.Close()
	lockErr := bus.lockFile.Close()
	if spiErr != nil {
		return spiErr
	}
	return lockErr
}

func ensureDeviceUnused(path string) error {
	target, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("inspect SPI device: %w", err)
	}
	targetStatus, ok := target.Sys().(*syscall.Stat_t)
	if !ok || target.Mode()&os.ModeCharDevice == 0 {
		return errors.New("SPI path is not a character device")
	}
	descriptors, err := filepath.Glob("/proc/[0-9]*/fd/*")
	if err != nil {
		return fmt.Errorf("enumerate process descriptors: %w", err)
	}
	for _, descriptor := range descriptors {
		parts := strings.Split(descriptor, "/")
		if len(parts) < 4 {
			continue
		}
		pid, err := strconv.Atoi(parts[2])
		if err != nil || pid == os.Getpid() {
			continue
		}
		info, err := os.Stat(descriptor)
		if err != nil || info.Mode()&os.ModeCharDevice == 0 {
			continue
		}
		status, ok := info.Sys().(*syscall.Stat_t)
		if ok && status.Rdev == targetStatus.Rdev {
			return fmt.Errorf("SPI device is already open by process %d", pid)
		}
	}
	return nil
}

// Configure writes then reads back each parameter, as the donor does.
func (bus *linuxSPI) Configure(
	mode byte,
	bitsPerWord byte,
	speedHz uint32,
) error {
	if err := bus.setByte("mode", spiIOCWRMode, spiIOCRDMode, mode); err != nil {
		return err
	}
	if err := bus.setByte(
		"bits per word",
		spiIOCWRBits,
		spiIOCRDBits,
		bitsPerWord,
	); err != nil {
		return err
	}
	return bus.setWord("speed", spiIOCWRSpeed, spiIOCRDSpeed, speedHz)
}

func (bus *linuxSPI) setByte(
	name string,
	write, read uintptr,
	value byte,
) error {
	stored := value
	if err := bus.ioctl(write, unsafe.Pointer(&stored)); err != nil {
		return fmt.Errorf("set SPI %s: %w", name, err)
	}
	err := bus.ioctl(read, unsafe.Pointer(&stored))
	runtime.KeepAlive(stored)
	if err != nil {
		return fmt.Errorf("read SPI %s: %w", name, err)
	}
	return nil
}

func (bus *linuxSPI) setWord(
	name string,
	write, read uintptr,
	value uint32,
) error {
	stored := value
	if err := bus.ioctl(write, unsafe.Pointer(&stored)); err != nil {
		return fmt.Errorf("set SPI %s: %w", name, err)
	}
	err := bus.ioctl(read, unsafe.Pointer(&stored))
	runtime.KeepAlive(stored)
	if err != nil {
		return fmt.Errorf("read SPI %s: %w", name, err)
	}
	return nil
}

func (bus *linuxSPI) Transfer(
	tx, rx []byte,
	speedHz uint32,
	delayUsecs uint16,
) error {
	length := len(tx)
	if length == 0 {
		length = len(rx)
	}
	if len(tx) != 0 && len(rx) != 0 && len(tx) != len(rx) {
		return fmt.Errorf("SPI buffers differ in length")
	}
	if length == 0 {
		return nil
	}

	message := spiIOCTransfer{
		Length:      uint32(length),
		SpeedHz:     speedHz,
		DelayUsecs:  delayUsecs,
		BitsPerWord: spiBitsPerWord,
	}
	if len(tx) > 0 {
		message.TXBuffer = uint64(uintptr(unsafe.Pointer(&tx[0])))
	}
	if len(rx) > 0 {
		message.RXBuffer = uint64(uintptr(unsafe.Pointer(&rx[0])))
	}
	err := bus.ioctl(spiIOCMessage(1), unsafe.Pointer(&message))
	runtime.KeepAlive(tx)
	runtime.KeepAlive(rx)
	if err != nil {
		return fmt.Errorf("SPI_IOC_MESSAGE: %w", err)
	}
	return nil
}

func (bus *linuxSPI) ioctl(request uintptr, argument unsafe.Pointer) error {
	_, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		bus.file.Fd(),
		request,
		uintptr(argument),
	)
	if errno != 0 {
		return errno
	}
	return nil
}
