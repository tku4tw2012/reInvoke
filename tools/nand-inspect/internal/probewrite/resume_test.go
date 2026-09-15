package probewrite

import (
	"context"
	"io"
	"testing"
)

func TestResumeApprovalAndProfileGateBeforeDeviceAccess(t *testing.T) {
	for _, ack := range []string{"", InstallAck, RestoreAck, ResumePreflightAck} {
		if err := ExecuteResume(context.Background(), nil, nil, ack, io.Discard, nil, nil); err == nil {
			t.Fatalf("wrong resume approval accepted: %q", ack)
		}
	}
	for _, ack := range []string{"", InstallAck, RestoreAck, ResumeAck} {
		if _, err := PrepareResume(context.Background(), nil, nil, ack, io.Discard, nil); err == nil {
			t.Fatalf("wrong resume-preflight approval accepted: %q", ack)
		}
	}
	if err := ExecuteResume(context.Background(), nil, nil, ResumeAck, io.Discard, nil, nil); err == nil {
		t.Fatal("accepted incomplete sources")
	}
	if _, err := PrepareResume(context.Background(), nil, nil, ResumePreflightAck, io.Discard, nil); err == nil {
		t.Fatal("accepted incomplete sources")
	}
	if _, err := PlanResume(nil); err == nil {
		t.Fatal("accepted incomplete offline plan")
	}
}
