// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

type testVoiceLights struct {
	starts []string
	stops  int
	err    error
}

func (lights *testVoiceLights) StartVoice(_ context.Context, name string) error {
	lights.starts = append(lights.starts, name)
	return lights.err
}

func (lights *testVoiceLights) StopVoice(context.Context) error {
	lights.stops++
	return lights.err
}

type testVoiceCue struct {
	started chan string
	stopped chan string
}

func (cue *testVoiceCue) Play(ctx context.Context, name string) error {
	select {
	case cue.started <- name:
	case <-ctx.Done():
		return ctx.Err()
	}
	<-ctx.Done()
	cue.stopped <- name
	return ctx.Err()
}

func newTestVoice(t *testing.T) (*voiceFeedbackController, *testVoiceLights, *testVoiceCue, *fakeStereoSoftvol, *time.Time) {
	t.Helper()
	now := time.Now()
	lights := &testVoiceLights{}
	cues := &testVoiceCue{started: make(chan string, 16), stopped: make(chan string, 16)}
	music, control := newTestMusicDuck()
	voice := &voiceFeedbackController{
		lights: lights, cues: cues, music: music, blocked: func() bool { return false },
		now: func() time.Time { return now },
	}
	t.Cleanup(func() { _ = voice.Reset() })
	return voice, lights, cues, control, &now
}

func expectVoiceCue(t *testing.T, channel <-chan string, want string) {
	t.Helper()
	select {
	case got := <-channel:
		if got != want {
			t.Fatalf("cue %q, want %q", got, want)
		}
	case <-time.After(time.Second):
		t.Fatalf("cue %q was not observed", want)
	}
}

func TestWAMPVoiceStateControlsActualEffects(t *testing.T) {
	voice, lights, cues, control, now := newTestVoice(t)
	original := control.levels()
	bluetooth := filepath.Join(t.TempDir(), "bluetooth-state")
	if err := os.WriteFile(bluetooth, []byte("connected"), 0600); err != nil {
		t.Fatal(err)
	}
	service := &wampService{voice: voice, bluetoothState: bluetooth}
	report := func(state string) {
		t.Helper()
		response := invokeWithKwargsForTest(t, service, "com.harman.extStateUpdate",
			[]interface{}{"voice"}, map[string]interface{}{"state": state})
		if messageType(response) != wampYield || voice.State() != state {
			t.Fatalf("voice report acknowledged without applying state: %v", response)
		}
		if got := response[4].(map[string]interface{})["state"]; got != state {
			t.Fatalf("voice result state = %v, want %q", got, state)
		}
	}
	report("")
	if len(lights.starts) != 0 || lights.stops != 0 || len(control.writes) != 0 {
		t.Fatal("initial idle touched default-image feedback")
	}
	report("listening")
	expectVoiceCue(t, cues.started, "listening")
	if !voice.Active() || control.levels() == original {
		t.Fatal("listening did not mark voice active and duck music")
	}
	report("listening")
	if len(lights.starts) != 1 {
		t.Fatal("lease refresh restarted the listening animation")
	}
	report("thinking")
	expectVoiceCue(t, cues.stopped, "listening")
	*now = now.Add(voiceThinkingWait - time.Millisecond)
	if err := voice.tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case cue := <-cues.started:
		t.Fatalf("processing cue played before 1350ms: %s", cue)
	default:
	}
	*now = now.Add(time.Millisecond)
	if err := voice.tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	expectVoiceCue(t, cues.started, "processing")
	report("speaking")
	expectVoiceCue(t, cues.stopped, "processing")
	report("")
	if voice.Active() || control.levels() != original || lights.stops != 1 {
		t.Fatal("idle did not release voice effects and restore actual music levels")
	}
	want := []string{"L_101_c_listening", "L_104_c_thinking", "L_105_c_cortanaspeaking"}
	if !reflect.DeepEqual(lights.starts, want) {
		t.Fatalf("voice LEDs = %v", lights.starts)
	}
	state, err := os.ReadFile(bluetooth)
	if err != nil || string(state) != "connected" {
		t.Fatal("voice updates changed the Bluetooth indicator authority")
	}
}

func TestVoiceFastReplyCancelsThinkingCue(t *testing.T) {
	for _, next := range []string{"speaking", ""} {
		t.Run(map[string]string{"speaking": "reply", "": "cancel"}[next], func(t *testing.T) {
			voice, _, cues, _, now := newTestVoice(t)
			if err := voice.Update(context.Background(), "thinking"); err != nil {
				t.Fatal(err)
			}
			*now = now.Add(500 * time.Millisecond)
			if err := voice.Update(context.Background(), next); err != nil {
				t.Fatal(err)
			}
			*now = now.Add(2 * time.Second)
			if err := voice.tick(context.Background()); err != nil {
				t.Fatal(err)
			}
			select {
			case cue := <-cues.started:
				t.Fatalf("stale thinking cue played after %s: %s", next, cue)
			default:
			}
		})
	}
}

func TestVoiceLeaseBoundsStaleState(t *testing.T) {
	voice, lights, cues, control, now := newTestVoice(t)
	original := control.levels()
	if err := voice.Update(context.Background(), "listening"); err != nil {
		t.Fatal(err)
	}
	expectVoiceCue(t, cues.started, "listening")
	*now = now.Add(voiceLease - time.Second)
	if !voice.Active() {
		t.Fatal("active lease expired too soon")
	}
	if err := voice.Update(context.Background(), "listening"); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(voiceLease)
	if voice.Active() {
		t.Fatal("expired session still steals music's short tap")
	}
	if err := voice.tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	expectVoiceCue(t, cues.stopped, "listening")
	if voice.State() != "" || control.levels() != original || lights.stops != 1 {
		t.Fatal("expired lease left stale audio or lights")
	}
	if len(lights.starts) != 1 {
		t.Fatal("lease renewal replayed a cue or animation")
	}
}

func TestWAMPVoiceMalformedAndOtherSubsystems(t *testing.T) {
	voice, lights, _, control, _ := newTestVoice(t)
	service := &wampService{voice: voice}
	for _, kwargs := range []map[string]interface{}{
		{}, {"state": true}, {"state": "sideways"}, {"state": "idle"},
	} {
		response := invokeWithKwargsForTest(t, service, "com.harman.extStateUpdate",
			[]interface{}{"voice"}, kwargs)
		if messageType(response) != wampError {
			t.Fatalf("invalid voice state accepted: %v", kwargs)
		}
	}
	response := invokeWithKwargsForTest(t, service, "com.harman.extStateUpdate",
		[]interface{}{"unrelated"}, map[string]interface{}{"state": "thinking"})
	if messageType(response) != wampYield || voice.Active() ||
		len(lights.starts) != 0 || len(control.writes) != 0 {
		t.Fatal("unrelated external state moved voice feedback")
	}
}

func TestVoiceMuteCleanup(t *testing.T) {
	voice, lights, cues, control, _ := newTestVoice(t)
	original := control.levels()
	mic := newMicrophoneMuteController(false, filepath.Join(t.TempDir(), "microphone-state"),
		newStubDSPSocket(t), nil, nil)
	voice.blocked = mic.VoiceBlocked
	mic.onVoiceBlocked = func() {
		if err := voice.Reset(); err != nil {
			t.Error(err)
		}
	}
	if err := voice.Update(context.Background(), "listening"); err != nil {
		t.Fatal(err)
	}
	expectVoiceCue(t, cues.started, "listening")
	if err := mic.Set(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	expectVoiceCue(t, cues.stopped, "listening")
	if voice.Active() || control.levels() != original || lights.stops != 1 {
		t.Fatal("microphone mute did not synchronously release voice-owned effects")
	}
	if err := voice.Update(context.Background(), "speaking"); err == nil {
		t.Fatal("muted microphone accepted voice effects")
	}
	if len(lights.starts) != 1 || len(control.writes) != 2 {
		t.Fatal("suppressed voice call still touched audio or lights")
	}
}

type lockedVoiceLEDWriter struct {
	mu      sync.Mutex
	packets [][]byte
}

func (writer *lockedVoiceLEDWriter) WriteMCUData(packet []byte) error {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	writer.packets = append(writer.packets, append([]byte(nil), packet...))
	return nil
}

func TestVoiceLEDCleanupNeverClearsMutedRed(t *testing.T) {
	directory := t.TempDir()
	for name, value := range map[string]byte{micMuteLEDName: 0xee, "L_101_c_listening": 0xaa, "L_312_d_shorttap": 0x11} {
		data := make([]byte, ledFrameBytes)
		for index := range data {
			data[index] = value
		}
		if err := os.WriteFile(filepath.Join(directory, name+".bin"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	writer := &lockedVoiceLEDWriter{}
	player := &ledPlayer{directory: directory, writer: writer}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := player.StartVoice(ctx, "L_101_c_listening"); err != nil {
		t.Fatal(err)
	}
	if err := player.SetMicrophoneMuted(ctx, true); err != nil {
		t.Fatal(err)
	}
	if err := player.StartVoice(ctx, "L_101_c_listening"); err == nil {
		t.Fatal("voice animation replaced muted red")
	}
	if err := player.StopVoice(ctx); err != nil {
		t.Fatal(err)
	}
	if err := player.Apply(ctx, inputEvent{Name: "action"}); err != nil {
		t.Fatal(err)
	}
	cancel()
	player.mu.Lock()
	player.stopLocked()
	player.mu.Unlock()
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if len(writer.packets) < 2 {
		t.Fatal("test did not apply listening and then muted red")
	}
	for _, packet := range writer.packets[1:] {
		if len(packet) != 2+ledFrameBytes || packet[2] != 0xee {
			t.Fatalf("voice cleanup/short tap cleared or replaced muted red: %x", packet)
		}
	}
}

func TestVoiceFailureRetriesWithoutReplayingCue(t *testing.T) {
	voice, lights, cues, _, now := newTestVoice(t)
	lights.err = errors.New("LED write failed")
	if err := voice.Update(context.Background(), "listening"); err == nil {
		t.Fatal("LED failure reported as a complete phase apply")
	}
	expectVoiceCue(t, cues.started, "listening")
	lights.err = nil
	*now = now.Add(voiceRetryWait)
	if err := voice.tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(lights.starts) != 2 {
		t.Fatal("failed voice LED did not retry")
	}
	select {
	case cue := <-cues.started:
		t.Fatalf("retry replayed the listening cue: %s", cue)
	default:
	}
}

func TestVoiceWAMPDisconnectCleanup(t *testing.T) {
	voice, lights, cues, control, _ := newTestVoice(t)
	original := control.levels()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	ready, release := make(chan struct{}), make(chan struct{})
	routerDone := make(chan error, 1)
	go func() {
		routerDone <- func() error {
			connection, err := listener.Accept()
			if err != nil {
				return err
			}
			defer connection.Close()
			_ = connection.SetDeadline(time.Now().Add(3 * time.Second))
			var handshake [4]byte
			if _, err := io.ReadFull(connection, handshake[:]); err != nil {
				return err
			}
			if _, err := connection.Write(handshake[:]); err != nil {
				return err
			}
			router := &wampConnection{connection: connection}
			if _, err := router.readFrame(); err != nil {
				return err
			}
			if err := router.writeFrame([]interface{}{wampWelcome, uint64(100), map[string]interface{}{}}); err != nil {
				return err
			}
			var voiceRegistration uint64
			for index, procedure := range procedures {
				message, err := router.readFrame()
				if err != nil {
					return err
				}
				if messageType(message) != wampRegister || message[3] != procedure {
					return &unexpectedMessage{message: message}
				}
				registration := uint64(200 + index)
				if procedure == "com.harman.extStateUpdate" {
					voiceRegistration = registration
				}
				if err := router.writeFrame([]interface{}{wampRegistered, message[1], registration}); err != nil {
					return err
				}
			}

			for index := 0; index < 2; index++ {
				message, err := router.readFrame()
				if err != nil {
					return err
				}
				if err := router.writeFrame([]interface{}{wampSubscribed, message[1], uint64(300 + index)}); err != nil {
					return err
				}
			}
			if _, err := router.readFrame(); err != nil {
				return err
			}
			if err := router.writeFrame([]interface{}{wampInvocation, uint64(9), voiceRegistration,
				map[string]interface{}{}, []interface{}{"voice"}, map[string]interface{}{"state": "listening"}}); err != nil {
				return err
			}
			reply, err := router.readFrame()
			if err != nil {
				return err
			}
			if messageType(reply) != wampYield {
				return &unexpectedMessage{message: reply}
			}
			close(ready)
			<-release
			return nil
		}()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	service := &wampService{address: listener.Addr().String(), realm: "default", voice: voice}
	serviceDone := make(chan error, 1)
	go func() { serviceDone <- service.run(ctx) }()
	select {
	case <-ready:
	case err := <-routerDone:
		t.Fatalf("router setup failed: %v", err)
	case <-ctx.Done():
		t.Fatal("voice WAMP session did not initialize")
	}
	expectVoiceCue(t, cues.started, "listening")
	close(release)
	select {
	case err := <-serviceDone:
		if err == nil {
			t.Fatal("router loss reported as success")
		}
	case <-ctx.Done():
		t.Fatal("router disconnect did not join voice cleanup")
	}
	if err := <-routerDone; err != nil {
		t.Fatal(err)
	}
	expectVoiceCue(t, cues.stopped, "listening")
	if voice.Active() || control.levels() != original || lights.stops != 1 {
		t.Fatal("WAMP session loss left stale voice effects")
	}
}

func TestVoiceRunExitRestoresMusic(t *testing.T) {
	voice, lights, cues, control, _ := newTestVoice(t)
	original := control.levels()
	if err := voice.Update(context.Background(), "listening"); err != nil {
		t.Fatal(err)
	}
	expectVoiceCue(t, cues.started, "listening")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		voice.Run(ctx)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("service exit did not join voice cleanup")
	}
	expectVoiceCue(t, cues.stopped, "listening")
	if voice.Active() || control.levels() != original || lights.stops != 1 {
		t.Fatal("service exit left active voice effects")
	}
}
