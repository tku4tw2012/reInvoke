// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"
)

const recoveredMCUVersion = "000116"

func main() {
	routerHost := flag.String("router-host", "127.0.0.1", "WAMP router host")
	routerPort := flag.Int("router-port", 9999, "WAMP RawSocket port")
	realm := flag.String("realm", "default", "WAMP realm")
	i2cPath := flag.String("i2c", "/dev/i2c-0", "I2C bus device")
	gpioRoot := flag.String(
		"gpio-root",
		"/sys/class/gpio",
		"GPIO sysfs root; empty disables rotary input",
	)
	gpioNumber := flag.Int("gpio", 3, "MCU interrupt GPIO")
	devmemPath := flag.String(
		"devmem",
		"/dev/mem",
		"physical register device used for the donor GPIO3 pinmux update",
	)
	protocolState := flag.String(
		"protocol-state",
		"/run/reinvoke/mcu-protocol-ready",
		"RAM marker for the once-per-boot MCU startup exchange",
	)
	allowUnmute := flag.Bool(
		"allow-unmute",
		false,
		"allow verified WAMP unmute requests after safe initialization",
	)
	playbackStatus := flag.String(
		"playback-status",
		"",
		"ALSA playback status path that owns automatic physical mute policy",
	)
	playbackLease := flag.String(
		"playback-lease",
		"",
		"RAM lease written while real Bluetooth PCM data is arriving",
	)
	playbackOwnerExecutable := flag.String(
		"playback-owner-executable",
		"",
		"executable permitted to activate the physical playback path",
	)
	blueALSACLI := flag.String(
		"bluealsa-cli",
		"",
		"BlueALSA CLI used for physical rotary volume control",
	)
	blueALSAPeer := flag.String(
		"bluealsa-peer",
		"",
		"allowlisted Bluetooth peer used for rotary volume control",
	)
	pairingAgentPID := flag.String(
		"pairing-agent-pid",
		"",
		"PID file for the pairing agent controlled by Bluetooth long press",
	)
	pairingAgentExecutable := flag.String(
		"pairing-agent-executable",
		"",
		"expected pairing agent executable path",
	)
	lightsDirectory := flag.String(
		"lights-dir",
		"",
		"directory containing reviewed 13-byte-frame LED animations",
	)
	flag.Parse()

	if *routerPort < 1 || *routerPort > 65535 {
		log.Fatal("router-port must be from 1 through 65535")
	}
	if *gpioNumber < 0 {
		log.Fatal("gpio must be non-negative")
	}
	if (*blueALSACLI == "") != (*blueALSAPeer == "") {
		log.Fatal("bluealsa-cli and bluealsa-peer must be supplied together")
	}
	playbackPolicyValues := 0
	for _, value := range []string{
		*playbackStatus,
		*playbackLease,
		*playbackOwnerExecutable,
	} {
		if value != "" {
			playbackPolicyValues++
		}
	}
	if playbackPolicyValues != 0 && playbackPolicyValues != 3 {
		log.Fatal(
			"playback-status, playback-lease, and playback-owner-executable must be supplied together",
		)
	}
	if (*pairingAgentPID == "") != (*pairingAgentExecutable == "") {
		log.Fatal(
			"pairing-agent-pid and pairing-agent-executable must be supplied together",
		)
	}

	bus, err := openLinuxI2C(*i2cPath)
	if err != nil {
		log.Fatal(err)
	}
	defer bus.Close()

	control := newController(bus, mutePolicy{
		AllowUnmute:         *allowUnmute,
		AllowPlaybackUnmute: playbackPolicyValues == 3,
	})
	if err := control.initialize(); err != nil {
		log.Fatalf("safe hardware initialization failed: %v", err)
	}
	if err := initializeMCUProtocolOnce(bus, *protocolState); err != nil {
		_ = control.muteAll()
		log.Fatalf("MCU protocol initialization failed: %v", err)
	}

	ctx, cancel := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer cancel()

	var source eventSource
	var gpioValue *os.File
	if *gpioRoot != "" {
		if err := configureMCUInterruptPin(*devmemPath); err != nil {
			log.Fatalf("configure MCU interrupt pin: %v", err)
		}
		gpioValue, err = prepareGPIO(*gpioRoot, *gpioNumber)
		if err != nil {
			log.Fatalf("initialize rotary input: %v", err)
		}
		defer gpioValue.Close()
		source = &gpioEventSource{
			value: gpioValue,
			bus:   bus,
			logError: func(err error) {
				log.Printf("rotary input: %v", err)
			},
		}
	}

	var inputControls inputControllerList
	var media *blueALSAController
	var lights *ledPlayer
	if *blueALSACLI != "" {
		media, err = newBlueALSAController(
			*blueALSACLI,
			*blueALSAPeer,
			nil,
		)
		if err != nil {
			log.Fatal(err)
		}
		inputControls = append(inputControls, media)
	}
	if *pairingAgentPID != "" {
		inputControls = append(inputControls, pairingSignalController{
			pidPath:    *pairingAgentPID,
			executable: *pairingAgentExecutable,
		})
	}
	if *lightsDirectory != "" {
		lights = &ledPlayer{
			directory: *lightsDirectory,
			writer:    bus,
			logf:      log.Printf,
		}
		inputControls = append(inputControls, lights)
		if err := lights.Start(
			ctx,
			"L_311_d_pluggedin",
			false,
		); err != nil {
			log.Fatalf("start boot LED animation: %v", err)
		}
	}
	var relayDone chan struct{}
	if source != nil {
		publications := make(chan inputEvent, 32)
		hardwareEvents := source.Events(ctx)
		relayDone = make(chan struct{})
		go func() {
			defer close(relayDone)
			runEventRelay(
				ctx,
				hardwareEvents,
				inputControls,
				publications,
				log.Printf,
			)
		}()
		source = relayedEventSource{events: publications}
	}

	service := wampService{
		address:     *routerHost + ":" + strconv.Itoa(*routerPort),
		realm:       *realm,
		controller:  control,
		media:       media,
		lights:      lights,
		events:      source,
		version:     recoveredMCUVersion,
		flushEvents: true,
	}
	log.Printf(
		"hardware initialized muted; WAMP unmute policy=%t",
		*allowUnmute,
	)
	heartbeatDone := make(chan error, 1)
	go func() {
		err := runMCUHeartbeat(ctx, bus, mcuHeartbeatInterval)
		if err != nil {
			cancel()
		}
		heartbeatDone <- err
	}()
	playbackDone := make(chan error, 1)
	if *playbackStatus != "" {
		go func() {
			err := runPlaybackPolicy(
				ctx,
				*playbackStatus,
				*playbackLease,
				*playbackOwnerExecutable,
				control,
				playbackPolicyInterval,
				log.Printf,
			)
			if err != nil {
				cancel()
			}
			playbackDone <- err
		}()
	} else {
		playbackDone <- nil
	}
	runErr := runWithReconnect(
		ctx,
		wampReconnectDelay,
		service.run,
		log.Printf,
	)
	cancel()
	heartbeatErr := <-heartbeatDone
	playbackErr := <-playbackDone
	if relayDone != nil {
		<-relayDone
	}
	muteErr := control.muteAll()
	if runErr != nil || heartbeatErr != nil ||
		playbackErr != nil || muteErr != nil {
		if runErr != nil {
			fmt.Fprintf(os.Stderr, "mcu-interface: %v\n", runErr)
		}
		if heartbeatErr != nil {
			fmt.Fprintf(os.Stderr, "mcu-interface: %v\n", heartbeatErr)
		}
		if playbackErr != nil {
			fmt.Fprintf(os.Stderr, "mcu-interface: %v\n", playbackErr)
		}
		if muteErr != nil {
			fmt.Fprintf(os.Stderr, "mcu-interface: shutdown: %v\n", muteErr)
		}
		os.Exit(1)
	}
}
