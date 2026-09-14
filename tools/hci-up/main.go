// Command reinvoke-hci-up brings a Bluetooth controller out of the DOWN state.
//
// Why this exists
//
// The kernel registers hci0 and downloads controller firmware on its own, but
// an HCI adapter is only opened when userspace asks. On a stock system that is
// bluetoothd's job. This runtime replaced BlueZ with the donor's Bluedroid
// stack, and nothing in that stack issues HCIDEVUP, so the adapter stayed DOWN
// with hci_version 0 and a zero address on every boot.
//
// Bluedroid then called into the HAL anyway and dereferenced state the DOWN
// adapter never populated, crashing with SIGSEGV in its stack_manager thread
// roughly every five seconds. Measured on candidate 05.4: issuing HCIDEVUP
// took hci0 from version 0 / 00:00:00:00:00:00 to version 8 / manufacturer 72
// (Marvell) / aa:bb:cc:dd:ee:ff immediately.
//
// Exit status is 0 when the adapter is up, including when it already was.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"syscall"
)

const (
	afBluetooth = 31
	btprotoHCI  = 1
	// HCIDEVUP is _IOW('H', 201, int).
	hciDevUp = 0x400448C9
)

// upOne issues HCIDEVUP for one adapter index.
func upOne(index int) error {
	fd, err := syscall.Socket(afBluetooth, syscall.SOCK_RAW, btprotoHCI)
	if err != nil {
		return fmt.Errorf("open HCI control socket: %w", err)
	}
	defer syscall.Close(fd)

	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd),
		uintptr(hciDevUp), uintptr(index))
	switch errno {
	case 0:
		return nil
	case syscall.EALREADY, syscall.EINPROGRESS:
		// Already up, or another caller is mid-open. Both are success here.
		return nil
	default:
		return fmt.Errorf("HCIDEVUP hci%d: %w", index, errno)
	}
}

func attr(index int, name string) string {
	b, err := os.ReadFile(fmt.Sprintf("/sys/class/bluetooth/hci%d/%s", index, name))
	if err != nil {
		return "unreadable"
	}
	return strings.TrimSpace(string(b))
}

func main() {
	index := flag.Int("index", 0, "HCI adapter index")
	quiet := flag.Bool("quiet", false, "only report failures")
	flag.Parse()

	if _, err := os.Stat(fmt.Sprintf("/sys/class/bluetooth/hci%d", *index)); err != nil {
		fmt.Fprintf(os.Stderr, "hci%d is not present; the driver has not registered it\n", *index)
		os.Exit(1)
	}

	before := attr(*index, "hci_version")
	if err := upOne(*index); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	after := attr(*index, "hci_version")

	// An adapter that is genuinely up reports a nonzero HCI version. Reporting
	// success on a zero-version adapter would hand Bluedroid the exact state
	// that makes it segfault, which is the failure this tool exists to prevent.
	if after == "0" || after == "unreadable" {
		fmt.Fprintf(os.Stderr, "hci%d still reports version %q after HCIDEVUP\n", *index, after)
		os.Exit(1)
	}
	if !*quiet {
		fmt.Printf("hci%d up: version %s -> %s, address %s\n",
			*index, before, after, attr(*index, "address"))
	}
}
