package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/codenexus/axon/pulse/internal/mcserver"
	"github.com/codenexus/axon/pulse/internal/protocol"
)

func TestExecuteDeleteInstanceStopsRemovesAndDeletesWorkingDir(t *testing.T) {
	workDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, "world.dat"), []byte("world"), 0o644); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "pulse.instances.json")

	manager := mcserver.NewManager([]mcserver.InstanceConfig{{
		ID:         "survival",
		Command:    []string{"sh", "-c", "sleep 5"},
		WorkingDir: workDir,
	}})
	if err := manager.Start("survival"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !manager.IsRunning("survival") {
		t.Fatal("expected instance to be running before delete")
	}

	result := executeDeleteInstance(manager, configPath, "", protocol.Command{ID: "cmd_1", Type: "delete_instance", InstanceID: "survival"})
	if !result.Success {
		t.Fatalf("executeDeleteInstance failed: %s", result.Message)
	}

	if manager.IsRunning("survival") {
		t.Fatal("expected instance to be stopped before deletion")
	}
	if _, ok := manager.InstanceConfig("survival"); ok {
		t.Fatal("expected instance to no longer be tracked after delete")
	}
	if _, err := os.Stat(workDir); !os.IsNotExist(err) {
		t.Fatalf("expected working_dir to be deleted, stat err: %v", err)
	}

	loaded, _, err := mcserver.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if len(loaded) != 0 {
		t.Fatalf("expected an empty persisted instance list, got %+v", loaded)
	}
}

func TestExecuteDeleteInstanceOnStoppedInstance(t *testing.T) {
	workDir := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "pulse.instances.json")

	manager := mcserver.NewManager([]mcserver.InstanceConfig{{
		ID:         "survival",
		Command:    []string{"sh", "-c", "sleep 5"},
		WorkingDir: workDir,
	}})
	if manager.IsRunning("survival") {
		t.Fatal("expected instance to start out stopped")
	}

	result := executeDeleteInstance(manager, configPath, "", protocol.Command{ID: "cmd_1", Type: "delete_instance", InstanceID: "survival"})
	if !result.Success {
		t.Fatalf("executeDeleteInstance failed: %s", result.Message)
	}
	if _, err := os.Stat(workDir); !os.IsNotExist(err) {
		t.Fatalf("expected working_dir to be deleted, stat err: %v", err)
	}
}

func TestExecuteDeleteInstanceUnknownInstance(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "pulse.instances.json")
	manager := mcserver.NewManager(nil)

	result := executeDeleteInstance(manager, configPath, "", protocol.Command{ID: "cmd_1", Type: "delete_instance", InstanceID: "does-not-exist"})
	if result.Success {
		t.Fatal("expected deleting an unknown instance to fail")
	}
}
