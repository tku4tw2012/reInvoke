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
	allowUnmute := flag.Bool(
		"allow-unmute",
		false,
		"allow verified WAMP unmute requests after safe initialization",
	)
	flag.Parse()

	if *routerPort < 1 || *routerPort > 65535 {
		log.Fatal("router-port must be from 1 through 65535")
	}
	if *gpioNumber < 0 {
		log.Fatal("gpio must be non-negative")
	}

	bus, err := openLinuxI2C(*i2cPath)
	if err != nil {
		log.Fatal(err)
	}
	defer bus.Close()

	control := newController(bus, mutePolicy{AllowUnmute: *allowUnmute})
	if err := control.initialize(); err != nil {
		log.Fatalf("safe hardware initialization failed: %v", err)
	}

	var source eventSource
	var gpioValue *os.File
	if *gpioRoot != "" {
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

	ctx, cancel := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer cancel()

	service := wampService{
		address:    *routerHost + ":" + strconv.Itoa(*routerPort),
		realm:      *realm,
		controller: control,
		events:     source,
		version:    recoveredMCUVersion,
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
	runErr := service.run(ctx)
	cancel()
	heartbeatErr := <-heartbeatDone
	muteErr := control.muteAll()
	if runErr != nil || heartbeatErr != nil || muteErr != nil {
		if runErr != nil {
			fmt.Fprintf(os.Stderr, "mcu-interface: %v\n", runErr)
		}
		if heartbeatErr != nil {
			fmt.Fprintf(os.Stderr, "mcu-interface: %v\n", heartbeatErr)
		}
		if muteErr != nil {
			fmt.Fprintf(os.Stderr, "mcu-interface: shutdown: %v\n", muteErr)
		}
		os.Exit(1)
	}
}
