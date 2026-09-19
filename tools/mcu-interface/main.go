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
	cueDirectory := flag.String("cue-dir", "",
		"directory holding the device cue sounds; empty disables cues")
	cuePlayerPath := flag.String("cue-player", "/opt/reinvoke/bin/aplay",
		"renderer used for device cues")
	cueLoader := flag.String("cue-loader", "",
		"dynamic loader for the cue renderer, if it needs one")
	cueLibraryPath := flag.String("cue-libpath", "",
		"LD_LIBRARY_PATH for the cue renderer")
	applyAppearanceDefaults := flag.Bool(
		"apply-appearance-defaults", true,
		"send the vendor LED brightness and colour at startup")
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
		"RAM state used to restore microphone mute after a service restart",
	)
	microphoneControlSocket := flag.String(
		"dsp-mic-control-socket",
		"/run/reinvoke/dsp-mic-control.sock",
		"root-only DSP microphone control socket",
	)
	playbackStatus := flag.String(
		"playback-status",
		"",
		"ALSA playback status path read to tell whether something is playing",
	)
	softvolCard := flag.Int(
		"softvol-card",
		0,
		"ALSA card carrying the softvol controls",
	)
	softvolControl := flag.String(
		"softvol-control",
		"",
		"ALSA softvol control to fade the user volume on; empty disables it",
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

	var gpioSource *gpioEventSource
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
		gpioSource = &gpioEventSource{
			value: gpioValue,
			bus:   bus,
			logError: func(err error) {
				log.Printf("rotary input: %v", err)
			},
		}
		source = gpioSource
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
	micMute := newMicrophoneMuteController(
		microphoneMuted,
		*microphoneState,
		*microphoneControlSocket,
		lights,
		log.Printf,
	)
	micMute.lifetime = ctx
	inputControls = append(inputControls, micMute)
	micMuteDone := make(chan struct{})
	go func() {
		defer close(micMuteDone)
		micMute.Run(ctx)
	}()
	if microphoneMuted {
		micMute.RequestReconcile()
	}
	indicatorLEDs := newIndicatorLEDController(bus)
	// Device cues: the short sounds the speaker makes about itself. The donor
	// played these from audio-ui; the asset names and the states that trigger
	// them come from its own table. See docs/device-cues.md.
	cues := &cuePlayer{
		directory: *cueDirectory,
		player:    *cuePlayerPath,
		loader:    *cueLoader,
		libPath:   *cueLibraryPath,
		logf:      log.Printf,
	}
	if media != nil {
		cues.volume = func() int { return media.displayLevel() }
	}

	appearance := newDeviceAppearanceController(bus, log.Printf)
	if media != nil {
		// The ring arc is drawn by the microcontroller from the level, which
		// is why no volume animation exists in the lights directory while
		// every other cue does. Without this the dial moves silently.
		media.ring = appearance
		// The cue for reaching the top of the range. Rendered asynchronously
		// so a rotary sweep is never waiting on audio.
		media.atMax = func() { cues.PlayAsync(ctx, "Volume_Max") }
	}
	// Open the speaker, then play Harman's own startup chime from the
	// installed rootfs.
	//
	// Both wait for the DSP to accept a volume, and they are in one sequence
	// so the order is fixed rather than a race between two goroutines woken
	// by the same signal.
	//
	// The outputs are opened here because the donor's initialisation leaves
	// them muted and its system-manager, which this runtime replaces with
	// init, is what called muteampcontrol afterwards. Opening them during
	// initialisation instead put the amplifier live for DSP bootup and for
	// the first gain change, and both were audible on this unit as pops.
	//
	// The chime waits for the same signal because the DSP sits between the
	// DAC and the speaker. Rendered earlier it plays through whatever gain
	// the DSP powered up with: the renderer exits cleanly, the log says the
	// cue played, and nothing is audible.
	if media != nil {
		go func() {
			if !media.WaitApplied(ctx, dspReadyTimeout) {
				// Open them anyway. A speaker that is silent with no way back
				// except a WAMP call is worse than one pop nobody is there to
				// hear, and the chime is skipped because there is no DSP to
				// carry it.
				log.Print("DSP did not accept a volume; opening outputs anyway")
				if err := control.OpenOutputs(); err != nil {
					log.Printf("open outputs: %v", err)
				}
				log.Print("CUE_SKIPPED Power_On: DSP did not accept a volume")
				return
			}
			if err := control.OpenOutputs(); err != nil {
				log.Printf("open outputs: %v", err)
			}
			cues.PlayAsync(ctx, "Power_On")
		}()
	} else {
		if err := control.OpenOutputs(); err != nil {
			log.Printf("open outputs: %v", err)
		}
		cues.PlayAsync(ctx, "Power_On")
	}

	if gpioSource != nil {
		// Replies to our own requests arrive on the button channel. Without
		// this they are counted as undecodable frames and logged as faults.
		gpioSource.frameObserver = appearance.OfferFrame
	}
	// The vendor's own startup appearance: LED_INTENSITY 50 and LED_RGB 000000
	// from caldata/FENV.bin. These opcodes were held back until they had been
	// sent to this unit's microcontroller and watched; that has happened, so
	// they are applied at startup as the vendor did.
	if *applyAppearanceDefaults {
		appearance.ApplyDefaults()
	}
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
		// The front lamp reports network state, which the donor drove from
		// audio-ui. Nothing drove it here, so it sat at whatever the
		// microcontroller lit at power-up and read as online regardless.
		frontWatcher := newNetworkStateWatcher(indicatorLEDs, log.Printf)
		go frontWatcher.Run(ctx)
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
		micMute:        micMute,
		appearance:     appearance,
		bluetoothState: *bluetoothState,
		playbackStatus: *playbackStatus,
		logf:           log.Printf,
	}
	log.Print("hardware initialized; outputs muted until the DSP is ready")
	heartbeatDone := make(chan error, 1)
	go func() {
		err := runMCUHeartbeat(ctx, bus, mcuHeartbeatInterval)
		if err != nil {
			cancel()
		}
		heartbeatDone <- err
	}()
	// There is no automatic speaker muting. The amplifier and DAC are opened
	// once the DAC is configured and stay open; they follow the explicit mute
	// procedures and nothing else. The policy that used to close them whenever
	// ALSA stopped, and only reopen them for an approved renderer, was this
	// project's invention. It made the speaker silent for every sound the
	// runtime did not itself play, including the vendor's own startup chime.
	playbackDone := make(chan error, 1)
	playbackDone <- nil
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
	<-micMuteDone
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
