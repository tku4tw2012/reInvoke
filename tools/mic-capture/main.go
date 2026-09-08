// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	defaultRuntimeDirectory = "/run/reinvoke/mic-capture"
	defaultAudioSocket      = "/run/reinvoke/mic-capture/audio.sock"
	defaultPrivacyState     = "/run/reinvoke/microphone-state"
	defaultDSPPID           = "/run/reinvoke/dsp-interface.pid"
	defaultDSPControl       = "/run/reinvoke/dsp-mic-control.sock"
	defaultDSPExecutable    = "/opt/reinvoke/bin/reinvoke-dsp-interface"
	defaultLoader           = "/opt/reinvoke/lib/ld-linux-armhf.so.3"
	defaultLibraryPath      = "/opt/reinvoke/lib"
	defaultArecord          = "/opt/reinvoke/bin/arecord"
	defaultCaptureDevice    = "hw:1,0"
	generationPollInterval  = 250 * time.Millisecond
	statePollInterval       = 100 * time.Millisecond
	restartDelay            = time.Second
)

type serviceConfig struct {
	runtimeDirectory string
	audioSocket      string
	privacyState     string
	dspPID           string
	dspControl       string
	dspExecutable    string
	source           sourceConfig
}

func main() {
	var cfg serviceConfig
	flag.StringVar(&cfg.runtimeDirectory, "runtime-dir", defaultRuntimeDirectory, "root-only runtime directory")
	flag.StringVar(&cfg.audioSocket, "audio-socket", defaultAudioSocket, "root-only microphone stream socket")
	flag.StringVar(&cfg.privacyState, "microphone-state", defaultPrivacyState, "microphone privacy state file")
	flag.StringVar(&cfg.dspPID, "dsp-pid", defaultDSPPID, "DSP interface PID file")
	flag.StringVar(&cfg.dspControl, "dsp-mic-socket", defaultDSPControl, "DSP mic control socket")
	flag.StringVar(&cfg.dspExecutable, "dsp-executable", defaultDSPExecutable, "expected DSP executable")
	flag.StringVar(&cfg.source.loader, "loader", defaultLoader, "dynamic linker")
	flag.StringVar(&cfg.source.libraryPath, "library-path", defaultLibraryPath, "donor library path")
	flag.StringVar(&cfg.source.arecord, "arecord", defaultArecord, "arecord binary")
	flag.StringVar(&cfg.source.device, "device", defaultCaptureDevice, "ALSA capture device")
	flag.Parse()

	if err := validateConfig(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "mic-capture: %v\n", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(
		context.Background(),
		syscall.SIGTERM,
		syscall.SIGINT,
	)
	defer stop()

	if err := run(ctx, cfg); err != nil && ctx.Err() == nil {
		fmt.Fprintf(os.Stderr, "mic-capture: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, cfg serviceConfig) error {
	lock, err := acquireLifecycleLock(
		filepath.Join(cfg.runtimeDirectory+".lock"),
	)
	if err != nil {
		return err
	}
	defer lock.Close()

	if err := prepareRuntimeDirectory(cfg.runtimeDirectory); err != nil {
		return err
	}

	hub := newClientHub(4)
	serverReady := make(chan struct{})
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- runAudioServer(ctx, cfg.audioSocket, hub, serverReady)
	}()
	select {
	case <-serverReady:
	case err := <-serverDone:
		return err
	case <-ctx.Done():
		return nil
	}

	var generation uint64
	for {
		if ctx.Err() != nil {
			return nil
		}
		dspGen, err := waitForDSPGeneration(ctx, cfg)
		if err != nil {
			return nil // context cancelled
		}
		generation++
		if err := runCapture(ctx, cfg, hub, dspGen, generation); err != nil {
			fmt.Fprintf(os.Stderr, "mic-capture: %v\n", err)
		}
		blockCtx, cancel := context.WithTimeout(
			context.Background(),
			2*time.Second,
		)
		_ = hub.block(blockCtx)
		cancel()
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(restartDelay):
		}
	}
}

func waitForDSPGeneration(
	ctx context.Context,
	cfg serviceConfig,
) (processGeneration, error) {
	ticker := time.NewTicker(generationPollInterval)
	defer ticker.Stop()
	for {
		gen, err := readDSPGeneration(cfg.dspPID, cfg.dspExecutable, cfg.dspControl)
		if err == nil {
			return gen, nil
		}
		select {
		case <-ctx.Done():
			return processGeneration{}, ctx.Err()
		case <-ticker.C:
		}
	}
}

func runCapture(
	ctx context.Context,
	cfg serviceConfig,
	hub *clientHub,
	dspGen processGeneration,
	generation uint64,
) error {
	source, err := startCaptureSource(ctx, cfg.source)
	if err != nil {
		return fmt.Errorf("start capture: %w", err)
	}
	defer source.stop()

	hub.enable(generation)

	// Poll mute state in a goroutine; start muted until confirmed unmuted.
	var mutedAtomic int32 = 1
	mutedCtx, cancelMuted := context.WithCancel(ctx)
	defer cancelMuted()
	go func() {
		ticker := time.NewTicker(statePollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-mutedCtx.Done():
				return
			case <-ticker.C:
				m, err := readMicrophoneMuted(cfg.privacyState)
				if err != nil {
					m = true
				}
				if m {
					atomic.StoreInt32(&mutedAtomic, 1)
				} else {
					atomic.StoreInt32(&mutedAtomic, 0)
				}
			}
		}
	}()

	var sequence uint64
	genTicker := time.NewTicker(generationPollInterval)
	defer genTicker.Stop()

	for {
		select {
		case period, ok := <-source.periods:
			if !ok {
				select {
				case err := <-source.done:
					return err
				default:
					return errors.New("capture source closed")
				}
			}
			if atomic.LoadInt32(&mutedAtomic) != 0 {
				continue
			}
			record, err := encodeRecord(
				generation,
				sequence,
				uint64(time.Now().UnixNano()),
				period,
			)
			if err != nil {
				continue
			}
			hub.broadcast(record)
			sequence++

		case <-genTicker.C:
			current, err := readDSPGeneration(
				cfg.dspPID,
				cfg.dspExecutable,
				cfg.dspControl,
			)
			if err != nil || current != dspGen {
				return errors.New("DSP generation changed")
			}

		case <-ctx.Done():
			return nil
		}
	}
}

func validateConfig(cfg serviceConfig) error {
	for label, value := range map[string]string{
		"runtime directory": cfg.runtimeDirectory,
		"audio socket":      cfg.audioSocket,
		"microphone state":  cfg.privacyState,
		"DSP PID":           cfg.dspPID,
		"DSP control":       cfg.dspControl,
		"DSP executable":    cfg.dspExecutable,
		"loader":            cfg.source.loader,
		"library path":      cfg.source.libraryPath,
		"arecord":           cfg.source.arecord,
	} {
		if !filepath.IsAbs(value) {
			return fmt.Errorf("%s must be an absolute path", label)
		}
	}
	if filepath.Dir(cfg.audioSocket) != cfg.runtimeDirectory {
		return errors.New("audio socket must be inside the runtime directory")
	}
	for _, executable := range []string{
		cfg.source.loader,
		cfg.source.arecord,
		cfg.dspExecutable,
	} {
		if err := validateRootExecutable(executable); err != nil {
			return fmt.Errorf("validate executable %s: %w", executable, err)
		}
	}
	if err := validateRootLibraryPath(cfg.source.libraryPath); err != nil {
		return fmt.Errorf("validate library path: %w", err)
	}
	return nil
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
