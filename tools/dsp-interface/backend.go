// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

// Injectable hardware backends.
//
// Everything the replacement does to the DSP goes through one of these three
// interfaces, so the link logic runs unchanged against real device nodes on
// the unit and against the in-memory backends below on a build host. None of
// the implementations here writes persistent storage.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"sync"
)

type spiBus interface {
	// Configure programs mode, bits per word, and maximum speed, then reads
	// each value back the way the donor does.
	Configure(mode byte, bitsPerWord byte, speedHz uint32) error

	// Transfer runs one full duplex SPI_IOC_MESSAGE(1). Either buffer may be
	// nil, and both must have the same length when both are supplied.
	Transfer(tx, rx []byte, speedHz uint32, delayUsecs uint16) error

	Close() error
}

type gpioLines interface {
	Export(pin int) error
	Unexport(pin int) error
	Direction(pin int, output bool) error
	Write(pin int, high bool) error
	Read(pin int) (bool, error)
}

type i2cBus interface {
	ReadRegister(address, register byte) (byte, error)
	WriteRegister(address, register, value byte) error
	UpdateRegister(address, register byte, update func(byte) byte) error
	Close() error
}

// spiTransfer is one recorded transfer.
type spiTransfer struct {
	Length     int
	SpeedHz    uint32
	DelayUsecs uint16
	TX         []byte
}

// memorySPI records every transfer and can answer reads from a script. It
// keeps a rolling digest instead of the whole image so that a full download
// costs no more memory than the image already loaded.
type memorySPI struct {
	mu sync.Mutex

	// Mode, Bits, and Speed hold the last configured values.
	Mode  byte
	Bits  byte
	Speed uint32

	// Transfers counts every transfer; Bytes counts transmitted bytes.
	Transfers int
	Bytes     int

	// Chunked counts transfers of exactly four bytes, which is the download
	// stage, and Serial counts single byte transfers, which is the frame path.
	Chunked int
	Serial  int

	// Recorded keeps whole transfers when KeepTransfers is set.
	Recorded      []spiTransfer
	KeepTransfers bool

	// Responses is consumed one byte per received byte. Reads past its end
	// return zero.
	Responses []byte

	digest hash.Hash
}

func newMemorySPI() *memorySPI {
	return &memorySPI{digest: sha256.New()}
}

func (bus *memorySPI) Configure(mode byte, bitsPerWord byte, speedHz uint32) error {
	bus.mu.Lock()
	defer bus.mu.Unlock()
	bus.Mode = mode
	bus.Bits = bitsPerWord
	bus.Speed = speedHz
	return nil
}

func (bus *memorySPI) Transfer(
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

	bus.mu.Lock()
	defer bus.mu.Unlock()
	bus.Transfers++
	bus.Bytes += length
	switch length {
	case imageChunkBytes:
		bus.Chunked++
	case 1:
		bus.Serial++
	}
	if len(tx) > 0 {
		bus.digest.Write(tx)
	}
	if bus.KeepTransfers {
		recorded := spiTransfer{
			Length:     length,
			SpeedHz:    speedHz,
			DelayUsecs: delayUsecs,
		}
		if len(tx) > 0 {
			recorded.TX = append([]byte(nil), tx...)
		}
		bus.Recorded = append(bus.Recorded, recorded)
	}
	for index := range rx {
		if len(bus.Responses) == 0 {
			rx[index] = 0
			continue
		}
		rx[index] = bus.Responses[0]
		bus.Responses = bus.Responses[1:]
	}
	return nil
}

func (bus *memorySPI) Close() error { return nil }

// StreamSHA256 is the digest of every byte transmitted so far.
func (bus *memorySPI) StreamSHA256() string {
	bus.mu.Lock()
	defer bus.mu.Unlock()
	return hex.EncodeToString(bus.digest.Sum(nil))
}

// Queue appends bytes the next reads will return.
func (bus *memorySPI) Queue(response []byte) {
	bus.mu.Lock()
	defer bus.mu.Unlock()
	bus.Responses = append(bus.Responses, response...)
}

// Counters returns the transfer statistics as one snapshot.
func (bus *memorySPI) Counters() (transfers, bytes, chunked, serial int) {
	bus.mu.Lock()
	defer bus.mu.Unlock()
	return bus.Transfers, bus.Bytes, bus.Chunked, bus.Serial
}

// memoryGPIO models sysfs lines. Reads default to high, which is the idle
// state of both the active-low ready line and the busy line.
type memoryGPIO struct {
	mu sync.Mutex

	values   map[int]bool
	exported map[int]bool
	outputs  map[int]bool

	// Log records every operation as "export 5", "out 13", "write 4=0", or
	// "read 12".
	Log []string

	// OnRead can override the value of a line, which is how a test drives the
	// ready and busy handshake.
	OnRead func(pin int, current bool) bool
}

func newMemoryGPIO() *memoryGPIO {
	return &memoryGPIO{
		values:   map[int]bool{},
		exported: map[int]bool{},
		outputs:  map[int]bool{},
	}
}

func (lines *memoryGPIO) Export(pin int) error {
	lines.mu.Lock()
	defer lines.mu.Unlock()
	lines.exported[pin] = true
	lines.Log = append(lines.Log, fmt.Sprintf("export %d", pin))
	return nil
}

func (lines *memoryGPIO) Unexport(pin int) error {
	lines.mu.Lock()
	defer lines.mu.Unlock()
	delete(lines.exported, pin)
	lines.Log = append(lines.Log, fmt.Sprintf("unexport %d", pin))
	return nil
}

func (lines *memoryGPIO) Direction(pin int, output bool) error {
	lines.mu.Lock()
	defer lines.mu.Unlock()
	lines.outputs[pin] = output
	direction := "in"
	if output {
		direction = "out"
	}
	lines.Log = append(lines.Log, fmt.Sprintf("%s %d", direction, pin))
	return nil
}

func (lines *memoryGPIO) Write(pin int, high bool) error {
	lines.mu.Lock()
	defer lines.mu.Unlock()
	lines.values[pin] = high
	value := 0
	if high {
		value = 1
	}
	lines.Log = append(lines.Log, fmt.Sprintf("write %d=%d", pin, value))
	return nil
}

func (lines *memoryGPIO) Read(pin int) (bool, error) {
	lines.mu.Lock()
	value, known := lines.values[pin]
	if !known {
		value = true
	}
	hook := lines.OnRead
	lines.Log = append(lines.Log, fmt.Sprintf("read %d", pin))
	lines.mu.Unlock()

	if hook != nil {
		value = hook(pin, value)
	}
	return value, nil
}

// Operations returns a copy of the log.
func (lines *memoryGPIO) Operations() []string {
	lines.mu.Lock()
	defer lines.mu.Unlock()
	return append([]string(nil), lines.Log...)
}

// memoryI2C models the expander at 0x20. Register 0x01 starts at 0xfb, which
// is the value the donor read back on the unit.
type memoryI2C struct {
	mu sync.Mutex

	registers map[[2]byte]byte

	// Log records every access as "read 20:01=fb" or "write 20:01=fa".
	Log []string
}

func newMemoryI2C() *memoryI2C {
	return &memoryI2C{
		registers: map[[2]byte]byte{
			{expanderAddress, expanderOutput}: 0xfb,
		},
	}
}

func (bus *memoryI2C) ReadRegister(address, register byte) (byte, error) {
	bus.mu.Lock()
	defer bus.mu.Unlock()
	value := bus.registers[[2]byte{address, register}]
	bus.Log = append(
		bus.Log,
		fmt.Sprintf("read %02x:%02x=%02x", address, register, value),
	)
	return value, nil
}

func (bus *memoryI2C) WriteRegister(address, register, value byte) error {
	bus.mu.Lock()
	defer bus.mu.Unlock()
	bus.registers[[2]byte{address, register}] = value
	bus.Log = append(
		bus.Log,
		fmt.Sprintf("write %02x:%02x=%02x", address, register, value),
	)
	return nil
}

func (bus *memoryI2C) UpdateRegister(
	address,
	register byte,
	update func(byte) byte,
) error {
	bus.mu.Lock()
	defer bus.mu.Unlock()
	key := [2]byte{address, register}
	current := bus.registers[key]
	bus.Log = append(
		bus.Log,
		fmt.Sprintf("read %02x:%02x=%02x", address, register, current),
	)
	value := update(current)
	bus.registers[key] = value
	bus.Log = append(
		bus.Log,
		fmt.Sprintf("write %02x:%02x=%02x", address, register, value),
	)
	return nil
}

func (bus *memoryI2C) Close() error { return nil }

// Operations returns a copy of the log.
func (bus *memoryI2C) Operations() []string {
	bus.mu.Lock()
	defer bus.mu.Unlock()
	return append([]string(nil), bus.Log...)
}
