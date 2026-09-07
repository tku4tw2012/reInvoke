// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
)

const maxMCUStartupReads = 8

type mcuStartupBus interface {
	WriteMCUCommand([6]byte) error
	ReadMCUEvent() ([6]byte, error)
}

func initializeMCUProtocolOnce(bus mcuStartupBus, statePath string) error {
	if statePath == "" {
		return errors.New("MCU protocol state path is required")
	}
	info, err := os.Lstat(statePath)
	if err == nil {
		if !info.Mode().IsRegular() {
			return errors.New("MCU protocol state is not a regular file")
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect MCU protocol state: %w", err)
	}
	if err := initializeMCUProtocol(bus); err != nil {
		return err
	}
	file, err := os.OpenFile(
		statePath,
		os.O_CREATE|os.O_EXCL|os.O_WRONLY,
		0o600,
	)
	if err != nil {
		return fmt.Errorf("record MCU protocol state: %w", err)
	}
	return file.Close()
}

func initializeMCUProtocol(bus mcuStartupBus) error {
	if err := bus.WriteMCUCommand([6]byte{0x01, 0xa6, 0x14}); err != nil {
		return fmt.Errorf("request MCU version: %w", err)
	}
	if err := bus.WriteMCUCommand([6]byte{0x23, 0, 0, 0, 0x2c, 0xb8}); err != nil {
		return fmt.Errorf("send MCU startup handshake: %w", err)
	}
	responses, err := readMCUStartupResponses(bus, 0x01, 0x23)
	if err != nil {
		return err
	}
	version := responses[0x01]
	if !bytes.Equal(version[:4], []byte{0x01, 0x01, 0x00, 0x01}) {
		return fmt.Errorf("unexpected MCU version response: %x", version)
	}
	handshake := responses[0x23]
	if handshake[0] != 0x23 {
		return fmt.Errorf("unexpected MCU startup handshake: %x", handshake)
	}

	if err := bus.WriteMCUCommand([6]byte{0x25, 0xa6, 0x14}); err != nil {
		return fmt.Errorf("complete MCU version exchange: %w", err)
	}
	if err := bus.WriteMCUCommand([6]byte{0x26}); err != nil {
		return fmt.Errorf("request MCU recovery flag: %w", err)
	}
	responses, err = readMCUStartupResponses(bus, 0x26)
	if err != nil {
		return err
	}
	recovery := responses[0x26]
	if recovery[0] != 0x26 {
		return fmt.Errorf("unexpected MCU recovery response: %x", recovery)
	}
	return nil
}

func readMCUStartupResponses(
	bus mcuStartupBus,
	opcodes ...byte,
) (map[byte][6]byte, error) {
	wanted := make(map[byte]bool, len(opcodes))
	for _, opcode := range opcodes {
		wanted[opcode] = true
	}
	responses := make(map[byte][6]byte, len(opcodes))
	for attempt := 0; attempt < maxMCUStartupReads; attempt++ {
		frame, err := bus.ReadMCUEvent()
		if err != nil {
			return nil, fmt.Errorf("read MCU startup response: %w", err)
		}
		if wanted[frame[0]] {
			responses[frame[0]] = frame
			if len(responses) == len(wanted) {
				return responses, nil
			}
		}
	}
	return nil, errors.New("MCU startup responses were incomplete")
}
