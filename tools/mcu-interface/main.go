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
	microphoneState := flag.String(
		"microphone-state",
		"/run/reinvoke/microphone-state",
		"RAM state used to restore microphone privacy after a service restart",
	)
	microphoneControlSocket := flag.String(
		"dsp-mic-control-socket",
		"/run/reinvoke/dsp-mic-control.sock",
		"root-only DSP microphone control socket",
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
	softvolCard := flag.Int(
		"softvol-card",
		0,
		"ALSA card carrying the softvol controls",
	)
	softvolControl := flag.String(
		"softvol-control",
		"music",
		"ALSA softvol control the user volume rides on; empty disables it",
	)
	musicVolumeState := flag.String(
		"music-volume-state",
		"",
		"optional private RAM music-volume preference restored by persistence",
	)
	pairingAgentPID := flag.String(
		"pairing-agent-pid",
		"",
		"PID file for the pairing agent controlled by Bluetooth presses",
	)
	pairingAgentExecutable := flag.String(
		"pairing-agent-executable",
		"",
		"expected pairing agent executable path",
	)
	bluetoothState := flag.String(
		"bluetooth-state",
		"",
		"authoritative pairing-agent state used for the rear indicator",
	)
	bluetoothControlCaller := flag.String(
		"bluetooth-control-caller",
		"/bin/reinvoke-identifiers",
		"fixed WAMP caller for the top-tap pause; empty disables it",
	)
	buttonDispatch := flag.String(
		"button-dispatch",
		"local",
		"local performs button actions here; publish-only announces them and "+
			"leaves them to whatever owns the button state machine",
	)
	provisioningSocket := flag.String(
		"provisioning-socket",
		"",
		"root-only socket used to open a Wi-Fi provisioning window",
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
	if !validButtonDispatch(*buttonDispatch) {
		log.Fatalf(
			"button-dispatch must be %q or %q",
			dispatchLocal,
			dispatchPublishOnly,
		)
	}
	if *gpioNumber < 0 {
		log.Fatal("gpio must be non-negative")
	}
	// The lease is optional now that the renderer is the donor stack, which
	// never writes one. Status and owner still travel together: without both,
	// the policy cannot tell who is holding the playback device.
	if (*playbackStatus == "") != (*playbackOwnerExecutable == "") {
		log.Fatal(
			"playback-status and playback-owner-executable must be supplied together",
		)
	}
	if *playbackLease != "" && *playbackStatus == "" {
		log.Fatal("playback-lease requires playback-status")
	}
	if (*pairingAgentPID == "") != (*pairingAgentExecutable == "") {
		log.Fatal(
			"pairing-agent-pid and pairing-agent-executable must be supplied together",
		)
	}
	microphoneMuted, err := loadOrInitializeMicrophoneState(*microphoneState)
	if err != nil {
		log.Fatal(err)
	}
	bus, err := openLinuxI2C(*i2cPath)
	if err != nil {
		log.Fatal(err)
	}
	defer bus.Close()

	control := newController(bus)
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
	var media *dspVolumeController
	var lights *ledPlayer
	var actionDone chan struct{}
	var volumeDone chan struct{}
	if *microphoneControlSocket != "" {
		media, err = newDSPVolumeController(*microphoneControlSocket)
		if err != nil {
			log.Fatal(err)
		}
		if *musicVolumeState != "" {
			if err := validateMusicStateDirectory(*musicVolumeState); err != nil {
				log.Fatal(err)
			}
			volume, err := readMusicVolume(*musicVolumeState)
			if err != nil {
				log.Fatal(err)
			}
			media.musicStatePath = *musicVolumeState
			media.savedVolume = volume
			media.hasSavedVolume = true
			media.volume = volume
		}
		// The DSP applies the level; 05.8.6 only remembered it. The caller is
		// the same fixed WAMP caller the top-tap pause uses.
		media.caller = *bluetoothControlCaller
		media.host = *routerHost
		media.port = *routerPort
		media.realm = *realm
		media.dspProcedure = "com.harman.dsp.volumeSet"
		media.logf = log.Printf
		// The control only exists once the donor stack has opened the named
		// PCM, so element lookups fail until then. Opening the card now and
		// letting the worker retry keeps that a transient, logged condition
		// rather than a startup failure.
		if *softvolControl != "" {
			control, err := openSoftvol(*softvolCard, *softvolControl)
			if err != nil {
				log.Printf("open softvol %s: %v", *softvolControl, err)
			} else {
				defer control.Close()
				media.softvol = control
			}
		}
		inputControls = append(inputControls, media)
		volumeDone = make(chan struct{})
		go func() {
			defer close(volumeDone)
			media.Run(ctx)
		}()
	}
	if *pairingAgentPID != "" && *buttonDispatch == dispatchLocal {
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
		if !microphoneMuted {
			if err := lights.Start(
				ctx,
				"L_311_d_pluggedin",
				false,
			); err != nil {
				log.Fatalf("start boot LED animation: %v", err)
			}
		}
	}
	if *bluetoothControlCaller != "" && *playbackStatus != "" &&
		*buttonDispatch == dispatchLocal {
		actions := &mediaActionController{
			caller: *bluetoothControlCaller, host: *routerHost, port: *routerPort,
			realm: *realm, playbackStatus: *playbackStatus,
			requests: make(chan struct{}, 1), logf: log.Printf,
		}
		inputControls = append(inputControls, actions)
		actionDone = make(chan struct{})
		go func() {
			defer close(actionDone)
			actions.Run(ctx)
		}()
	}
	if *provisioningSocket != "" && *buttonDispatch == dispatchLocal {
		inputControls = append(inputControls, provisioningController{
			socketPath: *provisioningSocket,
			lights:     lights,
		})
	}
	privacy := newMicrophonePrivacyController(
		microphoneMuted,
		*microphoneState,
		*microphoneControlSocket,
		lights,
		log.Printf,
	)
	privacy.lifetime = ctx
	inputControls = append(inputControls, privacy)
	privacyDone := make(chan struct{})
	go func() {
		defer close(privacyDone)
		privacy.Run(ctx)
	}()
	if microphoneMuted {
		privacy.RequestReconcile()
	}
	indicatorLEDs := newIndicatorLEDController(bus)
	appearance := newDeviceAppearanceController(bus, log.Printf)
	// The vendor's own startup appearance, from its settings store rather than
	// chosen here: LED_INTENSITY 50 and LED_RGB 000000.
	appearance.ApplyDefaults()
	bluetoothDone := make(chan error, 1)
	if *bluetoothState != "" {
		if err := ensureBluetoothStateDirectory(*bluetoothState); err != nil {
			log.Printf("bluetooth state directory: %v", err)
		}
		watcher := &bluetoothStateWatcher{
			path:      *bluetoothState,
			indicator: indicatorLEDs,
			logf:      log.Printf,
		}
		go func() {
			err := watcher.Run(ctx)
			if err != nil {
				cancel()
			}
			bluetoothDone <- err
		}()
	} else {
		bluetoothDone <- nil
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
		address:        *routerHost + ":" + strconv.Itoa(*routerPort),
		realm:          *realm,
		controller:     control,
		media:          media,
		lights:         lights,
		indicatorLEDs:  indicatorLEDs,
		events:         source,
		version:        recoveredMCUVersion,
		privacy:        privacy,
		appearance:     appearance,
		bluetoothState: *bluetoothState,
		playbackStatus: *playbackStatus,
		logf:           log.Printf,
	}
	log.Print("hardware initialized muted")
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
				playbackPolicyHoldoff,
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
	if actionDone != nil {
		<-actionDone
	}
	if volumeDone != nil {
		<-volumeDone
	}
	<-privacyDone
	heartbeatErr := <-heartbeatDone
	playbackErr := <-playbackDone
	if relayDone != nil {
		<-relayDone
	}
	bluetoothErr, bluetoothClearErr := finalizeBluetoothIndicator(
		bluetoothDone,
		indicatorLEDs,
	)
	muteErr := control.muteAll()
	if runErr != nil || heartbeatErr != nil ||
		playbackErr != nil || bluetoothErr != nil ||
		bluetoothClearErr != nil || muteErr != nil {
		if runErr != nil {
			fmt.Fprintf(os.Stderr, "mcu-interface: %v\n", runErr)
		}
		if heartbeatErr != nil {
			fmt.Fprintf(os.Stderr, "mcu-interface: %v\n", heartbeatErr)
		}
		if playbackErr != nil {
			fmt.Fprintf(os.Stderr, "mcu-interface: %v\n", playbackErr)
		}
		if bluetoothErr != nil {
			fmt.Fprintf(os.Stderr, "mcu-interface: %v\n", bluetoothErr)
		}
		if bluetoothClearErr != nil {
			fmt.Fprintf(os.Stderr, "mcu-interface: %v\n", bluetoothClearErr)
		}
		if muteErr != nil {
			fmt.Fprintf(os.Stderr, "mcu-interface: shutdown: %v\n", muteErr)
		}
		os.Exit(1)
	}
}
