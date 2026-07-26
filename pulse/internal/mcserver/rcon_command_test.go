package mcserver

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestRunConsoleCommandUnknownInstance(t *testing.T) {
	m := NewManager(nil)
	if _, err := m.RunConsoleCommand("does-not-exist", "list"); err == nil {
		t.Fatal("expected an error for an unknown instance")
	}
}

func TestRunConsoleCommandNotRunning(t *testing.T) {
	dir := t.TempDir()
	m := NewManager([]InstanceConfig{{
		ID:         "test-instance",
		Command:    []string{"sh", "-c", "sleep 5"},
		WorkingDir: dir,
	}})
	// Never started — stays Stopped.
	if _, err := m.RunConsoleCommand("test-instance", "list"); err == nil {
		t.Fatal("expected an error for a non-running instance")
	}
}

func TestRunConsoleCommandRCONNotConfigured(t *testing.T) {
	dir := t.TempDir() // no server.properties present
	m := NewManager([]InstanceConfig{{
		ID:         "test-instance",
		Command:    []string{"sh", "-c", "sleep 5"},
		WorkingDir: dir,
	}})
	if err := m.Start("test-instance"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { m.Stop("test-instance") })

	if _, err := m.RunConsoleCommand("test-instance", "list"); err == nil {
		t.Fatal("expected an error when RCON isn't configured")
	}
}

func TestRunConsoleCommandSuccess(t *testing.T) {
	received := make(chan string, 1)
	addr := startFakeRCONServerForTest(t, "secret", received)
	_, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split addr: %v", err)
	}

	dir := t.TempDir()
	props := "enable-rcon=true\nrcon.port=" + portStr + "\nrcon.password=secret\n"
	if err := os.WriteFile(filepath.Join(dir, "server.properties"), []byte(props), 0o644); err != nil {
		t.Fatalf("write server.properties: %v", err)
	}

	m := NewManager([]InstanceConfig{{
		ID:         "test-instance",
		Command:    []string{"sh", "-c", "sleep 5"},
		WorkingDir: dir,
	}})
	if err := m.Start("test-instance"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { m.Stop("test-instance") })

	output, err := m.RunConsoleCommand("test-instance", "say hello")
	if err != nil {
		t.Fatalf("RunConsoleCommand: %v", err)
	}
	if output != "ok:say hello" {
		t.Fatalf("unexpected output: %q", output)
	}

	select {
	case got := <-received:
		if got != "say hello" {
			t.Fatalf("fake server received %q, want %q", got, "say hello")
		}
	default:
		t.Fatal("fake server never received the command")
	}
}
