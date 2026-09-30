// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/tku4tw2012/reinvoke/tools/control/msgpack"
)

func TestFeedbackMatchesMCUVoiceArguments(t *testing.T) {
	directory := t.TempDir()
	statePath := filepath.Join(directory, "microphone-state")
	writeMicrophoneState(t, statePath, "unmuted\n")
	var calls []interface{}
	feedback := &phaseFeedback{
		statePath: statePath, logger: log.New(io.Discard, "", 0),
		call: func(_ context.Context, procedure string, args []interface{}, kwargs map[string]interface{}) error {
			if procedure != "com.harman.extStateUpdate" {
				t.Fatalf("unexpected voice procedure: %s", procedure)
			}
			calls = append(calls, []interface{}{args, kwargs})
			return nil
		},
	}
	for _, phase := range []voicePhase{phaseListening, phaseThinking, phaseSpeaking, phaseIdle} {
		feedback.apply(context.Background(), phase)
	}
	payload, err := msgpack.Encode(calls)
	if err != nil {
		t.Fatal(err)
	}
	payloadPath := filepath.Join(directory, "calls.msgpack")
	if err := os.WriteFile(payloadPath, payload, 0600); err != nil {
		t.Fatal(err)
	}
	mcuDirectory, err := filepath.Abs("../mcu-interface")
	if err != nil {
		t.Fatal(err)
	}
	// Compile this test into the real MCU package using a Go build overlay:
	// exercise its unexported parser without copying it or editing MCU files.
	testPath := filepath.Join(directory, "consumer_test.go")
	if err := os.WriteFile(testPath, []byte(mcuVoiceConsumerTest), 0600); err != nil {
		t.Fatal(err)
	}
	overlay, err := json.Marshal(map[string]interface{}{
		"Replace": map[string]string{
			filepath.Join(mcuDirectory, "voice_endpoint_contract_overlay_test.go"): testPath,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	overlayPath := filepath.Join(directory, "overlay.json")
	if err := os.WriteFile(overlayPath, overlay, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, filepath.Join(runtime.GOROOT(), "bin", "go"),
		"test", "-overlay", overlayPath, "-count=1", "-timeout=10s",
		"-run=^TestVoiceEndpointStateConsumer$", ".")
	command.Dir = mcuDirectory
	command.Env = append(os.Environ(),
		"GOWORK=off", "GO111MODULE=on", "GOTOOLCHAIN=local", "GOPROXY=off", "GOSUMDB=off",
		"VOICE_ENDPOINT_CONTRACT_CALLS="+payloadPath)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("real MCU voice contract rejected endpoint payloads: %v\n%s", err, output)
	}
}

const mcuVoiceConsumerTest = `package main

import (
	"os"
	"testing"
)

func TestVoiceEndpointStateConsumer(t *testing.T) {
	payload, err := os.ReadFile(os.Getenv("VOICE_ENDPOINT_CONTRACT_CALLS"))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeMessagePack(payload)
	if err != nil {
		t.Fatal(err)
	}
	calls, ok := decoded.([]interface{})
	states := []string{"listening", "thinking", "speaking", ""}
	if !ok || len(calls) != len(states) {
		t.Fatal("expected the four endpoint voice payloads")
	}
	for index, call := range calls {
		pair, ok := call.([]interface{})
		if !ok || len(pair) != 2 {
			t.Fatal("endpoint arguments are not a positional/keyword pair")
		}
		args, argsOK := pair[0].([]interface{})
		kwargs, kwargsOK := pair[1].(map[string]interface{})
		if !argsOK || !kwargsOK {
			t.Fatal("invalid endpoint WAMP argument types")
		}
		state, handled, err := voiceStateReport(args, kwargs)
		if err != nil || !handled || state != states[index] {
			t.Fatalf("endpoint phase %d rejected by real voiceStateReport: %q %t %v",
				index, state, handled, err)
		}
	}
}
`
