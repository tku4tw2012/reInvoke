// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"syscall"
	"time"
	"unsafe"
)

const (
	pollPriority = 0x0002
	pollError    = 0x0008

	gpioPollTimeoutMilliseconds = 500
	mcuDrainInterval            = 5 * time.Millisecond
	maxMCUPendingReads          = 1024
)

var errMCUDrainLimit = errors.New(
	"MCU interrupt remained low after 1024 pending reads",
)

type pollDescriptor struct {
	FileDescriptor int32
	Events         int16
	ReturnedEvents int16
}

type inputEvent struct {
	Name  string
	Step  string
	Topic string
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

func readGPIOLevel(value *os.File, buffer []byte) (bool, error) {
	if _, err := value.Seek(0, io.SeekStart); err != nil {
		return false, fmt.Errorf("seek GPIO value: %w", err)
	}
	count, err := value.Read(buffer)
	if err != nil {
		return false, fmt.Errorf("read GPIO value: %w", err)
	}
	if count == 0 || (buffer[0] != '0' && buffer[0] != '1') {
		return false, errors.New("GPIO value is not zero or one")
	}
	return buffer[0] == '1', nil
}

func gpioPollHasEdge(returnedEvents int16) (bool, error) {
	if returnedEvents&pollPriority != 0 {
		return true, nil
	}
	if returnedEvents != 0 {
		return false, fmt.Errorf(
			"GPIO poll returned without an edge: %#x",
			uint16(returnedEvents),
		)
	}
	return false, nil
}

func waitMCUDrain(ctx context.Context) bool {
	timer := time.NewTimer(mcuDrainInterval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (source *gpioEventSource) drainPendingEvents(
	ctx context.Context,
	buffer []byte,
	events chan<- inputEvent,
) error {
	for count := 0; count < maxMCUPendingReads; count++ {
		lineHigh, err := readGPIOLevel(source.value, buffer)
		if err != nil {
			return err
		}
		if lineHigh {
			return nil
		}
		frame, err := source.bus.ReadMCUEvent()
		if err != nil {
			return fmt.Errorf("drain pending MCU event: %w", err)
		}
		if events != nil {
			if event, ok := decodeMCUEvent(frame); ok {
				select {
				case events <- event:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}
		if !waitMCUDrain(ctx) {
			return ctx.Err()
		}
	}
	return errMCUDrainLimit
}

func (source *gpioEventSource) Events(
	ctx context.Context,
) <-chan inputEvent {
	events := make(chan inputEvent)
	go func() {
		defer close(events)
		buffer := make([]byte, 8)
		recoverySuppressed := false
		startupDiscardPending := true
		lineHigh, err := readGPIOLevel(source.value, buffer)
		if err != nil {
			source.logError(err)
		} else if !lineHigh {
			if err := source.drainPendingEvents(ctx, buffer, nil); err != nil {
				source.logError(err)
				recoverySuppressed = errors.Is(err, errMCUDrainLimit)
			} else {
				startupDiscardPending = false
			}
		} else {
			startupDiscardPending = false
		}

		for {
			if ctx.Err() != nil {
				return
			}
			descriptor := pollDescriptor{
				FileDescriptor: int32(source.value.Fd()),
				Events:         pollPriority,
			}
			result, _, errno := syscall.Syscall(
				syscall.SYS_POLL,
				uintptr(unsafe.Pointer(&descriptor)),
				1,
				gpioPollTimeoutMilliseconds,
			)
			if errno != 0 {
				if errno == syscall.EINTR {
					continue
				}
				source.logError(fmt.Errorf("poll GPIO: %w", errno))
				continue
			}
			if result == 0 {
				lineHigh, err := readGPIOLevel(source.value, buffer)
				if err != nil {
					source.logError(err)
					continue
				}
				if lineHigh {
					recoverySuppressed = false
					startupDiscardPending = false
				} else if !recoverySuppressed {
					publish := events
					if startupDiscardPending {
						publish = nil
					}
					if err := source.drainPendingEvents(
						ctx,
						buffer,
						publish,
					); err != nil && ctx.Err() == nil {
						source.logError(err)
						recoverySuppressed = errors.Is(
							err,
							errMCUDrainLimit,
						)
					} else if err == nil {
						startupDiscardPending = false
					}
				}
				continue
			}
			hasEdge, err := gpioPollHasEdge(descriptor.ReturnedEvents)
			if err != nil {
				source.logError(err)
				continue
			}
			if !hasEdge {
				continue
			}
			recoverySuppressed = false
			if _, err := readGPIOLevel(source.value, buffer); err != nil {
				source.logError(err)
				continue
			}
			publish := events
			if startupDiscardPending {
				publish = nil
			}
			if err := source.drainPendingEvents(
				ctx,
				buffer,
				publish,
			); err != nil && ctx.Err() == nil {
				source.logError(err)
				recoverySuppressed = errors.Is(err, errMCUDrainLimit)
			} else if err == nil {
				startupDiscardPending = false
			}
		}
	}()
	return events
}

func decodeMCUEvent(frame [6]byte) (inputEvent, bool) {
	if frame[0] != 0x04 {
		return inputEvent{}, false
	}
	switch frame[1] {
	case 0x00:
		return inputEvent{Name: "action", Topic: "com.harman.vui.keypress"}, true
	case 0x01:
		return inputEvent{
			Name:  "action-long",
			Topic: "com.harman.vui.keypress",
		}, true
	case 0x02:
		return inputEvent{
			Name:  "bluetooth",
			Topic: "com.harman.vui.keypress",
		}, true
	case 0x03:
		return inputEvent{
			Name:  "bluetooth-long",
			Topic: "com.harman.vui.keypress",
		}, true
	case 0x04:
		return inputEvent{
			Name:  "micmute",
			Topic: "com.harman.vui.keypress",
		}, true
	case 0x05:
		return inputEvent{
			Name:  "micmute-long",
			Topic: "com.harman.vui.keypress",
		}, true
	case 0x06:
		return inputEvent{
			Name:  "reset",
			Topic: "com.harman.vui.keypress",
		}, true
	case 0x07:
		return inputEvent{
			Name:  "reset-long",
			Topic: "com.harman.vui.keypress",
		}, true
	case 0x08:
		if frame[2] < 1 || frame[2] > 5 {
			return inputEvent{}, false
		}
		return inputEvent{Name: "volumeup", Step: strconv.Itoa(int(frame[2]))}, true
	case 0x09:
		if frame[2] < 1 || frame[2] > 5 {
			return inputEvent{}, false
		}
		return inputEvent{
			Name: "volumedown",
			Step: strconv.Itoa(int(frame[2])),
		}, true
	default:
		return inputEvent{}, false
	}
}

func (event inputEvent) publication() (string, []interface{}) {
	if event.Topic != "" {
		return event.Topic, []interface{}{event.Name}
	}
	return "com.harman.test.inputEvent", []interface{}{event.Name, event.Step}
}
