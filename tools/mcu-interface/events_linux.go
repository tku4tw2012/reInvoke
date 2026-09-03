// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"syscall"
	"unsafe"
)

const (
	pollPriority = 0x0002
	pollError    = 0x0008
)

type pollDescriptor struct {
	FileDescriptor int32
	Events         int16
	ReturnedEvents int16
}

type inputEvent struct {
	Name string
	Step string
}

type eventSource interface {
	Events(context.Context) <-chan inputEvent
}

type gpioEventSource struct {
	value *os.File
	bus   interface {
		ReadMCUEvent() ([6]byte, error)
	}
	logError func(error)
}

func prepareGPIO(root string, number int) (*os.File, error) {
	path := root + "/gpio" + strconv.Itoa(number)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.WriteFile(
			root+"/export",
			[]byte(strconv.Itoa(number)),
			0,
		); err != nil {
			return nil, fmt.Errorf("export GPIO %d: %w", number, err)
		}
	}
	if err := os.WriteFile(path+"/direction", []byte("in"), 0); err != nil {
		return nil, fmt.Errorf("configure GPIO direction: %w", err)
	}
	if err := os.WriteFile(path+"/edge", []byte("falling"), 0); err != nil {
		return nil, fmt.Errorf("configure GPIO edge: %w", err)
	}
	file, err := os.OpenFile(
		path+"/value",
		os.O_RDONLY|syscall.O_NONBLOCK,
		0,
	)
	if err != nil {
		return nil, fmt.Errorf("open GPIO value: %w", err)
	}
	return file, nil
}

func (source *gpioEventSource) Events(
	ctx context.Context,
) <-chan inputEvent {
	events := make(chan inputEvent)
	go func() {
		defer close(events)
		buffer := make([]byte, 8)
		_, _ = source.value.Seek(0, io.SeekStart)
		_, _ = source.value.Read(buffer)

		for {
			if ctx.Err() != nil {
				return
			}
			descriptor := pollDescriptor{
				FileDescriptor: int32(source.value.Fd()),
				Events:         pollPriority | pollError,
			}
			result, _, errno := syscall.Syscall(
				syscall.SYS_POLL,
				uintptr(unsafe.Pointer(&descriptor)),
				1,
				500,
			)
			if errno != 0 {
				if errno == syscall.EINTR {
					continue
				}
				source.logError(fmt.Errorf("poll GPIO: %w", errno))
				return
			}
			if result == 0 {
				continue
			}
			_, _ = source.value.Seek(0, io.SeekStart)
			_, _ = source.value.Read(buffer)
			frame, err := source.bus.ReadMCUEvent()
			if err != nil {
				source.logError(err)
				continue
			}
			event, ok := decodeMCUEvent(frame)
			if !ok {
				continue
			}
			select {
			case events <- event:
			case <-ctx.Done():
				return
			}
		}
	}()
	return events
}

func decodeMCUEvent(frame [6]byte) (inputEvent, bool) {
	if frame[0] != 0x04 || frame[2] < 1 || frame[2] > 5 {
		return inputEvent{}, false
	}
	name := ""
	switch frame[1] {
	case 0x08:
		name = "volumeup"
	case 0x09:
		name = "volumedown"
	default:
		return inputEvent{}, false
	}
	return inputEvent{Name: name, Step: strconv.Itoa(int(frame[2]))}, true
}
