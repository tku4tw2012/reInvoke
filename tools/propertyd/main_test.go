// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/binary"
	"path/filepath"
	"testing"
)

// donorFind reproduces __system_property_find from the donor's
// lib/libglibc_bridge.so rather than calling this package's own lookup: a
// writer that agrees with its own reader would pass while still being
// unreadable by the stack we have to satisfy.
//
// From the disassembly: the count is at +0; the table starts at +32 and each
// entry packs the name length in the top byte and the record offset in the
// low three; the record holds the name at +0, a serial at +32 whose top byte
// is the value length, and the value at +36.
func donorFind(area []byte, name string) (string, bool) {
	count := binary.LittleEndian.Uint32(area[0:])
	for i := uint32(0); i < count; i++ {
		entry := binary.LittleEndian.Uint32(area[32+i*4:])
		nameLen := entry >> 24
		offset := entry & 0x00FFFFFF
		if int(nameLen) != len(name) {
			continue
		}
		if string(area[offset:offset+nameLen]) != name {
			continue
		}
		serial := binary.LittleEndian.Uint32(area[offset+32:])
		if serial&1 != 0 {
			return "", false // dirty; a real reader would retry
		}
		valueLen := serial >> 24
		return string(area[offset+36 : offset+36+valueLen]), true
	}
	return "", false
}

func newTestArea(t *testing.T) *area {
	t.Helper()
	store, _, err := createArea(filepath.Join(t.TempDir(), "__properties__"))
	if err != nil {
		t.Fatalf("createArea: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestHeaderMatchesDonorContract(t *testing.T) {
	store := newTestArea(t)

	if got := binary.LittleEndian.Uint32(store.data[magicOffset:]); got != areaMagic {
		t.Errorf("magic = %#x, want %#x", got, areaMagic)
	}
	// Guard the value the donor actually checks. Upstream bionic uses
	// 0xFC6ED0AB; using it here would make every reader silently unmap.
	if got := binary.LittleEndian.Uint32(store.data[versionOffset:]); got != 0x45434F76 {
		t.Errorf("version = %#x, want 0x45434F76 (donor specific)", got)
	}
	if len(store.data) != areaSize {
		t.Errorf("area size = %d, want %d", len(store.data), areaSize)
	}
}

func TestDonorReaderSeesWrittenProperties(t *testing.T) {
	store := newTestArea(t)

	// The property the whole A2DP path is blocked on.
	if err := store.Set("service.servicemanager", "1"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := store.Set("ro.build.id", "reinvoke"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	for _, want := range []struct{ name, value string }{
		{"service.servicemanager", "1"},
		{"ro.build.id", "reinvoke"},
	} {
		got, ok := donorFind(store.data, want.name)
		if !ok {
			t.Fatalf("donor reader could not find %s", want.name)
		}
		if got != want.value {
			t.Errorf("%s = %q, want %q", want.name, got, want.value)
		}
	}

	if _, ok := donorFind(store.data, "absent.property"); ok {
		t.Error("donor reader found a property that was never set")
	}
}

func TestUpdateKeepsSingleRecord(t *testing.T) {
	store := newTestArea(t)

	if err := store.Set("service.servicemanager", ""); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := store.Set("service.servicemanager", "1"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if count := store.count(); count != 1 {
		t.Errorf("count = %d, want 1 after updating one property", count)
	}
	got, ok := donorFind(store.data, "service.servicemanager")
	if !ok || got != "1" {
		t.Errorf("after update got %q (found=%v), want \"1\"", got, ok)
	}
}

// An over-long value must be refused rather than truncated into the next
// record, which would corrupt the neighbouring property rather than fail.
func TestOversizeInputsRejected(t *testing.T) {
	store := newTestArea(t)

	long := make([]byte, valueMax+1)
	for i := range long {
		long[i] = 'x'
	}
	if err := store.Set("a.name", string(long)); err == nil {
		t.Error("expected an oversize value to be rejected")
	}

	longName := make([]byte, nameMax+1)
	for i := range longName {
		longName[i] = 'n'
	}
	if err := store.Set(string(longName), "v"); err == nil {
		t.Error("expected an oversize name to be rejected")
	}
	if err := store.Set("", "v"); err == nil {
		t.Error("expected an empty name to be rejected")
	}
}

func TestSetMessageLayoutMatchesDonorSetter(t *testing.T) {
	// __system_property_set builds cmd(4) + name[32] + value[92] and writes it
	// as one message; a mismatch here would silently set the wrong key.
	if messageSize != 128 {
		t.Fatalf("messageSize = %d, want 128", messageSize)
	}
	message := make([]byte, messageSize)
	binary.LittleEndian.PutUint32(message[:4], setCommand)
	copy(message[4:4+nameMax], "service.servicemanager")
	copy(message[4+nameMax:], "1")

	if name := trimNUL(message[4 : 4+nameMax]); name != "service.servicemanager" {
		t.Errorf("decoded name = %q", name)
	}
	if value := trimNUL(message[4+nameMax:]); value != "1" {
		t.Errorf("decoded value = %q", value)
	}
}

// A supervised restart of this daemon must not erase the live table. The
// consumers keep their own mapping of the same pages and are not restarted
// with it, so wiping service.servicemanager here would block the donor stack
// exactly as a missing property service did.
func TestRestartAdoptsTheExistingTable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "__properties__")

	first, adopted, err := createArea(path)
	if err != nil {
		t.Fatalf("createArea: %v", err)
	}
	if adopted {
		t.Error("a fresh area should not report an adopted table")
	}
	if err := first.Set("service.servicemanager", "1"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	second, adopted, err := createArea(path)
	if err != nil {
		t.Fatalf("createArea after restart: %v", err)
	}
	defer second.Close()
	if !adopted {
		t.Fatal("restart did not adopt the existing table")
	}
	if got, ok := donorFind(second.data, "service.servicemanager"); !ok || got != "1" {
		t.Errorf("after restart got %q (found=%v), want \"1\"", got, ok)
	}
}

// Two updates of the same length must not leave identical clean serials, or a
// reader that samples, copies and re-samples can accept a torn value.
func TestSameLengthUpdatesAdvanceTheSerial(t *testing.T) {
	store := newTestArea(t)
	if err := store.Set("ro.build.id", "aaa"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	offset, ok := store.find("ro.build.id")
	if !ok {
		t.Fatal("property missing")
	}
	before := binary.LittleEndian.Uint32(store.data[offset+recordSerialOffset:])
	if before&1 != 0 {
		t.Error("a settled serial must be clean")
	}

	if err := store.Set("ro.build.id", "bbb"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	after := binary.LittleEndian.Uint32(store.data[offset+recordSerialOffset:])
	if after&1 != 0 {
		t.Error("a settled serial must be clean")
	}
	if after == before {
		t.Errorf("serial unchanged across a same-length update: %#x", after)
	}
	if got, ok := donorFind(store.data, "ro.build.id"); !ok || got != "bbb" {
		t.Errorf("got %q (found=%v), want \"bbb\"", got, ok)
	}
}
