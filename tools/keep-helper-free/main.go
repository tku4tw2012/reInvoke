// Keep the USB boot helper able to see the iROM window.
//
// Why this is needed now and was not before
//
// While NAND held no bootable image the ROM fell back to USB boot and stayed
// there, so a camped helper saw subclass 0xFF on its first look every time. Of
// 27 archived flashes, 23 seized in three to four seconds with no misses.
//
// With a working image on NAND the ROM offers USB for roughly two seconds and
// then hands off to the NAND loader. Measured on candidate 05.6: windows of
// 1.8 and 3.5 seconds. The helper is the same speed; the target stopped
// standing still.
//
// The helper cannot see that window while it is busy. On an ordinary power-on
// the device answers at 0xFE, the helper commits to Phase 2, serves 08_IMAGE
// and waits on a stage that never completes. This releases it from that state
// so it returns to detection within milliseconds.
//
// Two mistakes are deliberately designed out, both of which cost real attempts:
//
//   - it matches on vendor and product across every port rather than a fixed
//     path. An earlier revision watched 3-1.2 and reported that the window
//     never appeared, while the device was enumerating on 2-1.2 the whole time.
//
//   - once 0xFF is seen it stops touching the helper entirely. After Phase 1
//     the device returns to 0xFE for the rest of the boot chain, which is the
//     flash running. Treating that as a stuck helper destroyed eight live
//     sessions in a row.
//
// It never opens the device, so it cannot compete for interface 0.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	vendor  = "1286"
	product = "8174"
)

// subclass returns the current interface subclass of the boot device, or "" if
// the device is not attached.
func subclass() (string, string) {
	entries, err := filepath.Glob("/sys/bus/usb/devices/*/idVendor")
	if err != nil {
		return "", ""
	}
	for _, entry := range entries {
		v, err := os.ReadFile(entry)
		if err != nil || strings.TrimSpace(string(v)) != vendor {
			continue
		}
		dir := filepath.Dir(entry)
		p, err := os.ReadFile(filepath.Join(dir, "idProduct"))
		if err != nil || strings.TrimSpace(string(p)) != product {
			continue
		}
		interfaces, _ := filepath.Glob(dir + ":*/bInterfaceSubClass")
		for _, iface := range interfaces {
			s, err := os.ReadFile(iface)
			if err == nil {
				return strings.ToLower(strings.TrimSpace(string(s))), filepath.Base(dir)
			}
		}
		return "?", filepath.Base(dir)
	}
	return "", ""
}

func helperPID() int {
	entries, _ := filepath.Glob("/proc/[0-9]*/comm")
	for _, entry := range entries {
		name, err := os.ReadFile(entry)
		if err != nil || strings.TrimSpace(string(name)) != "usb_boot_arm" {
			continue
		}
		pid, err := strconv.Atoi(filepath.Base(filepath.Dir(entry)))
		if err == nil {
			return pid
		}
	}
	return 0
}

func main() {
	log, err := os.OpenFile(
		"/home/tku/harman-kardon/reinvoke-archive/evidence/native056-flash-20260914/kick.log",
		os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		fmt.Fprintln(os.Stderr, "log:", err)
		os.Exit(1)
	}
	defer log.Close()
	say := func(format string, args ...interface{}) {
		fmt.Fprintf(log, "%s %s\n", time.Now().Format("15:04:05.000"),
			fmt.Sprintf(format, args...))
		log.Sync()
	}
	say("polling at 500 Hz for %s:%s (passive)", vendor, product)

	var since, handsOff time.Time
	last := "init"
	for {
		now, port := subclass()
		if now != last {
			switch now {
			case "":
				say("device gone")
			case "ff":
				say("%s subclass ff   <<<<< iROM WINDOW, hands off from here", port)
				handsOff = time.Now()
			default:
				say("%s subclass %s", port, now)
			}
			last = now
			since = time.Now()
		}
		// Once the window has been caught the device returns to 0xFE for the
		// rest of the boot chain, which is the flash running and must never be
		// interrupted. Releasing the helper there killed a live session.
		if !handsOff.IsZero() && time.Since(handsOff) < 10*time.Minute {
			time.Sleep(2 * time.Millisecond)
			continue
		}

		// A helper that has sat on a 0xFE device for a moment has committed to
		// Phase 2 and cannot see the window. Free it. Never touch it at 0xFF:
		// that is the session we want.
		if now != "" && now != "ff" && time.Since(since) > 1200*time.Millisecond {
			if pid := helperPID(); pid > 0 {
				syscall.Kill(pid, syscall.SIGTERM)
				say("released helper %d from a 0x%s device", pid, now)
				since = time.Now()
			}
		}
		time.Sleep(2 * time.Millisecond)
	}
}
