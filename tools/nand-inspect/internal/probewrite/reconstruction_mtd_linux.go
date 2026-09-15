package probewrite

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"time"
	"unsafe"
)

const reconstructionMemWrite = 0xc0304d18

// PLACE reaches the vendor page callback: 2048 main + 32 exposed spare bytes,
// one PAGEPROG, controller-generated BCH. RAW and OOB-only bypass that setup.
type reconstructionWriteRequest struct {
	Start, Len, OOBLen, Data, OOB uint64
	Mode                          uint8
	Padding                       [7]byte
}

func reconstructionPageRequest(offset int64, main, oob []byte) (reconstructionWriteRequest, error) {
	var req reconstructionWriteRequest
	if offset < 0 || offset+PageBytes > reconstructionSpan || offset%PageBytes != 0 ||
		len(main) != PageBytes || len(oob) != VisibleOOB || oob[0] != 0xff ||
		(ReconstructionSinglePhase && !allFF(oob)) {
		return req, fmt.Errorf("invalid fixed full-page PLACE request")
	}
	req.Start, req.Len, req.OOBLen = uint64(offset), PageBytes, VisibleOOB
	req.Data = uint64(uintptr(unsafe.Pointer(&main[0])))
	req.OOB = uint64(uintptr(unsafe.Pointer(&oob[0])))
	return req, nil
}

type ReconstructionMTD struct {
	lifecycle *MTD
	owner     int
	proof     *reconstructionProof
	order     []int
	next      int
	active    int
	lastPage  int
	failed    bool
	stop      context.Context
}

func CheckReconstructionRuntime() error {
	if err := requireReconstructionProfile(); err != nil {
		return err
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

func OpenReconstructionMTD(sessionDir string) (_ *ReconstructionMTD, result error) {
	if err := CheckReconstructionRuntime(); err != nil {
		return nil, err
	}
	fi, err := os.Lstat(sessionDir)
	if err != nil {
		return nil, err
	}
	raw, ok := fi.Sys().(*syscall.Stat_t)
	if !ok || !fi.IsDir() || fi.Mode().Perm() != 0700 || raw.Uid != uint32(os.Geteuid()) {
		return nil, fmt.Errorf("reconstruction evidence directory must be private and owned")
	}
	m, err := OpenMTD(sessionDir, false)
	if err != nil {
		return nil, err
	}
	d := &ReconstructionMTD{lifecycle: m, owner: os.Getpid(), active: -1, lastPage: -1}
	defer func() {
		if result != nil {
			if err := d.Close(); err != nil {
				result = fmt.Errorf("%v; cleanup: %w", result, err)
			}
		}
	}()
	lock, err := m.lock.Stat()
	if err != nil {
		return nil, err
	}
	stat, ok := lock.Sys().(*syscall.Stat_t)
	if !ok || !lock.Mode().IsRegular() || lock.Mode().Perm() != 0600 ||
		stat.Uid != uint32(os.Geteuid()) || stat.Nlink != 1 {
		return nil, fmt.Errorf("device-wide writer lock is not a private owned regular file")
	}
	if err := d.Check(); err != nil {
		return nil, err
	}
	return d, nil
}

func (d *ReconstructionMTD) owned() error {
	if d == nil || d.owner != os.Getpid() || d.lifecycle == nil ||
		d.lifecycle.master == nil || d.lifecycle.lock == nil {
		return fmt.Errorf("operation is not owned by this reconstruction process")
	}
	return nil
}

func reconstructionInfo(file *os.File, size int64) error {
	value, err := info(file)
	if err != nil || value.Type != 4 || value.Flags != 0x400 || int64(value.Size) != size ||
		value.EraseSize != EraseBytes || value.WriteSize != PageBytes || value.OOBSize != 64 {
		return fmt.Errorf("reconstruction MEMGETINFO mismatch: %+v error=%v", value, err)
	}
	return nil
}

func (d *ReconstructionMTD) Check() error {
	if err := d.owned(); err != nil {
		return err
	}
	if err := CheckReconstructionRuntime(); err != nil {
		return err
	}
	if err := d.lifecycle.checkTopology(); err != nil {
		return err
	}
	return reconstructionInfo(d.lifecycle.master, DeviceBytes)
}

func (d *ReconstructionMTD) Stats() (Stats, error) {
	if err := d.owned(); err != nil {
		return Stats{}, err
	}
	return d.lifecycle.Stats()
}

func (d *ReconstructionMTD) ReadMain(data []byte, absolute int64) (int, error) {
	if err := d.owned(); err != nil {
		return 0, err
	}
	if len(data) != PageBytes || absolute < 0 || absolute%PageBytes != 0 || absolute+PageBytes > DeviceBytes {
		return 0, fmt.Errorf("invalid full-device page read")
	}
	return d.lifecycle.master.ReadAt(data, absolute)
}

func (d *ReconstructionMTD) ReadSpare(data []byte, absolute int64) (int, error) {
	if err := d.owned(); err != nil {
		return 0, err
	}
	if len(data) != VisibleOOB || absolute < 0 || absolute%PageBytes != 0 || absolute+PageBytes > DeviceBytes {
		return 0, fmt.Errorf("invalid exposed-OOB read")
	}
	for i := range data {
		data[i] = 0xa5
	}
	var request [24]byte
	binary.LittleEndian.PutUint64(request[0:8], uint64(absolute))
	binary.LittleEndian.PutUint32(request[12:16], VisibleOOB)
	binary.LittleEndian.PutUint64(request[16:24], uint64(uintptr(unsafe.Pointer(&data[0]))))
	err := callIOCTL(d.lifecycle.master, memReadOOB64, unsafe.Pointer(&request[0]))
	runtime.KeepAlive(data)
	return int(binary.LittleEndian.Uint32(request[12:16])), err
}

func (d *ReconstructionMTD) IsBadBlock(block int) (bool, error) {
	if err := d.owned(); err != nil {
		return false, err
	}
	if block < 0 || block >= int(DeviceBytes/EraseBytes) {
		return false, fmt.Errorf("invalid physical block %d", block)
	}
	absolute := int64(block) * EraseBytes
	result, _, errno := syscall.Syscall(syscall.SYS_IOCTL, d.lifecycle.master.Fd(), memGetBadBlock, uintptr(unsafe.Pointer(&absolute)))
	runtime.KeepAlive(&absolute)
	runtime.KeepAlive(d.lifecycle.master)
	if errno != 0 {
		return false, errno
	}
	if result > 1 {
		return false, fmt.Errorf("invalid bad-block result %d", result)
	}
	return result == 1, nil
}

func (d *ReconstructionMTD) compareView(ctx context.Context, file *os.File, proof *reconstructionProof, log Journal) error {
	hash := sha256.New()
	page := make([]byte, PageBytes)
	blockHash := sha256.New()
	for offset := int64(0); offset < reconstructionSpan; offset += PageBytes {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := proof.tracker.read(func() error {
			n, err := file.ReadAt(page, offset)
			if err != nil || n != PageBytes {
				return fmt.Errorf("mapped page offset=%#x bytes=%d raw-error=%v", offset, n, err)
			}
			return nil
		}); err != nil {
			return err
		}
		hash.Write(page)
		blockHash.Write(page)
		if (offset+PageBytes)%EraseBytes == 0 {
			block := int((reconstructionStart + offset) / EraseBytes)
			if fmt.Sprintf("%x", blockHash.Sum(nil)) != proof.snapshot.blocks[block].main {
				return fmt.Errorf("mapped main differs at physical block %d", block)
			}
			blockHash.Reset()
			if block%32 == 0 {
				if err := log.Record(Event{Stage: "mapping-progress", Block: block, Detail: "read-only comparison; writes disabled"}); err != nil {
					return err
				}
			}
		}
	}
	if fmt.Sprintf("%x", hash.Sum(nil)) != proof.snapshot.view {
		return fmt.Errorf("complete mapped-view SHA-256 mismatch")
	}
	return proof.tracker.check(0)
}

func (d *ReconstructionMTD) EnableReconstruction(ctx context.Context, proof *reconstructionProof, log Journal) (result error) {
	if proof == nil || proof.device != d || proof.tracker == nil || proof.tracker.device != d ||
		proof.bundle == nil || !proof.bundle.validated || proof.snapshot == nil {
		return fmt.Errorf("write enable requires this device's complete fingerprint proof")
	}
	if err := d.Check(); err != nil {
		return err
	}
	order, err := proof.bundle.phaseOrder(proof.action)
	if err != nil {
		return err
	}
	state, err := reconstructionRequiredState(proof.action, true)
	if err != nil {
		return err
	}
	if err := proof.bundle.matchSnapshot(proof.snapshot, state); err != nil {
		return err
	}
	m := d.lifecycle
	if m.partition != nil || m.partitionAdded || d.proof != nil {
		return fmt.Errorf("refuse to reuse an existing mapping or write capability")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		return err
	}
	m.label = fmt.Sprintf("%s-%x", reconstructionPartitionPrefix, random)
	if err := log.Record(Event{Stage: "before-mapping", Block: -1, Absolute: reconstructionStart,
		Bytes: reconstructionSpan, Detail: "create owned read-only descriptor; no writable master"}); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	arg, data := partitionRangeArg(1, m.label, 0, reconstructionStart, reconstructionSpan)
	if err := m.addAndDiscoverPartition(func() error {
		err := callIOCTL(m.master, blkpg, unsafe.Pointer(&arg))
		runtime.KeepAlive(data)
		return err
	}, mtdEntries, func() { time.Sleep(50 * time.Millisecond) }); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	sysPath := fmt.Sprintf("/sys/class/mtd/mtd%d", m.createdIndex)
	for key, want := range map[string]string{
		"name": m.label, "size": strconv.FormatInt(reconstructionSpan, 10),
		"writesize": "2048", "erasesize": "131072", "oobsize": "64", "type": "nand",
		"dev": fmt.Sprintf("90:%d", m.createdIndex*2),
	} {
		value, err := readText(filepath.Join(sysPath, key))
		if err != nil || value != want {
			return fmt.Errorf("owned partition %s mismatch: %q want=%q error=%v", key, value, want, err)
		}
	}
	roMinor := m.createdIndex*2 + 1
	roPath := filepath.Join(m.sessionDir, "target-mtd")
	if err := syscall.Mknod(roPath, syscall.S_IFCHR|0600, (90<<8)|roMinor); err != nil {
		return err
	}
	m.partition, err = openMTDNode(roPath, roMinor, false)
	if err != nil {
		return err
	}
	if err := reconstructionInfo(m.partition, reconstructionSpan); err != nil {
		return err
	}
	if err := callIOCTL(m.partition, mtdFileMode, nil); err != nil {
		return err
	}
	if err := d.Check(); err != nil {
		return err
	}
	if err := d.compareView(ctx, m.partition, proof, log); err != nil {
		return err
	}
	if err := log.Record(Event{Stage: "mapping-verified", Block: -1,
		Detail: "entire RO partition matches " + state + " pinned view SHA256=" + proof.snapshot.view}); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	rwMinor := m.createdIndex * 2
	rwPath := filepath.Join(m.sessionDir, "target-mtd-rw")
	if err := syscall.Mknod(rwPath, syscall.S_IFCHR|0600, (90<<8)|rwMinor); err != nil {
		return err
	}
	rw, err := openMTDNode(rwPath, rwMinor, true)
	if err != nil {
		return err
	}
	defer func() {
		if rw != nil {
			if err := rw.Close(); err != nil {
				result = fmt.Errorf("enable result=%v; close unqualified RW descriptor: %w", result, err)
			}
		}
	}()
	if err := reconstructionInfo(rw, reconstructionSpan); err != nil {
		return err
	}
	if err := callIOCTL(rw, mtdFileMode, nil); err != nil {
		return err
	}
	if err := d.compareView(ctx, rw, proof, log); err != nil {
		return err
	}
	if err := d.Check(); err != nil {
		return err
	}
	if err := proof.tracker.check(0); err != nil {
		return err
	}
	if err := log.Record(Event{Stage: "writes-qualified", Block: -1,
		Detail: fmt.Sprintf("%s; %d whitelisted blocks; owned partition only", proof.action, len(order))}); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	ro := m.partition
	m.partition, rw = rw, nil
	if err := ro.Close(); err != nil {
		return err
	}
	d.proof, d.order, d.stop = proof, order, ctx
	return nil
}

func (d *ReconstructionMTD) writeOwner(block int) error {
	if err := requireReconstructionProfile(); err != nil {
		return err
	}
	if err := d.owned(); err != nil {
		return err
	}
	m := d.lifecycle
	if d.failed || d.proof == nil || d.proof.device != d || !m.partitionAdded ||
		m.partition == nil || m.partition == m.master || m.createdIndex < 0 ||
		d.next >= len(d.order) || d.order[d.next] != block || !reconstructionTargetBlock(block) {
		return fmt.Errorf("no owned ordered write capability for block %d", block)
	}
	stat, err := m.partition.Stat()
	if err != nil {
		return err
	}
	raw, ok := stat.Sys().(*syscall.Stat_t)
	if !ok || stat.Mode()&os.ModeCharDevice == 0 || raw.Rdev != uint64((90<<8)|m.createdIndex*2) {
		return fmt.Errorf("writable descriptor is not the owned partition")
	}
	flags, _, errno := syscall.Syscall(syscall.SYS_FCNTL, m.partition.Fd(), syscall.F_GETFL, 0)
	if errno != 0 || flags&syscall.O_ACCMODE != syscall.O_RDWR {
		return fmt.Errorf("partition descriptor is not O_RDWR: %v", errno)
	}
	return nil
}

func (d *ReconstructionMTD) EraseBlock(block int) error {
	if err := d.writeOwner(block); err != nil {
		return err
	}
	if d.active >= 0 {
		return fmt.Errorf("previous eraseblock has not been verified")
	}
	if d.stop == nil {
		return fmt.Errorf("write capability has no cancellation scope")
	}
	if err := d.Check(); err != nil {
		return err
	}
	bad, err := d.IsBadBlock(block)
	if err != nil || bad {
		return fmt.Errorf("erase bad/reserved block %d: %v", block, err)
	}
	if err := d.stop.Err(); err != nil {
		return err
	}
	d.active, d.lastPage = block, -1
	request := [2]uint32{uint32(int64(block)*EraseBytes - reconstructionStart), EraseBytes}
	err = callIOCTL(d.lifecycle.partition, memErase, unsafe.Pointer(&request))
	d.failed = err != nil
	return err
}

func (d *ReconstructionMTD) ProgramPage(block, page int, main, oob []byte) error {
	if err := d.writeOwner(block); err != nil {
		return err
	}
	if d.active != block || page < 0 || page >= EraseBytes/PageBytes || page <= d.lastPage {
		return fmt.Errorf("page outside the active freshly erased block or repeated/out of order")
	}
	req, err := reconstructionPageRequest(int64(block)*EraseBytes-reconstructionStart+int64(page*PageBytes), main, oob)
	if err != nil {
		return err
	}
	d.lastPage = page
	err = callIOCTL(d.lifecycle.partition, reconstructionMemWrite, unsafe.Pointer(&req))
	runtime.KeepAlive(main)
	runtime.KeepAlive(oob)
	d.failed = err != nil
	return err
}

func (d *ReconstructionMTD) FinishBlock(block int) error {
	if err := d.writeOwner(block); err != nil {
		return err
	}
	if d.active != block {
		return fmt.Errorf("cannot finish an inactive block")
	}
	if err := reconstructionInfo(d.lifecycle.partition, reconstructionSpan); err != nil {
		return err
	}
	d.active = -1
	d.next++
	return nil
}

func (d *ReconstructionMTD) Close() error {
	if d == nil || d.lifecycle == nil || d.owner != os.Getpid() {
		return fmt.Errorf("cleanup is not owned by this process")
	}
	d.proof = nil
	m := d.lifecycle
	return m.closeWithPartitionOps(mtdEntries, func(index int) error {
		arg, data := partitionRangeArg(2, m.label, index, reconstructionStart, reconstructionSpan)
		err := callIOCTL(m.master, blkpg, unsafe.Pointer(&arg))
		runtime.KeepAlive(data)
		return err
	}, m.removePartitionNodes)
}

var _ ReconstructionDevice = (*ReconstructionMTD)(nil)
