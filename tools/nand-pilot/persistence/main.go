// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

type request struct {
	Operation string   `json:"operation"`
	Profile   *profile `json:"profile,omitempty"`
}

type response struct {
	OK      bool     `json:"ok"`
	Code    string   `json:"code"`
	Profile *profile `json:"profile,omitempty"`
}

type service struct {
	mu        sync.Mutex
	store     storage
	current   snapshot
	lastWrite time.Time
	report    func(publicStatus)
}

func newStorage() storage {
	return storage{
		root: storePath, runtime: runtimeDir, bonds: bondsPath,
		uid: 0, check: verifyMounted,
	}
}

func checkRAM(path string) error {
	if err := trustedParents(path, 0); err != nil {
		return err
	}
	var fs syscall.Statfs_t
	if err := syscall.Statfs(path, &fs); err != nil {
		return errors.New("PERSIST_RAM_UNAVAILABLE")
	}
	if value := uint64(fs.Type) & 0xffffffff; value != 0x01021994 && value != 0x858458f6 {
		return errors.New("PERSIST_RUNTIME_NOT_RAM")
	}
	return nil
}

func prepare() error {
	for _, path := range []string{runtimeDir, bondsPath} {
		if err := checkRAM(path); err != nil {
			return err
		}
	}
	if err := ensurePrivateDirectory(runtimeDir, 0); err != nil {
		return err
	}
	if err := prepareMount(); err != nil {
		return err
	}
	store := newStorage()
	if err := store.cleanInterruptedWrites(); err != nil {
		return err
	}
	state, err := store.load()
	switch {
	case err == nil:
		if err := store.restore(state); err != nil {
			return err
		}
		log.Print("PERSIST_RESTORED")
	case err.Error() == "PERSIST_STATE_ABSENT":
		log.Print("PERSIST_STATE_ABSENT")
		state = snapshot{Version: 1, Files: map[string][]byte{}}
	default:
		return err
	}
	if err := atomicPrivate(filepath.Join(runtimeDir, "persistence-prepared"), []byte("1\n"), 0, nil); err != nil {
		return err
	}
	publishStatus(statusFor(state, "prepared", "PERSIST_PREPARED"))
	return nil
}

func rootPeer(connection *net.UnixConn) bool {
	raw, err := connection.SyscallConn()
	if err != nil {
		return false
	}
	var peer *syscall.Ucred
	var peerErr error
	err = raw.Control(func(fd uintptr) {
		peer, peerErr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	})
	return err == nil && peerErr == nil && peer != nil && peer.Uid == 0
}

func (s *service) handle(req request) (answer response) {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer func() {
		if answer.OK || s.report == nil {
			return
		}
		switch answer.Code {
		case "PERSIST_REQUEST_INVALID", "PERSIST_PROFILE_INVALID", "PERSIST_SAVE_RATE_LIMITED",
			"PERSIST_PROFILE_ABSENT", "PERSIST_STATE_ABSENT", "PERSIST_PROFILE_EXISTS":
			return
		}
		s.report(statusFor(snapshot{}, "degraded", answer.Code))
	}()
	if err := s.store.check(); err != nil {
		return response{Code: err.Error()}
	}
	switch req.Operation {
	case "load":
		if req.Profile != nil {
			return response{Code: "PERSIST_REQUEST_INVALID"}
		}
		durable, err := s.store.load()
		if err != nil {
			return response{Code: err.Error()}
		}
		if durable.WiFi == nil {
			return response{Code: "PERSIST_PROFILE_ABSENT"}
		}
		return response{OK: true, Code: "PERSIST_PROFILE_LOADED", Profile: durable.WiFi}
	case "save", "save-seed":
		if req.Profile == nil || validateProfile(*req.Profile) != nil {
			return response{Code: "PERSIST_PROFILE_INVALID"}
		}
		if req.Operation == "save-seed" {
			durable, err := s.store.load()
			if err != nil && err.Error() != "PERSIST_STATE_ABSENT" {
				return response{Code: err.Error()}
			}
			if s.current.WiFi != nil || (err == nil && durable.WiFi != nil) {
				return response{Code: "PERSIST_PROFILE_EXISTS"}
			}
		}
		if s.current.WiFi != nil && *s.current.WiFi == *req.Profile {
			durable, err := s.store.load()
			if err != nil {
				return response{Code: err.Error()}
			}
			if durable.WiFi == nil || *durable.WiFi != *req.Profile {
				return response{Code: "PERSIST_PROFILE_MISMATCH"}
			}
			return response{OK: true, Code: "PERSIST_PROFILE_UNCHANGED"}
		}
		if time.Since(s.lastWrite) < 30*time.Second {
			return response{Code: "PERSIST_SAVE_RATE_LIMITED"}
		}
		state, err := s.store.capture(req.Profile)
		if err == nil {
			err = retainRequiredSettings(s.current, state)
		}
		if err == nil {
			err = s.store.save(state)
		}
		if err != nil {
			return response{Code: err.Error()}
		}
		s.current = state
		s.lastWrite = time.Now()
		if s.report != nil {
			s.report(statusFor(state, "ready", "PERSIST_COMMITTED"))
		}
		return response{OK: true, Code: "PERSIST_PROFILE_SAVED"}
	default:
		return response{Code: "PERSIST_REQUEST_INVALID"}
	}
}

func (s *service) flush() (result error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer func() {
		if result != nil && s.report != nil {
			s.report(statusFor(snapshot{}, "degraded", result.Error()))
		}
	}()
	state, err := s.store.capture(s.current.WiFi)
	if err != nil {
		return err
	}
	if err := retainRequiredSettings(s.current, state); err != nil {
		return err
	}
	if err := s.store.save(state); err != nil {
		return err
	}
	s.current = state
	if s.report != nil {
		s.report(statusFor(state, "ready", "PERSIST_COMMITTED"))
	}
	return nil
}

func (s *service) connection(connection *net.UnixConn) {
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(3 * time.Second))
	if !rootPeer(connection) {
		return
	}
	content, err := io.ReadAll(io.LimitReader(connection, 4097))
	var req request
	if err != nil || len(content) > 4096 || decodeStrict(content, &req) != nil {
		_ = json.NewEncoder(connection).Encode(response{Code: "PERSIST_REQUEST_INVALID"})
		return
	}
	_ = json.NewEncoder(connection).Encode(s.handle(req))
}

func serve() error {
	if err := checkRAM(runtimeDir); err != nil {
		return err
	}
	marker, err := readPrivate(filepath.Join(runtimeDir, "persistence-prepared"), 0, 8)
	if err != nil || string(marker) != "1\n" {
		return errors.New("PERSIST_NOT_PREPARED")
	}
	store := newStorage()
	state, err := store.load()
	if err != nil {
		if err.Error() != "PERSIST_STATE_ABSENT" {
			return err
		}
		state = snapshot{Version: 1, Files: map[string][]byte{}}
	}
	s := &service{store: store, current: state, report: publishStatus}
	if _, err := os.Lstat(socketPath); !errors.Is(err, os.ErrNotExist) {
		return errors.New("PERSIST_SOCKET_EXISTS")
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socketPath, Net: "unix"})
	if err != nil {
		return errors.New("PERSIST_SOCKET_FAILED")
	}
	defer listener.Close()
	listener.SetUnlinkOnClose(true)
	if err := os.Chmod(socketPath, 0600); err != nil {
		return errors.New("PERSIST_SOCKET_UNSAFE")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		var lastError string
		for {
			select {
			case <-ctx.Done():
				_ = listener.Close()
				return
			case <-ticker.C:
				if err := s.flush(); err != nil {
					if err.Error() != lastError {
						log.Print(err.Error())
						lastError = err.Error()
					}
				} else {
					lastError = ""
				}
			}
		}
	}()
	log.Print("PERSIST_READY")
	publishStatus(statusFor(state, "ready", "PERSIST_READY"))
	for {
		connection, err := listener.AcceptUnix()
		if err != nil {
			cancel()
			<-done
			if err := s.flush(); err != nil {
				return err
			}
			log.Print("PERSIST_FLUSHED")
			publishStatus(statusFor(s.current, "stopped", "PERSIST_FLUSHED"))
			return nil
		}
		// One bounded peer at a time; only root can submit profile data.
		s.connection(connection)
	}
}

func main() {
	log.SetFlags(0)
	syscall.Umask(0077)
	if os.Geteuid() != 0 || len(os.Args) != 2 {
		log.Print("PERSIST_USAGE: reinvoke-persist prepare|serve")
		os.Exit(1)
	}
	var err error
	switch os.Args[1] {
	case "prepare":
		err = prepare()
	case "serve":
		err = serve()
	default:
		err = errors.New("PERSIST_USAGE: reinvoke-persist prepare|serve")
	}
	if err != nil {
		publishStatus(statusFor(snapshot{}, "volatile", err.Error()))
		log.Print(err.Error())
		os.Exit(1)
	}
}
