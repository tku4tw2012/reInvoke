// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

const (
	wampHello      = 1
	wampWelcome    = 2
	wampError      = 8
	wampPublish    = 16
	wampRegister   = 64
	wampRegistered = 65
	wampInvocation = 68
	wampYield      = 70

	wampReconnectDelay = 5 * time.Second
)

var procedures = []string{
	"com.harman.vui.getmcustatus",
	"com.harman.vui.mutedaccontrol",
	"com.harman.vui.muteampcontrol",
	"com.harman.volumeGet",
	"com.harman.volumeSet",
	"com.harman.volumeAdjust",
	"com.harman.musicMuteSet",
	"com.harman.musicMuteToggle",
	"com.harman.ledAnimate",
	"com.harman.ledOff",
}

type wampService struct {
	address     string
	realm       string
	controller  *controller
	media       *blueALSAController
	lights      *ledPlayer
	events      eventSource
	version     string
	flushEvents bool
}

type wampConnection struct {
	connection net.Conn
	writeMu    sync.Mutex
	idMu       sync.Mutex
	nextID     uint64
}

func (service *wampService) run(ctx context.Context) error {
	connection, err := net.DialTimeout("tcp", service.address, 5*time.Second)
	if err != nil {
		return fmt.Errorf("connect WAMP router: %w", err)
	}
	defer connection.Close()
	connectionDone := make(chan struct{})
	defer close(connectionDone)
	go func() {
		select {
		case <-ctx.Done():
			_ = connection.Close()
		case <-connectionDone:
		}
	}()
	client := &wampConnection{connection: connection, nextID: 1}
	sessionContext, stopSession := context.WithCancel(ctx)

	if err := client.negotiate(service.realm); err != nil {
		stopSession()
		return err
	}
	registrations, err := client.register(procedures)
	if err != nil {
		stopSession()
		return err
	}

	messages := make(chan []interface{})
	readErrors := make(chan error, 1)
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		defer close(messages)
		for {
			message, err := client.readFrame()
			if err != nil {
				readErrors <- err
				return
			}
			select {
			case messages <- message:
			case <-sessionContext.Done():
				return
			}
		}
	}()

	var events <-chan inputEvent
	if service.events != nil {
		events = service.events.Events(sessionContext)
		if service.flushEvents {
			events = discardPendingInputEvents(events)
		}
	}
	invocationErrors := make(chan error, 1)
	var invocations sync.WaitGroup
	defer func() {
		stopSession()
		_ = connection.Close()
		<-readerDone
		invocations.Wait()
	}()
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-readErrors:
			if ctx.Err() != nil {
				return nil
			}
			return err
		case err := <-invocationErrors:
			if ctx.Err() != nil {
				return nil
			}
			return err
		case event, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			topic, args := event.publication()
			if err := client.publish(topic, args); err != nil {
				return err
			}
		case message, ok := <-messages:
			if !ok {
				if ctx.Err() != nil {
					return nil
				}
				return errors.New("WAMP connection closed")
			}
			if messageType(message) != wampInvocation {
				continue
			}
			invocations.Add(1)
			go func(message []interface{}) {
				defer invocations.Done()
				if err := service.handleInvocation(
					ctx,
					client,
					registrations,
					message,
				); err != nil {
					select {
					case invocationErrors <- err:
					default:
					}
				}
			}(message)
		}
	}
}

func discardPendingInputEvents(
	events <-chan inputEvent,
) <-chan inputEvent {
	for {
		select {
		case _, ok := <-events:
			if !ok {
				return nil
			}
		default:
			return events
		}
	}
}

func (service *wampService) handleInvocation(
	ctx context.Context,
	client *wampConnection,
	registrations map[uint64]string,
	message []interface{},
) error {
	if messageType(message) != wampInvocation || len(message) < 4 {
		return nil
	}
	requestID, ok := unsigned(message[1])
	if !ok {
		return errors.New("WAMP invocation has invalid request ID")
	}
	registrationID, ok := unsigned(message[2])
	if !ok {
		return errors.New("WAMP invocation has invalid registration ID")
	}
	procedure, ok := registrations[registrationID]
	if !ok {
		return nil
	}
	args := []interface{}{}
	if len(message) > 4 {
		args, _ = message[4].([]interface{})
	}

	var result []interface{}
	resultKwargs := map[string]interface{}{}
	var events []mediaEvent
	var invocationError error
	switch procedure {
	case "com.harman.vui.getmcustatus":
		if len(args) != 0 {
			invocationError = errors.New("invalid argument format")
		} else {
			result = []interface{}{service.version}
		}
	case "com.harman.vui.mutedaccontrol":
		invocationError = applyMuteCommand(
			args,
			service.controller.setDACMute,
		)
	case "com.harman.vui.muteampcontrol":
		invocationError = applyMuteCommand(
			args,
			service.controller.setAmpMute,
		)
	case "com.harman.volumeGet":
		var snapshot blueALSASnapshot
		snapshot, invocationError = service.mediaSnapshot(ctx)
		if invocationError == nil {
			resultKwargs = mediaVolumeState(snapshot)
		}
	case "com.harman.volumeSet":
		var snapshot blueALSASnapshot
		var value int
		value, invocationError = mediaIntegerArgument(args, false)
		if invocationError == nil && service.media == nil {
			invocationError = errors.New("media backend is unavailable")
		}
		if invocationError == nil {
			snapshot, invocationError = service.media.SetVolume(ctx, value)
		}
		if invocationError == nil {
			result = []interface{}{value, "music"}
			resultKwargs = mediaVolumeState(snapshot)
			events = mediaVolumeEvents(snapshot, false)
		}
	case "com.harman.volumeAdjust":
		var snapshot blueALSASnapshot
		var delta int
		delta, invocationError = mediaIntegerArgument(args, true)
		if invocationError == nil && service.media == nil {
			invocationError = errors.New("media backend is unavailable")
		}
		if invocationError == nil {
			snapshot, invocationError = service.media.AdjustVolume(ctx, delta)
		}
		if invocationError == nil {
			result = []interface{}{snapshot.Volume, "music"}
			resultKwargs = mediaVolumeState(snapshot)
			events = mediaVolumeEvents(snapshot, false)
		}
	case "com.harman.musicMuteSet":
		var snapshot blueALSASnapshot
		var muted bool
		muted, invocationError = mediaMuteArgument(args)
		if invocationError == nil && service.media == nil {
			invocationError = errors.New("media backend is unavailable")
		}
		if invocationError == nil {
			snapshot, invocationError = service.media.SetMuted(ctx, muted)
		}
		if invocationError == nil {
			result = []interface{}{snapshot.Muted, "music"}
			resultKwargs = mediaVolumeState(snapshot)
			events = mediaVolumeEvents(snapshot, true)
		}
	case "com.harman.musicMuteToggle":
		var snapshot blueALSASnapshot
		if len(args) != 0 {
			invocationError = errors.New("invalid argument format")
		} else if service.media == nil {
			invocationError = errors.New("media backend is unavailable")
		}
		if invocationError == nil {
			snapshot, invocationError = service.media.ToggleMuted(ctx)
		}
		if invocationError == nil {
			result = []interface{}{snapshot.Muted, "music"}
			resultKwargs = mediaVolumeState(snapshot)
			events = mediaVolumeEvents(snapshot, true)
		}
	case "com.harman.ledAnimate":
		var name string
		var repeat bool
		name, repeat, invocationError = ledArguments(args)
		if invocationError == nil && service.lights == nil {
			invocationError = errors.New("LED player is unavailable")
		}
		if invocationError == nil {
			invocationError = service.lights.Start(ctx, name, repeat)
		}
	case "com.harman.ledOff":
		if len(args) != 0 {
			invocationError = errors.New("invalid argument format")
		} else if service.lights == nil {
			invocationError = errors.New("LED player is unavailable")
		} else {
			invocationError = service.lights.Stop()
		}
	default:
		invocationError = errors.New("unsupported procedure")
	}

	if invocationError != nil {
		return client.writeFrame([]interface{}{
			wampError,
			wampInvocation,
			requestID,
			map[string]interface{}{},
			"com.harman.error",
			[]interface{}{invocationError.Error()},
		})
	}
	for _, event := range events {
		if err := client.publish(event.topic, event.args); err != nil {
			return err
		}
	}
	return client.writeFrame([]interface{}{
		wampYield,
		requestID,
		map[string]interface{}{},
		result,
		resultKwargs,
	})
}

type mediaEvent struct {
	topic string
	args  []interface{}
}

func (service *wampService) mediaSnapshot(
	ctx context.Context,
) (blueALSASnapshot, error) {
	if service.media == nil {
		return blueALSASnapshot{}, errors.New("media backend is unavailable")
	}
	return service.media.Snapshot(ctx)
}

func mediaIntegerArgument(args []interface{}, signed bool) (int, error) {
	if len(args) != 2 || args[1] != "music" {
		return 0, errors.New("invalid argument format")
	}
	switch value := args[0].(type) {
	case uint64:
		if value <= uint64(^uint(0)>>1) {
			return int(value), nil
		}
	case int64:
		maximum := int64(^uint(0) >> 1)
		minimum := -maximum - 1
		if value >= minimum && value <= maximum &&
			(signed || value >= 0) {
			return int(value), nil
		}
	case int:
		if signed || value >= 0 {
			return value, nil
		}
	}
	return 0, errors.New("invalid argument format")
}

func mediaMuteArgument(args []interface{}) (bool, error) {
	if len(args) != 2 || args[1] != "music" {
		return false, errors.New("invalid argument format")
	}
	muted, ok := args[0].(bool)
	if !ok {
		return false, errors.New("invalid argument format")
	}
	return muted, nil
}

func ledArguments(args []interface{}) (string, bool, error) {
	if len(args) != 2 {
		return "", false, errors.New("invalid argument format")
	}
	name, ok := args[0].(string)
	if !ok {
		return "", false, errors.New("invalid argument format")
	}
	state, ok := unsigned(args[1])
	if !ok || state > 1 {
		return "", false, errors.New("invalid argument format")
	}
	return name, state == 1, nil
}

func mediaVolumeState(snapshot blueALSASnapshot) map[string]interface{} {
	mute := uint64(0)
	if snapshot.Muted {
		mute = 1
	}
	return map[string]interface{}{
		"music": map[string]interface{}{
			"mute":   mute,
			"volume": uint64(snapshot.Volume),
		},
		"system": map[string]interface{}{
			"mute":   uint64(0),
			"volume": uint64(70),
		},
	}
}

func mediaVolumeEvents(
	snapshot blueALSASnapshot,
	includeMute bool,
) []mediaEvent {
	volume := snapshot.Volume
	if snapshot.Muted {
		volume = 0
	}
	events := []mediaEvent{{
		topic: "com.harman.volumeChanged",
		args:  []interface{}{"music", volume},
	}}
	if includeMute {
		events = append(events, mediaEvent{
			topic: "com.harman.musicMuteChanged",
			args:  []interface{}{snapshot.Muted},
		})
	}
	return events
}

func applyMuteCommand(
	args []interface{},
	apply func(bool) error,
) error {
	if len(args) != 1 {
		return errors.New("invalid argument format")
	}
	command, ok := args[0].(string)
	if !ok || (command != "mute" && command != "unmute") {
		return errors.New("invalid argument format")
	}
	return apply(command == "mute")
}

func (client *wampConnection) negotiate(realm string) error {
	if _, err := client.connection.Write([]byte{0x7f, 0xf2, 0, 0}); err != nil {
		return fmt.Errorf("write RawSocket handshake: %w", err)
	}
	handshake := make([]byte, 4)
	if _, err := io.ReadFull(client.connection, handshake); err != nil {
		return fmt.Errorf("read RawSocket handshake: %w", err)
	}
	if handshake[0] != 0x7f || handshake[1]&0x0f != 2 {
		return fmt.Errorf("RawSocket handshake rejected: %x", handshake)
	}
	if err := client.writeFrame([]interface{}{
		wampHello,
		realm,
		map[string]interface{}{
			"roles": map[string]interface{}{
				"callee":    map[string]interface{}{},
				"publisher": map[string]interface{}{},
			},
		},
	}); err != nil {
		return err
	}
	response, err := client.readFrame()
	if err != nil {
		return err
	}
	if messageType(response) != wampWelcome {
		return fmt.Errorf("expected WELCOME, received %v", response)
	}
	return nil
}

func (client *wampConnection) register(
	names []string,
) (map[uint64]string, error) {
	registrations := make(map[uint64]string, len(names))
	for _, name := range names {
		requestID := client.requestID()
		if err := client.writeFrame([]interface{}{
			wampRegister,
			requestID,
			map[string]interface{}{},
			name,
		}); err != nil {
			return nil, err
		}
		response, err := client.readFrame()
		if err != nil {
			return nil, err
		}
		if messageType(response) != wampRegistered || len(response) < 3 {
			return nil, fmt.Errorf(
				"registration failed for %s: %v",
				name,
				response,
			)
		}
		responseRequestID, requestOK := unsigned(response[1])
		registrationID, registrationOK := unsigned(response[2])
		if !requestOK || !registrationOK || responseRequestID != requestID {
			return nil, fmt.Errorf(
				"invalid registration response for %s",
				name,
			)
		}
		registrations[registrationID] = name
	}
	return registrations, nil
}

func (client *wampConnection) publish(
	topic string,
	args []interface{},
) error {
	return client.writeFrame([]interface{}{
		wampPublish,
		client.requestID(),
		map[string]interface{}{},
		topic,
		args,
	})
}

func (client *wampConnection) requestID() uint64 {
	client.idMu.Lock()
	defer client.idMu.Unlock()
	id := client.nextID
	client.nextID++
	return id
}

func (client *wampConnection) writeFrame(message []interface{}) error {
	payload, err := encodeMessagePack(message)
	if err != nil {
		return err
	}
	if len(payload) > 0xffffff {
		return errors.New("WAMP frame exceeds RawSocket limit")
	}
	header := []byte{
		0,
		byte(len(payload) >> 16),
		byte(len(payload) >> 8),
		byte(len(payload)),
	}
	client.writeMu.Lock()
	defer client.writeMu.Unlock()
	if err := writeAll(client.connection, header); err != nil {
		return err
	}
	return writeAll(client.connection, payload)
}

func (client *wampConnection) readFrame() ([]interface{}, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(client.connection, header); err != nil {
		return nil, err
	}
	if header[0] != 0 {
		return nil, fmt.Errorf("unsupported RawSocket frame type %d", header[0])
	}
	length := int(header[1])<<16 | int(header[2])<<8 | int(header[3])
	payload := make([]byte, length)
	if _, err := io.ReadFull(client.connection, payload); err != nil {
		return nil, err
	}
	decoded, err := decodeMessagePack(payload)
	if err != nil {
		return nil, err
	}
	message, ok := decoded.([]interface{})
	if !ok {
		return nil, errors.New("WAMP message is not an array")
	}
	return message, nil
}

func writeAll(writer io.Writer, buffer []byte) error {
	for len(buffer) > 0 {
		written, err := writer.Write(buffer)
		if err != nil {
			return err
		}
		buffer = buffer[written:]
	}
	return nil
}

func messageType(message []interface{}) uint64 {
	if len(message) == 0 {
		return 0
	}
	value, _ := unsigned(message[0])
	return value
}

func unsigned(value interface{}) (uint64, bool) {
	switch number := value.(type) {
	case uint64:
		return number, true
	case int64:
		if number >= 0 {
			return uint64(number), true
		}
	case int:
		if number >= 0 {
			return uint64(number), true
		}
	}
	return 0, false
}
