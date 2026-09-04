// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"io"
	"net"
	"reflect"
	"sync"
	"testing"
	"time"
)

type channelEventSource struct {
	events <-chan inputEvent
}

func (source channelEventSource) Events(context.Context) <-chan inputEvent {
	return source.events
}

func TestDiscardPendingInputEventsKeepsFutureEvents(t *testing.T) {
	channel := make(chan inputEvent, 2)
	channel <- inputEvent{Name: "stale"}
	events := discardPendingInputEvents(channel)
	select {
	case event := <-events:
		t.Fatalf("stale event was retained: %#v", event)
	default:
	}
	fresh := inputEvent{Name: "fresh"}
	channel <- fresh
	if event := <-events; event != fresh {
		t.Fatalf("event = %#v, want %#v", event, fresh)
	}
}

func TestMinimumWAMPSurface(t *testing.T) {
	expected := []string{
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

func TestVolumeSetUsesBlueALSAAsAuthority(t *testing.T) {
	var calls [][]string
	media, err := newBlueALSAController(
		"bluealsa-cli",
		"AA:BB:CC:11:22:33",
		func(ctx context.Context, args ...string) ([]byte, error) {
			calls = append(calls, append([]string(nil), args...))
			switch args[0] {
			case "list-pcms":
				return []byte(
					"/org/bluealsa/hci0/dev_AA_BB_CC_11_22_33/a2dpsnk/source\n",
				), nil
			case "info":
				return []byte(
					"Volume: L: 64 R: 64\nMuted: L: N R: N\n",
				), nil
			case "volume":
				return nil, nil
			default:
				return nil, errors.New("unexpected command")
			}
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	service := wampService{media: media}
	response := invokeForTest(
		t,
		&service,
		"com.harman.volumeSet",
		[]interface{}{uint64(25), "music"},
	)
	if messageType(response) != wampYield {
		t.Fatalf("response = %#v", response)
	}
	wantCall := []string{
		"volume",
		"/org/bluealsa/hci0/dev_AA_BB_CC_11_22_33/a2dpsnk/source",
		"32",
		"32",
	}
	if !reflect.DeepEqual(calls[len(calls)-1], wantCall) {
		t.Fatalf("volume call = %v, want %v", calls[len(calls)-1], wantCall)
	}
	if !reflect.DeepEqual(response[3], []interface{}{uint64(25), "music"}) {
		t.Fatalf("result args = %#v", response[3])
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

func TestRequestIDsAreUniqueAcrossConcurrentPublishers(t *testing.T) {
	client := &wampConnection{nextID: 1}
	const count = 100
	ids := make(chan uint64, count)
	var publishers sync.WaitGroup
	for index := 0; index < count; index++ {
		publishers.Add(1)
		go func() {
			defer publishers.Done()
			ids <- client.requestID()
		}()
	}
	publishers.Wait()
	close(ids)

	seen := make(map[uint64]bool, count)
	for id := range ids {
		if seen[id] {
			t.Fatalf("duplicate request ID %d", id)
		}
		seen[id] = true
	}
	if len(seen) != count {
		t.Fatalf("request ID count = %d, want %d", len(seen), count)
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
			context.Background(),
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

	var response []interface{}
	for {
		var err error
		response, err = peer.readFrame()
		if err != nil {
			t.Fatal(err)
		}
		if messageType(response) != wampPublish {
			break
		}
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
