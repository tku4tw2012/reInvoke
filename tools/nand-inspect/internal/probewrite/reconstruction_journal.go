package probewrite

import (
	"encoding/json"
	"fmt"
	"io"
	"time"
)

type ReconstructionJournal struct {
	File interface {
		io.Writer
		Sync() error
	}
	Output io.Writer
}

func (j *ReconstructionJournal) Record(event Event) error {
	data, err := json.Marshal(struct {
		Time string `json:"time"`
		Event
	}{time.Now().UTC().Format(time.RFC3339Nano), event})
	if err != nil {
		return err
	}
	data = append(data, '\n')
	n, err := j.File.Write(data)
	if err != nil || n != len(data) {
		return fmt.Errorf("RAM journal write bytes=%d/%d raw-error=%v", n, len(data), err)
	}
	if err := j.File.Sync(); err != nil {
		return fmt.Errorf("RAM journal sync: %w", err)
	}
	n, err = j.Output.Write(data)
	if err != nil || n != len(data) {
		return fmt.Errorf("host journal transport bytes=%d/%d raw-error=%v", n, len(data), err)
	}
	return nil
}

func CloseReconstruction(d io.Closer, log Journal, result error) error {
	record := func(stage string) {
		if err := log.Record(Event{Stage: stage, Block: -1, Detail: fmt.Sprint(result)}); err != nil {
			result = fmt.Errorf("result=%v; journal %s: %w", result, stage, err)
		}
	}
	if result != nil {
		record("phase-failed-no-retry")
	}
	record("before-cleanup")
	if err := d.Close(); err != nil {
		result = fmt.Errorf("result=%v; device cleanup: %w", result, err)
		record("cleanup-failed")
	} else {
		record("cleanup-complete-no-reboot")
	}
	return result
}
