package mcserver

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestAddInstancePersistsAndRegisters(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "pulse.instances.json")

	m := NewManager(nil)
	cfg := InstanceConfig{
		ID:           "new-inst",
		Name:         "New Instance",
		GamePlatform: "bedrock",
		Version:      "1.21.0",
		SoftwareType: "vanilla",
		Command:      []string{"./bedrock_server"},
		WorkingDir:   filepath.Join(dir, "new-inst"),
		Env:          []string{"LD_LIBRARY_PATH=."},
		Port:         19133,
	}

	if err := m.AddInstance(cfg, configPath, ""); err != nil {
		t.Fatalf("AddInstance: %v", err)
	}

	got, ok := m.InstanceConfig("new-inst")
	if !ok {
		t.Fatal("expected new-inst to be registered in memory")
	}
	if got.Port != 19133 || len(got.Env) != 1 {
		t.Fatalf("in-memory config missing Env/Port: %+v", got)
	}

	loaded, _, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if len(loaded) != 1 || loaded[0].ID != "new-inst" || loaded[0].Port != 19133 {
		t.Fatalf("unexpected persisted config: %+v", loaded)
	}
}

func TestAddInstanceRejectsDuplicateID(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "pulse.instances.json")
	existing := InstanceConfig{ID: "dupe", WorkingDir: dir, Command: []string{"sh"}}
	m := NewManager([]InstanceConfig{existing})
	if err := SaveConfig(configPath, []InstanceConfig{existing}, ""); err != nil {
		t.Fatalf("seed SaveConfig: %v", err)
	}

	err := m.AddInstance(InstanceConfig{ID: "dupe", WorkingDir: dir}, configPath, "")
	if err == nil {
		t.Fatal("expected an error adding a duplicate ID")
	}
}

func TestAddInstanceRollsBackOnWriteFailure(t *testing.T) {
	m := NewManager(nil)
	cfg := InstanceConfig{ID: "will-fail", WorkingDir: t.TempDir()}

	// A configPath under a nonexistent parent directory can't be written to,
	// so SaveConfig's temp-file creation fails deterministically.
	badPath := filepath.Join(t.TempDir(), "nonexistent-subdir", "pulse.instances.json")

	if err := m.AddInstance(cfg, badPath, ""); err == nil {
		t.Fatal("expected an error from a bad configPath")
	}
	if _, ok := m.InstanceConfig("will-fail"); ok {
		t.Fatal("expected the in-memory insert to be rolled back after a failed persist")
	}
}

func TestAddInstanceConcurrentAddsProduceValidConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "pulse.instances.json")
	if err := SaveConfig(configPath, nil, ""); err != nil {
		t.Fatalf("seed SaveConfig: %v", err)
	}

	m := NewManager(nil)
	const n = 20
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cfg := InstanceConfig{
				ID:         fmt.Sprintf("inst-%d", i),
				WorkingDir: dir,
				Command:    []string{"sh"},
			}
			errs[i] = m.AddInstance(cfg, configPath, "")
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("AddInstance(%d): %v", i, err)
		}
	}

	loaded, _, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig after concurrent adds: %v", err)
	}
	if len(loaded) != n {
		t.Fatalf("expected %d persisted instances, got %d", n, len(loaded))
	}

	// LoadConfig succeeding already proves the file is valid JSON; re-parse
	// directly too, for a clearer failure message if this ever regresses.
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config file: %v", err)
	}
	var raw json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("config file is not valid JSON: %v", err)
	}
}
