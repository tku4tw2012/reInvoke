// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

// Command reinvoke-propertyd provides the Android property area and the
// property_service socket that the donor stack expects.
//
// Why this exists: the donor Bluedroid stack calls defaultServiceManager()
// from its A2DP connection_state_cb. The donor's libbinder spins there until
// the property "service.servicemanager" reads back as set, and servicemanager
// only sets it once it can reach a property service. Without one, the callback
// never returns, the BTIF thread stays blocked inside it, the A2DP state
// machine never leaves "opening", and every decoded audio packet is discarded.
// Observed on hardware: the callback and the first "Waiting for initialization"
// share a millisecond, thread 1171 then spins 1257 times and emits no further
// btif_av event, while 707 SBC packets arrive and are dropped.
//
// The layout below is not the published bionic one. It was recovered from the
// donor's own lib/libglibc_bridge.so, which mmaps the area read-only from an
// inherited descriptor and rejects it unless the magic at +8 is "PROP" and the
// version at +12 is 0x45434F76. Upstream bionic uses 0xFC6ED0AB there, so an
// implementation written from the public spec is unmapped without a diagnostic.
package main

import (
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

const (
	// areaSize is the mapping the donor loader asks for; the descriptor and
	// this length are handed to readers through ANDROID_PROPERTY_WORKSPACE.
	areaSize = 32768
	// infoStart leaves room for the table of contents between the header and
	// the first record, matching the donor's index arithmetic.
	infoStart = 1024
	infoSize  = 128

	countOffset   = 0
	serialOffset  = 4
	magicOffset   = 8
	versionOffset = 12
	tocOffset     = 32

	areaMagic = 0x504F5250 // "PROP"
	// areaVersion is donor specific; see the package comment.
	areaVersion = 0x45434F76

	nameMax  = 32
	valueMax = 92

	// A record is name[32], serial, value[92]. The serial carries the value
	// length in its top byte and a dirty flag in bit 0, so a reader that
	// samples it twice can tell it read a torn value.
	recordSerialOffset = 32
	recordValueOffset  = 36

	maxRecords = (areaSize - infoStart) / infoSize

	// setCommand is the only message the donor's __system_property_set sends.
	setCommand  = 1
	messageSize = 4 + nameMax + valueMax
)

type area struct {
	mu   sync.Mutex
	data []byte
	file *os.File
}

func createArea(path string) (*area, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create area directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open property area: %w", err)
	}
	if err := file.Truncate(areaSize); err != nil {
		file.Close()
		return nil, fmt.Errorf("size property area: %w", err)
	}
	data, err := syscall.Mmap(
		int(file.Fd()),
		0,
		areaSize,
		syscall.PROT_READ|syscall.PROT_WRITE,
		syscall.MAP_SHARED,
	)
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("map property area: %w", err)
	}
	a := &area{data: data, file: file}
	a.format()
	return a, nil
}

// format rewrites the header. Readers validate magic and version before they
// trust anything else, so those are written last.
func (a *area) format() {
	for i := range a.data {
		a.data[i] = 0
	}
	binary.LittleEndian.PutUint32(a.data[countOffset:], 0)
	binary.LittleEndian.PutUint32(a.data[serialOffset:], 0)
	binary.LittleEndian.PutUint32(a.data[magicOffset:], areaMagic)
	binary.LittleEndian.PutUint32(a.data[versionOffset:], areaVersion)
}

func (a *area) count() uint32 {
	return binary.LittleEndian.Uint32(a.data[countOffset:])
}

func (a *area) recordOffset(index uint32) uint32 {
	entry := binary.LittleEndian.Uint32(a.data[tocOffset+index*4:])
	return entry & 0x00FFFFFF
}

func (a *area) recordName(offset uint32) string {
	record := a.data[offset : offset+infoSize]
	for i := 0; i < nameMax; i++ {
		if record[i] == 0 {
			return string(record[:i])
		}
	}
	return string(record[:nameMax])
}

func (a *area) find(name string) (uint32, bool) {
	for i := uint32(0); i < a.count(); i++ {
		offset := a.recordOffset(i)
		if a.recordName(offset) == name {
			return offset, true
		}
	}
	return 0, false
}

// writeValue publishes a value with the dirty flag raised across the copy so a
// concurrent reader can detect the tear and retry, which is what the donor's
// __system_property_read does with the same field.
func (a *area) writeValue(offset uint32, value string) {
	record := a.data[offset : offset+infoSize]
	serial := binary.LittleEndian.Uint32(record[recordSerialOffset:])
	binary.LittleEndian.PutUint32(record[recordSerialOffset:], serial|1)

	for i := 0; i < valueMax; i++ {
		record[recordValueOffset+i] = 0
	}
	copy(record[recordValueOffset:recordValueOffset+valueMax], value)

	binary.LittleEndian.PutUint32(
		record[recordSerialOffset:],
		uint32(len(value))<<24,
	)
	binary.LittleEndian.PutUint32(
		a.data[serialOffset:],
		binary.LittleEndian.Uint32(a.data[serialOffset:])+1,
	)
}

func (a *area) Set(name, value string) error {
	if name == "" || len(name) >= nameMax {
		return fmt.Errorf("property name must be 1..%d bytes", nameMax-1)
	}
	if len(value) >= valueMax {
		return fmt.Errorf("property value must be under %d bytes", valueMax)
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if offset, ok := a.find(name); ok {
		a.writeValue(offset, value)
		return nil
	}

	index := a.count()
	if index >= maxRecords {
		return errors.New("property area is full")
	}
	offset := uint32(infoStart + index*infoSize)
	record := a.data[offset : offset+infoSize]
	for i := range record {
		record[i] = 0
	}
	copy(record[:nameMax], name)
	a.writeValue(offset, value)

	// Publish the record before it is counted: a reader walking the table must
	// never index an entry whose contents are still being written.
	binary.LittleEndian.PutUint32(
		a.data[tocOffset+index*4:],
		uint32(len(name))<<24|offset,
	)
	binary.LittleEndian.PutUint32(a.data[countOffset:], index+1)
	return nil
}

func (a *area) Get(name string) (string, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	offset, ok := a.find(name)
	if !ok {
		return "", false
	}
	record := a.data[offset : offset+infoSize]
	length := binary.LittleEndian.Uint32(record[recordSerialOffset:]) >> 24
	if length > valueMax {
		return "", false
	}
	return string(record[recordValueOffset : recordValueOffset+length]), true
}

func (a *area) Close() error {
	err := syscall.Munmap(a.data)
	if closeErr := a.file.Close(); err == nil {
		err = closeErr
	}
	return err
}

func trimNUL(raw []byte) string {
	for i, b := range raw {
		if b == 0 {
			return string(raw[:i])
		}
	}
	return string(raw)
}

// serve answers one client. The donor's setter writes a single fixed-size
// message and then waits for the peer to hang up, so the close is the ack.
func serve(connection net.Conn, store *area, logf func(string, ...interface{})) {
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(5 * time.Second))

	message := make([]byte, messageSize)
	if _, err := io.ReadFull(connection, message); err != nil {
		logf("property request read failed: %v", err)
		return
	}
	command := binary.LittleEndian.Uint32(message[:4])
	if command != setCommand {
		logf("ignoring property command %d", command)
		return
	}
	name := trimNUL(message[4 : 4+nameMax])
	value := trimNUL(message[4+nameMax:])
	if err := store.Set(name, value); err != nil {
		logf("set %s failed: %v", name, err)
		return
	}
	logf("set %s=%q", name, value)
}

func listen(socketPath string) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o755); err != nil {
		return nil, fmt.Errorf("create socket directory: %w", err)
	}
	// A stale node from a previous boot would make Listen fail; the directory
	// is private to root, so removing it is safe.
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("clear stale socket: %w", err)
	}
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("listen on property socket: %w", err)
	}
	// The donor stack does not run as root in every configuration and opens
	// this by absolute path, so it must be writable by its clients.
	if err := os.Chmod(socketPath, 0o666); err != nil {
		listener.Close()
		return nil, fmt.Errorf("relax socket mode: %w", err)
	}
	return listener, nil
}

func main() {
	areaPath := flag.String("area", "/dev/__properties__", "property area file")
	socketPath := flag.String("socket", "/dev/socket/property_service", "property service socket")
	readyPath := flag.String("ready", "", "optional file written once the area is published")
	flag.Parse()

	log.SetFlags(0)
	log.SetPrefix("propertyd: ")

	store, err := createArea(*areaPath)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()

	listener, err := listen(*socketPath)
	if err != nil {
		log.Fatal(err)
	}
	defer listener.Close()

	if *readyPath != "" {
		if err := os.WriteFile(*readyPath, []byte(fmt.Sprintf("%d\n", areaSize)), 0o644); err != nil {
			log.Fatalf("publish ready file: %v", err)
		}
	}
	log.Printf("area %s size %d socket %s", *areaPath, areaSize, *socketPath)

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-signals
		_ = listener.Close()
	}()

	for {
		connection, err := listener.Accept()
		if err != nil {
			log.Printf("property service stopped: %v", err)
			return
		}
		go serve(connection, store, log.Printf)
	}
}
