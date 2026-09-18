// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import "strings"

// playbackState reports the ALSA PCM state named in a status file.
//
// This is a reader, not a policy. It tells the play/pause button whether
// something is currently playing. Nothing here mutes the speaker: the
// amplifier and DAC are opened when the hardware is initialised and follow
// only the explicit mute procedures.
func playbackState(status []byte) (string, bool) {
	line := strings.TrimSpace(strings.SplitN(string(status), "\n", 2)[0])
	if line == "closed" {
		return line, true
	}
	fields := strings.Fields(line)
	if len(fields) != 2 || fields[0] != "state:" {
		return "", false
	}
	switch fields[1] {
	case "OPEN", "SETUP", "PREPARED", "RUNNING", "XRUN", "DRAINING",
		"PAUSED", "SUSPENDED", "DISCONNECTED":
		return fields[1], true
	default:
		return "", false
	}
}
