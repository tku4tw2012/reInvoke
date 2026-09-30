// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

type options struct {
	ConfigPath string
	Bundle     string
	Router     string
	Realm      string
	RuntimeDir string
	Fixture    string
	NoPlayback bool
	Seconds    int
	Once       bool
}

func parseOptions(args []string, output io.Writer) (options, error) {
	var opts options
	flags := flag.NewFlagSet("reinvoke-voice", flag.ContinueOnError)
	flags.SetOutput(output)
	flags.StringVar(&opts.ConfigPath, "config", "/etc/reinvoke-voice/voice.json", "private host connection config")
	flags.StringVar(&opts.Bundle, "bundle", "/opt/reinvoke/voice", "isolated donor bundle directory")
	flags.StringVar(&opts.Router, "router", "127.0.0.1:9999", "existing MCU WAMP router host:port")
	flags.StringVar(&opts.Realm, "realm", "default", "WAMP realm")
	flags.StringVar(&opts.RuntimeDir, "runtime-dir", "/run/reinvoke", "singleton and worker lock directory")
	flags.StringVar(&opts.Fixture, "fixture", "", "absolute PCM fixture path (requires --once)")
	flags.BoolVar(&opts.NoPlayback, "no-playback", false, "validate replies without playback")
	flags.IntVar(&opts.Seconds, "seconds", 0, "worker runtime in seconds, 0 means indefinite (maximum 86400)")
	flags.BoolVar(&opts.Once, "once", false, "run one session without reconnecting")
	if err := flags.Parse(args); err != nil {
		return opts, err
	}
	if flags.NArg() != 0 {
		return opts, errors.New("unexpected positional arguments")
	}
	if opts.ConfigPath == "" || !filepath.IsAbs(opts.Bundle) || !filepath.IsAbs(opts.RuntimeDir) {
		return opts, errors.New("--config must be nonempty; --bundle and --runtime-dir must be absolute")
	}
	host, _, err := splitAddress(opts.Router)
	if err != nil || (net.ParseIP(host) == nil && !validHostname(host)) {
		return opts, errors.New("--router must be a valid host:port")
	}
	if opts.Realm == "" || len(opts.Realm) > 255 {
		return opts, errors.New("--realm must contain 1..255 bytes")
	}
	if opts.Seconds < 0 || opts.Seconds > 86400 {
		return opts, errors.New("--seconds must be 0..86400")
	}
	if opts.Fixture != "" && (!filepath.IsAbs(opts.Fixture) || !opts.Once) {
		return opts, errors.New("--fixture must be absolute and requires --once")
	}
	return opts, nil
}

func validateFiles(opts options) error {
	for _, name := range []string{
		"lib/ld-linux-armhf.so.3", "lib/unit-link.so", "bin/cortana", "share/handoff-original.table",
	} {
		info, err := os.Stat(filepath.Join(opts.Bundle, name))
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("bundle requires a regular %s file", name)
		}
		if (name == "lib/ld-linux-armhf.so.3" || name == "bin/cortana") && info.Mode().Perm()&0111 == 0 {
			return fmt.Errorf("bundle %s is not executable", name)
		}
	}
	if opts.Fixture != "" {
		file, err := openRegular(opts.Fixture, syscall.O_RDONLY, 0)
		if err != nil {
			return errors.New("--fixture must be a readable regular, non-symlink file")
		}
		return file.Close()
	}
	return nil
}

func singleton(runtimeDir string) (*os.File, error) {
	if err := os.MkdirAll(runtimeDir, 0700); err != nil {
		return nil, fmt.Errorf("create runtime directory: %w", err)
	}
	file, err := openRegular(filepath.Join(runtimeDir, "voice.lock"), syscall.O_CREAT|syscall.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("open voice singleton lock: %w", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, fmt.Errorf("another voice supervisor may be active: %w", err)
	}
	// Keep the inode in place: unlinking a flock file would allow a second lock.
	return file, nil
}

func run(ctx context.Context, args []string, output io.Writer) error {
	opts, err := parseOptions(args, output)
	if err != nil {
		return err
	}
	cfg, err := loadConfig(opts.ConfigPath)
	if err != nil {
		return err
	}
	if err := validateFiles(opts); err != nil {
		return err
	}
	lock, err := singleton(opts.RuntimeDir)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := enableSubreaper(); err != nil {
		return err
	}
	logger := log.New(output, "reinvoke-voice: ", 0)
	return supervise(ctx, opts.Once, logger, func(ctx context.Context) error {
		return runSession(ctx, cfg, opts, logger, func(nonce uint32) *exec.Cmd {
			return workerCommand(cfg, opts, nonce)
		})
	})
}

func supervise(ctx context.Context, once bool, logger *log.Logger, session func(context.Context) error) error {
	delay := 2 * time.Second
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := session(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if once {
			return err
		}
		if err != nil {
			logger.Printf("session failed: %v; reconnecting in %s", err, delay)
		} else {
			delay = 2 * time.Second
			logger.Printf("session completed; reconnecting in %s", delay)
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		if err != nil && delay < 30*time.Second {
			delay *= 2
			if delay > 30*time.Second {
				delay = 30 * time.Second
			}
		}
	}
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	err := run(ctx, os.Args[1:], os.Stderr)
	cancel()
	if err != nil && !errors.Is(err, flag.ErrHelp) && !errors.Is(err, context.Canceled) {
		log.New(os.Stderr, "reinvoke-voice: ", 0).Print(err)
		os.Exit(1)
	}
}
