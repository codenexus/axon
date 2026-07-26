package main

import (
	"testing"

	"github.com/codenexus/axon/pulse/internal/protocol"
)

func TestCreationJobPhaseTransitions(t *testing.T) {
	job := &creationJob{phase: "preparing"}

	done, phase, _ := job.snapshot()
	if done || phase != "preparing" {
		t.Fatalf("expected in-progress at 'preparing', got done=%v phase=%q", done, phase)
	}

	job.setPhase("downloading")
	done, phase, _ = job.snapshot()
	if done || phase != "downloading" {
		t.Fatalf("expected in-progress at 'downloading', got done=%v phase=%q", done, phase)
	}

	job.finish(protocol.CommandResult{CommandID: "cmd_1", Success: true})
	done, _, result := job.snapshot()
	if !done {
		t.Fatal("expected done after finish")
	}
	if result.CommandID != "cmd_1" || !result.Success {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestRunCreateInstanceJobInvalidPayload(t *testing.T) {
	job := &creationJob{phase: "preparing"}
	cmd := protocol.Command{ID: "cmd_bad", InstanceID: "inst_x", Payload: []byte("not json")}

	// nil manager is safe here: an invalid payload fails json.Unmarshal and
	// returns before manager is ever touched.
	runCreateInstanceJob(job, nil, "", "", cmd)

	done, _, result := job.snapshot()
	if !done {
		t.Fatal("expected the job to finish on invalid payload")
	}
	if result.Success {
		t.Fatal("expected failure for invalid payload")
	}
}
