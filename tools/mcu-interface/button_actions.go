// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import "os"

// Physical controls are reported twice: once as the raw key name the donor
// published, and once as the action the retail speaker would have taken.
//
// The action names are not invented here. They were recovered from the stock
// audio-ui state machine, which held its transitions as string maps and
// dispatched a button to one of these names depending on the state it was in.
// docs/vendor-button-semantics.md records the recovery and its evidence grade.
//
// Keeping the vendor names matters because reinvoked replaces Cortana, and the
// retail dispatch is the behaviour it will need to reproduce. Publishing them
// now means reinvoked can subscribe and own the state machine, exactly as
// audio-ui did, without this service having to be rebuilt.
const buttonActionTopic = "com.harman.vui.action"

// Dispatch modes. Local keeps this service performing the actions it has a
// target for. Publish-only announces them and performs none, which is what a
// replacement state machine needs so the two cannot both act on one press.
const (
	dispatchLocal       = "local"
	dispatchPublishOnly = "publish-only"
)

// validButtonDispatch reports whether a dispatch mode is one this service
// implements. An unrecognised mode is refused rather than defaulted, because
// silently falling back to local would make two owners act on the same press.
func validButtonDispatch(mode string) bool {
	return mode == dispatchLocal || mode == dispatchPublishOnly
}

// Actions the retail speaker reached from a button. Only a subset has a target
// in this runtime; the rest are published so reinvoked can implement them.
const (
	actionMusicPause    = "music-pause"
	actionVoiceCancel   = "voice-cancel"
	actionVoiceTrigger  = "voice-trigger"
	actionBluetoothPair = "bluetooth-pair"
	actionBugreport     = "bugreport"
	actionMicMute       = "micmute"
	actionWiFiSetup     = "wifisetup-enter"
	actionVolumeUp      = "volumeup"
	actionVolumeDown    = "volumedown"
)

// resolveButtonAction names the action a press stands for. Where the retail
// dispatch depended on state, the same state is consulted here.
func resolveButtonAction(
	event inputEvent,
	playbackStatusPath string,
	readFile func(string) ([]byte, error),
) string {
	switch event.Name {
	case "action":
		// The retail speaker paused only while something was playing; in every
		// other state the short tap cancelled whatever was speaking. It never
		// resumed from this button, so neither does this.
		if playbackRunning(playbackStatusPath, readFile) {
			return actionMusicPause
		}
		return actionVoiceCancel
	case "action-long":
		return actionVoiceTrigger
	case "bluetooth":
		return actionBluetoothPair
	case "bluetooth-long":
		// Retail sent a diagnostic bundle here and shipped with it disabled.
		return actionBugreport
	case "micmute":
		return actionMicMute
	case "micmute-long":
		return actionWiFiSetup
	case "volumeup":
		return actionVolumeUp
	case "volumedown":
		return actionVolumeDown
	default:
		return ""
	}
}

// playbackRunning reports whether the renderer is actually playing. An
// unreadable or unrecognised status is treated as not playing, so a missing
// file can never turn a tap into a pause the speaker cannot justify.
func playbackRunning(path string, readFile func(string) ([]byte, error)) bool {
	if path == "" {
		return false
	}
	if readFile == nil {
		readFile = os.ReadFile
	}
	status, err := readFile(path)
	if err != nil {
		return false
	}
	state, valid := playbackState(status)
	return valid && state == "RUNNING"
}
