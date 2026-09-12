// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"unicode/utf8"
)

const (
	maxSnapshot = 2 * 1024 * 1024
	maxFile     = 64 * 1024
	maxFiles    = 128
)

var addressPattern = regexp.MustCompile(`^[0-9A-F]{2}(:[0-9A-F]{2}){5}$`)

type profile struct {
	SSID     string `json:"ssid"`
	PSK      string `json:"psk"`
	Security string `json:"security"`
	Hidden   bool   `json:"hidden,omitempty"`
}

type snapshot struct {
	Version int               `json:"version"`
	WiFi    *profile          `json:"wifi,omitempty"`
	Files   map[string][]byte `json:"files"`
}

type envelope struct {
	State  json.RawMessage `json:"state"`
	SHA256 string          `json:"sha256"`
}

type storage struct {
	root       string
	runtime    string
	bonds      string
	uid        uint32
	check      func() error
	beforeMove func() error
}

func validateProfile(p profile) error {
	if len(p.SSID) < 1 || len(p.SSID) > 32 || !utf8.ValidString(p.SSID) ||
		strings.ContainsAny(p.SSID, "\x00\r\n") || p.Security != "wpa2-psk" {
		return errors.New("PERSIST_PROFILE_INVALID")
	}
	key, err := hex.DecodeString(p.PSK)
	if err != nil || len(key) != 32 || len(p.PSK) != 64 {
		return errors.New("PERSIST_PROFILE_INVALID")
	}
	return nil
}

func decodeStrict(content []byte, value interface{}) error {
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return errors.New("PERSIST_STATE_CORRUPT")
	}
	var extra interface{}
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("PERSIST_STATE_CORRUPT")
	}
	return nil
}

func trustedParents(path string, uid uint32) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("PERSIST_PATH_UNSAFE")
	}
	for {
		info, err := os.Lstat(path)
		if err != nil {
			return errors.New("PERSIST_PATH_UNSAFE")
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		// Nonzero uid is used only by unprivileged offline fixtures under the
		// developer's group-writable checkout. The executable always passes 0.
		forbidden := os.FileMode(0022)
		if uid != 0 {
			forbidden = 0002
		}
		if !ok || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 ||
			(stat.Uid != uid && stat.Uid != 0) || info.Mode().Perm()&forbidden != 0 {
			return errors.New("PERSIST_PATH_UNSAFE")
		}
		if path == "/" {
			return nil
		}
		path = filepath.Dir(path)
	}
}

func ensurePrivateDirectory(path string, uid uint32) error {
	if err := trustedParents(filepath.Dir(path), uid); err != nil {
		return err
	}
	createErr := os.Mkdir(path, 0700)
	if createErr != nil && !errors.Is(createErr, os.ErrExist) {
		return errors.New("PERSIST_DIRECTORY_CREATE_FAILED")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return errors.New("PERSIST_DIRECTORY_UNSAFE")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || stat.Uid != uid || info.Mode().Perm() != 0700 {
		return errors.New("PERSIST_DIRECTORY_UNSAFE")
	}
	if createErr == nil {
		parent, err := os.Open(filepath.Dir(path))
		if err != nil {
			return errors.New("PERSIST_SYNC_FAILED")
		}
		defer parent.Close()
		if err := parent.Sync(); err != nil {
			return errors.New("PERSIST_SYNC_FAILED")
		}
	}
	return nil
}

func readPrivate(path string, uid uint32, limit int) ([]byte, error) {
	if err := trustedParents(filepath.Dir(path), uid); err != nil {
		return nil, err
	}
	before, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, os.ErrNotExist
		}
		return nil, errors.New("PERSIST_FILE_UNSAFE")
	}
	owner, ok := before.Sys().(*syscall.Stat_t)
	if !ok || !before.Mode().IsRegular() || owner.Uid != uid || owner.Nlink != 1 ||
		before.Mode().Perm()&0077 != 0 || before.Size() < 1 || before.Size() > int64(limit) {
		return nil, errors.New("PERSIST_FILE_UNSAFE")
	}
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		if errors.Is(err, syscall.ENOENT) {
			return nil, os.ErrNotExist
		}
		return nil, errors.New("PERSIST_FILE_UNSAFE")
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, errors.New("PERSIST_FILE_UNSAFE")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !os.SameFile(before, info) || !info.Mode().IsRegular() || stat.Uid != uid || stat.Nlink != 1 ||
		info.Mode().Perm()&0077 != 0 || info.Size() < 1 || info.Size() > int64(limit) {
		return nil, errors.New("PERSIST_FILE_UNSAFE")
	}
	content, err := io.ReadAll(io.LimitReader(file, int64(limit+1)))
	after, statErr := file.Stat()
	if err != nil || statErr != nil || len(content) > limit ||
		int64(len(content)) != info.Size() || after.Size() != info.Size() ||
		!after.ModTime().Equal(info.ModTime()) {
		return nil, errors.New("PERSIST_FILE_CHANGED")
	}
	return content, nil
}

func atomicPrivate(path string, content []byte, uid uint32, beforeMove func() error) error {
	if err := trustedParents(filepath.Dir(path), uid); err != nil {
		return err
	}
	if _, err := readPrivate(path, uid, maxSnapshot); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".state-")
	if err != nil {
		return errors.New("PERSIST_WRITE_FAILED")
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if _, err := file.Write(content); err != nil {
		return errors.New("PERSIST_WRITE_FAILED")
	}
	if err := file.Sync(); err != nil {
		return errors.New("PERSIST_SYNC_FAILED")
	}
	if err := file.Close(); err != nil {
		return errors.New("PERSIST_WRITE_FAILED")
	}
	if beforeMove != nil {
		if err := beforeMove(); err != nil {
			return err
		}
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return errors.New("PERSIST_REPLACE_FAILED")
	}
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return errors.New("PERSIST_SYNC_FAILED")
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return errors.New("PERSIST_SYNC_FAILED")
	}
	return nil
}

func validateFile(name string, data []byte) error {
	if len(data) == 0 || len(data) > maxFile {
		return errors.New("PERSIST_STATE_LIMIT")
	}
	switch name {
	case "microphone-state":
		if string(data) == "muted\n" || string(data) == "unmuted\n" {
			return nil
		}
	case "music-volume":
		value, err := strconv.Atoi(strings.TrimSuffix(string(data), "\n"))
		if err == nil && value >= 0 && value <= 100 && string(data) == strconv.Itoa(value)+"\n" {
			return nil
		}
	default:
		parts := strings.Split(name, "/")
		if len(parts) == 4 && parts[0] == "bluetooth" &&
			addressPattern.MatchString(parts[1]) && addressPattern.MatchString(parts[2]) &&
			(parts[3] == "info" || parts[3] == "attributes") {
			if !utf8.Valid(data) || bytes.ContainsRune(data, 0) {
				break
			}
			if parts[3] == "info" {
				return validateBondInfo(data)
			}
			return nil
		}
	}
	return errors.New("PERSIST_STATE_INVALID")
}

func validateBondInfo(data []byte) error {
	section := ""
	general := false
	keySections := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSuffix(strings.TrimPrefix(line, "["), "]")
			if section == "General" {
				general = true
			}
			if strings.HasSuffix(section, "Key") {
				keySections[section] = false
			}
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found || section == "" || key == "" {
			return errors.New("PERSIST_BOND_INVALID")
		}
		if key == "Key" && strings.HasSuffix(section, "Key") {
			decoded, err := hex.DecodeString(value)
			if err != nil || len(decoded) != 16 || len(value) != 32 {
				return errors.New("PERSIST_BOND_INVALID")
			}
			keySections[section] = true
		}
	}
	if !general {
		return errors.New("PERSIST_BOND_INVALID")
	}
	for _, complete := range keySections {
		if !complete {
			return errors.New("PERSIST_BOND_INVALID")
		}
	}
	return nil
}

func validateSnapshot(state snapshot) error {
	if state.Version != 1 || state.Files == nil || len(state.Files) > maxFiles ||
		(state.WiFi == nil && len(state.Files) == 0) {
		return errors.New("PERSIST_STATE_INVALID")
	}
	if state.WiFi != nil {
		if err := validateProfile(*state.WiFi); err != nil {
			return err
		}
	}
	total := 0
	for name, data := range state.Files {
		total += len(data)
		if err := validateFile(name, data); err != nil {
			return err
		}
	}
	if total > maxSnapshot/2 {
		return errors.New("PERSIST_STATE_LIMIT")
	}
	return nil
}

func (s storage) load() (snapshot, error) {
	if err := s.check(); err != nil {
		return snapshot{}, err
	}
	if err := ensurePrivateDirectory(s.root, s.uid); err != nil {
		return snapshot{}, err
	}
	content, err := readPrivate(filepath.Join(s.root, "state.json"), s.uid, maxSnapshot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return snapshot{}, errors.New("PERSIST_STATE_ABSENT")
		}
		return snapshot{}, err
	}
	return decodeSnapshot(content)
}

func decodeSnapshot(content []byte) (snapshot, error) {
	var wrapped envelope
	if err := decodeStrict(content, &wrapped); err != nil {
		return snapshot{}, err
	}
	hash := sha256.Sum256(wrapped.State)
	if wrapped.SHA256 != hex.EncodeToString(hash[:]) {
		return snapshot{}, errors.New("PERSIST_STATE_CORRUPT")
	}
	var state snapshot
	if err := decodeStrict(wrapped.State, &state); err != nil {
		return snapshot{}, err
	}
	if err := validateSnapshot(state); err != nil {
		return snapshot{}, err
	}
	return state, nil
}

func (s storage) save(state snapshot) error {
	if err := s.check(); err != nil {
		return err
	}
	if err := validateSnapshot(state); err != nil {
		return err
	}
	if err := ensurePrivateDirectory(s.root, s.uid); err != nil {
		return err
	}
	data, err := json.Marshal(state)
	if err != nil {
		return errors.New("PERSIST_STATE_INVALID")
	}
	hash := sha256.Sum256(data)
	wrapped, err := json.Marshal(envelope{State: data, SHA256: hex.EncodeToString(hash[:])})
	if err != nil || len(wrapped) > maxSnapshot {
		return errors.New("PERSIST_STATE_LIMIT")
	}
	old, err := readPrivate(filepath.Join(s.root, "state.json"), s.uid, maxSnapshot)
	if err == nil {
		if _, decodeErr := decodeSnapshot(old); decodeErr != nil {
			return decodeErr
		}
		if bytes.Equal(old, wrapped) {
			return nil
		}
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return atomicPrivate(filepath.Join(s.root, "state.json"), wrapped, s.uid, s.beforeMove)
}

func retainRequiredSettings(before, after snapshot) error {
	for _, name := range []string{"microphone-state", "music-volume"} {
		if _, existed := before.Files[name]; existed {
			if _, exists := after.Files[name]; !exists {
				return errors.New("PERSIST_RUNTIME_STATE_MISSING")
			}
		}
	}
	return nil
}

func (s storage) capture(wifi *profile) (snapshot, error) {
	state := snapshot{Version: 1, WiFi: wifi, Files: map[string][]byte{}}
	for _, name := range []string{"microphone-state", "music-volume"} {
		data, err := readPrivate(filepath.Join(s.runtime, name), s.uid, maxFile)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return snapshot{}, err
		}
		state.Files[name] = data
	}
	if err := trustedParents(s.bonds, s.uid); err != nil {
		return snapshot{}, err
	}
	entries := 0
	err := filepath.WalkDir(s.bonds, func(path string, item os.DirEntry, walkErr error) error {
		entries++
		if walkErr != nil || entries > 512 {
			return errors.New("PERSIST_BONDS_SCAN_FAILED")
		}
		if path == s.bonds {
			return nil
		}
		relative, err := filepath.Rel(s.bonds, path)
		if err != nil {
			return errors.New("PERSIST_PATH_UNSAFE")
		}
		parts := strings.Split(relative, string(filepath.Separator))
		if item.Type()&os.ModeSymlink != 0 &&
			((len(parts) <= 2 && addressPattern.MatchString(parts[len(parts)-1])) ||
				(len(parts) == 3 && (parts[2] == "info" || parts[2] == "attributes"))) {
			return errors.New("PERSIST_BONDS_PATH_UNSAFE")
		}
		if item.IsDir() {
			if len(parts) > 2 || !addressPattern.MatchString(parts[len(parts)-1]) {
				return filepath.SkipDir
			}
			return trustedParents(path, s.uid)
		}
		// Excludes discovery caches, adapter flags, sockets, pids and unpaired names.
		if len(parts) != 3 || !addressPattern.MatchString(parts[0]) ||
			!addressPattern.MatchString(parts[1]) || (parts[2] != "info" && parts[2] != "attributes") {
			return nil
		}
		data, err := readPrivate(path, s.uid, maxFile)
		if err != nil {
			return err
		}
		name := "bluetooth/" + filepath.ToSlash(relative)
		if err := validateFile(name, data); err != nil {
			return err
		}
		state.Files[name] = data
		if len(state.Files) > maxFiles {
			return errors.New("PERSIST_STATE_LIMIT")
		}
		return nil
	})
	if err != nil {
		return snapshot{}, err
	}
	return state, validateSnapshot(state)
}

func (s storage) cleanInterruptedWrites() error {
	entries, err := os.ReadDir(s.root)
	if err != nil || len(entries) > 256 {
		return errors.New("PERSIST_STORE_SCAN_FAILED")
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), ".state-") {
			continue
		}
		path := filepath.Join(s.root, entry.Name())
		info, err := os.Lstat(path)
		if err != nil {
			return errors.New("PERSIST_INTERRUPTED_FILE_UNSAFE")
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || !info.Mode().IsRegular() || stat.Uid != s.uid || stat.Nlink != 1 ||
			info.Mode().Perm() != 0600 || info.Size() > maxSnapshot {
			return errors.New("PERSIST_INTERRUPTED_FILE_UNSAFE")
		}
		if err := os.Remove(path); err != nil {
			return errors.New("PERSIST_INTERRUPTED_CLEANUP_FAILED")
		}
	}
	return nil
}

func (s storage) restore(state snapshot) error {
	if err := validateSnapshot(state); err != nil {
		return err
	}
	// Startup only: reject a live/nonempty BlueZ tree rather than merge identities.
	entries, err := os.ReadDir(s.bonds)
	if err != nil || len(entries) != 0 {
		return errors.New("PERSIST_RESTORE_TARGET_NOT_EMPTY")
	}
	if err := trustedParents(s.bonds, s.uid); err != nil {
		return err
	}
	for name, data := range state.Files {
		target := filepath.Join(s.runtime, name)
		if strings.HasPrefix(name, "bluetooth/") {
			parts := strings.Split(name, "/")
			directory := s.bonds
			for _, part := range parts[1:3] {
				directory = filepath.Join(directory, part)
				if err := ensurePrivateDirectory(directory, s.uid); err != nil {
					return err
				}
			}
			target = filepath.Join(directory, parts[3])
		}
		if err := atomicPrivate(target, data, s.uid, nil); err != nil {
			return err
		}
	}
	return nil
}
