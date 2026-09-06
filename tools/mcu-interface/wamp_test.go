// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

type channelEventSource struct {
	events <-chan inputEvent
}

type failingLEDWriter struct{}

func (failingLEDWriter) WriteMCUData([]byte) error {
	return errors.New("injected LED failure")
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
		"com.harman.ledSet",
		"com.harman.ledOff",
		"com.harman.dsp.micMute",
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

func TestVolumeSetClampsAndReturnsEffectiveValue(t *testing.T) {
	for _, test := range []struct {
		name  string
		value interface{}
		want  uint64
	}{
		{name: "negative", value: int64(-1), want: 0},
		{name: "above maximum", value: uint64(101), want: 100},
	} {
		t.Run(test.name, func(t *testing.T) {
			media, err := newBlueALSAController(
				"bluealsa-cli",
				"AA:BB:CC:11:22:33",
				func(_ context.Context, args ...string) ([]byte, error) {
					switch args[0] {
					case "list-pcms":
						return []byte(
							"/org/bluealsa/hci0/dev_AA_BB_CC_11_22_33/" +
								"a2dpsnk/source\n",
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
			response := invokeForTest(
				t,
				&wampService{media: media},
				"com.harman.volumeSet",
				[]interface{}{test.value, "music"},
			)
			if messageType(response) != wampYield {
				t.Fatalf("response = %#v", response)
			}
			want := []interface{}{test.want, "music"}
			if !reflect.DeepEqual(response[3], want) {
				t.Fatalf("result args = %#v, want %#v", response[3], want)
			}
		})
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
		details, ok := hello[2].(map[string]interface{})
		if !ok {
			routerDone <- &unexpectedMessage{message: hello}
			return
		}
		roles, ok := details["roles"].(map[string]interface{})
		if !ok {
			routerDone <- &unexpectedMessage{message: hello}
			return
		}
		for _, role := range []string{
			"callee",
			"caller",
			"publisher",
			"subscriber",
		} {
			if _, ok := roles[role]; !ok {
				routerDone <- &unexpectedMessage{message: hello}
				return
			}
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
		for index, expected := range []string{dspSessionTopic, dspBootTopic} {
			subscribe, err := router.readFrame()
			if err != nil {
				routerDone <- err
				return
			}
			if messageType(subscribe) != wampSubscribe ||
				subscribe[3] != expected {
				routerDone <- &unexpectedMessage{message: subscribe}
				return
			}
			if err := router.writeFrame([]interface{}{
				wampSubscribed,
				subscribe[1],
				uint64(300 + index),
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
	case <-time.After(2 * time.Second):
		select {
		case err := <-routerDone:
			t.Fatalf("router failed before publication: %v", err)
		default:
			t.Fatal("service did not publish rotary event")
		}
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

func startMicControlResponder(
	t *testing.T,
	responses ...string,
) (string, <-chan string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "dsp-mic.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	requests := make(chan string, len(responses))
	go func() {
		defer close(requests)
		for _, response := range responses {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			request := make([]byte, 2)
			if _, err := io.ReadFull(connection, request); err == nil {
				requests <- string(request)
				_, _ = connection.Write([]byte(response))
			}
			_ = connection.Close()
		}
	}()
	return path, requests
}

func TestMicrophoneMuteUsesPrivateControlSocket(t *testing.T) {
	socketPath, requests := startMicControlResponder(t, "OK\n")
	statePath := filepath.Join(t.TempDir(), "microphone-state")
	privacy := newMicrophonePrivacyController(
		false,
		statePath,
		socketPath,
		nil,
		nil,
	)
	if err := privacy.Set(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if request := <-requests; request != "1\n" {
		t.Fatalf("request = %q, want mute", request)
	}
	content, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != microphoneMutedState ||
		!privacy.muted || !privacy.desired || privacy.unknown {
		t.Fatalf(
			"state=%q muted=%t desired=%t unknown=%t",
			content,
			privacy.muted,
			privacy.desired,
			privacy.unknown,
		)
	}
}

type countingPrivacyLEDWriter struct {
	mu    sync.Mutex
	calls int
}

func (writer *countingPrivacyLEDWriter) WriteMCUData([]byte) error {
	writer.mu.Lock()
	writer.calls++
	writer.mu.Unlock()
	return nil
}

func (writer *countingPrivacyLEDWriter) count() int {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	return writer.calls
}

func TestPrivacyAnimationOutlivesWAMPSession(t *testing.T) {
	socketPath, requests := startMicControlResponder(t, "OK\n")
	lightsDirectory := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(lightsDirectory, micPrivacyLEDName+".bin"),
		make([]byte, ledFrameBytes),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	writer := &countingPrivacyLEDWriter{}
	lights := &ledPlayer{directory: lightsDirectory, writer: writer}
	privacy := newMicrophonePrivacyController(
		false,
		filepath.Join(t.TempDir(), "microphone-state"),
		socketPath,
		lights,
		nil,
	)
	processCtx, cancelProcess := context.WithCancel(context.Background())
	privacy.lifetime = processCtx
	sessionCtx, cancelSession := context.WithCancel(context.Background())
	if err := privacy.Set(sessionCtx, true); err != nil {
		t.Fatal(err)
	}
	if request := <-requests; request != "1\n" {
		t.Fatalf("request = %q, want mute", request)
	}
	before := writer.count()
	cancelSession()
	time.Sleep(ledChunkDelay + 100*time.Millisecond)
	if after := writer.count(); after <= before {
		t.Fatalf(
			"privacy animation stopped with WAMP session: before=%d after=%d",
			before,
			after,
		)
	}
	cancelProcess()
	if err := lights.SetPrivacyMuted(context.Background(), false); err != nil {
		t.Fatal(err)
	}
}

func TestCancelledSessionCannotResumeQueuedPrivacyMutation(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "microphone-state")
	privacy := newMicrophonePrivacyController(
		false,
		statePath,
		filepath.Join(t.TempDir(), "missing.sock"),
		nil,
		nil,
	)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	privacy.mu.Lock()
	go func() { done <- privacy.Set(ctx, true) }()
	cancel()
	privacy.mu.Unlock()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("queued privacy error = %v, want cancellation", err)
	}
	if _, err := os.Stat(statePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cancelled session persisted privacy state: %v", err)
	}
}

func TestMicrophoneMutePrecedesIndicatorFailure(t *testing.T) {
	socketPath, requests := startMicControlResponder(t, "OK\n")
	stateDirectory := t.TempDir()
	lightsDirectory := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(lightsDirectory, micPrivacyLEDName+".bin"),
		make([]byte, ledFrameBytes),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	privacy := newMicrophonePrivacyController(
		false,
		filepath.Join(stateDirectory, "microphone-state"),
		socketPath,
		&ledPlayer{directory: lightsDirectory, writer: failingLEDWriter{}},
		nil,
	)
	if err := privacy.Set(context.Background(), true); err == nil {
		t.Fatal("indicator failure was not reported")
	}
	if request := <-requests; request != "1\n" {
		t.Fatalf("request = %q, want mute", request)
	}
	if !privacy.muted {
		t.Fatal("microphone was not muted after indicator failure")
	}
}

func TestFailedUnmuteIsImmediatelyRemuted(t *testing.T) {
	socketPath, requests := startMicControlResponder(t, "ERR\n", "OK\n")
	statePath := filepath.Join(t.TempDir(), "microphone-state")
	if err := persistMicrophoneState(statePath, true); err != nil {
		t.Fatal(err)
	}
	privacy := newMicrophonePrivacyController(
		true,
		statePath,
		socketPath,
		nil,
		nil,
	)
	if err := privacy.Set(context.Background(), false); err == nil {
		t.Fatal("failed unmute was reported as successful")
	}
	if first, second := <-requests, <-requests; first != "0\n" || second != "1\n" {
		t.Fatalf("requests = %q, %q, want unmute then mute", first, second)
	}
	if !privacy.muted || !privacy.desired || privacy.unknown {
		t.Fatalf(
			"muted=%t desired=%t unknown=%t, want restored mute",
			privacy.muted,
			privacy.desired,
			privacy.unknown,
		)
	}
}

func TestFailedUnmuteRecoveryRetriesUntilMuted(t *testing.T) {
	socketPath, requests := startMicControlResponder(
		t,
		"ERR\n",
		"ERR\n",
		"OK\n",
	)
	statePath := filepath.Join(t.TempDir(), "microphone-state")
	if err := persistMicrophoneState(statePath, true); err != nil {
		t.Fatal(err)
	}
	privacy := newMicrophonePrivacyController(
		true,
		statePath,
		socketPath,
		nil,
		nil,
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go privacy.Run(ctx)
	if err := privacy.Set(ctx, false); err == nil {
		t.Fatal("failed unmute was reported as successful")
	}
	for index, want := range []string{"0\n", "1\n", "1\n"} {
		select {
		case request := <-requests:
			if request != want {
				t.Fatalf("request %d = %q, want %q", index, request, want)
			}
		case <-time.After(2 * microphoneReconcileInterval):
			t.Fatalf("request %d was not received", index)
		}
	}
	privacy.mu.Lock()
	defer privacy.mu.Unlock()
	if !privacy.muted || !privacy.desired || privacy.unknown {
		t.Fatalf(
			"muted=%t desired=%t unknown=%t, want reconciled mute",
			privacy.muted,
			privacy.desired,
			privacy.unknown,
		)
	}
}

func TestDSPBootReconcilesConfirmedMicrophoneMute(t *testing.T) {
	socketPath, requests := startMicControlResponder(t, "OK\n")
	statePath := filepath.Join(t.TempDir(), "microphone-state")
	privacy := newMicrophonePrivacyController(
		true,
		statePath,
		socketPath,
		nil,
		nil,
	)
	service := wampService{privacy: privacy}
	handled, err := service.handleDSPSessionEvent(
		context.Background(),
		nil,
		44,
		[]interface{}{
			wampEvent,
			uint64(44),
			uint64(1),
			map[string]interface{}{},
			[]interface{}{"dsp"},
		},
	)
	if err != nil || !handled {
		t.Fatalf("handled=%t error=%v", handled, err)
	}
	if request := <-requests; request != "1\n" {
		t.Fatalf("request = %q, want mute reconciliation", request)
	}
}

func TestDSPBootReconcileFailureKeepsWAMPSession(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "microphone-state")
	var logged string
	privacy := newMicrophonePrivacyController(
		true,
		statePath,
		filepath.Join(t.TempDir(), "missing.sock"),
		nil,
		func(format string, args ...interface{}) {
			logged = fmt.Sprintf(format, args...)
		},
	)
	service := wampService{privacy: privacy, logf: privacy.logf}

	handled, err := service.handleDSPSessionEvent(
		context.Background(),
		nil,
		44,
		[]interface{}{
			wampEvent,
			uint64(44),
			uint64(1),
			map[string]interface{}{},
			[]interface{}{"dsp"},
		},
	)
	if err != nil || !handled {
		t.Fatalf("handled=%t error=%v", handled, err)
	}
	if !strings.Contains(logged, "restore DSP microphone mute") {
		t.Fatalf("log = %q, want reconciliation failure", logged)
	}
	select {
	case <-privacy.reconcile:
	default:
		t.Fatal("reconciliation retry was not requested")
	}
}

func TestWAMPMicrophoneMuteUsesPrivacyOwner(t *testing.T) {
	socketPath, requests := startMicControlResponder(t, "OK\n")
	privacy := newMicrophonePrivacyController(
		false,
		filepath.Join(t.TempDir(), "microphone-state"),
		socketPath,
		nil,
		nil,
	)
	service := wampService{privacy: privacy}
	response := invokeForTest(
		t,
		&service,
		"com.harman.dsp.micMute",
		[]interface{}{uint64(1)},
	)
	if messageType(response) != wampYield {
		t.Fatalf("response = %#v", response)
	}
	if request := <-requests; request != "1\n" {
		t.Fatalf("request = %q, want mute", request)
	}
}

func TestIndicatorLEDArgumentsMatchRecoveredContract(t *testing.T) {
	target, mode, color, err := indicatorLEDArguments(
		[]interface{}{"front", "ignored", uint64(7)},
		map[string]interface{}{
			"mode":    "slow-blink",
			"color":   "amber",
			"unknown": true,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if target != "front" || mode != "slow-blink" || color != "amber" {
		t.Fatalf(
			"arguments = (%q, %q, %q)",
			target,
			mode,
			color,
		)
	}

	for _, test := range []struct {
		name   string
		args   []interface{}
		kwargs map[string]interface{}
	}{
		{name: "missing target", args: nil},
		{name: "target type", args: []interface{}{uint64(1)}},
		{
			name:   "mode type",
			args:   []interface{}{"front"},
			kwargs: map[string]interface{}{"mode": uint64(1)},
		},
		{
			name:   "color type",
			args:   []interface{}{"front"},
			kwargs: map[string]interface{}{"color": false},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, _, _, err := indicatorLEDArguments(
				test.args,
				test.kwargs,
			); err == nil {
				t.Fatal("invalid arguments accepted")
			}
		})
	}
}

func TestWAMPLedSetUsesKeywordArguments(t *testing.T) {
	writer := &recordingIndicatorLEDWriter{}
	service := wampService{
		indicatorLEDs: newIndicatorLEDController(writer),
	}
	response := invokeWithKwargsForTest(
		t,
		&service,
		"com.harman.ledSet",
		[]interface{}{"front", "ignored", uint64(7)},
		map[string]interface{}{
			"mode":    "slow-blink",
			"color":   "amber",
			"unknown": true,
		},
	)
	if messageType(response) != wampYield {
		t.Fatalf("response = %#v", response)
	}
	want := [][6]byte{{indicatorLEDCode, 3, 0, 0, 0, 0}}
	if frames := writer.recorded(); !reflect.DeepEqual(frames, want) {
		t.Fatalf("frames = %x, want %x", frames, want)
	}
}

func TestWAMPLedSetRequiresStringTarget(t *testing.T) {
	for _, args := range [][]interface{}{
		nil,
		{uint64(1)},
	} {
		writer := &recordingIndicatorLEDWriter{}
		service := wampService{
			indicatorLEDs: newIndicatorLEDController(writer),
		}
		response := invokeForTest(
			t,
			&service,
			"com.harman.ledSet",
			args,
		)
		if messageType(response) != wampError {
			t.Fatalf("args = %#v, response = %#v", args, response)
		}
		if frames := writer.recorded(); len(frames) != 0 {
			t.Fatalf("invalid args wrote frames: %x", frames)
		}
	}
}

func TestWAMPLedSetPropagatesWriteFailure(t *testing.T) {
	writer := &recordingIndicatorLEDWriter{
		errs: []error{errors.New("injected LED failure")},
	}
	service := wampService{
		indicatorLEDs: newIndicatorLEDController(writer),
	}
	response := invokeWithKwargsForTest(
		t,
		&service,
		"com.harman.ledSet",
		[]interface{}{"back"},
		map[string]interface{}{"mode": "on", "color": "ignored"},
	)
	if messageType(response) != wampError {
		t.Fatalf("response = %#v", response)
	}
	want := []interface{}{"set indicator LEDs: injected LED failure"}
	if !reflect.DeepEqual(response[5], want) {
		t.Fatalf("error args = %#v, want %#v", response[5], want)
	}
}

type unexpectedMessage struct {
	message interface{}
}

func (err *unexpectedMessage) Error() string {
	return fmt.Sprintf("unexpected WAMP message: %#v", err.message)
}

func invokeForTest(
	t *testing.T,
	service *wampService,
	procedure string,
	args []interface{},
) []interface{} {
	t.Helper()
	return invokeWithKwargsForTest(t, service, procedure, args, nil)
}

func invokeWithKwargsForTest(
	t *testing.T,
	service *wampService,
	procedure string,
	args []interface{},
	kwargs map[string]interface{},
) []interface{} {
	t.Helper()
	serviceConnection, peerConnection := net.Pipe()
	defer serviceConnection.Close()
	defer peerConnection.Close()
	client := &wampConnection{connection: serviceConnection}
	peer := &wampConnection{connection: peerConnection}
	done := make(chan error, 1)
	go func() {
		message := []interface{}{
			uint64(wampInvocation),
			uint64(9),
			uint64(77),
			map[string]interface{}{},
			args,
		}
		if kwargs != nil {
			message = append(message, kwargs)
		}
		done <- service.handleInvocation(
			context.Background(),
			client,
			map[uint64]string{77: procedure},
			message,
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

// A router may deliver an event or an invocation between a setup request and
// its reply. Those messages must reach the session loop rather than being
// mistaken for the reply or dropped.
func TestSetupResponseQueuesInterleavedMessages(t *testing.T) {
	clientConnection, routerConnection := net.Pipe()
	defer clientConnection.Close()
	defer routerConnection.Close()
	client := &wampConnection{connection: clientConnection, nextID: 1}
	router := &wampConnection{connection: routerConnection, nextID: 1}

	requestID := client.requestID()
	routerDone := make(chan error, 1)
	go func() {
		for _, message := range [][]interface{}{
			{wampEvent, uint64(300), uint64(7), map[string]interface{}{}},
			{wampInvocation, uint64(9), uint64(201),
				map[string]interface{}{}},
			{wampRegistered, requestID, uint64(205)},
		} {
			if err := router.writeFrame(message); err != nil {
				routerDone <- err
				return
			}
		}
		routerDone <- nil
	}()

	var deferred [][]interface{}
	response, err := client.awaitSetupResponse(
		wampRegistered,
		requestID,
		"registration",
		&deferred,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-routerDone; err != nil {
		t.Fatal(err)
	}
	registrationID, ok := unsigned(response[2])
	if !ok || registrationID != 205 {
		t.Fatalf("registration response = %v, want registration 205", response)
	}
	if len(deferred) != 2 {
		t.Fatalf("queued %d messages, want the event and the invocation",
			len(deferred))
	}
	if messageType(deferred[0]) != wampEvent ||
		messageType(deferred[1]) != wampInvocation {
		t.Fatalf("queued the wrong messages: %v", deferred)
	}
}

// A router that answers a setup request with ERROR must fail the session
// instead of waiting for a reply that will never arrive.
func TestSetupResponseFailsOnError(t *testing.T) {
	clientConnection, routerConnection := net.Pipe()
	defer clientConnection.Close()
	defer routerConnection.Close()
	client := &wampConnection{connection: clientConnection, nextID: 1}
	router := &wampConnection{connection: routerConnection, nextID: 1}

	requestID := client.requestID()
	go func() {
		_ = router.writeFrame([]interface{}{
			wampError,
			uint64(wampRegister),
			requestID,
			map[string]interface{}{},
			"wamp.error.procedure_already_exists",
		})
	}()

	var deferred [][]interface{}
	if _, err := client.awaitSetupResponse(
		wampRegistered,
		requestID,
		"registration",
		&deferred,
	); err == nil {
		t.Fatal("registration error was accepted")
	}
}
