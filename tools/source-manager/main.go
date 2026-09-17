// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

// Command reinvoke-source-manager owns which audio source may use the speaker
// and routes source-agnostic media control to whichever one does.
//
// The donor split this between music-source-manager, which arbitrated sources,
// and audio-ui, which turned a button press into "pause whatever is playing".
// This runtime had neither: the top button called com.harman.bluetooth.pause
// directly and the source registry was a fixed table that answered
// get-active with a keyword map where the donor answers with a positional URI.
// That works while Bluetooth is the only source and breaks as soon as an alert
// or a second transport wants the speaker.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	callTimeout = 5 * time.Second

	// The donor published a readiness event once a service could answer and a
	// heartbeat while it still could, and podium.conf gated dependants on
	// wait_ready and wait_heartbeats. A pid file only proves a fork happened:
	// servicemanager crash-looped 1380 times on 05.8.7 while looking up.
	heartbeatInterval = 5 * time.Second
)

type service struct {
	sources *registry

	mu   sync.Mutex
	conn *connection

	// shutdown is closed when a peer asks this service to stop, so the process
	// exits through the same path as a signal rather than being killed.
	shutdownOnce sync.Once
	shutdown     chan struct{}

	logf func(string, ...interface{})
}

// requestShutdown asks the process to stop. It is safe to call more than once,
// because podium.conf stopped services by name and could repeat a request.
func (s *service) requestShutdown() {
	s.shutdownOnce.Do(func() {
		if s.shutdown != nil {
			close(s.shutdown)
		}
	})
}

// publish emits an event if a session is up. A missing session is not an error:
// events are advisory, and dropping one is better than failing the call that
// produced it.
func (s *service) publish(topic string, args []interface{}) error {
	s.mu.Lock()
	conn := s.conn
	s.mu.Unlock()
	if conn == nil {
		return nil
	}
	return conn.publish(topic, args)
}

// trackPosition reads a [position, duration] payload in seconds. The donor sent
// milliseconds from AVRCP; this keeps whatever unit the caller used and only
// rejects values that cannot be a position.
func trackPosition(args []interface{}) (int64, int64, error) {
	if len(args) < 1 || len(args) > 3 {
		return 0, 0, errors.New("invalid argument format")
	}
	// A leading source URI is accepted because the donor sent one from some
	// call sites and not others.
	if uri, ok := args[0].(string); ok {
		_ = uri
		args = args[1:]
	}
	if len(args) == 0 {
		return 0, 0, errors.New("track position is required")
	}
	position, ok := signedNumber(args[0])
	if !ok || position < 0 {
		return 0, 0, errors.New("track position must be a non-negative number")
	}
	var duration int64
	if len(args) > 1 {
		value, ok := signedNumber(args[1])
		if !ok || value < 0 {
			return 0, 0, errors.New("track duration must be a non-negative number")
		}
		duration = value
	}
	return position, duration, nil
}

// sourceVolume reads a volumeSet payload. The source URI is optional; without
// one the caller means the active source.
func sourceVolume(args []interface{}) (string, int, error) {
	if len(args) == 0 || len(args) > 2 {
		return "", 0, errors.New("invalid argument format")
	}
	uri := ""
	if len(args) == 2 {
		name, ok := args[0].(string)
		if !ok {
			return "", 0, errors.New("source must be a string")
		}
		uri = name
		args = args[1:]
	}
	level, ok := signedNumber(args[0])
	if !ok {
		return "", 0, errors.New("volume must be a number")
	}
	if level < 0 || level > 100 {
		return "", 0, fmt.Errorf("volume %d is outside 0 to 100", level)
	}
	return uri, int(level), nil
}

func signedNumber(value interface{}) (int64, bool) {
	switch number := value.(type) {
	case int64:
		return number, true
	case uint64:
		return int64(number), true
	case int:
		return int64(number), true
	case float64:
		if number != float64(int64(number)) {
			return 0, false
		}
		return int64(number), true
	}
	return 0, false
}

// call invokes a procedure on whichever peer provides it, ignoring the result.
func (s *service) call(procedure string, args []interface{}) error {
	s.mu.Lock()
	conn := s.conn
	s.mu.Unlock()
	if conn == nil {
		return fmt.Errorf("no router session for %s", procedure)
	}
	_, err := conn.callProcedure(procedure, args, callTimeout)
	return err
}

// forward sends a media verb to the active source, which is what makes the
// control source-agnostic: the caller asks to pause, not to pause Bluetooth.
func (s *service) forward(verb string) error {
	active := s.sources.Active()
	if active == "" {
		return fmt.Errorf("no active source for %s", verb)
	}
	return s.call(active+"."+verb, nil)
}

func main() {
	host := flag.String("router-host", "127.0.0.1", "WAMP router host")
	port := flag.Int("router-port", 9999, "WAMP router port")
	realm := flag.String("realm", "default", "WAMP realm")
	name := flag.String("service-name", "music-source-manager",
		"name used for the ready and heartbeat topics")
	seed := flag.String("register", "",
		"comma separated source URIs admitted at startup")
	flag.Parse()

	log.SetFlags(0)
	log.SetPrefix("source-manager: ")

	s := &service{
		sources:  newRegistry(),
		shutdown: make(chan struct{}),
		logf:     log.Printf,
	}
	for _, uri := range strings.Split(*seed, ",") {
		uri = strings.TrimSpace(uri)
		if uri == "" {
			continue
		}
		if err := s.sources.Register(uri); err != nil {
			log.Fatalf("seed %s: %v", uri, err)
		}
		log.Printf("registered %s", uri)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		// A shutdown procedure and a signal mean the same thing: stop. Both
		// end the run loop rather than killing the process, so the session is
		// closed cleanly and the router is not left with a half-open peer.
		select {
		case <-signals:
		case <-s.shutdown:
		}
		cancel()
	}()

	address := *host + ":" + strconv.Itoa(*port)
	for ctx.Err() == nil {
		if err := s.run(ctx, address, *realm, *name); err != nil && ctx.Err() == nil {
			log.Printf("session ended: %v", err)
		}
		select {
		case <-ctx.Done():
		case <-time.After(5 * time.Second):
		}
	}
}
