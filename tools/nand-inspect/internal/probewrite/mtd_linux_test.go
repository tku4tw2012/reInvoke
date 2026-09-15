//go:build !nandpilot

package probewrite

import (
	"context"
	"encoding/binary"
	"errors"
	"runtime"
	"testing"
	"unsafe"
)

func TestLinuxABI(t *testing.T) {
	if unsafe.Sizeof(mtdInfo{}) != 32 || unsafe.Offsetof(mtdInfo{}.Unused) != 24 ||
		unsafe.Sizeof(Stats{}) != 16 {
		t.Fatal("MTD ABI size mismatch")
	}
	arg, data := partitionArg(1, "reinvoke-probe-test", 42)
	expected := uintptr(24)
	pointerOffset := uintptr(16)
	if unsafe.Sizeof(uintptr(0)) == 4 {
		expected, pointerOffset = 16, 12
	}
	if unsafe.Sizeof(arg) != expected || unsafe.Offsetof(arg.Data) != pointerOffset ||
		len(data) != 152 || arg.DataLen != 152 || arg.Op != 1 {
		t.Fatal("BLKPG ABI mismatch")
	}
	if binary.LittleEndian.Uint64(data[:8]) != uint64(Start) ||
		binary.LittleEndian.Uint64(data[8:16]) != uint64(Span) ||
		binary.LittleEndian.Uint64(data[:8])+binary.LittleEndian.Uint64(data[8:16]) != uint64(End) ||
		binary.LittleEndian.Uint32(data[16:20]) != 42 ||
		string(data[20:40]) != "reinvoke-probe-test\x00" {
		t.Fatal("BLKPG payload mismatch")
	}
}

func TestBackendBounds(t *testing.T) {
	for _, test := range []struct {
		offset int64
		size   int
		align  int
	}{
		{-1, PageBytes, PageBytes}, {Span, PageBytes, PageBytes},
		{Span - PageBytes, PageBytes + 1, PageBytes},
		{1, EraseBytes, EraseBytes}, {0, 0, PageBytes},
		{int64(Blocks) * EraseBytes, EraseBytes, EraseBytes},
		{0, 83 * EraseBytes, EraseBytes}, {0, 99 * EraseBytes, EraseBytes},
		{0, 373 * EraseBytes, EraseBytes}, {0, int(DeviceBytes), EraseBytes},
	} {
		if err := bounds(test.offset, test.size, test.align); err == nil {
			t.Fatalf("invalid bounds accepted: %+v", test)
		}
	}
	if bounds(0, EraseBytes, EraseBytes) != nil ||
		bounds(Span-EraseBytes, EraseBytes, EraseBytes) != nil ||
		bounds(Span-PageBytes, PageBytes, PageBytes) != nil {
		t.Fatal("last block rejected")
	}
	m := &MTD{}
	if err := m.Erase(0); err == nil {
		t.Fatal("erase without writable partition accepted")
	}
	if _, err := m.WriteAt(make([]byte, PageBytes), 0); err == nil {
		t.Fatal("write without writable partition accepted")
	}
	if err := m.Sync(); err == nil {
		t.Fatal("sync without partition accepted")
	}
}

func TestHardwareGuardRefusesHost(t *testing.T) {
	if runtime.GOARCH == "arm" {
		t.Skip("host-only guard test")
	}
	if err := CheckRuntime(); err == nil {
		t.Fatal("host allowed hardware action")
	}
}

func TestTemporaryIndexZeroAccepted(t *testing.T) {
	index, err := createdPartitionIndex(map[int]string{1: "mv_nand", 0: "ours"}, "ours")
	if err != nil || index != 0 {
		t.Fatalf("lowest available index rejected: %d %v", index, err)
	}
	for _, entries := range []map[int]string{
		{1: "ours"}, {1: "mv_nand", 0: "ours", 2: "ours"},
	} {
		if _, err := createdPartitionIndex(entries, "ours"); err == nil {
			t.Fatal("ambiguous/master index accepted")
		}
	}
	index, err = createdPartitionIndex(map[int]string{1: "mv_nand", 128: "ours"}, "ours")
	if err == nil || index != 128 {
		t.Fatal("unique unsupported index must be retained for cleanup")
	}
	index, err = createdPartitionIndex(map[int]string{1: "mv_nand"}, "ours")
	if err != nil || index != -1 {
		t.Fatal("absent registration must not be adopted")
	}
}

func TestMappingECCModeIsExplicit(t *testing.T) {
	before := Stats{Corrected: 8, Failed: 2, BadBlocks: 2}
	after := before
	after.Failed++
	if validateMappingStats(before, after, false) == nil {
		t.Fatal("mapping error accepted outside recovery mode")
	}

	if err := validateMappingStats(before, after, true); err != nil {
		t.Fatal(err)
	}
	after.Corrected--
	if validateMappingStats(before, after, true) == nil {
		t.Fatal("recovery permitted counter reset")
	}
	after = before
	after.BadBlocks++
	if validateMappingStats(before, after, true) == nil {
		t.Fatal("recovery permitted bad-block inventory change")
	}
	if err := validateMappingStats(before, before, false); err != nil {
		t.Fatal(err)
	}
}

func TestWritesRequireProductionRuntimeAndPreflight(t *testing.T) {
	if runtime.GOARCH == "arm" {
		t.Skip("host-only runtime guard test")
	}
	for _, recovery := range []bool{false, true} {
		m := &MTD{recovery: recovery}
		if err := m.EnableWrites(context.Background(), nil, nil, nil); err == nil {
			t.Fatalf("write enablement was not rejected: %v", err)
		}
		if m.partition != nil || m.writeEnabled {
			t.Fatal("disabled write enablement touched device state")
		}
	}
}

func TestMappingKernelGateDelegatesReadOnly(t *testing.T) {
	const observed = "Linux version 3.8.13-reinvoke-audio-sd8887 (reinvoke@reinvoke) (gcc version 4.9 20140827 (prerelease) (GCC) ) #1-mtd-cleanup SMP PREEMPT Thu Jan 1 00:00:00 UTC 1970"
	if mappingKernelBanner != observed {
		t.Fatal("mapping kernel pin differs from the observed cleanup-fixed banner")
	}
	for _, test := range []struct {
		name, arch, banner string
		recovery           bool
		allowed            bool
	}{
		{"accepted", "arm", observed, false, true},
		{"old kernel", "arm", "Linux version 3.8.13-reinvoke-audio-sd8887 #1 SMP PREEMPT", false, false},
		{"release only", "arm", "3.8.13-reinvoke-audio-sd8887", false, false},
		{"suffix", "arm", observed + " modified", false, false},
		{"prefix", "arm", "modified " + observed, false, false},
		{"missing banner", "arm", "", false, false},
		{"host", "amd64", observed, false, false},
		{"arm64", "arm64", observed, false, false},
		{"recovery", "arm", observed, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			err := qualifyReadOnlyMapping(test.arch, test.banner, test.recovery, func(writable bool) error {
				calls++
				if writable {
					t.Fatal("mapping gate delegated writable creation")
				}
				return nil
			})
			if test.allowed && (err != nil || calls != 1) {
				t.Fatalf("accepted gate failed: calls=%d err=%v", calls, err)
			}
			if !test.allowed && (err == nil || calls != 0) {
				t.Fatalf("rejected gate reached creation: calls=%d err=%v", calls, err)
			}
		})
	}
	createError := errors.New("injected read-only creation failure")
	if err := qualifyReadOnlyMapping("arm", observed, false, func(bool) error { return createError }); !errors.Is(err, createError) {
		t.Fatalf("creation error was lost: %v", err)
	}
}

func TestMappingProductionRuntimeGateRefusesHost(t *testing.T) {
	if runtime.GOARCH == "arm" {
		t.Skip("host-only guard test")
	}
	m := &MTD{}
	if err := m.CheckMapping(context.Background(), nil, nil, nil); err == nil {
		t.Fatal("production mapping entry point accepted host runtime")
	}
	if m.partition != nil || m.writeEnabled {
		t.Fatal("host guard touched device state")
	}
}

func TestPartitionCreationIsAlwaysReadOnly(t *testing.T) {
	for _, test := range []struct {
		writable, enabled bool
	}{{true, false}, {false, true}} {
		m := &MTD{writeEnabled: test.enabled}
		if err := m.makePartition(context.Background(), test.writable, nil); err == nil || err.Error() != readOnlyCreationReason {
			t.Fatalf("phase A partition guard failed: %+v err=%v", test, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m := &MTD{}
	if err := m.makePartition(ctx, false, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled creation reached device access: %v", err)
	}
	m.recovery = true
	snapshot := &Snapshot{stats: &statsTracker{}, Hashes: make([]string, Blocks), device: m}
	if err := m.makePartition(context.Background(), false, snapshot); err == nil {
		t.Fatal("recovery accepted an installation preflight")
	}
}
