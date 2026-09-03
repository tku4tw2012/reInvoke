// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

// The SPI link to the DSP: reset pulse, boot image download, and the queued
// message handshake recovered from the donor's msgproc.
//
// Every device access goes through the injected backends, so this file runs
// unchanged on the unit and on a build host.

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

const (
	// The DSP reset is bit 0 of output register 0x01 on the IO expander at
	// 0x20, the same expander the MCU service owns for the amplifier and DAC
	// mute bits. Both sides read-modify-write, so neither clobbers the other.
	expanderAddress = 0x20
	expanderOutput  = 0x01
	dspResetMask    = byte(0x01)

	// Programmed by the donor's dspopen.
	spiMode        = byte(3)
	spiBitsPerWord = byte(8)
	spiSpeedHz     = uint32(1000000)

	// The recorded frame path uses single byte transfers with a one
	// microsecond inter-transfer delay; the download stage uses four byte
	// transfers with no delay.
	frameDelayUsecs    = uint16(1)
	downloadDelayUsecs = uint16(0)

	resetHoldDelay    = 20 * time.Millisecond
	resetSettleDelay  = 10 * time.Millisecond
	downloadStageWait = 10 * time.Millisecond
	handshakeDelay    = time.Microsecond
	requestPulseDelay = 20 * time.Millisecond
	releaseDelay      = 2 * time.Microsecond
	readyPollInterval = 200 * time.Microsecond
	maxDevicePayload  = 64
	maxHeaderShifts   = 4
)

// pinout holds the five handshake lines. The roles are inferred from the order
// in which the donor reads and writes them, not from a schematic.
type pinout struct {
	Strobe     int // pulsed low then high before every transfer
	ChipSelect int // exported only for the image download, active low
	Ready      int // read, active low
	Active     int // framed around every transfer
	Busy       int // read before queuing a transmit
}

func defaultPinout() pinout {
	return pinout{Strobe: 4, ChipSelect: 5, Ready: 12, Active: 13, Busy: 15}
}

type linkStats struct {
	DownloadTransfers int
	DownloadBytes     int
	FramesSent        int
	FramesReceived    int
	FramesRejected    int
	FrameResyncs      int
}

type link struct {
	spi  spiBus
	gpio gpioLines
	i2c  i2cBus

	pins         pinout
	sleep        func(time.Duration)
	readyTimeout time.Duration

	mu    sync.Mutex
	queue [][]byte
	stats linkStats

	booted bool
	image  []byte
}

type linkOptions struct {
	Pins         pinout
	Sleep        func(time.Duration)
	ReadyTimeout time.Duration
}

func newLink(spi spiBus, gpio gpioLines, i2c i2cBus, options linkOptions) *link {
	sleep := options.Sleep
	if sleep == nil {
		sleep = time.Sleep
	}
	timeout := options.ReadyTimeout
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	return &link{
		spi:          spi,
		gpio:         gpio,
		i2c:          i2c,
		pins:         options.Pins,
		sleep:        sleep,
		readyTimeout: timeout,
	}
}

// Stats returns a snapshot of the transfer counters.
func (l *link) Stats() linkStats {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.stats
}

// setReset drives the DSP reset bit through a read-modify-write, leaving every
// other bit of the expander output register untouched.
func (l *link) setReset(asserted bool) error {
	if err := l.i2c.UpdateRegister(
		expanderAddress,
		expanderOutput,
		func(current byte) byte {
			if asserted {
				return current | dspResetMask
			}
			return current &^ dspResetMask
		},
	); err != nil {
		return fmt.Errorf("update IO expander: %w", err)
	}
	return nil
}

// pulseReset reproduces the recorded set, clear, wait, set, wait sequence.
func (l *link) pulseReset() error {
	if err := l.setReset(true); err != nil {
		return err
	}
	if err := l.setReset(false); err != nil {
		return err
	}
	l.sleep(resetHoldDelay)
	if err := l.setReset(true); err != nil {
		return err
	}
	l.sleep(resetSettleDelay)
	return nil
}

// Boot configures the bus, resets the DSP, streams the image, and leaves the
// handshake lines in their idle state.
func (l *link) Boot(image bootImage) error {
	return l.BootContext(context.Background(), image)
}

func (l *link) BootContext(ctx context.Context, image bootImage) error {
	if len(image.Stream) == 0 {
		return errors.New("boot image is empty")
	}
	if err := l.spi.Configure(
		spiMode,
		spiBitsPerWord,
		spiSpeedHz,
	); err != nil {
		return fmt.Errorf("configure SPI: %w", err)
	}
	if err := l.prepareDownloadLines(); err != nil {
		return err
	}
	if err := l.pulseReset(); err != nil {
		return fmt.Errorf("pulse DSP reset: %w", err)
	}
	if err := l.download(ctx, image.Stream); err != nil {
		return err
	}
	if err := l.prepareHandshake(); err != nil {
		return err
	}
	l.mu.Lock()
	l.booted = true
	l.image = append([]byte(nil), image.Stream...)
	l.mu.Unlock()
	return nil
}

func (l *link) prepareDownloadLines() error {
	for _, pin := range []int{
		l.pins.Strobe,
		l.pins.Active,
		l.pins.Ready,
		l.pins.Busy,
	} {
		if err := l.gpio.Export(pin); err != nil {
			return fmt.Errorf("export GPIO %d: %w", pin, err)
		}
	}
	if err := l.gpio.Direction(l.pins.Strobe, true); err != nil {
		return fmt.Errorf("configure strobe GPIO: %w", err)
	}
	for _, pin := range []int{l.pins.Active, l.pins.Ready, l.pins.Busy} {
		if err := l.gpio.Direction(pin, false); err != nil {
			return fmt.Errorf("configure GPIO %d as input: %w", pin, err)
		}
	}
	if err := l.gpio.Write(l.pins.Strobe, true); err != nil {
		return fmt.Errorf("raise strobe: %w", err)
	}
	for _, pin := range []int{l.pins.Active, l.pins.Ready, l.pins.Busy} {
		if _, err := l.gpio.Read(pin); err != nil {
			return fmt.Errorf("sample GPIO %d: %w", pin, err)
		}
	}
	return nil
}

// download streams the bit reversed image as four byte transfers with GPIO 5
// held low as an active-low chip select.
func (l *link) download(ctx context.Context, stream []byte) error {
	if err := l.gpio.Export(l.pins.ChipSelect); err != nil {
		return fmt.Errorf("export chip select: %w", err)
	}
	defer func() {
		_ = l.gpio.Write(l.pins.ChipSelect, true)
		_ = l.gpio.Unexport(l.pins.ChipSelect)
	}()

	if err := l.gpio.Direction(l.pins.ChipSelect, true); err != nil {
		return fmt.Errorf("configure chip select: %w", err)
	}
	if err := l.gpio.Write(l.pins.ChipSelect, true); err != nil {
		return fmt.Errorf("raise chip select: %w", err)
	}
	if err := l.gpio.Write(l.pins.ChipSelect, false); err != nil {
		return fmt.Errorf("assert chip select: %w", err)
	}

	for offset := 0; offset < len(stream); offset += imageChunkBytes {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		chunk := stream[offset : offset+imageChunkBytes]
		if err := l.spi.Transfer(
			chunk,
			nil,
			spiSpeedHz,
			downloadDelayUsecs,
		); err != nil {
			return fmt.Errorf("stream image at offset %d: %w", offset, err)
		}
		l.mu.Lock()
		l.stats.DownloadTransfers++
		l.stats.DownloadBytes += len(chunk)
		l.mu.Unlock()

		// The donor restores the saved bus speed here and pauses. The held
		// build holds one speed, so only the pause is observable.
		if offset+imageChunkBytes == speedRestoreOffset {
			if err := l.spi.Configure(
				spiMode,
				spiBitsPerWord,
				spiSpeedHz,
			); err != nil {
				return fmt.Errorf("restore SPI speed: %w", err)
			}
			l.sleep(downloadStageWait)
		}
	}
	return nil
}

// prepareHandshake exports and orients the four message-path lines.
func (l *link) prepareHandshake() error {
	if err := l.gpio.Direction(l.pins.Active, true); err != nil {
		return fmt.Errorf("configure transfer GPIO: %w", err)
	}
	return l.gpio.Write(l.pins.Active, false)
}

// Enqueue adds one message to the transmit ring.
func (l *link) Enqueue(id uint16, payload []byte) error {
	message, err := buildFrame(id, payload)
	if err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.booted {
		return errors.New("DSP link is not booted")
	}
	l.queue = append(l.queue, message)
	return nil
}

func (l *link) dequeue() []byte {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.queue) == 0 {
		return nil
	}
	message := l.queue[0]
	l.queue = l.queue[1:]
	return message
}

func (l *link) requeue(message []byte) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.queue = append([][]byte{message}, l.queue...)
}

// Poll runs one msgproc iteration. It reports whether it moved any traffic;
// the caller sleeps when it did not, exactly as Dsp_msg_process does.
func (l *link) Poll() (*frame, bool, error) {
	if err := l.gpio.Direction(l.pins.Active, true); err != nil {
		return nil, false, fmt.Errorf("configure transfer line: %w", err)
	}
	if err := l.gpio.Write(l.pins.Active, false); err != nil {
		return nil, false, fmt.Errorf("lower transfer line: %w", err)
	}

	if message := l.dequeue(); message != nil {
		busy, err := l.gpio.Read(l.pins.Busy)
		if err != nil {
			l.requeue(message)
			return nil, false, fmt.Errorf("read busy line: %w", err)
		}
		if busy {
			l.requeue(message)
			return nil, false, nil
		}
		if err := l.transmit(message); err != nil {
			return nil, false, err
		}
		return nil, true, nil
	}

	ready, err := l.gpio.Read(l.pins.Ready)
	if err != nil {
		return nil, false, fmt.Errorf("read ready line: %w", err)
	}
	if ready {
		return nil, false, nil
	}
	received, err := l.receive()
	if err != nil {
		return nil, true, err
	}
	return received, true, nil
}

func (l *link) transmit(message []byte) error {
	if err := l.gpio.Write(l.pins.Active, true); err != nil {
		return fmt.Errorf("raise transfer line: %w", err)
	}
	l.sleep(requestPulseDelay)
	if err := l.gpio.Write(l.pins.Active, false); err != nil {
		return l.finishTransfer(fmt.Errorf("lower transfer line: %w", err))
	}
	if err := l.waitReady(); err != nil {
		l.requeue(message)
		return l.finishTransfer(err)
	}
	if err := l.strobe(); err != nil {
		l.requeue(message)
		return l.finishTransfer(err)
	}
	if _, err := l.transferSerial(message, false); err != nil {
		return l.finishTransfer(fmt.Errorf("send frame: %w", err))
	}
	if err := l.finishTransfer(nil); err != nil {
		return err
	}

	l.mu.Lock()
	l.stats.FramesSent++
	l.mu.Unlock()
	return nil
}

func (l *link) receive() (*frame, error) {
	if err := l.strobe(); err != nil {
		return nil, err
	}
	header, length, err := l.readSynchronizedHeader()
	if err != nil {
		l.mu.Lock()
		l.stats.FramesRejected++
		l.mu.Unlock()
		return nil, l.finishTransfer(err)
	}
	body, err := l.transferSerial(make([]byte, devicePayloadRead(length)), true)
	if err != nil {
		return nil, l.finishTransfer(fmt.Errorf("read frame payload: %w", err))
	}
	if err := l.finishTransfer(nil); err != nil {
		return nil, err
	}

	decoded, err := decodeDeviceFrame(header, body)
	if err != nil {
		l.mu.Lock()
		l.stats.FramesRejected++
		l.mu.Unlock()
		return nil, fmt.Errorf("%w: header=%x body=%x", err, header, body)
	}
	l.mu.Lock()
	l.stats.FramesReceived++
	l.mu.Unlock()
	return &decoded, nil
}

func (l *link) readSynchronizedHeader() ([]byte, int, error) {
	header, err := l.transferSerial(make([]byte, 5), true)
	if err != nil {
		return nil, 0, fmt.Errorf("read frame header: %w", err)
	}
	for shift := 0; shift <= maxHeaderShifts; shift++ {
		id, length, parseErr := parseDeviceHeader(header)
		if parseErr == nil &&
			id <= messageIDTest &&
			length <= maxDevicePayload {
			if shift > 0 {
				l.mu.Lock()
				l.stats.FrameResyncs++
				l.mu.Unlock()
			}
			return header, length, nil
		}
		if shift == maxHeaderShifts {
			return nil, 0, fmt.Errorf(
				"device frame synchronization failed: header=%x",
				header,
			)
		}
		next, readErr := l.transferSerial(make([]byte, 1), true)
		if readErr != nil {
			return nil, 0, fmt.Errorf("resynchronize frame header: %w", readErr)
		}
		copy(header, header[1:])
		header[len(header)-1] = next[0]
	}
	return nil, 0, errors.New("device frame synchronization exhausted")
}

func (l *link) finishTransfer(cause error) error {
	l.sleep(releaseDelay)
	if err := l.gpio.Write(l.pins.Active, false); err != nil {
		if cause != nil {
			return fmt.Errorf("%v; release transfer line: %w", cause, err)
		}
		return fmt.Errorf("release transfer line: %w", err)
	}
	return cause
}

// strobe pulses the strobe line low then high and raises the transfer line,
// which is the last thing the donor does before every transfer.
func (l *link) strobe() error {
	if err := l.gpio.Write(l.pins.Strobe, false); err != nil {
		return fmt.Errorf("lower strobe: %w", err)
	}
	l.sleep(handshakeDelay)
	if err := l.gpio.Write(l.pins.Strobe, true); err != nil {
		return fmt.Errorf("raise strobe: %w", err)
	}
	if err := l.gpio.Write(l.pins.Active, true); err != nil {
		return fmt.Errorf("raise transfer line: %w", err)
	}
	l.sleep(handshakeDelay)
	return nil
}

func (l *link) waitReady() error {
	deadline := time.Now().Add(l.readyTimeout)
	for {
		ready, err := l.gpio.Read(l.pins.Ready)
		if err != nil {
			return fmt.Errorf("read ready line: %w", err)
		}
		if !ready {
			return nil
		}
		if !time.Now().Before(deadline) {
			l.mu.Lock()
			image := append([]byte(nil), l.image...)
			l.mu.Unlock()
			if len(image) > 0 {
				if err := l.download(context.Background(), image); err != nil {
					return fmt.Errorf(
						"DSP did not report ready; recovery download: %w",
						err,
					)
				}
			}
			return errors.New("DSP did not report ready after recovery download")
		}
		l.sleep(readyPollInterval)
	}
}

// transferSerial moves one frame a byte at a time, which is what the recorded
// ioctl trace shows the donor doing for every message-path byte.
func (l *link) transferSerial(buffer []byte, read bool) ([]byte, error) {
	out := make([]byte, len(buffer))
	for index := range buffer {
		tx := buffer[index : index+1]
		var rx []byte
		if read {
			rx = out[index : index+1]
			tx = nil
		}
		if err := l.spi.Transfer(
			tx,
			rx,
			spiSpeedHz,
			frameDelayUsecs,
		); err != nil {
			return nil, err
		}
	}
	if !read {
		copy(out, buffer)
	}
	return out, nil
}

// Close releases the message-path lines. It leaves the DSP reset bit alone so
// that it cannot fight the MCU service for the expander.
func (l *link) Close() error {
	activeErr := l.gpio.Write(l.pins.Active, false)
	for _, pin := range []int{
		l.pins.Strobe,
		l.pins.Active,
		l.pins.Ready,
		l.pins.Busy,
	} {
		_ = l.gpio.Unexport(pin)
	}
	spiErr := l.spi.Close()
	i2cErr := l.i2c.Close()
	if spiErr != nil {
		return spiErr
	}
	if activeErr != nil {
		return activeErr
	}
	return i2cErr
}

func isFrameError(err error) bool {
	return errors.Is(err, errFrameRejected) || errors.Is(err, errFrameChecksum)
}
