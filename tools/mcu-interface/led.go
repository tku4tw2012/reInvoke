// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	ledFrameBytes     = 13
	ledChunkBytes     = 390
	ledOffFrameCount  = 3
	ledChunkDelay     = 280 * time.Millisecond
	maxLEDAssetBytes  = 1024 * 1024
	ledAnimationCode  = byte(0x0e)
	ledFirstChunkFlag = byte(0x01)
	micPrivacyLEDName = "L_108_c_error"
)

type ledWriter interface {
	WriteMCUData([]byte) error
}

type ledPlayer struct {
	directory string
	writer    ledWriter
	logf      func(string, ...interface{})

	mu           sync.Mutex
	cancel       context.CancelFunc
	done         chan struct{}
	privacyMuted bool
}

func (player *ledPlayer) Apply(
	ctx context.Context,
	event inputEvent,
) error {
	switch event.Name {
	case "action":
		return player.Start(ctx, "L_312_d_shorttap", false)
	case "bluetooth-long":
		return player.Start(ctx, "L_302_d_wifisetup", false)
	default:
		return nil
	}
}

func (player *ledPlayer) Start(
	parent context.Context,
	name string,
	repeat bool,
) error {
	return player.start(parent, name, repeat, false)
}

func (player *ledPlayer) start(
	parent context.Context,
	name string,
	repeat bool,
	force bool,
) error {
	if !validLEDName(name) {
		return errors.New("invalid LED animation name")
	}
	player.mu.Lock()
	blocked := player.privacyMuted && !force
	player.mu.Unlock()
	if blocked {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(player.directory, name+".bin"))
	if err != nil {
		return fmt.Errorf("read LED animation: %w", err)
	}
	if len(data) == 0 || len(data) > maxLEDAssetBytes ||
		len(data)%ledFrameBytes != 0 {
		return errors.New("invalid LED animation length")
	}

	player.mu.Lock()
	defer player.mu.Unlock()
	if player.privacyMuted && !force {
		return nil
	}
	player.stopLocked()
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	started := make(chan error, 1)
	player.cancel = cancel
	player.done = done
	go func() {
		defer close(done)
		if err := runLEDAnimationStarted(
			ctx,
			player.writer,
			data,
			repeat,
			started,
		); err != nil &&
			player.logf != nil {
			player.logf("LED animation %s: %v", name, err)
		}
		if !repeat && ctx.Err() == nil {
			if err := clearLEDs(player.writer); err != nil && player.logf != nil {
				player.logf("clear LED after animation %s: %v", name, err)
			}
		}
	}()
	if err := <-started; err != nil {
		cancel()
		<-done
		player.cancel = nil
		player.done = nil
		return err
	}
	return nil
}

func (player *ledPlayer) SetPrivacyMuted(
	parent context.Context,
	muted bool,
) error {
	player.mu.Lock()
	player.privacyMuted = muted
	if !muted {
		defer player.mu.Unlock()
		player.stopLocked()
		return clearLEDs(player.writer)
	}
	player.mu.Unlock()
	return player.start(parent, micPrivacyLEDName, true, true)
}

func (player *ledPlayer) Clear() error {
	return player.Stop()
}

func (player *ledPlayer) Stop() error {
	player.mu.Lock()
	defer player.mu.Unlock()
	if player.privacyMuted {
		return nil
	}
	player.stopLocked()
	return clearLEDs(player.writer)
}

func (player *ledPlayer) stopLocked() {
	if player.cancel != nil {
		player.cancel()
		<-player.done
		player.cancel = nil
		player.done = nil
	}
}

func runLEDAnimation(
	ctx context.Context,
	writer ledWriter,
	data []byte,
	repeat bool,
) error {
	return runLEDAnimationStarted(ctx, writer, data, repeat, nil)
}

func runLEDAnimationStarted(
	ctx context.Context,
	writer ledWriter,
	data []byte,
	repeat bool,
	started chan<- error,
) error {
	firstWrite := true
	for {
		for offset := 0; offset < len(data); offset += ledChunkBytes {
			end := offset + ledChunkBytes
			if end > len(data) {
				end = len(data)
			}
			flag := byte(0)
			if offset == 0 {
				flag = ledFirstChunkFlag
			}
			packet := make([]byte, 2, 2+end-offset)
			packet[0] = ledAnimationCode
			packet[1] = flag
			packet = append(packet, data[offset:end]...)
			err := writer.WriteMCUData(packet)
			if firstWrite {
				if started != nil {
					started <- err
				}
				firstWrite = false
			}
			if err != nil {
				return fmt.Errorf("send LED animation chunk: %w", err)
			}
			if end < len(data) || repeat {
				timer := time.NewTimer(ledChunkDelay)
				select {
				case <-ctx.Done():
					if !timer.Stop() {
						<-timer.C
					}
					return nil
				case <-timer.C:
				}
			}
		}
		if !repeat {
			return nil
		}
	}
}

// clearLEDs reproduces the donor ledOff packet: three all-zero frames.
func clearLEDs(writer ledWriter) error {
	packet := make([]byte, 2+ledOffFrameCount*ledFrameBytes)
	packet[0] = ledAnimationCode
	packet[1] = ledFirstChunkFlag
	if err := writer.WriteMCUData(packet); err != nil {
		return fmt.Errorf("clear LED ring: %w", err)
	}
	return nil
}

func validLEDName(name string) bool {
	if len(name) == 0 || len(name) > 80 {
		return false
	}
	for _, character := range name {
		if (character < 'a' || character > 'z') &&
			(character < 'A' || character > 'Z') &&
			(character < '0' || character > '9') &&
			character != '_' && character != '-' {
			return false
		}
	}
	return true
}
