// Command reinvoke-pairing-agent turns Bluetooth button presses into Bluedroid
// pairing requests.
//
// Why this exists
//
// The MCU service reports button presses by signalling a pairing agent: it
// reads a PID file, confirms the process is the expected executable, then
// sends SIGUSR1 for a long press and SIGUSR2 for a short one. That contract
// was written for the BlueZ agent, which this runtime no longer ships, so
// every long press failed with "read pairing agent PID: no such file or
// directory" and the speaker never became discoverable. Observed on candidate
// 05.5: the top panel lit, the Bluetooth indicator did not.
//
// This keeps the MCU's contract and redirects the result to the donor stack,
// which registers com.harman.bluetoothPairing on the WAMP router. Calling that
// procedure was measured to move the adapter from BT_SCAN_MODE_CONNECTABLE to
// BT_SCAN_MODE_CONNECTABLE_DISCOVERABLE.
//
// The WAMP call is delegated to reinvoke-identifiers, which already speaks the
// router's rawsocket protocol, rather than reimplementing a second client.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

const callTimeout = 15 * time.Second

// writePIDFile publishes this process's PID where the MCU expects it. The
// write is atomic so the MCU can never read a partial value.
func writePIDFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create PID directory: %w", err)
	}
	temporary := path + ".tmp"
	content := fmt.Sprintf("%d\n", os.Getpid())
	if err := os.WriteFile(temporary, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write PID file: %w", err)
	}
	if err := os.Rename(temporary, path); err != nil {
		return fmt.Errorf("publish PID file: %w", err)
	}
	return nil
}

// requestPairing asks the donor stack to enter discoverable mode.
func requestPairing(caller, procedure string) error {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, caller, "--call", procedure)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w: %s", procedure, err, output)
	}
	return nil
}

func main() {
	pidPath := flag.String("pid-file", "/run/reinvoke/pairing-agent.pid",
		"where the MCU service looks for this agent")
	caller := flag.String("caller", "/bin/reinvoke-identifiers",
		"WAMP client used to reach the router")
	longPress := flag.String("long-press", "com.harman.bluetoothPairing",
		"procedure called for a long press")
	shortPress := flag.String("short-press", "",
		"procedure called for a short press; empty ignores short presses")
	flag.Parse()

	if _, err := os.Stat(*caller); err != nil {
		fmt.Fprintf(os.Stderr, "pairing agent: WAMP client unavailable: %v\n", err)
		os.Exit(1)
	}
	if err := writePIDFile(*pidPath); err != nil {
		fmt.Fprintf(os.Stderr, "pairing agent: %v\n", err)
		os.Exit(1)
	}
	defer os.Remove(*pidPath)

	// SIGUSR1 is the MCU's long press, SIGUSR2 its short press.
	presses := make(chan os.Signal, 4)
	signal.Notify(presses, syscall.SIGUSR1, syscall.SIGUSR2)
	stopping := make(chan os.Signal, 2)
	signal.Notify(stopping, syscall.SIGINT, syscall.SIGTERM)

	fmt.Printf("pairing agent: ready, pid %d, long press -> %s\n",
		os.Getpid(), *longPress)

	for {
		select {
		case <-stopping:
			return
		case received := <-presses:
			procedure := *longPress
			if received == syscall.SIGUSR2 {
				procedure = *shortPress
			}
			if procedure == "" {
				continue
			}
			if err := requestPairing(*caller, procedure); err != nil {
				// Report and keep serving: a failed call must not leave the
				// button dead for the rest of the session.
				fmt.Fprintf(os.Stderr, "pairing agent: %v\n", err)
				continue
			}
			fmt.Printf("pairing agent: called %s\n", procedure)
		}
	}
}
