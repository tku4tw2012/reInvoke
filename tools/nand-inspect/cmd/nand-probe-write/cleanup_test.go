//go:build !nandpilot

package main

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestCleanupFailurePreservesOriginalOperationErrorAndFailurePhase(t *testing.T) {
	file := &journalFile{}
	var output bytes.Buffer
	log := &journal{file: file, output: &output}
	operationError := errors.New("discovery failed after successful ADD")
	cleanupError := errors.New("cleanup incomplete: owned partition cannot be resolved")
	calls := 0
	err := closeWithJournal(closeFunc(func() error { calls++; return cleanupError }), log, operationError)
	if err == nil || calls != 1 || !strings.Contains(err.Error(), operationError.Error()) ||
		!strings.Contains(err.Error(), cleanupError.Error()) {
		t.Fatalf("operation or cleanup failure lost: calls=%d err=%v", calls, err)
	}
	if !reflect.DeepEqual(journalStages(t, output.Bytes()), []string{"operation-failed", "before-close", "close-failed"}) ||
		!bytes.Equal(file.Bytes(), output.Bytes()) {
		t.Fatalf("unresolved cleanup reported a closed-success phase: %s", &output)
	}
}
