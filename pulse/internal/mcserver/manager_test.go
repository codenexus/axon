package mcserver

import (
	"testing"
	"time"

	"github.com/codenexus/axon/pulse/internal/protocol"
)

func TestStartStopLifecycle(t *testing.T) {
	dir := t.TempDir()
	cfg := []InstanceConfig{{
		ID:           "test-instance",
		Name:         "Test Instance",
		GamePlatform: "java",
		Version:      "1.21.1",
		SoftwareType: "vanilla",
		Command:      []string{"sh", "-c", "sleep 5"},
		WorkingDir:   dir,
	}}
	m := NewManager(cfg)

	statuses := m.Statuses()
	if len(statuses) != 1 || statuses[0].RunningState != protocol.StateStopped {
		t.Fatalf("expected one stopped instance, got %+v", statuses)
	}

	if err := m.Start("test-instance"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	statuses = m.Statuses()
	if statuses[0].RunningState != protocol.StateRunning {
		t.Fatalf("expected running after Start, got %s", statuses[0].RunningState)
	}

	if err := m.Stop("test-instance"); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	// Stop signals the process; give the Wait() goroutine a moment to observe exit.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if m.Statuses()[0].RunningState == protocol.StateStopped {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("expected stopped after Stop, got %s", m.Statuses()[0].RunningState)
}

func TestStartUnknownInstance(t *testing.T) {
	m := NewManager(nil)
	if err := m.Start("does-not-exist"); err == nil {
		t.Fatal("expected error starting unknown instance")
	}
}
