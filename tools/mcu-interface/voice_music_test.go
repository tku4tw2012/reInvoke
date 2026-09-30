// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
)

type fakeStereoSoftvol struct {
	mu         sync.Mutex
	value      [2]int
	writes     [][2]int
	readErr    error
	writeErr   error
	writeOnErr bool
	rejectSwap bool
	closed     bool
}

func (control *fakeStereoSoftvol) ReadChannels() ([2]int, error) {
	control.mu.Lock()
	defer control.mu.Unlock()
	return control.value, control.readErr
}

func (control *fakeStereoSoftvol) CompareAndSwapChannels(before, after [2]int) (bool, error) {
	control.mu.Lock()
	defer control.mu.Unlock()
	if control.rejectSwap || control.value != before {
		return false, nil
	}
	if control.writeErr != nil && !control.writeOnErr {
		return false, control.writeErr
	}
	control.value = after
	control.writes = append(control.writes, after)
	return true, control.writeErr
}

func (control *fakeStereoSoftvol) Close() error {
	control.mu.Lock()
	defer control.mu.Unlock()
	control.closed = true
	return nil
}

func (control *fakeStereoSoftvol) levels() [2]int {
	value, _ := control.ReadChannels()
	return value
}

func (control *fakeStereoSoftvol) externalWrite(value [2]int) {
	control.mu.Lock()
	defer control.mu.Unlock()
	control.value = value
}

func newTestMusicDuck() (*voiceMusicDuck, *fakeStereoSoftvol) {
	control := &fakeStereoSoftvol{value: [2]int{250, 237}}
	return &voiceMusicDuck{control: control, playing: func() bool { return true }}, control
}

func TestVoiceMusicDuckRestoresActualChannels(t *testing.T) {
	duck, control := newTestMusicDuck()
	original := control.levels()
	for turn := 0; turn < 3; turn++ {
		for tick := 0; tick < 5; tick++ {
			if err := duck.SetActive(true); err != nil {
				t.Fatal(err)
			}
			want := [2]int{voiceMusicLevel(original[0]), voiceMusicLevel(original[1])}
			if got := control.levels(); got != want {
				t.Fatalf("duck accumulated across ticks/turns: %v != %v", got, want)
			}
		}
		if err := duck.SetActive(false); err != nil {
			t.Fatal(err)
		}
		if got := control.levels(); got != original {
			t.Fatalf("actual stereo baseline not restored: %v", got)
		}
	}
	if len(control.writes) != 6 {
		t.Fatalf("idempotent phase refresh wrote music again: %v", control.writes)
	}
}

func TestVoiceMusicDuckDoesNotTouchIdleOrCreatePCM(t *testing.T) {
	opened := 0
	active := false
	duck := &voiceMusicDuck{
		playing: func() bool { return active },
		open: func() (stereoSoftvol, error) {
			opened++
			return nil, errors.New("music control does not exist")
		},
	}
	for _, enabled := range []bool{false, true, true, false} {
		if err := duck.SetActive(enabled); err != nil {
			t.Fatal(err)
		}
	}
	if opened != 0 {
		t.Fatal("idle voice touched or initialized an ALSA control")
	}
	active = true
	if err := duck.SetActive(true); err == nil || opened != 1 {
		t.Fatal("missing active music control did not return a retryable failure")
	}
}

func TestVoiceMusicDuckPreservesUserChanges(t *testing.T) {
	duck, control := newTestMusicDuck()
	if err := duck.SetActive(true); err != nil {
		t.Fatal(err)
	}
	user := [2]int{163, 155}
	control.externalWrite(user)
	if err := duck.SetActive(false); err != nil {
		t.Fatal(err)
	}
	if got := control.levels(); got != user {
		t.Fatalf("voice restored over an external user's change: %v", got)
	}
	if err := duck.SetActive(true); err != nil {
		t.Fatal(err)
	}
	if err := duck.Write(211); err != nil {
		t.Fatal(err)
	}
	if got, err := duck.Read(); err != nil || got != 211 {
		t.Fatalf("dial read the attenuated level instead of its own trim: %d %v", got, err)
	}
	if got := control.levels(); got != [2]int{voiceMusicLevel(211), voiceMusicLevel(211)} {
		t.Fatalf("dial write bypassed the active music-only duck: %v", got)
	}
	if err := duck.SetActive(false); err != nil {
		t.Fatal(err)
	}
	if got := control.levels(); got != [2]int{211, 211} {
		t.Fatalf("voice lost the dial change: %v", got)
	}
}

func TestVoiceMusicDuckRetriesUncertainWritesAndRestore(t *testing.T) {
	duck, control := newTestMusicDuck()
	original := control.levels()
	control.writeErr, control.writeOnErr = errors.New("uncertain write"), true
	if err := duck.SetActive(true); err == nil {
		t.Fatal("uncertain write was silently accepted")
	}
	control.writeErr = nil
	if err := duck.SetActive(true); err != nil {
		t.Fatal(err)
	}
	if len(control.writes) != 1 {
		t.Fatal("retry applied a second duck after a write that actually succeeded")
	}
	control.writeErr, control.writeOnErr = errors.New("restore failed"), false
	if err := duck.SetActive(false); err == nil {
		t.Fatal("failed restore was silently accepted")
	}
	control.writeErr = nil
	if err := duck.SetActive(false); err != nil {
		t.Fatal(err)
	}
	if control.levels() != original {
		t.Fatal("retry lost the saved pre-voice level")
	}
}

func TestVoiceMusicDoesNotPushDSPOrChangeMasterState(t *testing.T) {
	duck, control := newTestMusicDuck()
	media := &dspVolumeController{volume: 61, softvol: duck, notify: make(chan struct{}, 1)}
	before := media.effectiveLevel()
	if err := duck.SetActive(true); err != nil {
		t.Fatal(err)
	}
	if media.effectiveLevel() != before || media.volume != 61 || len(media.ducks) != 0 || len(media.notify) != 0 {
		t.Fatal("voice duck changed the master DSP level or queued a DSP push")
	}
	if err := media.fadeToTarget(context.Background(), 248); err != nil {
		t.Fatal(err)
	}
	if control.levels()[0] != voiceMusicLevel(248) {
		t.Fatal("existing dial fade fought the voice duck")
	}
	if err := duck.SetActive(false); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(control.levels(), [2]int{248, 248}) {
		t.Fatal("dial's final trim was not restored")
	}
}
