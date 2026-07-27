package mcserver

import (
	"path/filepath"
	"testing"
)

func TestRemoveInstanceUntracksAndPersists(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "pulse.instances.json")
	cfg := InstanceConfig{
		ID:         "gone-soon",
		Name:       "Gone Soon",
		WorkingDir: filepath.Join(dir, "gone-soon"),
		Command:    []string{"sh", "-c", "sleep 5"},
	}

	m := NewManager(nil)
	if err := m.AddInstance(cfg, configPath, ""); err != nil {
		t.Fatalf("seed AddInstance: %v", err)
	}

	if err := m.RemoveInstance("gone-soon", configPath, ""); err != nil {
		t.Fatalf("RemoveInstance: %v", err)
	}

	if _, ok := m.InstanceConfig("gone-soon"); ok {
		t.Fatal("expected gone-soon to no longer be tracked in memory")
	}

	loaded, _, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if len(loaded) != 0 {
		t.Fatalf("expected an empty persisted instance list, got %+v", loaded)
	}
}

func TestRemoveInstanceLeavesOtherInstancesIntact(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "pulse.instances.json")
	keep := InstanceConfig{ID: "keep", WorkingDir: filepath.Join(dir, "keep"), Command: []string{"sh"}}
	remove := InstanceConfig{ID: "remove", WorkingDir: filepath.Join(dir, "remove"), Command: []string{"sh"}}

	m := NewManager(nil)
	if err := m.AddInstance(keep, configPath, ""); err != nil {
		t.Fatalf("seed AddInstance(keep): %v", err)
	}
	if err := m.AddInstance(remove, configPath, ""); err != nil {
		t.Fatalf("seed AddInstance(remove): %v", err)
	}

	if err := m.RemoveInstance("remove", configPath, ""); err != nil {
		t.Fatalf("RemoveInstance: %v", err)
	}

	if _, ok := m.InstanceConfig("keep"); !ok {
		t.Fatal("expected keep to still be tracked in memory")
	}
	if _, ok := m.InstanceConfig("remove"); ok {
		t.Fatal("expected remove to no longer be tracked in memory")
	}

	loaded, _, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if len(loaded) != 1 || loaded[0].ID != "keep" {
		t.Fatalf("unexpected persisted config: %+v", loaded)
	}
}

func TestRemoveInstanceUnknownID(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "pulse.instances.json")
	m := NewManager(nil)

	if err := m.RemoveInstance("does-not-exist", configPath, ""); err == nil {
		t.Fatal("expected an error removing an unknown instance id")
	}
}

func TestRemoveInstanceRollsBackOnWriteFailure(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "pulse.instances.json")
	cfg := InstanceConfig{ID: "stubborn", WorkingDir: dir, Command: []string{"sh"}}

	m := NewManager(nil)
	if err := m.AddInstance(cfg, configPath, ""); err != nil {
		t.Fatalf("seed AddInstance: %v", err)
	}

	// A configPath under a nonexistent parent directory can't be written to,
	// so SaveConfig's temp-file creation fails deterministically — mirrors
	// TestAddInstanceRollsBackOnWriteFailure's approach for the inverse op.
	badPath := filepath.Join(t.TempDir(), "nonexistent-subdir", "pulse.instances.json")

	if err := m.RemoveInstance("stubborn", badPath, ""); err == nil {
		t.Fatal("expected an error from a bad configPath")
	}
	if _, ok := m.InstanceConfig("stubborn"); !ok {
		t.Fatal("expected the in-memory removal to be rolled back after a failed persist")
	}
}
