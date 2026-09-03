// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

// reinvoke-dsp-interface is an owned replacement for the donor dsp-client.
//
// It loads a DSP boot image from a supplied path, reverses the bit order of
// every byte, streams the result to the DSP in four byte SPI transfers, and
// then serves the recovered WAMP surface: eight registered procedures, one
// subscription, and the com.harman.dsp.version publication.
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
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
)

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
	allowStateChanging := flag.Bool(
		"allow-state-changing-procedures",
		false,
		"allow DSP state-changing WAMP procedures; default is read-only",
	)
	dryRun := flag.Bool(
		"dry-run",
		false,
		"use in-memory backends and open no device node",
	)
	useWAMP := flag.Bool("wamp", true, "join the WAMP router after boot")
	readyTimeout := flag.Duration(
		"ready-timeout",
		2*time.Second,
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
	}
	if err := policy.validate(); err != nil {
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
	if err := dsp.BootContext(ctx, image); err != nil {
		_ = dsp.Close()
		log.Fatalf("boot DSP: %v", err)
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
	runErr := service.run(ctx)
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
