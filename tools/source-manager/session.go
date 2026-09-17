// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"
)

const (
	wampError      = 8
	wampHello      = 1
	wampWelcome    = 2
	wampPublish    = 16
	wampCall       = 48
	wampResult     = 50
	wampRegister   = 64
	wampRegistered = 65
	wampInvocation = 68
	wampYield      = 70
)

// procedures this service owns. The media verbs are the donor's
// source-agnostic names; the transport-specific ones stay with the stack that
// registers them.
var procedures = []string{
	"com.harman.source.register",
	"com.harman.source.start",
	"com.harman.source.get-active",
	"com.harman.source.get-registered",
	"com.harman.source.flush",
	"com.harman.music.pause",
	"com.harman.music.resume",
	"com.harman.music.stop",
	"com.harman.music.cmdPlayPause",
}

func (s *service) run(ctx context.Context, address, realm, name string) error {
	raw, err := net.DialTimeout("tcp", address, 5*time.Second)
	if err != nil {
		return fmt.Errorf("dial router: %w", err)
	}
	defer raw.Close()
	conn := newConnection(raw)
	if err := conn.negotiate(realm); err != nil {
		return fmt.Errorf("negotiate: %w", err)
	}

	registrations := map[uint64]string{}
	for _, procedure := range procedures {
		id, err := conn.register(procedure)
		if err != nil {
			// Another service already owning a name is a configuration
			// question, not a crash: say which one and keep the rest.
			s.logf("register %s: %v", procedure, err)
			continue
		}
		registrations[id] = procedure
	}

	s.mu.Lock()
	s.conn = conn
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.conn = nil
		s.mu.Unlock()
	}()

	// Readiness is published only once the procedures are actually answerable,
	// which is the distinction a pid file cannot make.
	if err := conn.publish("com.harman.ready."+name, nil); err != nil {
		return fmt.Errorf("publish ready: %w", err)
	}
	s.logf("ready with %d procedures", len(registrations))

	beats := time.NewTicker(heartbeatInterval)
	defer beats.Stop()
	errs := make(chan error, 1)
	go func() {
		for {
			message, err := conn.readFrame()
			if err != nil {
				errs <- err
				return
			}
			// Call replies belong to whoever is waiting for them; only
			// what is left is an invocation for this service to answer.
			if conn.deliver(message) {
				continue
			}
			if err := s.dispatch(conn, registrations, message); err != nil {
				errs <- err
				return
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-errs:
			return err
		case <-beats.C:
			if err := conn.publish("com.harman.heartbeat."+name, nil); err != nil {
				return fmt.Errorf("publish heartbeat: %w", err)
			}
		}
	}
}

// dispatch answers one invocation.
func (s *service) dispatch(
	conn *connection,
	registrations map[uint64]string,
	message []interface{},
) error {
	if messageType(message) != wampInvocation || len(message) < 4 {
		return nil
	}
	request, ok := unsigned(message[1])
	if !ok {
		return nil
	}
	registration, ok := unsigned(message[2])
	if !ok {
		return nil
	}
	procedure, known := registrations[registration]
	if !known {
		return nil
	}
	var args []interface{}
	if len(message) >= 5 {
		args, _ = message[4].([]interface{})
	}

	result, err := s.handle(procedure, args)
	if err != nil {
		s.logf("%s: %v", procedure, err)
		return conn.writeFrame([]interface{}{
			wampError, wampInvocation, request,
			map[string]interface{}{}, "com.harman.error",
			[]interface{}{err.Error()},
		})
	}
	return conn.writeFrame([]interface{}{
		wampYield, request, map[string]interface{}{}, result,
	})
}

func firstString(args []interface{}) (string, error) {
	if len(args) == 0 {
		return "", errors.New("expected one positional argument")
	}
	text, ok := args[0].(string)
	if !ok {
		return "", errors.New("expected a string argument")
	}
	return text, nil
}

// handle implements the contract. Reply shapes follow the donor's own tests:
// get-active and get-registered answer positionally, and get-active answers
// with an empty string rather than omitting the value when nothing is active.
func (s *service) handle(procedure string, args []interface{}) ([]interface{}, error) {
	switch procedure {
	case "com.harman.source.register":
		uri, err := firstString(args)
		if err != nil {
			return nil, err
		}
		if err := s.sources.Register(uri); err != nil {
			return nil, err
		}
		s.logf("registered %s", uri)
		return []interface{}{}, nil

	case "com.harman.source.start":
		uri, err := firstString(args)
		if err != nil {
			return nil, err
		}
		displaced, err := s.sources.Start(uri)
		if err != nil {
			return nil, err
		}
		if displaced != "" {
			// The donor stops the source it displaces; without this two
			// sources would render into the same DAC at once.
			if err := s.call(displaced+".stop", nil); err != nil {
				s.logf("stop displaced %s: %v", displaced, err)
			}
		}
		s.logf("active source is %s", uri)
		_ = s.publishState("playing", uri)
		return []interface{}{}, nil

	case "com.harman.source.get-active":
		return []interface{}{s.sources.Active()}, nil

	case "com.harman.source.get-registered":
		registered := s.sources.Registered()
		out := make([]interface{}, len(registered))
		for i, uri := range registered {
			out[i] = uri
		}
		return out, nil

	case "com.harman.source.flush":
		s.sources.Flush()
		s.logf("registry flushed")
		return []interface{}{}, nil

	case "com.harman.music.pause", "com.harman.music.cmdPlayPause":
		if err := s.forward("pause"); err != nil {
			return nil, err
		}
		_ = s.publishState("paused", s.sources.Active())
		return []interface{}{}, nil

	case "com.harman.music.resume":
		if err := s.forward("resume"); err != nil {
			return nil, err
		}
		_ = s.publishState("playing", s.sources.Active())
		return []interface{}{}, nil

	case "com.harman.music.stop":
		if err := s.forward("stop"); err != nil {
			return nil, err
		}
		_ = s.publishState("stopped", s.sources.Active())
		return []interface{}{}, nil
	}
	return nil, fmt.Errorf("unhandled procedure %s", procedure)
}

func (s *service) publishState(state, uri string) error {
	s.mu.Lock()
	conn := s.conn
	s.mu.Unlock()
	if conn == nil {
		return nil
	}
	return conn.publish("com.harman.music.stateChanged",
		[]interface{}{state, uri})
}
