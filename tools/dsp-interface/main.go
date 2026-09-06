// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

// reinvoke-dsp-interface is an owned replacement for the donor dsp-client.
//
// It loads a DSP boot image from a supplied path, reverses the bit order of
// every byte, streams the result to the DSP in four byte SPI transfers, and
// then serves seven recovered WAMP procedures, a root-only microphone-control
// socket, one subscription, and the com.harman.dsp.version publication.
//
// Two donor behaviours are deliberately absent. It writes no persistent
// storage, so there is no memory dump file and no crash dump directory, and it
// never calls the amplifier or DAC mute procedures, so a DSP boot cannot
// unmute the speakers behind the back of the service that owns that policy.
//
// The recovered contract is documented in docs/emulation/dsp-boundary.md.
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
	"syscall"
	"time"
)

const dspBootSettleDelay = time.Second

func waitForDSPSettle(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func main() {
	imagePath := flag.String("image", "", "path to dsp-img.ldr (required)")
	allowUnverified := flag.Bool(
		"allow-unverified-image",
		false,
		"accept an image that is not the recovered dsp-img.ldr",
	)
	spiPath := flag.String("spi", "/dev/spidev0.0", "SPI device node")
	i2cPath := flag.String("i2c", "/dev/i2c-0", "I2C bus carrying the expander")
	gpioRoot := flag.String("gpio-root", "/sys/class/gpio", "GPIO sysfs root")
	devmemPath := flag.String(
		"devmem",
		"/bin/busybox",
		"BusyBox path used to restore the DSP message pinmux",
	)
	routerHost := flag.String("router-host", "127.0.0.1", "WAMP router host")
	routerPort := flag.Int("router-port", 9999, "WAMP RawSocket port")
	realm := flag.String("realm", "default", "WAMP realm")
	bootupTopic := flag.String(
		"bootup-topic",
		"com.harman.dsp.bootup",
		"topic published when the DSP reports EVENT_DSP_BOOTUP; empty disables",
	)
	mutePolicyProcedure := flag.String(
		"mute-policy-procedure",
		"",
		"procedure of the service that owns the amplifier and DAC mute policy",
	)
	bootStatePath := flag.String(
		"boot-state",
		"",
		"RAM marker created after EVENT_DSP_BOOTUP; empty disables",
	)
	allowStateChanging := flag.Bool(
		"allow-state-changing-procedures",
		false,
		"allow unverified DSP command procedures; default transmits no command",
	)
	microphoneState := flag.String(
		"microphone-state",
		"/run/reinvoke/microphone-state",
		"RAM state used to restore microphone privacy during DSP boot",
	)
	microphoneControlSocket := flag.String(
		"mic-control-socket",
		"/run/reinvoke/dsp-mic-control.sock",
		"root-only socket used by the MCU privacy owner",
	)
	dryRun := flag.Bool(
		"dry-run",
		false,
		"use in-memory backends and open no device node",
	)
	useWAMP := flag.Bool("wamp", true, "join the WAMP router after boot")
	readyTimeout := flag.Duration(
		"ready-timeout",
		500*time.Millisecond,
		"how long to wait for the DSP ready line before failing a transmit",
	)
	flag.Parse()

	if *imagePath == "" {
		log.Fatal("-image is required")
	}
	if *routerPort < 1 || *routerPort > 65535 {
		log.Fatal("router-port must be from 1 through 65535")
	}
	policy := mutePolicy{
		NotifyTopic:     *bootupTopic,
		PolicyProcedure: *mutePolicyProcedure,
		BootStatePath:   *bootStatePath,
	}
	if err := policy.validate(); err != nil {
		log.Fatal(err)
	}
	if err := clearDSPBootState(*bootStatePath); err != nil {
		log.Fatal(err)
	}
	image, err := loadBootImage(*imagePath)
	if err != nil {
		log.Fatal(err)
	}
	if err := verifyBootImage(image); err != nil {
		if !*allowUnverified {
			log.Fatalf("%v; pass -allow-unverified-image to accept it", err)
		}
		log.Printf("WARNING: %v", err)
	}
	log.Printf(
		"loaded %s: %d bytes, %d four byte transfers, sha256 %s, "+
			"bit reversed sha256 %s",
		*imagePath,
		len(image.Stream),
		image.Transfers(),
		image.SourceSHA256,
		image.StreamSHA256,
	)
	ctx, cancel := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer cancel()

	spi, gpio, i2c, err := openBackends(*dryRun, *spiPath, *gpioRoot, *i2cPath)
	if err != nil {
		log.Fatal(err)
	}

	dsp := newLink(spi, gpio, i2c, linkOptions{
		Pins:         defaultPinout(),
		ReadyTimeout: *readyTimeout,
	})
	if !*dryRun {
		if _, err := configureDSPPinmux(
			ctx,
			*devmemPath,
			dspPinmuxLockPath,
			false,
			nil,
		); err != nil {
			_ = dsp.Close()
			// The download-mode write may already have landed before the
			// verification read failed, so try to leave the shared register in
			// message mode rather than exiting with GPIO5 still switched away.
			restoreCtx, cancelRestore := detachedPinmuxContext()
			_, restoreErr := configureDSPPinmux(
				restoreCtx,
				*devmemPath,
				dspPinmuxLockPath,
				true,
				nil,
			)
			cancelRestore()
			if restoreErr != nil {
				log.Fatalf(
					"select DSP download pinmux: %v; restore message pinmux: %v",
					err,
					restoreErr,
				)
			}
			log.Fatalf("select DSP download pinmux: %v", err)
		}
	}
	bootErr := dsp.BootContext(ctx, image)
	var pinmuxErr error
	if !*dryRun {
		var value uint32
		// Deliberately detached from ctx. A signal during download cancels ctx
		// and fails the boot, and reusing it here would make exec.CommandContext
		// refuse to run, stranding GPIO5 in download mode for the rest of the
		// boot with no supervisor restart to repair it.
		restoreCtx, cancelRestore := detachedPinmuxContext()
		value, pinmuxErr = configureDSPPinmux(
			restoreCtx,
			*devmemPath,
			dspPinmuxLockPath,
			true,
			nil,
		)
		cancelRestore()
		if pinmuxErr == nil {
			log.Printf("restored DSP message pinmux 0x%08X", value)
		}
	}
	if bootErr != nil {
		_ = dsp.Close()
		if pinmuxErr != nil {
			log.Fatalf(
				"boot DSP: %v; restore message pinmux: %v",
				bootErr,
				pinmuxErr,
			)
		}
		log.Fatalf("boot DSP: %v", bootErr)
	}
	if pinmuxErr != nil {
		_ = dsp.Close()
		log.Fatalf("restore DSP message pinmux: %v", pinmuxErr)
	}
	stats := dsp.Stats()
	log.Printf(
		"downloaded image in %d transfers of %d bytes",
		stats.DownloadTransfers,
		stats.DownloadBytes,
	)
	if memory, ok := spi.(*memorySPI); ok {
		transfers, bytes, chunked, serial := memory.Counters()
		log.Printf(
			"dry run: %d transfers, %d bytes, %d four byte, %d single byte, "+
				"transmitted sha256 %s",
			transfers,
			bytes,
			chunked,
			serial,
			memory.StreamSHA256(),
		)
	}

	if !*useWAMP {
		if err := dsp.Close(); err != nil {
			log.Fatal(err)
		}
		return
	}

	service := &wampService{
		address:  *routerHost + ":" + strconv.Itoa(*routerPort),
		realm:    *realm,
		link:     dsp,
		policy:   policy,
		safeOnly: !*allowStateChanging,
		logf:     log.Printf,
	}
	runtimeContext, stopRuntime := context.WithCancel(ctx)
	device := make(chan frame, 8)
	pumpDone := make(chan error, 1)
	serviceReady := make(chan struct{})
	bootEvents := make(chan struct{}, 1)
	service.device = device
	service.ready = serviceReady
	service.bootEvents = bootEvents
	go func() {
		defer close(device)
		pumpDone <- service.pump(runtimeContext, device)
	}()
	wampDone := make(chan error, 1)
	go func() {
		wampDone <- runWithReconnect(
			runtimeContext,
			wampReconnectDelay,
			service.run,
			func(err error) bool {
				return !errors.Is(err, errDSPLinkFailure)
			},
			log.Printf,
		)
	}()
	bootTimer := time.NewTimer(5 * time.Second)
	select {
	case <-bootEvents:
	case pumpErr := <-pumpDone:
		stopRuntime()
		<-wampDone
		_ = dsp.Close()
		log.Fatalf("wait for DSP boot event: %v", pumpErr)
	case wampErr := <-wampDone:
		stopRuntime()
		<-pumpDone
		_ = dsp.Close()
		log.Fatalf("WAMP stopped before DSP boot event: %v", wampErr)
	case <-bootTimer.C:
		stopRuntime()
		<-wampDone
		<-pumpDone
		_ = dsp.Close()
		log.Fatal("timed out waiting for DSP boot event")
	case <-ctx.Done():
		bootTimer.Stop()
		stopRuntime()
		<-wampDone
		<-pumpDone
		_ = dsp.Close()
		return
	}
	bootTimer.Stop()
	if !waitForDSPSettle(ctx, dspBootSettleDelay) {
		stopRuntime()
		<-wampDone
		<-pumpDone
		_ = dsp.Close()
		return
	}
	micControlDone := make(chan error, 1)
	micControlReady := make(chan struct{})
	service.micControlMu.Lock()
	go func() {
		micControlDone <- runMicControlServer(
			runtimeContext,
			*microphoneControlSocket,
			service,
			micControlReady,
		)
	}()
	select {
	case <-micControlReady:
	case micControlErr := <-micControlDone:
		service.micControlMu.Unlock()
		stopRuntime()
		<-wampDone
		<-pumpDone
		_ = dsp.Close()
		log.Fatalf("start DSP microphone control: %v", micControlErr)
	case pumpErr := <-pumpDone:
		service.micControlMu.Unlock()
		stopRuntime()
		<-wampDone
		<-micControlDone
		_ = dsp.Close()
		log.Fatalf("start DSP microphone control: %v", pumpErr)
	case wampErr := <-wampDone:
		service.micControlMu.Unlock()
		stopRuntime()
		<-pumpDone
		<-micControlDone
		_ = dsp.Close()
		log.Fatalf("WAMP stopped before DSP readiness: %v", wampErr)
	case <-ctx.Done():
		service.micControlMu.Unlock()
		stopRuntime()
		<-wampDone
		<-micControlDone
		<-pumpDone
		_ = dsp.Close()
		return
	}
	microphoneMuted, err := microphoneMuteRequired(*microphoneState)
	if err == nil && microphoneMuted {
		err = service.dispatchLink(
			runtimeContext,
			micMuteSpec,
			[]interface{}{uint64(1)},
		)
	}
	if err == nil {
		close(serviceReady)
	}
	service.micControlMu.Unlock()
	if err != nil {
		stopRuntime()
		<-wampDone
		<-micControlDone
		<-pumpDone
		_ = dsp.Close()
		log.Fatalf("restore microphone state after DSP boot: %v", err)
	}
	if microphoneMuted {
		log.Printf("restored microphone mute before DSP service readiness")
	}

	var runErr error
	wampFinished := false
	micControlFinished := false
	pumpFinished := false
	select {
	case runErr = <-wampDone:
		wampFinished = true
	case runErr = <-micControlDone:
		micControlFinished = true
	case pumpErr := <-pumpDone:
		pumpFinished = true
		if pumpErr != nil {
			runErr = fmt.Errorf("%w: %v", errDSPLinkFailure, pumpErr)
		} else if ctx.Err() == nil {
			runErr = fmt.Errorf("%w: message pump stopped", errDSPLinkFailure)
		}
	case <-ctx.Done():
	}
	stopRuntime()
	if !wampFinished {
		if err := <-wampDone; runErr == nil {
			runErr = err
		}
	}
	if !micControlFinished {
		if err := <-micControlDone; runErr == nil {
			runErr = err
		}
	}
	if !pumpFinished {
		if err := <-pumpDone; runErr == nil {
			runErr = err
		}
	}
	closeErr := dsp.Close()
	if runErr != nil || closeErr != nil {
		if runErr != nil {
			fmt.Fprintf(os.Stderr, "dsp-interface: %v\n", runErr)
		}
		if closeErr != nil {
			fmt.Fprintf(os.Stderr, "dsp-interface: shutdown: %v\n", closeErr)
		}
		os.Exit(1)
	}
}

// openBackends returns either the real device nodes or the in-memory
// backends, which is what makes the whole link testable on a build host.
func openBackends(
	dryRun bool,
	spiPath, gpioRoot, i2cPath string,
) (spiBus, gpioLines, i2cBus, error) {
	if dryRun {
		return newMemorySPI(), newMemoryGPIO(), newMemoryI2C(), nil
	}
	spi, err := openLinuxSPI(spiPath)
	if err != nil {
		return nil, nil, nil, err
	}
	i2c, err := openLinuxI2C(i2cPath)
	if err != nil {
		_ = spi.Close()
		return nil, nil, nil, err
	}
	return spi, newSysfsGPIO(gpioRoot), i2c, nil
}
