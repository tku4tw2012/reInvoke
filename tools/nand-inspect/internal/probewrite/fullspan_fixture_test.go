//go:build nandpilot || nandbsl || nandbslcompact

package probewrite

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"testing"
)

var pilotFF = bytes.Repeat([]byte{0xff}, EraseBytes)

// Sparse test flash exercises either complete profile without allocating its span.
type pilotBytes map[int64]byte

func (p pilotBytes) ReadAt(data []byte, offset int64) (int, error) {
	if offset < 0 || int64(len(data)) > Span-offset {
		return 0, io.EOF
	}
	for copied := 0; copied < len(data); {
		copied += copy(data[copied:], pilotFF)
	}
	for position, value := range p {
		if position >= offset && position-offset < int64(len(data)) {
			data[position-offset] = value
		}
	}
	return len(data), nil
}

type pilotDevice struct {
	data                        pilotBytes
	stats                       Stats
	enabled                     bool
	erases                      []int
	writes, enables, identities int
	eccBlock, correctedBlock    int
}

func (d *pilotDevice) Geometry() (Geometry, error) {
	return Geometry{DeviceBytes, Start, Span, PageBytes, EraseBytes, 64}, nil
}
func (d *pilotDevice) CheckIdentity() error { d.identities++; return nil }
func (d *pilotDevice) ReadAt(data []byte, offset int64) (int, error) {
	if int(offset/EraseBytes) == d.eccBlock {
		d.stats.Failed++
	}
	if int(offset/EraseBytes) == d.correctedBlock {
		d.stats.Corrected++
	}
	return d.data.ReadAt(data, offset)
}
func (d *pilotDevice) ReadOOB(offset int64) ([]byte, error) {
	if err := bounds(offset, PageBytes, PageBytes); err != nil {
		return nil, err
	}
	return pilotFF[:VisibleOOB], nil
}
func (d *pilotDevice) IsBad(offset int64) (bool, error) {
	return false, bounds(offset, EraseBytes, EraseBytes)
}
func (d *pilotDevice) Stats() (Stats, error) { return d.stats, nil }
func (d *pilotDevice) EnableWrites(ctx context.Context, _ *Bundle, snapshot *Snapshot, _ Journal) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if snapshot == nil || snapshot.device != d || snapshot.stats == nil {
		return fmt.Errorf("preflight handoff missing")
	}
	d.enables++
	d.enabled = true
	return nil
}
func (d *pilotDevice) Erase(offset int64) error {
	if !d.enabled {
		return fmt.Errorf("not enabled")
	}
	if err := bounds(offset, EraseBytes, EraseBytes); err != nil {
		return err
	}
	d.erases = append(d.erases, int(offset/EraseBytes))
	for position := range d.data {
		if position >= offset && position < offset+EraseBytes {
			delete(d.data, position)
		}
	}
	return nil
}
func (d *pilotDevice) WriteAt(data []byte, offset int64) (int, error) {
	if !d.enabled || len(data) != PageBytes {
		return 0, fmt.Errorf("unapproved page write")
	}
	if err := bounds(offset, len(data), PageBytes); err != nil {
		return 0, err
	}
	d.writes++
	for index, value := range data {
		if value != 0xff {
			position := offset + int64(index)
			old, exists := d.data[position]
			if !exists {
				old = 0xff
			}
			d.data[position] = old & value
		}
	}
	return len(data), nil
}
func (d *pilotDevice) Sync() error { return nil }

type pilotJournal struct {
	preflightBlocks int
	last            string
}

func (j *pilotJournal) Record(event Event) error {
	if event.Stage == "preflight-block" {
		j.preflightBlocks++
	}
	j.last = event.Stage
	return nil
}

type pilotByteCount int64

func (c *pilotByteCount) Write(data []byte) (int, error) {
	*c += pilotByteCount(len(data))
	return len(data), nil
}

func pilotFixture(t *testing.T, headerChanged bool) (*Bundle, *pilotDevice) {
	t.Helper()
	if Blocks < 3 {
		t.Fatal("complete profile fixture needs separate header, preserved and final blocks")
	}
	last := int64(Blocks-1) * EraseBytes
	original := pilotBytes{0: 0x11, EraseBytes: 0x12, last: 0x22}
	probe := pilotBytes{0: 0x11, EraseBytes: 0x12, last: 0x44}
	if headerChanged {
		probe[0] = 0x33
	}
	device := &pilotDevice{data: pilotBytes{}, eccBlock: -1, correctedBlock: -1}
	for position, value := range original {
		device.data[position] = value
	}
	bundle := &Bundle{image: probe, original: original}
	if err := bundle.buildPlan(); err != nil {
		t.Fatal(err)
	}
	return bundle, device
}
