// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

const persistenceSocket = "/run/reinvoke/persistence.sock"
const wifiPersistenceStatus = "/run/reinvoke/wifi-persistence-status"
const stationSeedPath = "/etc/reinvoke-wifi/station-seed.json"

var errSavedProfileAbsent = errors.New("saved station profile absent")

type stationProfile struct {
	SSID     string `json:"ssid"`
	PSK      string `json:"psk"`
	Security string `json:"security"`
	Hidden   bool   `json:"hidden,omitempty"`
}

type persistenceRequest struct {
	Operation string          `json:"operation"`
	Profile   *stationProfile `json:"profile,omitempty"`
}

type persistenceResponse struct {
	OK      bool            `json:"ok"`
	Code    string          `json:"code"`
	Profile *stationProfile `json:"profile,omitempty"`
}

func profileFromRequest(request wifiRequest) stationProfile {
	return stationProfile{
		SSID: request.SSID, PSK: hex.EncodeToString(deriveWPA2PSK(request.Passphrase, request.SSID)),
		Security: request.Security, Hidden: request.Hidden,
	}
}

func validateStationProfile(profile stationProfile) error {
	if len(profile.SSID) < 1 || len(profile.SSID) > 32 || !utf8.ValidString(profile.SSID) ||
		strings.ContainsAny(profile.SSID, "\x00\r\n") || profile.Security != "wpa2-psk" {
		return errors.New("saved station profile invalid")
	}
	key, err := hex.DecodeString(profile.PSK)
	if err != nil || len(key) != 32 || len(profile.PSK) != 64 {
		return errors.New("saved station profile invalid")
	}
	return nil
}

func renderStationConfig(profile stationProfile, controlPath string) []byte {
	config := "ctrl_interface=" + controlPath + "\nupdate_config=0\nnetwork={\n\tssid=" +
		hex.EncodeToString([]byte(profile.SSID)) + "\n\tpsk=" + profile.PSK + "\n\tkey_mgmt=WPA-PSK\n"
	if profile.Hidden {
		config += "\tscan_ssid=1\n"
	}
	return []byte(config + "}\n")
}

func exchangePersistence(request persistenceRequest) (persistenceResponse, error) {
	if err := validateRootDirectory(filepath.Dir(persistenceSocket), true); err != nil {
		return persistenceResponse{}, errors.New("persistence unavailable")
	}
	info, err := os.Lstat(persistenceSocket)
	if err != nil || info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != 0600 {
		return persistenceResponse{}, errors.New("persistence unavailable")
	}
	uid, err := fileOwnerUID(info)
	if err != nil || uid != 0 {
		return persistenceResponse{}, errors.New("persistence unavailable")
	}
	connection, err := net.DialTimeout("unix", persistenceSocket, 2*time.Second)
	if err != nil {
		return persistenceResponse{}, errors.New("persistence unavailable")
	}
	defer connection.Close()
	unix, ok := connection.(*net.UnixConn)
	if !ok || verifyRootPeer(unix) != nil {
		return persistenceResponse{}, errors.New("persistence peer invalid")
	}
	_ = connection.SetDeadline(time.Now().Add(5 * time.Second))
	if err := json.NewEncoder(connection).Encode(request); err != nil {
		return persistenceResponse{}, errors.New("persistence request failed")
	}
	if err := unix.CloseWrite(); err != nil {
		return persistenceResponse{}, errors.New("persistence request failed")
	}
	var response persistenceResponse
	decoder := json.NewDecoder(io.LimitReader(connection, 4097))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&response); err != nil {
		return persistenceResponse{}, errors.New("persistence response invalid")
	}
	var extra interface{}
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return persistenceResponse{}, errors.New("persistence response invalid")
	}
	if !response.OK {
		// Do not echo server-controlled data, credentials, SSIDs or MACs.
		return response, errors.New("persistence operation failed")
	}
	return response, nil
}

func saveStation(profile stationProfile) error {
	_, err := exchangePersistence(persistenceRequest{Operation: "save", Profile: &profile})
	return err
}

func saveSeedStation(profile stationProfile) error {
	_, err := exchangePersistence(persistenceRequest{Operation: "save-seed", Profile: &profile})
	return err
}

func loadStation() (stationProfile, error) {
	response, err := exchangePersistence(persistenceRequest{Operation: "load"})
	if err != nil && !response.OK && response.Profile == nil &&
		(response.Code == "PERSIST_PROFILE_ABSENT" || response.Code == "PERSIST_STATE_ABSENT") {
		return stationProfile{}, errSavedProfileAbsent
	}
	if err != nil || response.Profile == nil {
		return stationProfile{}, errors.New("saved station unavailable")
	}
	if err := validateStationProfile(*response.Profile); err != nil {
		return stationProfile{}, err
	}
	return *response.Profile, nil
}

func (m wpaManager) Resume(ctx context.Context, profile stationProfile) error {
	if err := validateStationProfile(profile); err != nil {
		return err
	}
	return m.applyProfile(ctx, profile)
}

func (m wpaManager) ResumeSeed(ctx context.Context, profile stationProfile) error {
	if m.saveSeedProfile == nil {
		return errors.New("PERSIST_WIFI_SEED_SAVE_UNAVAILABLE")
	}
	if err := m.Resume(ctx, profile); err != nil {
		return err
	}
	m.saveProfile = m.saveSeedProfile
	m.saveAssociatedProfile(profile)
	return nil
}

func selectBootProfile(saved, seed func() (stationProfile, error)) (stationProfile, bool, error) {
	profile, err := saved()
	if err == nil {
		return profile, false, nil
	}
	// Unavailable/corrupt storage is not proof that a saved profile is absent.
	if !errors.Is(err, errSavedProfileAbsent) {
		return stationProfile{}, false, err
	}
	profile, err = seed()
	return profile, err == nil, err
}

func decodeSeedProfile(content []byte) (stationProfile, error) {
	if len(content) < 1 || len(content) > maxRequestBytes || !utf8.Valid(content) {
		return stationProfile{}, errors.New("PERSIST_WIFI_SEED_INVALID")
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	open, err := decoder.Token()
	if err != nil || open != json.Delim('{') {
		return stationProfile{}, errors.New("PERSIST_WIFI_SEED_INVALID")
	}
	seen := map[string]bool{}
	for decoder.More() {
		key, err := decoder.Token()
		name, ok := key.(string)
		if err != nil || !ok || seen[name] {
			return stationProfile{}, errors.New("PERSIST_WIFI_SEED_INVALID")
		}
		seen[name] = true
		value, err := decoder.Token()
		if err != nil {
			return stationProfile{}, errors.New("PERSIST_WIFI_SEED_INVALID")
		}
		switch name {
		case "ssid", "psk", "security":
			if _, ok := value.(string); !ok {
				return stationProfile{}, errors.New("PERSIST_WIFI_SEED_INVALID")
			}
		case "hidden":
			if _, ok := value.(bool); !ok {
				return stationProfile{}, errors.New("PERSIST_WIFI_SEED_INVALID")
			}
		default:
			return stationProfile{}, errors.New("PERSIST_WIFI_SEED_INVALID")
		}
	}
	decoder = json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	var profile stationProfile
	if err := decoder.Decode(&profile); err != nil {
		return stationProfile{}, errors.New("PERSIST_WIFI_SEED_INVALID")
	}
	var extra interface{}
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) || validateStationProfile(profile) != nil {
		return stationProfile{}, errors.New("PERSIST_WIFI_SEED_INVALID")
	}
	return profile, nil
}

func loadSeedProfile() (stationProfile, error) {
	fail := errors.New("PERSIST_WIFI_SEED_UNAVAILABLE")
	if err := validateRootDirectory(filepath.Dir(stationSeedPath), false); err != nil {
		return stationProfile{}, fail
	}
	for _, parent := range []string{"/", "/etc"} {
		info, err := os.Lstat(parent)
		if err != nil || !info.IsDir() || info.Mode().Perm()&0022 != 0 {
			return stationProfile{}, fail
		}
		uid, err := fileOwnerUID(info)
		if err != nil || uid != 0 {
			return stationProfile{}, fail
		}
	}
	before, err := os.Lstat(stationSeedPath)
	if err != nil {
		return stationProfile{}, fail
	}
	stat, ok := before.Sys().(*syscall.Stat_t)
	if !ok || !before.Mode().IsRegular() || stat.Uid != 0 || stat.Nlink != 1 ||
		before.Mode().Perm() != 0600 || before.Size() < 1 || before.Size() > maxRequestBytes {
		return stationProfile{}, fail
	}
	fd, err := syscall.Open(stationSeedPath, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return stationProfile{}, fail
	}
	file := os.NewFile(uintptr(fd), stationSeedPath)
	defer file.Close()
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) || after.Size() != before.Size() ||
		!after.ModTime().Equal(before.ModTime()) {
		return stationProfile{}, fail
	}
	content, err := io.ReadAll(io.LimitReader(file, maxRequestBytes+1))
	final, finalErr := file.Stat()
	if err != nil || finalErr != nil || int64(len(content)) != before.Size() ||
		final.Size() != before.Size() || !final.ModTime().Equal(before.ModTime()) {
		return stationProfile{}, fail
	}
	return decodeSeedProfile(content)
}

func validWiFiStatus(token string) bool {
	switch token {
	case "PERSIST_WIFI_SAVED", "PERSIST_WIFI_SAVE_FAILED", "PERSIST_WIFI_ASSOCIATED",
		"PERSIST_WIFI_RESUME_FAILED", "PERSIST_WIFI_PROFILE_UNAVAILABLE", "PERSIST_WIFI_APPLY_FAILED":
		return true
	default:
		return false
	}
}

func recordWiFiStatus(token string) {
	if !validWiFiStatus(token) {
		log.Print("PERSIST_WIFI_STATUS_INVALID")
		return
	}
	if info, err := os.Lstat(wifiPersistenceStatus); err == nil {
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || !info.Mode().IsRegular() || stat.Uid != 0 ||
			stat.Nlink != 1 || info.Mode().Perm() != 0600 || info.Size() > 128 {
			log.Print("PERSIST_WIFI_STATUS_UNSAFE")
			return
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		log.Print("PERSIST_WIFI_STATUS_UNAVAILABLE")
		return
	}
	if err := writePrivateFile(wifiPersistenceStatus, []byte(token+"\n")); err != nil {
		log.Print("PERSIST_WIFI_STATUS_WRITE_FAILED")
	}
}
