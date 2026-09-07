// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

const (
	defaultRuntimeDirectory = "/run/reinvoke/mic-capture"
	defaultAudioSocket      = "/run/reinvoke/mic-capture/audio.sock"
	defaultAuthoritySocket  = "/run/reinvoke/mic-privacy.sock"
	defaultAuthorityEpoch   = "/run/reinvoke/mic-privacy.epoch"
	defaultPrivacyState     = "/run/reinvoke/microphone-state"
	defaultMCUPID           = "/run/reinvoke/mcu-interface.pid"
	defaultMCUExecutable    = "/opt/reinvoke/bin/reinvoke-mcu-interface"
	defaultDSPPID           = "/run/reinvoke/dsp-interface.pid"
	defaultDSPControl       = "/run/reinvoke/dsp-mic-control.sock"
	defaultDSPExecutable    = "/opt/reinvoke/bin/reinvoke-dsp-interface"
	defaultLoader           = "/opt/reinvoke/lib/ld-linux-armhf.so.3"
	defaultLibraryPath      = "/opt/reinvoke/lib"
	defaultAREcord          = "/opt/reinvoke/bin/arecord"
	defaultCaptureDevice    = "hw:1,0"
	generationPollInterval  = 50 * time.Millisecond
	restartDelay            = time.Second
	privacyDrainPeriods     = 64
)

type serviceConfig struct {
	runtimeDirectory string
	audioSocket      string
	authoritySocket  string
	authorityEpoch   string
	privacyState     string
	mcuPID           string
	mcuExecutable    string
	dspPID           string
	dspControl       string
	dspExecutable    string
	source           sourceConfig
}

func main() {
	var config serviceConfig
	flag.StringVar(
		&config.runtimeDirectory,
		"runtime-dir",
		defaultRuntimeDirectory,
		"root-only runtime directory",
	)
	flag.StringVar(
		&config.audioSocket,
		"audio-socket",
		defaultAudioSocket,
		"root-only microphone stream socket",
	)
	flag.StringVar(
		&config.authoritySocket,
		"privacy-authority-socket",
		defaultAuthoritySocket,
		"MCU privacy authority socket",
	)
	flag.StringVar(
		&config.authorityEpoch,
		"privacy-authority-epoch",
		defaultAuthorityEpoch,
		"MCU privacy authority epoch",
	)
	flag.StringVar(
		&config.privacyState,
		"microphone-state",
		defaultPrivacyState,
		"confirmed microphone privacy state",
	)
	flag.StringVar(
		&config.mcuPID,
		"mcu-pid",
		defaultMCUPID,
		"MCU privacy-owner PID file",
	)
	flag.StringVar(
		&config.mcuExecutable,
		"mcu-executable",
		defaultMCUExecutable,
		"expected MCU privacy-owner executable",
	)
	flag.StringVar(
		&config.dspPID,
		"dsp-pid",
		defaultDSPPID,
		"DSP service PID file",
	)
	flag.StringVar(
		&config.dspControl,
		"dsp-control-socket",
		defaultDSPControl,
		"DSP microphone control socket",
	)
	flag.StringVar(
		&config.dspExecutable,
		"dsp-executable",
		defaultDSPExecutable,
		"expected DSP service executable",
	)
	flag.StringVar(&config.source.loader, "loader", defaultLoader, "ELF loader")
	flag.StringVar(
		&config.source.libraryPath,
		"library-path",
		defaultLibraryPath,
		"capture-helper library path",
	)
	flag.StringVar(
		&config.source.arecord,
		"arecord",
		defaultAREcord,
		"trusted arecord executable",
	)
	flag.StringVar(
		&config.source.device,
		"device",
		defaultCaptureDevice,
		"fixed ALSA capture device",
	)
	flag.Parse()

	ctx, cancel := signalContext()
	defer cancel()
	if err := run(ctx, config); err != nil {
		log.Printf("ERROR: %v", err)
		os.Exit(1)
	}
}

func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
}

func run(ctx context.Context, config serviceConfig) error {
	if os.Geteuid() != 0 {
		return errors.New("microphone capture owner must run as root")
	}
	if err := validateConfig(config); err != nil {
		return err
	}
	lock, err := acquireLifecycleLock(
		filepath.Join(filepath.Dir(config.runtimeDirectory), "mic-capture.lock"),
	)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := prepareRuntimeDirectory(config.runtimeDirectory); err != nil {
		return err
	}
	defer os.RemoveAll(config.runtimeDirectory)

	hub := newClientHub(defaultClientQueuePeriods)
	serverReady := make(chan struct{})
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- runAudioServer(
			ctx,
			config.audioSocket,
			hub,
			serverReady,
		)
	}()
	select {
	case <-serverReady:
	case err := <-serverDone:
		return err
	case <-ctx.Done():
		return nil
	}

	for {
		if err := ctx.Err(); err != nil {
			_ = hub.block(context.Background())
			return nil
		}
		err := runCaptureGeneration(ctx, config, hub, serverDone)
		_ = hub.block(context.Background())
		if err == nil || ctx.Err() != nil {
			return nil
		}
		log.Printf("capture generation stopped: %v", err)
		timer := time.NewTimer(restartDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

func runCaptureGeneration(
	ctx context.Context,
	config serviceConfig,
	hub *clientHub,
	serverDone <-chan error,
) error {
	generationContext, cancelGeneration := context.WithCancel(ctx)
	defer cancelGeneration()
	generation, err := waitForDSPGeneration(generationContext, config)
	if err != nil {
		return err
	}
	mcuGeneration, err := waitForMCUGeneration(generationContext, config)
	if err != nil {
		return err
	}
	source, err := startCaptureSource(generationContext, config.source)
	if err != nil {
		return err
	}
	defer source.stop()

	// One complete period proves hw_params and trigger completed. It and every
	// subsequent period stay discarded until the MCU privacy transaction
	// authorizes this generation.
	select {
	case _, ok := <-source.periods:
		if !ok {
			return <-source.done
		}
	case err := <-source.done:
		return err
	case <-ctx.Done():
		return nil
	}

	captureGeneration, err := randomGeneration()
	if err != nil {
		return err
	}
	authority, err := startAuthoritySession(
		generationContext,
		config.authoritySocket,
		captureGeneration,
	)
	if err != nil {
		return err
	}
	defer authority.close()

	var (
		authorityEpoch string
		sequence       uint64
		lastGeneration uint64
		pendingDrain   *authorityEvent
		drainRemaining int
	)
	ticker := time.NewTicker(generationPollInterval)
	defer ticker.Stop()
	for {
		select {
		case event, ok := <-authority.events:
			if !ok {
				select {
				case err := <-authority.done:
					return err
				default:
					return errors.New("privacy authority stopped")
				}
			}
			if event.kind == "drain" {
				if pendingDrain != nil {
					event.result <- errors.New(
						"privacy drain is already in progress",
					)
					continue
				}
				blockCtx, cancel := context.WithTimeout(
					generationContext,
					authorityCommandTimeout,
				)
				eventErr := hub.block(blockCtx)
				cancel()
				if eventErr != nil {
					event.result <- eventErr
					continue
				}
				pendingDrain = &event
				drainRemaining = privacyDrainPeriods
				continue
			}
			eventErr := handleAuthorityEvent(
				generationContext,
				config,
				hub,
				&authorityEpoch,
				event,
			)
			event.result <- eventErr
		case period, ok := <-source.periods:
			if !ok {
				return <-source.done
			}
			if pendingDrain != nil {
				drainRemaining = advancePrivacyDrain(drainRemaining, period)
				if drainRemaining == 0 {
					pendingDrain.result <- nil
					pendingDrain = nil
				}
				continue
			}
			enabled, deliveryGeneration, _ := hub.state()
			if !enabled {
				continue
			}

			if deliveryGeneration != lastGeneration {
				lastGeneration = deliveryGeneration
				sequence = 0
			}
			record, err := encodeRecord(
				deliveryGeneration,
				sequence,
				uint64(time.Now().UnixNano()),
				period,
			)
			if err != nil {
				return err
			}
			sequence++
			hub.broadcast(record)
		case err := <-source.done:
			return err
		case err := <-authority.done:
			_ = hub.block(context.Background())
			return err
		case err := <-serverDone:
			return err
		case <-ticker.C:
			current, err := readDSPGeneration(
				config.dspPID,
				config.dspExecutable,
				config.dspControl,
			)
			if err != nil || current != generation {
				return errors.New("DSP generation changed")
			}
			currentMCU, err := readProcessIdentity(
				config.mcuPID,
				config.mcuExecutable,
			)
			if err != nil || currentMCU != mcuGeneration {
				return errors.New("MCU privacy authority changed")
			}
			if authorityEpoch != "" {
				currentEpoch, err := readAuthorityEpoch(
					config.authorityEpoch,
				)
				if err != nil || currentEpoch != authorityEpoch {
					return errors.New("MCU privacy authority changed")
				}
			}
			muted, err := readMicrophoneMuted(config.privacyState)
			if err != nil {
				blockCtx, cancel := context.WithTimeout(
					ctx,
					authorityCommandTimeout,
				)
				_ = hub.block(blockCtx)
				cancel()
				return fmt.Errorf("privacy state became invalid: %w", err)
			}
			if muted {
				blockCtx, cancel := context.WithTimeout(
					ctx,
					authorityCommandTimeout,
				)
				_ = hub.block(blockCtx)
				cancel()
			}
		case <-ctx.Done():
			return nil
		}
	}
}

func periodIsZero(period []byte) bool {
	for _, value := range period {
		if value != 0 {
			return false
		}
	}
	return true
}

func advancePrivacyDrain(remaining int, period []byte) int {
	if remaining <= 0 {
		return 0
	}
	if !periodIsZero(period) {
		return privacyDrainPeriods
	}
	return remaining - 1
}

func handleAuthorityEvent(
	ctx context.Context,
	config serviceConfig,
	hub *clientHub,
	authorityEpoch *string,
	event authorityEvent,
) error {
	switch event.kind {
	case "block":
		blockCtx, cancel := context.WithTimeout(ctx, authorityCommandTimeout)
		defer cancel()
		return hub.block(blockCtx)
	case "state":
		currentEpoch, err := readAuthorityEpoch(config.authorityEpoch)
		if err != nil || currentEpoch != event.epoch {
			_ = hub.block(context.Background())
			return errors.New("privacy authority epoch mismatch")
		}
		*authorityEpoch = event.epoch
		if event.muted {
			return hub.block(ctx)
		}
		// STATE records the confirmed policy but never authorizes delivery.
		// The MCU sends ALLOW only after confirmed unmute and after preserving
		// logical user policy through the synchronization transaction.
		return nil
	case "allow":
		currentEpoch, err := readAuthorityEpoch(config.authorityEpoch)
		if err != nil || currentEpoch != event.epoch ||
			event.epoch != *authorityEpoch {
			_ = hub.block(context.Background())
			return errors.New("privacy authority epoch mismatch")
		}
		muted, err := readMicrophoneMuted(config.privacyState)
		if err != nil || muted {
			_ = hub.block(context.Background())
			return errors.New("privacy state does not permit capture")
		}
		deliveryGeneration, err := randomGeneration()
		if err != nil {
			_ = hub.block(context.Background())
			return err
		}
		hub.enable(deliveryGeneration)
		return nil
	default:
		return errors.New("unsupported privacy authority event")
	}
}

func waitForDSPGeneration(
	ctx context.Context,
	config serviceConfig,
) (processGeneration, error) {
	ticker := time.NewTicker(generationPollInterval)
	defer ticker.Stop()
	for {
		generation, err := readDSPGeneration(
			config.dspPID,
			config.dspExecutable,
			config.dspControl,
		)
		if err == nil {
			return generation, nil
		}
		select {
		case <-ctx.Done():
			return processGeneration{}, ctx.Err()
		case <-ticker.C:
		}
	}
}

func waitForMCUGeneration(
	ctx context.Context,
	config serviceConfig,
) (processIdentity, error) {
	ticker := time.NewTicker(generationPollInterval)
	defer ticker.Stop()
	for {
		generation, err := readProcessIdentity(
			config.mcuPID,
			config.mcuExecutable,
		)
		if err == nil {
			return generation, nil
		}
		select {
		case <-ctx.Done():
			return processIdentity{}, ctx.Err()
		case <-ticker.C:
		}
	}
}

func randomGeneration() (uint64, error) {
	for {
		var content [8]byte
		if _, err := rand.Read(content[:]); err != nil {
			return 0, fmt.Errorf("generate capture generation: %w", err)
		}
		generation := binary.LittleEndian.Uint64(content[:])
		if generation != 0 {
			return generation, nil
		}
	}
}

func acquireLifecycleLock(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lifecycle lock: %w", err)
	}
	if err := syscall.Flock(
		int(file.Fd()),
		syscall.LOCK_EX|syscall.LOCK_NB,
	); err != nil {
		file.Close()
		return nil, errors.New("another microphone capture owner is running")
	}
	return file, nil
}

func validateConfig(config serviceConfig) error {
	for label, value := range map[string]string{
		"runtime directory":        config.runtimeDirectory,
		"audio socket":             config.audioSocket,
		"privacy authority socket": config.authoritySocket,
		"privacy authority epoch":  config.authorityEpoch,
		"microphone state":         config.privacyState,
		"MCU PID":                  config.mcuPID,
		"MCU executable":           config.mcuExecutable,
		"DSP PID":                  config.dspPID,
		"DSP control socket":       config.dspControl,
		"DSP executable":           config.dspExecutable,
		"loader":                   config.source.loader,
		"library path":             config.source.libraryPath,
		"arecord":                  config.source.arecord,
	} {
		if !filepath.IsAbs(value) {
			return fmt.Errorf("%s must be absolute", label)
		}
	}
	if filepath.Dir(config.audioSocket) != config.runtimeDirectory {
		return errors.New("audio socket must be inside the runtime directory")
	}
	for _, executable := range []string{
		config.source.loader,
		config.source.arecord,
		config.mcuExecutable,
		config.dspExecutable,
	} {
		if err := validateRootExecutable(executable); err != nil {
			return fmt.Errorf("validate executable %s: %w", executable, err)
		}
	}
	if err := validateRootLibraryPath(config.source.libraryPath); err != nil {
		return fmt.Errorf("validate capture library path: %w", err)
	}
	return nil
}
