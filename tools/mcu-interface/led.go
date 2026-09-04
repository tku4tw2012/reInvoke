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
	ledChunkDelay     = 280 * time.Millisecond
	maxLEDAssetBytes  = 1024 * 1024
	ledAnimationCode  = byte(0x0e)
	ledFirstChunkFlag = byte(0x01)
)

type ledWriter interface {
	WriteMCUData([]byte) error
}

type ledPlayer struct {
	directory string
	writer    ledWriter
	logf      func(string, ...interface{})

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
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
	if !validLEDName(name) {
		return errors.New("invalid LED animation name")
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
	player.stopLocked()
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	player.cancel = cancel
	player.done = done
	go func() {
		defer close(done)
		if err := runLEDAnimation(ctx, player.writer, data, repeat); err != nil &&
			player.logf != nil {
			player.logf("LED animation %s: %v", name, err)
		}
	}()
	return nil
}

func (player *ledPlayer) Stop() error {
	player.mu.Lock()
	defer player.mu.Unlock()
	player.stopLocked()
	return player.writer.WriteMCUData(
		append([]byte{ledAnimationCode, ledFirstChunkFlag}, make([]byte, 13)...),
	)
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
			if err := writer.WriteMCUData(packet); err != nil {
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
