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

	logf func(string, ...interface{})
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

	s := &service{sources: newRegistry(), logf: log.Printf}
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
		<-signals
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
