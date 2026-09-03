// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"io"
	"net"
	"reflect"
	"testing"
	"time"
)

type channelEventSource struct {
	events <-chan inputEvent
}

func (source channelEventSource) Events(context.Context) <-chan inputEvent {
	return source.events
}

func TestMinimumWAMPSurface(t *testing.T) {
	expected := []string{
		"com.harman.vui.getmcustatus",
		"com.harman.vui.mutedaccontrol",
		"com.harman.vui.muteampcontrol",
	}
	if !reflect.DeepEqual(procedures, expected) {
		t.Fatalf("procedures = %#v, want %#v", procedures, expected)
	}
}

func TestGetMCUStatusReturnsCapturedVersion(t *testing.T) {
	hardware := newRecordingHardware(0)
	control := newController(hardware, mutePolicy{})
	service := wampService{
		controller: control,
		version:    recoveredMCUVersion,
	}

	response := invokeForTest(
		t,
		&service,
		"com.harman.vui.getmcustatus",
		[]interface{}{},
	)
	if messageType(response) != wampYield {
		t.Fatalf("response = %#v", response)
	}
	expected := []interface{}{recoveredMCUVersion}
	if !reflect.DeepEqual(response[3], expected) {
		t.Fatalf("result args = %#v, want %#v", response[3], expected)
	}
}

func TestWAMPUnmuteIsDeniedByDefault(t *testing.T) {
	hardware := newRecordingHardware(0)
	control := newController(hardware, mutePolicy{})
	control.initialized = true
	service := wampService{
		controller: control,
		version:    recoveredMCUVersion,
	}

	response := invokeForTest(
		t,
		&service,
		"com.harman.vui.mutedaccontrol",
		[]interface{}{"unmute"},
	)
	if messageType(response) != wampError {
		t.Fatalf("response = %#v", response)
	}
	if response[4] != "com.harman.error" {
		t.Fatalf("error URI = %#v", response[4])
	}
}

func TestMessagePackWAMPRoundTrip(t *testing.T) {
	message := []interface{}{
		wampPublish,
		uint64(42),
		map[string]interface{}{},
		"com.harman.test.inputEvent",
		[]interface{}{"volumeup", "3"},
	}
	payload, err := encodeMessagePack(message)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeMessagePack(payload)
	if err != nil {
		t.Fatal(err)
	}
	expected := []interface{}{
		uint64(wampPublish),
		uint64(42),
		map[string]interface{}{},
		"com.harman.test.inputEvent",
		[]interface{}{"volumeup", "3"},
	}
	if !reflect.DeepEqual(decoded, expected) {
		t.Fatalf("decoded = %#v, want %#v", decoded, expected)
	}
}

func TestServiceRegistersAndPublishesVerifiedEvent(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	routerDone := make(chan error, 1)
	routerRelease := make(chan struct{})
	published := make(chan []interface{}, 1)
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			routerDone <- err
			return
		}
		defer connection.Close()
		handshake := make([]byte, 4)
		if _, err := io.ReadFull(connection, handshake); err != nil {
			routerDone <- err
			return
		}
		if !reflect.DeepEqual(handshake, []byte{0x7f, 0xf2, 0, 0}) {
			routerDone <- &unexpectedMessage{message: handshake}
			return
		}
		if _, err := connection.Write(handshake); err != nil {
			routerDone <- err
			return
		}
		router := &wampConnection{connection: connection}
		hello, err := router.readFrame()
		if err != nil {
			routerDone <- err
			return
		}
		if messageType(hello) != wampHello {
			routerDone <- &unexpectedMessage{message: hello}
			return
		}
		if err := router.writeFrame([]interface{}{
			wampWelcome,
			uint64(100),
			map[string]interface{}{},
		}); err != nil {
			routerDone <- err
			return
		}
		for index, expected := range procedures {
			register, err := router.readFrame()
			if err != nil {
				routerDone <- err
				return
			}
			if messageType(register) != wampRegister ||
				register[3] != expected {
				routerDone <- &unexpectedMessage{message: register}
				return
			}
			if err := router.writeFrame([]interface{}{
				wampRegistered,
				register[1],
				uint64(200 + index),
			}); err != nil {
				routerDone <- err
				return
			}
		}
		event, err := router.readFrame()
		if err != nil {
			routerDone <- err
			return
		}
		published <- event
		routerDone <- nil
		<-routerRelease
	}()

	eventChannel := make(chan inputEvent, 1)
	eventChannel <- inputEvent{Name: "volumeup", Step: "2"}
	close(eventChannel)
	ctx, cancel := context.WithCancel(context.Background())
	service := wampService{
		address: listener.Addr().String(),
		realm:   "default",
		events:  channelEventSource{events: eventChannel},
		version: recoveredMCUVersion,
	}
	serviceDone := make(chan error, 1)
	go func() {
		serviceDone <- service.run(ctx)
	}()

	select {
	case event := <-published:
		if messageType(event) != wampPublish ||
			event[3] != "com.harman.test.inputEvent" ||
			!reflect.DeepEqual(
				event[4],
				[]interface{}{"volumeup", "2"},
			) {
			t.Fatalf("published event = %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("service did not publish rotary event")
	}
	if err := <-routerDone; err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case err := <-serviceDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("service did not stop")
	}
	close(routerRelease)
}

type unexpectedMessage struct {
	message interface{}
}

func (err *unexpectedMessage) Error() string {
	return "unexpected WAMP message"
}

func invokeForTest(
	t *testing.T,
	service *wampService,
	procedure string,
	args []interface{},
) []interface{} {
	t.Helper()
	serviceConnection, peerConnection := net.Pipe()
	defer serviceConnection.Close()
	defer peerConnection.Close()
	client := &wampConnection{connection: serviceConnection}
	peer := &wampConnection{connection: peerConnection}
	done := make(chan error, 1)
	go func() {
		done <- service.handleInvocation(
			client,
			map[uint64]string{77: procedure},
			[]interface{}{
				uint64(wampInvocation),
				uint64(9),
				uint64(77),
				map[string]interface{}{},
				args,
			},
		)
	}()

	response, err := peer.readFrame()
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("invocation handler did not finish")
	}
	return response
}
