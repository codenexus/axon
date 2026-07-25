package mcserver

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/codenexus/axon/pulse/internal/protocol"
)

// TestReconcileAdoptsStillRunningInstance simulates a Pulse restart: one
// Manager starts a process (writing the PID file Start always writes), then
// a brand new Manager — standing in for a freshly (re)started Pulse process
// with no memory of the first one — is pointed at the same working_dir and
// asked to Reconcile. It should adopt the still-running process rather than
// assuming it's Stopped.
func TestReconcileAdoptsStillRunningInstance(t *testing.T) {
	dir := t.TempDir()
	cfg := []InstanceConfig{{
		ID:         "survival",
		Command:    []string{"sh", "-c", "sleep 5"},
		WorkingDir: dir,
	}}

	oldManager := NewManager(cfg)
	if err := oldManager.Start("survival"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { oldManager.Stop("survival") })

	if _, err := os.Stat(pidFilePath(dir)); err != nil {
		t.Fatalf("expected Start to write a pid file: %v", err)
	}

	newManager := NewManager(cfg) // fresh in-memory state, as if Pulse just restarted
	if newManager.IsRunning("survival") {
		t.Fatal("expected fresh Manager to start out believing the instance is stopped")
	}

	newManager.Reconcile()

	if !newManager.IsRunning("survival") {
		t.Fatal("expected Reconcile to adopt the still-running instance")
	}
	statuses := newManager.Statuses()
	if len(statuses) != 1 || statuses[0].RunningState != protocol.StateRunning {
		t.Fatalf("expected adopted instance to report Running, got %+v", statuses)
	}

	// The new Manager should be able to stop the process it adopted, even
	// though it never spawned it (no *exec.Cmd, only a pid).
	if err := newManager.Stop("survival"); err != nil {
		t.Fatalf("Stop (adopted): %v", err)
	}
	// watchReattached only polls every 2s (it can't block on Wait() for a
	// process it didn't spawn), so allow comfortably more than one tick —
	// production callers use 30s for the same reason.
	if err := newManager.WaitStopped("survival", 5*time.Second); err != nil {
		t.Fatalf("WaitStopped (adopted): %v", err)
	}
	if _, err := os.Stat(pidFilePath(dir)); !os.IsNotExist(err) {
		t.Fatalf("expected pid file to be cleaned up after stop, stat err = %v", err)
	}
}

// TestReconcileIgnoresStaleOrMismatchedPIDFile guards against adopting an
// unrelated process that happens to have reused a stale PID (e.g. after a
// host reboot) — the fingerprint check in processMatches should refuse to
// adopt it and clean up the stale file instead.
func TestReconcileIgnoresStaleOrMismatchedPIDFile(t *testing.T) {
	dir := t.TempDir()
	cfg := []InstanceConfig{{
		ID:         "survival",
		Command:    []string{"sh", "-c", "sleep 5"},
		WorkingDir: dir,
	}}

	// A real, live process — but with a command fingerprint that doesn't
	// match cfg.Command at all, standing in for "some unrelated process
	// happens to have this pid now".
	writePIDFile(dir, os.Getpid(), []string{"totally-unrelated-binary"})

	m := NewManager(cfg)
	m.Reconcile()

	if m.IsRunning("survival") {
		t.Fatal("expected Reconcile not to adopt a process with a mismatched command fingerprint")
	}
	if _, err := os.Stat(pidFilePath(dir)); !os.IsNotExist(err) {
		t.Fatalf("expected stale/mismatched pid file to be removed, stat err = %v", err)
	}
}

func TestReconcileIgnoresDeadPID(t *testing.T) {
	dir := t.TempDir()
	cfg := []InstanceConfig{{
		ID:         "survival",
		Command:    []string{"sh", "-c", "sleep 5"},
		WorkingDir: dir,
	}}

	// A pid essentially guaranteed not to be alive.
	writePIDFile(dir, 1<<30, []string{"sh"})

	m := NewManager(cfg)
	m.Reconcile()

	if m.IsRunning("survival") {
		t.Fatal("expected Reconcile not to adopt a dead pid")
	}
	if _, err := os.Stat(pidFilePath(dir)); !os.IsNotExist(err) {
		t.Fatalf("expected dead-pid file to be removed, stat err = %v", err)
	}
}

func TestReconcileNoPIDFileLeavesInstanceStopped(t *testing.T) {
	dir := t.TempDir()
	cfg := []InstanceConfig{{
		ID:         "survival",
		Command:    []string{"sh", "-c", "sleep 5"},
		WorkingDir: dir,
	}}

	m := NewManager(cfg)
	m.Reconcile()

	if m.IsRunning("survival") {
		t.Fatal("expected no-op Reconcile when there's no pid file at all")
	}
}

func TestPIDFilePathIsInsideWorkingDir(t *testing.T) {
	dir := t.TempDir()
	if got, want := pidFilePath(dir), filepath.Join(dir, ".pulse-pid"); got != want {
		t.Fatalf("pidFilePath = %q, want %q", got, want)
	}
}
