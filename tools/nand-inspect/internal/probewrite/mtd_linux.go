package probewrite

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const (
	memGetInfo     = 0x80204d01
	memErase       = 0x40084d02
	memGetBadBlock = 0x40084d0b
	eccGetStats    = 0x80104d12
	memReadOOB64   = 0xc0184d16
	mtdFileMode    = 0x4d13
	blkpg          = 0x1269
	tmpfsMagic     = 0x01021994
	masterIndex    = 1
)

const readOnlyCreationReason = "partition creation is read-only; writable access requires separate qualification"

const mappingKernelBanner = "Linux version 3.8.13-reinvoke-audio-sd8887 (reinvoke@reinvoke) (gcc version 4.9 20140827 (prerelease) (GCC) ) #1-mtd-cleanup SMP PREEMPT Thu Jan 1 00:00:00 UTC 1970"

type mtdInfo struct {
	Type      uint8
	Padding   [3]byte
	Flags     uint32
	Size      uint32
	EraseSize uint32
	WriteSize uint32
	OOBSize   uint32
	Unused    uint64
}

type blkpgArgument struct {
	Op      int32
	Flags   int32
	DataLen int32
	Data    unsafe.Pointer
}

type MTD struct {
	master         *os.File
	partition      *os.File
	lock           *os.File
	sessionDir     string
	label          string
	createdIndex   int
	partitionAdded bool
	writeEnabled   bool
	recovery       bool
	resume         bool
}

func callIOCTL(file *os.File, request uintptr, pointer unsafe.Pointer) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, file.Fd(), request, uintptr(pointer))
	runtime.KeepAlive(pointer)
	runtime.KeepAlive(file)
	if errno != 0 {
		return errno
	}
	return nil
}

func info(file *os.File) (mtdInfo, error) {
	var value mtdInfo
	err := callIOCTL(file, memGetInfo, unsafe.Pointer(&value))
	return value, err
}

func readText(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func RequireRAMPath(path string) error {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return err
	}
	if uint64(stat.Type) != tmpfsMagic {
		return fmt.Errorf("device-side files must be on tmpfs: %s", path)
	}
	return nil
}

func CheckRuntime() error {
	if runtime.GOARCH != "arm" || os.Geteuid() != 0 {
		return fmt.Errorf("hardware operations require root on the ARM Invoke; use plan for host-only inspection")
	}
	release, err := readText("/proc/sys/kernel/osrelease")
	if err != nil || release != "3.8.13-reinvoke-audio-sd8887" {
		return fmt.Errorf("unsupported running kernel %q: %v", release, err)
	}
	cmdline, err := readText("/proc/cmdline")
	if err != nil || !strings.Contains(" "+cmdline+" ", " root=/dev/ram ") ||
		!strings.Contains(" "+cmdline+" ", " init=/init ") {
		return fmt.Errorf("not the expected RAM boot: %v", err)
	}
	meminfo, err := readText("/proc/meminfo")
	if err != nil {
		return err
	}
	var freeKB uint64
	for _, line := range strings.Split(meminfo, "\n") {
		if strings.HasPrefix(line, "MemFree:") {
			if _, err := fmt.Sscanf(line, "MemFree: %d kB", &freeKB); err != nil {
				return err
			}
		}
	}
	if freeKB < 16*1024 {
		return fmt.Errorf("less than 16 MiB genuinely free RAM after staging; do not proceed")
	}
	return nil
}

func openMTDNode(path string, minor int, writable bool) (*os.File, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	stat, ok := before.Sys().(*syscall.Stat_t)
	want := uint64((90 << 8) | minor)
	if !ok || before.Mode()&os.ModeCharDevice == 0 || before.Mode()&os.ModeSymlink != 0 || stat.Rdev != want {
		return nil, fmt.Errorf("unexpected MTD node: %s", path)
	}
	flags := os.O_RDONLY
	if writable {
		flags = os.O_RDWR
	}
	file, err := os.OpenFile(path, flags|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) {
		file.Close()
		return nil, fmt.Errorf("MTD node changed while opening")
	}
	return file, nil
}

func OpenMTD(sessionDir string, recovery bool) (*MTD, error) {
	if err := CheckRuntime(); err != nil {
		return nil, err
	}
	if err := RequireRAMPath(sessionDir); err != nil {
		return nil, err
	}
	m := &MTD{sessionDir: sessionDir, createdIndex: -1, recovery: recovery}
	fail := func(err error) (*MTD, error) {
		if closeErr := m.Close(); closeErr != nil {
			return nil, fmt.Errorf("%v; cleanup: %v", err, closeErr)
		}
		return nil, err
	}
	var err error
	m.lock, err = os.OpenFile("/run/reinvoke/nand-probe-write.lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return fail(err)
	}
	if err := syscall.Flock(int(m.lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return fail(fmt.Errorf("another writer owns the lock: %w", err))
	}
	m.master, err = openMTDNode("/dev/reinvoke-nand-ro", 3, false)
	if err != nil {
		return fail(err)
	}
	if err := m.checkTopology(); err != nil {
		return fail(err)
	}
	return m, nil
}

func CheckResumeRuntime() error {
	if !ResumeSupported {
		return fmt.Errorf("resume requires the explicit nandpilot build profile")
	}
	if err := CheckRuntime(); err != nil {
		return err
	}
	banner, err := readText("/proc/version")
	if err != nil {
		return err
	}
	return qualifyWriteKernel(runtime.GOARCH, banner, func() error { return nil })
}

func OpenResumeMTD(sessionDir string) (*MTD, error) {
	if err := CheckResumeRuntime(); err != nil {
		return nil, err
	}
	m, err := OpenMTD(sessionDir, false)
	if err != nil {
		return nil, err
	}
	m.resume = true
	return m, nil
}

var mtdLine = regexp.MustCompile(`^mtd([0-9]+): [0-9a-fA-F]+ [0-9a-fA-F]+ "([^"]+)"$`)

func mtdEntries() (map[int]string, error) {
	text, err := readText("/proc/mtd")
	if err != nil {
		return nil, err
	}
	lines := strings.Split(text, "\n")
	if len(lines) < 2 || !strings.HasPrefix(lines[0], "dev:") {
		return nil, fmt.Errorf("invalid /proc/mtd")
	}
	entries := make(map[int]string)
	for _, line := range lines[1:] {
		match := mtdLine.FindStringSubmatch(line)
		if match == nil {
			return nil, fmt.Errorf("unrecognized /proc/mtd entry %q", line)
		}
		index, err := strconv.Atoi(match[1])
		if err != nil {
			return nil, err
		}
		entries[index] = match[2]
	}
	return entries, nil
}

func (m *MTD) checkTopology() error {
	entries, err := mtdEntries()
	if err != nil {
		return err
	}
	expected := 1
	if m.createdIndex >= 0 {
		expected++
	}
	if len(entries) != expected || entries[masterIndex] != "mv_nand" ||
		(m.createdIndex >= 0 && entries[m.createdIndex] != m.label) {
		return fmt.Errorf("unexpected MTD topology; do not delete or adopt unknown partitions")
	}
	mounts, err := readText("/proc/mounts")
	if err != nil {
		return err
	}
	for _, line := range strings.Split(mounts, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			return fmt.Errorf("invalid mount table line")
		}
		if strings.Contains(fields[0], "mtd") || strings.Contains(fields[0], "ubi") ||
			fields[2] == "yaffs2" || fields[2] == "jffs2" || fields[2] == "ubifs" {
			return fmt.Errorf("NAND filesystem mounted: %s", fields[1])
		}
	}
	return noOtherMTDUsers()
}

func noOtherMTDUsers() error {
	processes, err := os.ReadDir("/proc")
	if err != nil {
		return err
	}
	self := strconv.Itoa(os.Getpid())
	for _, process := range processes {
		if process.Name() == self {
			continue
		}
		if _, err := strconv.Atoi(process.Name()); err != nil {
			continue
		}
		dir := filepath.Join("/proc", process.Name(), "fd")
		fds, err := os.ReadDir(dir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("cannot check NAND users: %w", err)
		}
		for _, fd := range fds {
			stat, err := os.Stat(filepath.Join(dir, fd.Name()))
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return fmt.Errorf("cannot inspect process descriptor: %w", err)
			}
			raw, ok := stat.Sys().(*syscall.Stat_t)
			if ok && stat.Mode()&os.ModeDevice != 0 {
				major := (raw.Rdev >> 8) & 0xfff
				if major == 90 || major == 31 {
					return fmt.Errorf("another process has an MTD descriptor: pid %s", process.Name())
				}
			}
		}
	}
	return nil
}

func (m *MTD) Geometry() (Geometry, error) {
	value, err := info(m.master)
	if err != nil {
		return Geometry{}, err
	}
	if value.Type != 4 || value.Flags != 0x400 {
		return Geometry{}, fmt.Errorf("unexpected NAND type/flags")
	}
	return Geometry{int64(value.Size), Start, Span, int(value.WriteSize), int(value.EraseSize), int(value.OOBSize)}, nil
}

func bounds(offset int64, length, alignment int) error {
	if offset < 0 || offset > Span || int64(length) > Span-offset ||
		length <= 0 || offset%int64(alignment) != 0 {
		return fmt.Errorf("operation outside fixed extent or misaligned: offset=%#x length=%d", offset, length)
	}
	return nil
}

func (m *MTD) ReadAt(data []byte, offset int64) (int, error) {
	if err := bounds(offset, len(data), PageBytes); err != nil {
		return 0, err
	}
	return m.master.ReadAt(data, Start+offset)
}

func (m *MTD) ReadOOB(offset int64) ([]byte, error) {
	if err := bounds(offset, PageBytes, PageBytes); err != nil {
		return nil, err
	}
	data := bytes.Repeat([]byte{0xa5}, VisibleOOB)
	var request [24]byte
	binary.LittleEndian.PutUint64(request[0:], uint64(Start+offset))
	binary.LittleEndian.PutUint32(request[12:], VisibleOOB)
	binary.LittleEndian.PutUint64(request[16:], uint64(uintptr(unsafe.Pointer(&data[0]))))
	err := callIOCTL(m.master, memReadOOB64, unsafe.Pointer(&request[0]))
	runtime.KeepAlive(data)
	if err != nil {
		return nil, err
	}
	if binary.LittleEndian.Uint32(request[12:]) != VisibleOOB {
		return nil, fmt.Errorf("short visible-OOB transfer")
	}
	return data, nil
}

func (m *MTD) IsBad(offset int64) (bool, error) {
	if err := bounds(offset, EraseBytes, EraseBytes); err != nil {
		return false, err
	}
	absolute := Start + offset
	result, _, errno := syscall.Syscall(syscall.SYS_IOCTL, m.master.Fd(), memGetBadBlock, uintptr(unsafe.Pointer(&absolute)))
	runtime.KeepAlive(&absolute)
	if errno != 0 {
		return false, errno
	}
	if result > 1 {
		return false, fmt.Errorf("unexpected bad-block query result %d", result)
	}
	return result == 1, nil
}

func (m *MTD) Stats() (Stats, error) {
	var stats Stats
	err := callIOCTL(m.master, eccGetStats, unsafe.Pointer(&stats))
	return stats, err
}

func (m *MTD) CheckIdentity() error {
	if err := m.checkTopology(); err != nil {
		return err
	}
	type sentinel struct {
		offset int64
		bytes  int
		hash   string
	}
	sentinels := []sentinel{{Start - EraseBytes, EraseBytes, BeforeHash}, {End, EraseBytes, AfterHash}}
	for block := 1; block < 9; block++ {
		sentinels = append(sentinels, sentinel{int64((block+1)*EraseBytes - 4096), 848, TableHash})
	}
	for _, check := range sentinels {
		before, err := m.Stats()
		if err != nil {
			return err
		}
		data := make([]byte, check.bytes)
		n, err := m.master.ReadAt(data, check.offset)
		if err != nil || n != check.bytes || digest(data) != check.hash {
			return fmt.Errorf("identity/boundary mismatch at %#x: bytes=%d error=%v", check.offset, n, err)
		}
		after, err := m.Stats()
		if err != nil {
			return err
		}
		if after.Failed != before.Failed || after.Corrected < before.Corrected ||
			after.BadBlocks != before.BadBlocks || after.BBTBlocks != before.BBTBlocks {
			return fmt.Errorf("identity/boundary ECC error at %#x", check.offset)
		}
	}
	return nil
}

func partitionArg(op int32, name string, index int) (blkpgArgument, *[152]byte) {
	return partitionRangeArg(op, name, index, Start, Span)
}

func partitionRangeArg(op int32, name string, index int, start, span int64) (blkpgArgument, *[152]byte) {
	data := new([152]byte)
	binary.LittleEndian.PutUint64(data[0:], uint64(start))
	binary.LittleEndian.PutUint64(data[8:], uint64(span))
	binary.LittleEndian.PutUint32(data[16:], uint32(index))
	copy(data[20:84], name)
	return blkpgArgument{Op: op, DataLen: 152, Data: unsafe.Pointer(&data[0])}, data
}

func createdPartitionIndex(entries map[int]string, label string) (int, error) {
	found := -1
	for index, value := range entries {
		if value != label {
			continue
		}
		if found >= 0 {
			return -1, fmt.Errorf("ambiguous temporary partition registration")
		}
		found = index
	}
	if found == masterIndex {
		return -1, fmt.Errorf("temporary label unexpectedly belongs to master")
	}
	if found > 127 {
		return found, fmt.Errorf("temporary partition minor exceeds supported range")
	}
	return found, nil
}

func validateMappingStats(before, after Stats, recovery bool) error {
	if after.Failed < before.Failed || after.Corrected < before.Corrected ||
		after.BadBlocks != before.BadBlocks || after.BBTBlocks != before.BBTBlocks {
		return fmt.Errorf("ECC state reset or bad-block inventory changed during mapping check")
	}
	if !recovery && after.Failed != before.Failed {
		return fmt.Errorf("uncorrectable ECC during mapping check")
	}
	return nil
}

func mappingStats(device Device, tracker *statsTracker, allowedFailed uint64) (Stats, error) {
	if tracker == nil {
		return Stats{}, fmt.Errorf("mapping requires the original preflight statistics tracker")
	}
	stats, err := tracker.check(device)
	if err != nil {
		return Stats{}, err
	}
	// Only failed reads explicitly allowed in changed recovery blocks are counted.
	if uint64(stats.Failed) != uint64(tracker.initial.Failed)+allowedFailed {
		return Stats{}, fmt.Errorf("uncorrectable ECC since operation start; no mapping qualification")
	}
	return stats, nil
}

func recheckMappingState(ctx context.Context, device Device, bundle *Bundle, snapshot *Snapshot) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateSnapshot(device, bundle, snapshot); err != nil {
		return err
	}
	if snapshot.resume {
		return recheckResumeMapping(ctx, device, bundle, snapshot)
	}
	if _, err := mappingStats(device, snapshot.stats, snapshot.Failed); err != nil {
		return err
	}
	if err := checkGeometry(device); err != nil {
		return err
	}
	if _, err := mappingStats(device, snapshot.stats, snapshot.Failed); err != nil {
		return err
	}
	for index := 0; index < Blocks; index++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		original, err := bundle.verifiedBlock(index, true)
		if err != nil {
			return err
		}
		recoverChanged := snapshot.restore && bundle.Plan.Blocks[index].Changed
		if !recoverChanged && snapshot.Hashes[index] != digest(original) {
			return fmt.Errorf("mapping requires an original-data preflight at block %d", index)
		}
		if err := checkedBlockStatus(device, index, snapshot.stats); err != nil {
			return err
		}
		data, oob, stats, err := checkedRead(device, index, true, snapshot.stats)
		if err != nil {
			return err
		}
		if digest(data) != snapshot.Hashes[index] ||
			(!recoverChanged && (!bytes.Equal(data, original) || !allFF(oob) || stats.Failed != 0)) {
			return fmt.Errorf("mapping original data/ECC/OOB recheck failed at block %d", index)
		}
		snapshot.Failed += uint64(stats.Failed)
		if _, err := mappingStats(device, snapshot.stats, snapshot.Failed); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func qualifyReadOnlyMapping(arch, banner string, recovery bool, create func(bool) error) error {
	if arch != "arm" || banner != mappingKernelBanner || recovery {
		return fmt.Errorf("mapping qualification requires the exact cleanup-fixed ARM kernel and non-recovery mode")
	}
	return create(false)
}

func qualifyWriteKernel(arch, banner string, enable func() error) error {
	if arch != "arm" || banner != mappingKernelBanner {
		return fmt.Errorf("write enablement requires the exact cleanup-fixed ARM kernel")
	}
	return enable()
}

func checkMappingPhase(ctx context.Context, device Device, bundle *Bundle, snapshot *Snapshot, journal Journal, create func() error) error {
	if snapshot != nil && (snapshot.restore || snapshot.resume) {
		return fmt.Errorf("check-map does not permit a recovery/resume preflight")
	}
	return checkPartitionPhase(ctx, device, bundle, snapshot, journal, create)
}

func checkPartitionPhase(ctx context.Context, device Device, bundle *Bundle, snapshot *Snapshot, journal Journal, create func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateSnapshot(device, bundle, snapshot); err != nil {
		return err
	}
	if err := journal.Record(Event{Stage: "mapping-requested", Block: -1, Absolute: Start, Bytes: Span, Detail: "read-only; cleanup pending"}); err != nil {
		return err
	}
	// Recheck after journaling so cancellation or state changes during output
	// cannot become a new baseline or allow creation.
	if err := recheckMappingState(ctx, device, bundle, snapshot); err != nil {
		return err
	}
	if err := create(); err != nil {
		return err
	}
	if err := recheckMappingState(ctx, device, bundle, snapshot); err != nil {
		return err
	}
	if err := journal.Record(Event{Stage: "mapping-readback-verified", Block: -1, Absolute: Start, Bytes: Span, Detail: "read-only; cleanup pending"}); err != nil {
		return err
	}
	if _, err := mappingStats(device, snapshot.stats, snapshot.Failed); err != nil {
		return err
	}
	return ctx.Err()
}

func (m *MTD) makePartition(ctx context.Context, writable bool, snapshot *Snapshot) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if writable || m.writeEnabled {
		return fmt.Errorf("%s", readOnlyCreationReason)
	}
	if err := m.validateSnapshotMode(snapshot); err != nil {
		return err
	}
	if m.partition != nil || m.createdIndex >= 0 || m.partitionAdded {
		return fmt.Errorf("partition already created")
	}
	if err := m.checkTopology(); err != nil {
		return err
	}
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		return err
	}
	m.label = fmt.Sprintf("reinvoke-probe-%x", random)
	arg, data := partitionArg(1, m.label, 0)
	if _, err := mappingStats(m, snapshot.stats, snapshot.Failed); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	err := m.addAndDiscoverPartition(func() error {
		err := callIOCTL(m.master, blkpg, unsafe.Pointer(&arg))
		runtime.KeepAlive(data)
		return err
	}, mtdEntries, func() { time.Sleep(50 * time.Millisecond) })
	if err != nil {
		return err
	}
	// After ADD, retain the created index for Close before honoring a stop.
	if err := ctx.Err(); err != nil {
		return err
	}
	sysPath := fmt.Sprintf("/sys/class/mtd/mtd%d", m.createdIndex)
	for key, want := range map[string]string{
		"name": m.label, "size": strconv.FormatInt(Span, 10),
		"writesize": "2048", "erasesize": "131072", "oobsize": "64", "type": "nand",
		"dev": fmt.Sprintf("90:%d", m.createdIndex*2),
	} {
		got, err := readText(filepath.Join(sysPath, key))
		if err != nil || got != want {
			return fmt.Errorf("temporary partition %s mismatch: %q vs %q: %v", key, got, want, err)
		}
	}
	if err := m.checkTopology(); err != nil {
		return err
	}
	minor := m.createdIndex*2 + 1
	node := filepath.Join(m.sessionDir, "target-mtd")
	if err := syscall.Mknod(node, syscall.S_IFCHR|0600, (90<<8)|minor); err != nil {
		return err
	}
	m.partition, err = openMTDNode(node, minor, false)
	if err != nil {
		return err
	}
	value, err := info(m.partition)
	if err != nil || value.Type != 4 || value.Size != uint32(Span) ||
		value.EraseSize != EraseBytes || value.WriteSize != PageBytes ||
		value.OOBSize != 64 || value.Flags != 0x400 {
		return fmt.Errorf("partition MEMGETINFO mismatch: %+v error=%v", value, err)
	}
	if err := callIOCTL(m.partition, mtdFileMode, nil); err != nil {
		return fmt.Errorf("select normal hardware-ECC file mode: %w", err)
	}
	m.writeEnabled = false
	return nil
}

func (m *MTD) validateSnapshotMode(snapshot *Snapshot) error {
	if snapshot == nil || snapshot.stats == nil || len(snapshot.Hashes) != Blocks ||
		snapshot.device != m || snapshot.restore != m.recovery || snapshot.resume != m.resume ||
		snapshot.resume != snapshot.stats.resume ||
		(snapshot.resume && (!snapshot.complete || snapshot.restore || !ResumeSupported ||
			snapshot.Failed != 0 || snapshot.stats.last.Failed != snapshot.stats.initial.Failed)) {
		return fmt.Errorf("mapping requires a complete matching-mode preflight snapshot")
	}
	return nil
}

func (m *MTD) comparePartition(ctx context.Context, partition *os.File, bundle *Bundle, snapshot *Snapshot) error {
	if snapshot.resume {
		return compareResumePartition(ctx, m, m.master, partition, bundle, snapshot)
	}
	// The old kernel has no sysfs offset. Compare the entire nonuniform extent.
	for offset := int64(0); offset < Span; offset += EraseBytes {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := checkedBlockStatus(m, int(offset/EraseBytes), snapshot.stats); err != nil {
			return err
		}
		before, err := mappingStats(m, snapshot.stats, snapshot.Failed)
		if err != nil {
			return err
		}
		main, relative := make([]byte, EraseBytes), make([]byte, EraseBytes)
		n1, err1 := m.master.ReadAt(main, Start+offset)
		n2, err2 := partition.ReadAt(relative, offset)
		if err1 != nil || err2 != nil || n1 != EraseBytes || n2 != EraseBytes ||
			!bytes.Equal(main, relative) || digest(main) != snapshot.Hashes[int(offset/EraseBytes)] {
			return fmt.Errorf("temporary partition mapping failed at relative %#x", offset)
		}
		after, err := snapshot.stats.check(m)
		if err != nil {
			return err
		}
		recoverChanged := snapshot.restore && bundle.Plan.Blocks[int(offset/EraseBytes)].Changed
		if err := validateMappingStats(before, after, recoverChanged); err != nil {
			return fmt.Errorf("mapping at %#x: %w", offset, err)
		}
		snapshot.Failed += uint64(after.Failed - before.Failed)
		if _, err := mappingStats(m, snapshot.stats, snapshot.Failed); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func (m *MTD) addAndDiscoverPartition(add func() error, entries func() (map[int]string, error), pause func()) error {
	if err := add(); err != nil {
		return fmt.Errorf("create temporary partition: %w", err)
	}
	// ADD has already changed kernel state even if discovery subsequently fails.
	m.partitionAdded = true
	m.createdIndex = -1
	for attempt := 0; attempt < 20; attempt++ {
		current, err := entries()
		if err != nil {
			return err
		}
		index, err := createdPartitionIndex(current, m.label)
		m.createdIndex = index
		if err != nil {
			return err
		}
		if index >= 0 {
			return nil
		}
		pause()
	}
	return fmt.Errorf("ADD returned success but no matching partition appeared; no retry")
}

func (m *MTD) CheckMapping(ctx context.Context, bundle *Bundle, snapshot *Snapshot, journal Journal) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := CheckRuntime(); err != nil {
		return err
	}
	banner, err := readText("/proc/version")
	if err != nil {
		return err
	}
	return qualifyReadOnlyMapping(runtime.GOARCH, banner, m.recovery, func(writable bool) error {
		return checkMappingPhase(ctx, m, bundle, snapshot, journal, func() error {
			if err := m.makePartition(ctx, writable, snapshot); err != nil {
				return err
			}
			return m.comparePartition(ctx, m.partition, bundle, snapshot)
		})
	})
}

func enableWritesPhase(ctx context.Context, device Device, bundle *Bundle, snapshot *Snapshot, journal Journal, acquire func() error) error {
	if err := validateSnapshot(device, bundle, snapshot); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := journal.Record(Event{Stage: "write-enable-requested", Block: -1, Absolute: Start, Bytes: Span}); err != nil {
		return err
	}
	if err := recheckMappingState(ctx, device, bundle, snapshot); err != nil {
		return err
	}
	for index := 0; index < Blocks; index++ {
		if _, err := bundle.verifiedBlock(index, false); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := acquire(); err != nil {
		return err
	}
	if err := recheckMappingState(ctx, device, bundle, snapshot); err != nil {
		return err
	}
	if err := journal.Record(Event{Stage: "write-descriptor-verified", Block: -1, Absolute: Start, Bytes: Span}); err != nil {
		return err
	}
	if _, err := mappingStats(device, snapshot.stats, snapshot.Failed); err != nil {
		return err
	}
	return ctx.Err()
}

func (m *MTD) acquireWritablePartition(ctx context.Context, bundle *Bundle, snapshot *Snapshot) (result error) {
	if !m.partitionAdded || m.createdIndex < 0 || m.createdIndex > 127 ||
		m.createdIndex == masterIndex || m.partition == nil || m.writeEnabled {
		return fmt.Errorf("no qualified read-only partition")
	}
	if err := m.checkTopology(); err != nil {
		return err
	}
	if _, err := mappingStats(m, snapshot.stats, snapshot.Failed); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	minor := m.createdIndex * 2
	node := filepath.Join(m.sessionDir, "target-mtd-rw")
	if err := syscall.Mknod(node, syscall.S_IFCHR|0600, (90<<8)|minor); err != nil {
		return err
	}
	rw, err := openMTDNode(node, minor, true)
	if err != nil {
		return err
	}
	defer func() {
		if rw != nil {
			if err := rw.Close(); err != nil {
				result = fmt.Errorf("operation result=%v; close unqualified RW descriptor: %w", result, err)
			}
		}
	}()
	value, err := info(rw)
	if err != nil || value.Type != 4 || value.Size != uint32(Span) ||
		value.EraseSize != EraseBytes || value.WriteSize != PageBytes ||
		value.OOBSize != 64 || value.Flags != 0x400 {
		return fmt.Errorf("RW partition MEMGETINFO mismatch: %+v error=%v", value, err)
	}
	if err := callIOCTL(rw, mtdFileMode, nil); err != nil {
		return fmt.Errorf("select normal hardware-ECC RW mode: %w", err)
	}
	if err := m.comparePartition(ctx, rw, bundle, snapshot); err != nil {
		return err
	}
	ro := m.partition
	m.partition, rw = rw, nil
	return ro.Close()
}

func (m *MTD) EnableWrites(ctx context.Context, bundle *Bundle, snapshot *Snapshot, journal Journal) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := CheckRuntime(); err != nil {
		return err
	}
	banner, err := readText("/proc/version")
	if err != nil {
		return err
	}
	return qualifyWriteKernel(runtime.GOARCH, banner, func() error {
		if err := m.validateSnapshotMode(snapshot); err != nil {
			return err
		}
		if err := checkPartitionPhase(ctx, m, bundle, snapshot, journal, func() error {
			if err := m.makePartition(ctx, false, snapshot); err != nil {
				return err
			}
			return m.comparePartition(ctx, m.partition, bundle, snapshot)
		}); err != nil {
			return err
		}
		if err := enableWritesPhase(ctx, m, bundle, snapshot, journal, func() error {
			return m.acquireWritablePartition(ctx, bundle, snapshot)
		}); err != nil {
			return err
		}
		m.writeEnabled = true
		return nil
	})
}

func (m *MTD) Erase(offset int64) error {
	if !m.writeEnabled || m.partition == nil {
		return fmt.Errorf("no writable partition")
	}
	if err := bounds(offset, EraseBytes, EraseBytes); err != nil {
		return err
	}
	request := [2]uint32{uint32(offset), EraseBytes}
	return callIOCTL(m.partition, memErase, unsafe.Pointer(&request))
}

func (m *MTD) WriteAt(data []byte, offset int64) (int, error) {
	if !m.writeEnabled || m.partition == nil || len(data) != PageBytes {
		return 0, fmt.Errorf("no writable partition or not a full page")
	}
	if err := bounds(offset, len(data), PageBytes); err != nil {
		return 0, err
	}
	return m.partition.WriteAt(data, offset)
}

func (m *MTD) Sync() error {
	if m.partition == nil {
		return fmt.Errorf("no open partition")
	}
	// This kernel has no MTD fsync callback. Program/erase already wait for
	// completion; closing the RW fd calls mtd_sync. Recheck fd identity here.
	value, err := info(m.partition)
	if err != nil {
		return err
	}
	if value.Size != uint32(Span) || value.Type != 4 {
		return fmt.Errorf("partition identity changed")
	}
	return nil
}

func (m *MTD) removeAddedPartition(entries func() (map[int]string, error), remove func(int) error, removeNodes func(int) error) error {
	if !m.partitionAdded {
		return nil
	}
	if m.master == nil || m.label == "" {
		return fmt.Errorf("ADD succeeded but its master handle or owned label is unavailable")
	}
	current, err := entries()
	if err != nil {
		return fmt.Errorf("resolve successful ADD before cleanup: %w", err)
	}
	if current[masterIndex] != "mv_nand" {
		return fmt.Errorf("cannot establish master ownership for successful ADD cleanup")
	}
	index, err := createdPartitionIndex(current, m.label)
	if err != nil {
		return fmt.Errorf("resolve owned partition for cleanup: %w", err)
	}
	if index < 0 {
		return fmt.Errorf("ADD succeeded but no unique owned partition is visible; cleanup unconfirmed")
	}
	if m.createdIndex >= 0 && index != m.createdIndex {
		return fmt.Errorf("owned partition index changed from %d to %d; refusing deletion", m.createdIndex, index)
	}
	m.createdIndex = index
	if err := remove(index); err != nil {
		return fmt.Errorf("remove temporary partition mtd%d: %w; do not blindly retry", index, err)
	}
	current, err = entries()
	if err != nil {
		return fmt.Errorf("verify partition removal: %w", err)
	}
	if current[masterIndex] != "mv_nand" {
		return fmt.Errorf("master identity changed while verifying partition removal")
	}
	if _, exists := current[index]; exists {
		return fmt.Errorf("partition index still listed after removal; nodes left untouched")
	}
	for _, label := range current {
		if label == m.label {
			return fmt.Errorf("owned partition label still listed after removal; nodes left untouched")
		}
	}
	m.partitionAdded = false
	m.createdIndex = -1
	return removeNodes(index)
}

func (m *MTD) removePartitionNodes(index int) error {
	var errors []string
	for _, path := range []string{
		fmt.Sprintf("/dev/mtd%d", index),
		fmt.Sprintf("/dev/mtd%dro", index),
		fmt.Sprintf("/dev/mtdblock%d", index),
		fmt.Sprintf("/dev/mtd/mtd%d", index),
		fmt.Sprintf("/dev/mtd/mtd%dro", index),
		fmt.Sprintf("/dev/mtd/mtdblock%d", index),
	} {
		stat, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			errors = append(errors, err.Error())
			continue
		}
		raw, ok := stat.Sys().(*syscall.Stat_t)
		if !ok || stat.Mode()&os.ModeDevice == 0 {
			errors = append(errors, "unexpected node left untouched: "+path)
			continue
		}
		dev := raw.Rdev
		if dev != uint64(90<<8|index*2) &&
			dev != uint64(90<<8|(index*2+1)) && dev != uint64(31<<8|index) {
			errors = append(errors, "unrelated device node left untouched: "+path)
			continue
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			errors = append(errors, err.Error())
		}
	}
	for _, name := range []string{"target-mtd", "target-mtd-rw"} {
		if err := os.Remove(filepath.Join(m.sessionDir, name)); err != nil && !os.IsNotExist(err) {
			errors = append(errors, err.Error())
		}
	}
	if len(errors) != 0 {
		return fmt.Errorf("node cleanup: %s", strings.Join(errors, "; "))
	}
	return nil
}

func (m *MTD) closeWithPartitionOps(entries func() (map[int]string, error), remove func(int) error, removeNodes func(int) error) error {
	var errors []string
	if m.partition != nil {
		if err := m.partition.Close(); err != nil {
			errors = append(errors, err.Error())
		}
		m.partition = nil
	}
	if err := m.removeAddedPartition(entries, remove, removeNodes); err != nil {
		errors = append(errors, err.Error())
	}
	if m.master != nil {
		if err := m.master.Close(); err != nil {
			errors = append(errors, err.Error())
		}
		m.master = nil
	}
	if m.lock != nil {
		if err := m.lock.Close(); err != nil {
			errors = append(errors, err.Error())
		}
		m.lock = nil
	}
	if len(errors) > 0 {
		return fmt.Errorf("cleanup incomplete: %s", strings.Join(errors, "; "))
	}
	return nil
}

func (m *MTD) Close() error {
	return m.closeWithPartitionOps(mtdEntries, func(index int) error {
		arg, data := partitionArg(2, m.label, index)
		err := callIOCTL(m.master, blkpg, unsafe.Pointer(&arg))
		runtime.KeepAlive(data)
		return err
	}, m.removePartitionNodes)
}

var _ Device = (*MTD)(nil)
var _ io.ReaderAt = (*MTD)(nil)
