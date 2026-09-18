// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const syntheticBond = "bluetooth/02:00:00:00:00:01/02:00:00:00:00:02/info"

func fixtureStore(t *testing.T) storage {
	t.Helper()
	base, err := filepath.Abs(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := storage{
		root: filepath.Join(base, "persist"), runtime: filepath.Join(base, "run"),
		bonds: filepath.Join(base, "bluetooth"), uid: uint32(os.Geteuid()),
		check: func() error { return nil },
	}
	for _, directory := range []string{s.root, s.runtime, s.bonds} {
		if err := os.Mkdir(directory, 0700); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func testProfile() *profile {
	return &profile{SSID: "synthetic-network", PSK: strings.Repeat("a", 64), Security: "wpa2-psk"}
}

func testSnapshot() snapshot {
	return snapshot{Version: 1, WiFi: testProfile(), Files: map[string][]byte{
		"microphone-state": []byte("muted\n"), "music-volume": []byte("7\n"),
		syntheticBond: []byte("[General]\nTrusted=true\n[LinkKey]\nKey=00112233445566778899001122334455\n"),
	}}
}

func TestSaveRestoreAcrossFreshBootAndUnchangedWrite(t *testing.T) {
	s := fixtureStore(t)
	state := testSnapshot()
	if err := s.save(state); err != nil {
		t.Fatal(err)
	}
	first, _ := os.Stat(filepath.Join(s.root, "state.json"))
	s.beforeMove = func() error { t.Fatal("unchanged snapshot was rewritten"); return nil }
	if err := s.save(state); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(filepath.Join(s.root, "state.json"))
	if !os.SameFile(first, after) {
		t.Fatal("unchanged state changed inode")
	}
	fresh := fixtureStore(t)
	fresh.root = s.root
	loaded, err := fresh.load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.WiFi == nil || *loaded.WiFi != *state.WiFi {
		t.Fatal("station profile not retained")
	}
	if err := fresh.restore(loaded); err != nil {
		t.Fatal(err)
	}
	if err := fresh.restore(loaded); err == nil {
		t.Fatal("restore into live bond tree accepted")
	}
	for name, expected := range state.Files {
		path := filepath.Join(fresh.runtime, name)
		if strings.HasPrefix(name, "bluetooth/") {
			path = filepath.Join(fresh.bonds, strings.TrimPrefix(name, "bluetooth/"))
		}
		data, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(data, expected) {
			t.Fatal("restored state mismatch")
		}
		info, _ := os.Stat(path)
		if info.Mode().Perm() != 0600 {
			t.Fatal("restored file permissions too broad")
		}
	}
	recaptured, err := fresh.capture(loaded.WiFi)
	if err != nil || len(recaptured.Files) != len(loaded.Files) {
		t.Fatalf("capture restored state: %v", err)
	}
}

func TestNoEmptySuccessOrUnsupportedStorage(t *testing.T) {
	s := fixtureStore(t)
	if _, err := s.load(); err == nil || err.Error() != "PERSIST_STATE_ABSENT" {
		t.Fatal("absent state reported success")
	}
	if err := s.save(snapshot{Version: 1, Files: map[string][]byte{}}); err == nil {
		t.Fatal("empty snapshot reported success")
	}
	if _, err := s.capture(nil); err == nil {
		t.Fatal("empty capture reported success")
	}
	s.check = func() error { return errors.New("PERSIST_NOT_MOUNTED") }
	if err := s.save(testSnapshot()); err == nil || err.Error() != "PERSIST_NOT_MOUNTED" {
		t.Fatal("unmounted backing storage accepted")
	}
	if _, err := os.Stat(filepath.Join(s.root, "state.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("fallback RAM persistence file created")
	}
}

func TestAtomicFailurePreservesCommittedState(t *testing.T) {
	s := fixtureStore(t)
	if err := s.save(testSnapshot()); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(s.root, "state.json"))
	s.beforeMove = func() error { return errors.New("synthetic interruption before rename") }
	changed := testSnapshot()
	changed.Files["music-volume"] = []byte("8\n")
	if err := s.save(changed); err == nil {
		t.Fatal("interrupted commit reported success")
	}
	after, _ := os.ReadFile(filepath.Join(s.root, "state.json"))
	if !bytes.Equal(before, after) {
		t.Fatal("old generation damaged")
	}
	if _, err := s.load(); err != nil {
		t.Fatal("old generation not loadable")
	}
	entries, _ := os.ReadDir(s.root)
	if len(entries) != 1 {
		t.Fatal("failed update left staging files")
	}
}

func TestInvalidCorruptAndUnsafeState(t *testing.T) {
	for _, name := range []string{
		"../escape", "bluetooth/../../escape", "pid", "wpa_supplicant.conf", "playback-lease",
		"bluetooth/02:00:00:00:00:01/cache/02:00:00:00:00:02",
	} {
		state := testSnapshot()
		state.Files[name] = []byte("synthetic\n")
		if err := validateSnapshot(state); err == nil {
			t.Fatal("non-allowlisted file accepted")
		}
	}
	for _, data := range []string{"", "101\n", "-1\n", "07\n", "7\npayload"} {
		if err := validateFile("music-volume", []byte(data)); err == nil {
			t.Fatal("invalid volume accepted")
		}
	}
	s := fixtureStore(t)
	path := filepath.Join(s.root, "state.json")
	if err := s.save(testSnapshot()); err != nil {
		t.Fatal(err)
	}
	content, _ := os.ReadFile(path)
	content = bytes.Replace(content, []byte("muted"), []byte("altered"), 1)
	// The JSON []byte encoding may not contain a plaintext value; damage the hash.
	content = bytes.Replace(content, []byte(`"sha256":"`), []byte(`"sha256":"0`), 1)
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.load(); err == nil {
		t.Fatal("corrupt checksum accepted")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(s.runtime, "secret"), path); err != nil {
		t.Fatal(err)
	}
	if err := s.save(testSnapshot()); err == nil {
		t.Fatal("snapshot symlink accepted")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.load(); err == nil {
		t.Fatal("public snapshot accepted")
	}
}

func TestCaptureRejectsUnsafeBondsAndLimits(t *testing.T) {
	s := fixtureStore(t)
	if err := s.restore(testSnapshot()); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.bonds, strings.TrimPrefix(syntheticBond, "bluetooth/"))
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(s.runtime, "microphone-state"), path); err != nil {
		t.Fatal(err)
	}
	if _, err := s.capture(testProfile()); err == nil {
		t.Fatal("bond symlink accepted")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), maxFile+1), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.capture(testProfile()); err == nil {
		t.Fatal("oversized bond accepted")
	}
}

func TestProfileServiceDurabilityAndNoFalseSuccess(t *testing.T) {
	store := fixtureStore(t)
	service := service{store: store, current: snapshot{Version: 1, Files: map[string][]byte{}}}
	if service.handle(request{Operation: "load"}).OK {
		t.Fatal("missing profile load reported success")
	}

	profile := testProfile()
	if response := service.handle(request{Operation: "save", Profile: profile}); !response.OK {
		t.Fatalf("save failed: %s", response.Code)
	}
	loaded, err := store.load()
	if err != nil || loaded.WiFi == nil || *loaded.WiFi != *profile {
		t.Fatal("save success preceded durable profile")
	}
	bad := *profile
	bad.PSK = "not-a-derived-key"
	if service.handle(request{Operation: "save", Profile: &bad}).OK {
		t.Fatal("invalid key accepted")
	}
	if service.handle(request{Operation: "save", Profile: nil}).OK {
		t.Fatal("missing profile accepted")
	}
	next := *profile
	next.SSID = "another-synthetic-network"
	if response := service.handle(request{Operation: "save", Profile: &next}); response.OK ||
		response.Code != "PERSIST_SAVE_RATE_LIMITED" {
		t.Fatal("write wear rate limit missing")
	}
	if response := service.handle(request{Operation: "load"}); !response.OK || *response.Profile != *profile {
		t.Fatal("failed save replaced working profile")
	}
	store.check = func() error { return errors.New("PERSIST_NOT_MOUNTED") }
	service.store = store
	if service.handle(request{Operation: "load"}).OK {
		t.Fatal("RAM cached profile pretended storage still mounted")
	}
}

func TestCorruptLiveStoreAndMissingRAMDoNotReportSuccess(t *testing.T) {
	store := fixtureStore(t)
	state := testSnapshot()
	if err := store.save(state); err != nil {
		t.Fatal(err)
	}
	if err := store.restore(state); err != nil {
		t.Fatal(err)
	}
	s := service{store: store, current: state}
	if err := os.Remove(filepath.Join(store.runtime, "microphone-state")); err != nil {
		t.Fatal(err)
	}
	if err := s.flush(); err == nil || err.Error() != "PERSIST_RUNTIME_STATE_MISSING" {
		t.Fatal("missing runtime micMute replaced committed state")
	}
	path := filepath.Join(store.root, "state.json")
	if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, req := range []request{{Operation: "load"}, {Operation: "save", Profile: state.WiFi}} {
		if s.handle(req).OK {
			t.Fatal("RAM cache disguised corrupted live store")
		}
	}
	if err := store.save(state); err == nil {
		t.Fatal("corrupt state silently overwritten")
	}
}

func TestBondKeysAndInterruptedCleanupValidation(t *testing.T) {
	for _, data := range []string{
		"[General]\n[LinkKey]\nKey=short\n",
		"[General]\n[LinkKey]\n",
		"[General]\n[LongTermKey]\nKey=not-hexadecimal-key-material-now\n",
		"[General]\ntruncated-key",
	} {
		if err := validateFile(syntheticBond, []byte(data)); err == nil {
			t.Fatal("incomplete bond key accepted")
		}
	}
	store := fixtureStore(t)
	stage := filepath.Join(store.root, ".state-interrupted")
	if err := os.WriteFile(stage, []byte{}, 0600); err != nil {
		t.Fatal(err)
	}
	if err := store.cleanInterruptedWrites(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stage); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("interrupted staging file retained")
	}
	if err := os.Symlink(filepath.Join(store.runtime, "do-not-delete"), stage); err != nil {
		t.Fatal(err)
	}
	if err := store.cleanInterruptedWrites(); err == nil {
		t.Fatal("unsafe interrupted staging path accepted")
	}
}

func TestPrivateOwnershipHardlinkAndDirectoryModes(t *testing.T) {
	store := fixtureStore(t)
	if err := store.save(testSnapshot()); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(store.root, "state.json")
	if _, err := readPrivate(path, store.uid+1, maxSnapshot); err == nil {
		t.Fatal("wrong ownership accepted")
	}
	link := filepath.Join(store.root, "other")
	if err := os.Link(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := store.load(); err == nil {
		t.Fatal("hardlinked state accepted")
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(store.root, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := store.load(); err == nil {
		t.Fatal("non-private storage directory accepted")
	}
}

func TestSeedCommitIsAtomicAbsentOnly(t *testing.T) {
	store := fixtureStore(t)
	s := service{store: store, current: snapshot{Version: 1, Files: map[string][]byte{}}}
	if response := s.handle(request{Operation: "save-seed", Profile: testProfile()}); !response.OK {
		t.Fatalf("first seed commit failed: %s", response.Code)
	}
	before, _ := os.ReadFile(filepath.Join(store.root, "state.json"))
	replacement := *testProfile()
	replacement.SSID = "different-synthetic-seed"
	if response := s.handle(request{Operation: "save-seed", Profile: &replacement}); response.OK ||
		response.Code != "PERSIST_PROFILE_EXISTS" {
		t.Fatal("seed overwrote a saved profile")
	}
	after, _ := os.ReadFile(filepath.Join(store.root, "state.json"))
	if !bytes.Equal(before, after) {
		t.Fatal("rejected seed modified committed snapshot")
	}
	// An independently visible durable profile also wins over an empty RAM cache.
	s.current = snapshot{Version: 1, Files: map[string][]byte{}}
	if response := s.handle(request{Operation: "save-seed", Profile: &replacement}); response.OK ||
		response.Code != "PERSIST_PROFILE_EXISTS" {
		t.Fatal("durable saved profile lost to seed through stale RAM cache")
	}
}
