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
)

var procedures = []string{
	"com.harman.vui.getmcustatus",
	"com.harman.vui.mutedaccontrol",
	"com.harman.vui.muteampcontrol",
}

type wampService struct {
	address    string
	realm      string
	controller *controller
	events     eventSource
	version    string
}

type wampConnection struct {
	connection net.Conn
	writeMu    sync.Mutex
	nextID     uint64
}

func (service *wampService) run(ctx context.Context) error {
	connection, err := net.DialTimeout("tcp", service.address, 5*time.Second)
	if err != nil {
		return fmt.Errorf("connect WAMP router: %w", err)
	}
	defer connection.Close()
	client := &wampConnection{connection: connection, nextID: 1}

	if err := client.negotiate(service.realm); err != nil {
		return err
	}
	registrations, err := client.register(procedures)
	if err != nil {
		return err
	}

	messages := make(chan []interface{})
	readErrors := make(chan error, 1)
	go func() {
		defer close(messages)
		for {
			message, err := client.readFrame()
			if err != nil {
				readErrors <- err
				return
			}
			select {
			case messages <- message:
			case <-ctx.Done():
				return
			}
		}
	}()

	var events <-chan inputEvent
	if service.events != nil {
		events = service.events.Events(ctx)
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-readErrors:
			return err
		case event, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			if err := client.publish(
				"com.harman.test.inputEvent",
				[]interface{}{event.Name, event.Step},
			); err != nil {
				return err
			}
		case message, ok := <-messages:
			if !ok {
				return errors.New("WAMP connection closed")
			}
			if err := service.handleInvocation(
				client,
				registrations,
				message,
			); err != nil {
				return err
			}
		}
	}
}

func (service *wampService) handleInvocation(
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
	return client.writeFrame([]interface{}{
		wampYield,
		requestID,
		map[string]interface{}{},
		result,
	})
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
