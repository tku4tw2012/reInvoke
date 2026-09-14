// Command reinvoke-hci-down closes a Bluetooth controller so the donor stack
// can claim it.
//
// Why this exists, and why it is the opposite of what candidate 05.5 shipped
//
// The donor's libbt-vendor.so drives the controller through the kernel's HCI
// user channel, which the kernel only grants when the adapter is DOWN. If
// anything opens the adapter first, the vendor transport cannot attach. The
// stack still starts, still logs every property write as successful, and still
// answers procedure calls, but not one HCI packet ever reaches the radio.
//
// Candidate 05.5 shipped a helper that issued HCIDEVUP before starting the
// stack, reasoning that removing BlueZ removed the only thing that opened the
// adapter. That was backwards, and it is what kept Bluetooth dark.
//
// Measured on 05.5, same unit and build, only the adapter state differing at
// launch:
//
//	adapter UP at launch    btsnoop_hci.log stopped at 44 bytes, header only
//	adapter DOWN at launch  btsnoop_hci.log grew past 13000 bytes
//
// With the adapter closed first the stack drives everything itself: it becomes
// discoverable on request, publishes its own name and extended inquiry
// response, resolves a full SDP record, pairs without a PIN, and negotiates
// A2DP at 44100 Hz stereo.
//
// Exit status is 0 when the adapter is closed, including when it already was.
package main

import (
	"flag"
	"fmt"
	"os"
	"syscall"
	"time"
	"unsafe"
)

const (
	afBluetooth = 31
	btprotoHCI  = 1
	// HCIDEVDOWN is _IOW('H', 202, int).
	hciDevDown = 0x400448CA
	// HCIGETDEVINFO is _IOR('H', 211, int).
	hciGetDevInfo = 0x800448D3
)

// devInfo mirrors struct hci_dev_info. Only DevID and Flags are read, but the
// whole layout must be present so the kernel writes within bounds.
type devInfo struct {
	DevID    uint16
	Name     [8]byte
	BDAddr   [6]byte
	Flags    uint32
	Type     uint8
	Features [8]byte
	PktType  uint32
	LinkPol  uint32
	LinkMode uint32
	ACLMtu   uint16
	ACLPkts  uint16
	SCOMtu   uint16
	SCOPkts  uint16
	Stat     [10]uint32
}

func closeAdapter(index int) error {
	fd, err := syscall.Socket(afBluetooth, syscall.SOCK_RAW, btprotoHCI)
	if err != nil {
		return fmt.Errorf("open HCI control socket: %w", err)
	}
	defer syscall.Close(fd)

	// A stack that just exited can still hold the user channel for a moment,
	// which is ordinary on a supervisor restart rather than a fault.
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd),
			uintptr(hciDevDown), uintptr(index))
		switch errno {
		case 0, syscall.EALREADY:
			return nil
		case syscall.EBUSY:
			if time.Now().After(deadline) {
				return fmt.Errorf(
					"hci%d is held by another process; it cannot be released", index)
			}
			time.Sleep(200 * time.Millisecond)
		default:
			return fmt.Errorf("HCIDEVDOWN hci%d: %w", index, errno)
		}
	}
}

// stillOpen reports whether the kernel continues to hold the adapter. The
// donor transport cannot attach until this is false.
//
// This asks the kernel through HCIGETDEVINFO rather than reading sysfs. There
// is no flags attribute on this kernel, and hci_version keeps its last value
// after the adapter closes, so a sysfs check reports an adapter that is long
// closed as still open.
func stillOpen(index int) (bool, error) {
	fd, err := syscall.Socket(afBluetooth, syscall.SOCK_RAW, btprotoHCI)
	if err != nil {
		return false, fmt.Errorf("open HCI control socket: %w", err)
	}
	defer syscall.Close(fd)

	var info devInfo
	info.DevID = uint16(index)
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd),
		uintptr(hciGetDevInfo), uintptr(unsafe.Pointer(&info)))
	if errno != 0 {
		return false, fmt.Errorf("HCIGETDEVINFO hci%d: %w", index, errno)
	}
	// Bit 0 is HCI_UP. Unlike the scan bits, which the kernel does not track
	// for commands issued on a raw socket, this one is reliable.
	return info.Flags&1 != 0, nil
}

func main() {
	index := flag.Int("index", 0, "HCI adapter index")
	settle := flag.Duration("settle", 300*time.Millisecond,
		"pause after closing so the kernel releases the adapter")
	quiet := flag.Bool("quiet", false, "only report failures")
	flag.Parse()

	if _, err := os.Stat(fmt.Sprintf("/sys/class/bluetooth/hci%d", *index)); err != nil {
		fmt.Fprintf(os.Stderr,
			"hci%d is not present; the driver has not registered it\n", *index)
		os.Exit(1)
	}

	if err := closeAdapter(*index); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	time.Sleep(*settle)

	// Report the observed state rather than the ioctl's return value. A stack
	// launched against an adapter that is still open fails silently, which is
	// the exact failure this command exists to prevent.
	open, err := stillOpen(*index)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot confirm hci%d is closed: %v\n", *index, err)
		os.Exit(1)
	}
	if open {
		fmt.Fprintf(os.Stderr,
			"hci%d is still open; the donor transport cannot attach\n", *index)
		os.Exit(1)
	}
	if !*quiet {
		fmt.Printf("hci%d closed; the donor stack can claim the user channel\n", *index)
	}
}
