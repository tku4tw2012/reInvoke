// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"strconv"
)

type inputController interface {
	Apply(context.Context, inputEvent) error
}

type inputControllerList []inputController

func (controllers inputControllerList) Apply(
	ctx context.Context,
	event inputEvent,
) error {
	var firstError error
	for _, controller := range controllers {
		if err := controller.Apply(ctx, event); err != nil && firstError == nil {
			firstError = err
		}
	}
	return firstError
}

type relayedEventSource struct {
	events <-chan inputEvent
}

func (source relayedEventSource) Events(context.Context) <-chan inputEvent {
	return source.events
}

func runEventRelay(
	ctx context.Context,
	events <-chan inputEvent,
	controller inputController,
	publications chan<- inputEvent,
	logf func(string, ...interface{}),
) {
	defer close(publications)
	workerContext, stopWorker := context.WithCancel(ctx)
	defer stopWorker()
	var workerDone chan struct{}
	var buttonEvents chan inputEvent
	var volumeDeltas chan int
	if controller != nil {
		workerDone = make(chan struct{})
		buttonEvents = make(chan inputEvent, 4)
		volumeDeltas = make(chan int, 1)
		go func() {
			defer close(workerDone)
			runInputWorker(
				workerContext,
				controller,
				buttonEvents,
				volumeDeltas,
				logf,
			)
		}()
		defer func() {
			stopWorker()
			<-workerDone
		}()
	}
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			if buttonEvents != nil {
				queued := false
				if delta, isVolume := volumeDelta(event); isVolume {
					queued = queueVolumeDelta(volumeDeltas, delta)
				} else {
					select {
					case buttonEvents <- event:
						queued = true
					default:
					}
				}
				if !queued && logf != nil {
					logf("input control queue full; dropped %s", event.Name)
				}
			}
			select {
			case publications <- event:
			default:
				if logf != nil {
					logf("input publication queue full; dropped %s", event.Name)
				}
			}
		}
	}
}

func runInputWorker(
	ctx context.Context,
	controller inputController,
	buttons <-chan inputEvent,
	deltas <-chan int,
	logf func(string, ...interface{}),
) {
	for {
		select {
		case <-ctx.Done():
			return
		case event := <-buttons:
			if err := controller.Apply(ctx, event); err != nil && logf != nil {
				logf("input control %s: %v", event.Name, err)
			}
		default:
			select {
			case <-ctx.Done():
				return
			case event := <-buttons:
				if err := controller.Apply(
					ctx,
					event,
				); err != nil && logf != nil {
					logf("input control %s: %v", event.Name, err)
				}
			case delta := <-deltas:
				name := "volumeup"
				if delta < 0 {
					name = "volumedown"
					delta = -delta
				}
				event := inputEvent{Name: name, Step: strconv.Itoa(delta)}
				if err := controller.Apply(
					ctx,
					event,
				); err != nil && logf != nil {
					logf("input control %s: %v", event.Name, err)
				}
			}
		}
	}
}

func volumeDelta(event inputEvent) (int, bool) {
	step, err := strconv.Atoi(event.Step)
	if err != nil || step < 1 || step > 5 {
		return 0, false
	}
	switch event.Name {
	case "volumeup":
		return step, true
	case "volumedown":
		return -step, true
	default:
		return 0, false
	}
}

func queueVolumeDelta(deltas chan int, delta int) bool {
	select {
	case deltas <- delta:
		return true
	default:
	}
	select {
	case pending := <-deltas:
		delta += pending
	default:
		select {
		case deltas <- delta:
			return true
		default:
			return false
		}
	}
	if delta > 100 {
		delta = 100
	}
	if delta < -100 {
		delta = -100
	}
	if delta == 0 {
		return true
	}
	select {
	case deltas <- delta:
		return true
	default:
		return false
	}
}
