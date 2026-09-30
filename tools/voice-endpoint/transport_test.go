// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func startTLSHost(t *testing.T, maxVersion uint16, handler func(*tls.Conn) error) (config, <-chan error) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "not-localhost"},
		NotBefore: time.Unix(0, 0), NotAfter: time.Unix(86400, 0),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result, finished := make(chan error, 1), make(chan struct{})
	go func() {
		defer close(finished)
		raw, err := listener.Accept()
		if err != nil {
			result <- err
			return
		}
		defer raw.Close()
		stop := closeOnCancel(ctx, raw)
		defer stop()
		if err := raw.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
			result <- err
			return
		}
		connection := tls.Server(raw, &tls.Config{
			MinVersion: tls.VersionTLS12, MaxVersion: maxVersion,
			Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}},
		})
		result <- handler(connection)
	}()
	t.Cleanup(func() {
		cancel()
		listener.Close()
		select {
		case <-finished:
		case <-time.After(4 * time.Second):
			t.Error("TLS test listener failed to stop")
		}
	})
	return config{
		Endpoint: "localhost:" + strconv.Itoa(listener.Addr().(*net.TCPAddr).Port),
		Pin:      sha256.Sum256(der), Token: testToken, Device: "unit.test_1", IOTimeoutMS: 30000,
	}, result
}

func readTestHello(connection net.Conn) (hello, error) {
	line, err := bufio.NewReaderSize(connection, helloLimit).ReadSlice('\n')
	if err != nil {
		return hello{}, err
	}
	if len(line) > helloLimit {
		return hello{}, errors.New("hello exceeded its wire limit")
	}
	fields, err := jsonObject(line)
	if err != nil || len(fields) != 6 {
		return hello{}, errors.New("hello did not contain exactly the six streaming protocol fields")
	}
	var message hello
	if err := json.Unmarshal(line, &message); err != nil {
		return hello{}, errors.New("invalid hello JSON types")
	}
	return message, nil
}

func acceptTestHello(connection net.Conn) error {
	message, err := readTestHello(connection)
	if err != nil {
		return err
	}
	if message.Token != testToken || message.Protocol != "RIWAKE03" || message.Nonce == 0 {
		return errors.New("incorrect hello authentication or nonce")
	}
	return writeAll(connection, []byte("{\"ok\":true}\n"))
}

func TestPinnedTLSLeafAndMinimumVersion(t *testing.T) {
	for _, version := range []uint16{tls.VersionTLS12, tls.VersionTLS13} {
		t.Run(strconv.Itoa(int(version)), func(t *testing.T) {
			cfg, server := startTLSHost(t, version, func(connection *tls.Conn) error {
				if err := acceptTestHello(connection); err != nil {
					return err
				}
				_, err := io.Copy(io.Discard, connection)
				return err
			})
			connection, _, err := connectHost(context.Background(), cfg, options{})
			if err != nil {
				t.Fatal("correct DER pin must accept an expired, self-signed certificate:", err)
			}
			connection.Close()
			if err := <-server; err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
				t.Fatal(err)
			}
		})
	}
	t.Run("wrong-pin", func(t *testing.T) {
		cfg, _ := startTLSHost(t, tls.VersionTLS13, func(connection *tls.Conn) error {
			return acceptTestHello(connection)
		})
		cfg.Pin[0] ^= 0xff
		connection, _, err := connectHost(context.Background(), cfg, options{})
		if connection != nil {
			connection.Close()
		}
		requirePrivateError(t, err)
		if !strings.Contains(err.Error(), "pin mismatch") {
			t.Fatal("connection was not rejected by the leaf pin")
		}
	})
	settings := pinnedTLS("localhost", [32]byte{})
	if settings.MinVersion != tls.VersionTLS12 || !settings.InsecureSkipVerify || settings.VerifyConnection == nil {
		t.Fatal("TLS must always use TLS >= 1.2 with mandatory replacement pin verification")
	}
	if settings.VerifyConnection(tls.ConnectionState{}) == nil {
		t.Fatal("empty peer certificate list was accepted")
	}
	leaf := &x509.Certificate{Raw: []byte("leaf")}
	other := &x509.Certificate{Raw: []byte("intermediate")}
	settings = pinnedTLS("localhost", sha256.Sum256(other.Raw))
	if settings.VerifyConnection(tls.ConnectionState{PeerCertificates: []*x509.Certificate{leaf, other}}) == nil {
		t.Fatal("an intermediate pin was accepted instead of the leaf pin")
	}
}

func TestHelloAuthFieldsNonceAndBinaryHandoff(t *testing.T) {
	var previous uint32
	for _, test := range []struct {
		name     string
		opts     options
		source   string
		playback bool
	}{
		{"normal", options{}, "socket", true},
		{"fixture", options{Fixture: "/tmp/test.pcm", Once: true, NoPlayback: true}, "fixture", false},
		{"socket-no-playback", options{NoPlayback: true}, "socket", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			received := make(chan hello, 1)
			reply := []byte{0, 255, 'R', 'I', 'R', 'E', 'P', 'L', 'Y', '3', '\n'}
			cfg, server := startTLSHost(t, tls.VersionTLS13, func(connection *tls.Conn) error {
				message, err := readTestHello(connection)
				if err != nil {
					return err
				}
				received <- message
				if err := writeAll(connection, []byte("{\"ok\":true}\n")); err != nil {
					return err
				}
				var request [1]byte
				if _, err := io.ReadFull(connection, request[:]); err != nil {
					return err
				}
				if request[0] != 0xa5 {
					return errors.New("binary request was changed")
				}
				return writeAll(connection, reply)
			})
			connection, nonce, err := connectHost(context.Background(), cfg, test.opts)
			if err != nil {
				t.Fatal(err)
			}
			defer connection.Close()
			message := <-received
			if message.Protocol != "RIWAKE03" || message.Device != cfg.Device ||
				message.Nonce != nonce || nonce == 0 || nonce == previous ||
				message.Token != testToken ||
				message.Source != test.source || message.Playback != test.playback {
				t.Fatal("hello authentication fields, fresh nonce, source or playback did not match")
			}
			previous = nonce
			if err := writeAll(connection, []byte{0xa5}); err != nil {
				t.Fatal(err)
			}
			got := make([]byte, len(reply))
			if _, err := io.ReadFull(connection, got); err != nil || !bytes.Equal(got, reply) {
				t.Fatal("binary bytes after the hello were lost or altered")
			}
			if err := <-server; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestHelloAckValidationAndBounds(t *testing.T) {
	for name, ack := range map[string]string{
		"rejected":           `{"ok":false,"error":"` + testToken + `"}` + "\n",
		"echoed-extra-field": `{"ok":true,"error":"` + testToken + `"}` + "\n",
		"empty":              "{}\n", "array": "[]\n", "null": "null\n", "string": "{\"ok\":\"true\"}\n",
		"duplicate": "{\"ok\":true,\"ok\":true}\n", "case": "{\"OK\":true}\n",
		"trailing-json": "{\"ok\":true}{}\n", "no-newline": "{\"ok\":true}",
		"too-large":         strings.Repeat(" ", helloLimit) + "{\"ok\":true}\n",
		"prefetched-binary": "{\"ok\":true}\nRIWAKE03",
		"invalid-utf8":      "{\"ok\":true,\"bad\":\"\xff\"}\n",
	} {
		t.Run(name, func(t *testing.T) {
			cfg, _ := startTLSHost(t, tls.VersionTLS13, func(connection *tls.Conn) error {
				if _, err := readTestHello(connection); err != nil {
					return err
				}
				return writeAll(connection, []byte(ack))
			})
			connection, _, err := connectHost(context.Background(), cfg, options{})
			if connection != nil {
				connection.Close()
			}
			requirePrivateError(t, err)
		})
	}
	t.Run("exact-1024-byte-line", func(t *testing.T) {
		ack := "{\"ok\":true}\n"
		ack = strings.Repeat(" ", helloLimit-len(ack)) + ack
		cfg, server := startTLSHost(t, tls.VersionTLS13, func(connection *tls.Conn) error {
			if _, err := readTestHello(connection); err != nil {
				return err
			}
			return writeAll(connection, []byte(ack))
		})
		connection, _, err := connectHost(context.Background(), cfg, options{})
		if err != nil {
			t.Fatal(err)
		}
		connection.Close()
		if err := <-server; err != nil {
			t.Fatal(err)
		}
	})
	if err := exchangeHello(nil, hello{Device: strings.Repeat("a", helloLimit)}); err == nil {
		t.Fatal("outbound oversized hello was not bounded before any network access")
	}
}

func TestHostCancellationDuringHandshakeAckAndIdle(t *testing.T) {
	t.Run("tls-handshake", func(t *testing.T) {
		listener, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer listener.Close()
		accepted := make(chan net.Conn, 1)
		go func() {
			connection, err := listener.Accept()
			if err == nil {
				accepted <- connection
			}
		}()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan error, 1)
		go func() {
			connection, _, err := connectHost(ctx, config{Endpoint: listener.Addr().String()}, options{})
			if connection != nil {
				connection.Close()
			}
			done <- err
		}()
		raw := <-accepted
		defer raw.Close()
		cancel()
		select {
		case err := <-done:
			if err == nil {
				t.Fatal("cancelled TLS handshake succeeded")
			}
		case <-time.After(time.Second):
			t.Fatal("cancellation did not interrupt TLS handshake")
		}
	})
	t.Run("ack", func(t *testing.T) {
		helloRead := make(chan struct{})
		cfg, _ := startTLSHost(t, tls.VersionTLS13, func(connection *tls.Conn) error {
			if _, err := readTestHello(connection); err != nil {
				return err
			}
			close(helloRead)
			_, err := io.Copy(io.Discard, connection)
			return err
		})
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan error, 1)
		go func() {
			connection, _, err := connectHost(ctx, cfg, options{})
			if connection != nil {
				connection.Close()
			}
			done <- err
		}()
		<-helloRead
		cancel()
		select {
		case err := <-done:
			requirePrivateError(t, err)
		case <-time.After(time.Second):
			t.Fatal("cancellation did not interrupt hello acknowledgment")
		}
	})
	t.Run("idle", func(t *testing.T) {
		cfg, server := startTLSHost(t, tls.VersionTLS13, func(connection *tls.Conn) error {
			if err := acceptTestHello(connection); err != nil {
				return err
			}
			var data [1]byte
			_, err := connection.Read(data[:])
			if err == nil {
				return errors.New("unexpected idle bytes")
			}
			return nil
		})
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		connection, _, err := connectHost(ctx, cfg, options{})
		if err != nil {
			t.Fatal(err)
		}
		defer connection.Close()
		cancel()
		select {
		case err := <-server:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("cancellation did not close the idle TLS transport")
		}
	})
}

func TestWorkerFactoryRunsOnlyAfterValidHostAck(t *testing.T) {
	for _, accepted := range []bool{false, true} {
		t.Run(fmt.Sprint(accepted), func(t *testing.T) {
			cfg, _ := startTLSHost(t, tls.VersionTLS13, func(connection *tls.Conn) error {
				if _, err := readTestHello(connection); err != nil {
					return err
				}
				return writeAll(connection, []byte(fmt.Sprintf("{\"ok\":%t}\n", accepted)))
			})
			var output bytes.Buffer
			called := false
			router := startIdleActionRouter(t)
			err := runSession(context.Background(), cfg, options{Router: router, Realm: "default"}, log.New(&output, "", 0),
				func(nonce uint32) *exec.Cmd {
					called = true
					if nonce == 0 {
						t.Error("worker factory received a zero nonce")
					}
					return nil
				})
			if err == nil || called != accepted {
				t.Fatal("worker factory did not honor the hello acknowledgment boundary")
			}
			if strings.Contains(output.String(), testToken) {
				t.Fatal("session logged authentication material")
			}
		})
	}
}

func TestFullTLSSessionWithActualWorkerPipes(t *testing.T) {
	fixture := filepath.Join(t.TempDir(), "fixture.pcm")
	if err := os.WriteFile(fixture, []byte{0, 0}, 0600); err != nil {
		t.Fatal(err)
	}
	received := make(chan hello, 1)
	cfg, server := startTLSHost(t, tls.VersionTLS13, func(connection *tls.Conn) error {
		message, err := readTestHello(connection)
		if err != nil {
			return err
		}
		received <- message
		if err := writeAll(connection, []byte("{\"ok\":true}\n")); err != nil {
			return err
		}
		request := make([]byte, len(fakeRequest()))
		if _, err := io.ReadFull(connection, request); err != nil {
			return err
		}
		if !bytes.Equal(request, fakeRequest()) {
			return errors.New("TLS worker request changed")
		}
		if err := writeAll(connection, fakeReply()); err != nil {
			return err
		}
		receipt := make([]byte, len(fakeReceipt()))
		if _, err := io.ReadFull(connection, receipt); err != nil {
			return err
		}
		if !bytes.Equal(receipt, fakeReceipt()) {
			return errors.New("TLS worker receipt changed")
		}
		_, err = io.Copy(io.Discard, connection)
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return nil
		}
		return err
	})
	cmd := helperCommand(t, "exchange")
	cmd.Env = append(cmd.Env, "REINVOKE_TEST_QUIET_PHASES=1")
	var nonce uint32
	var output bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	router := startIdleActionRouter(t)
	err := runSession(ctx, cfg, options{Fixture: fixture, Once: true, NoPlayback: true, Router: router, Realm: "default"}, log.New(&output, "", 0),
		func(value uint32) *exec.Cmd {
			nonce = value
			return cmd
		})
	if err != nil {
		t.Fatal(err)
	}
	message := <-received
	if nonce == 0 || nonce != message.Nonce || message.Source != "fixture" || message.Playback || message.Token != testToken {
		t.Fatal("worker session did not use the authenticated hello parameters")
	}
	if err := <-server; err != nil {
		t.Fatal(err)
	}
	assertReaped(t, cmd.Process.Pid)
	if strings.Contains(output.String(), testToken) {
		t.Fatal("TLS pipe session logged the token")
	}
}

func TestFullTLSSessionCancellation(t *testing.T) {
	ready := make(chan struct{})
	cfg, _ := startTLSHost(t, tls.VersionTLS13, func(connection *tls.Conn) error {
		if err := acceptTestHello(connection); err != nil {
			return err
		}
		var data [6]byte
		if _, err := io.ReadFull(connection, data[:]); err != nil {
			return err
		}
		if string(data[:]) != "READY\n" {
			return errors.New("worker did not become ready")
		}
		close(ready)
		_, err := io.Copy(io.Discard, connection)
		return err
	})
	cmd := helperCommand(t, "graceful")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	router := startIdleActionRouter(t)
	go func() {
		done <- runSession(ctx, cfg, options{Router: router, Realm: "default"}, log.New(io.Discard, "", 0), func(uint32) *exec.Cmd { return cmd })
	}()
	<-ready
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("TLS network cancellation did not retain its context error:", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("full TLS session did not terminate on cancellation")
	}
	assertReaped(t, cmd.Process.Pid)
}
