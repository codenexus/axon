package mcserver

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// SaveConfig atomically rewrites the instance config file at path with the
// given instances + backupsDir, in the exact shape LoadConfig reads back.
// Writes to a temp file in the same directory, then os.Rename over the
// original — an os.Rename within one directory is atomic on both POSIX and
// Windows, so a crash mid-write can never leave path holding a corrupt or
// half-written config (the same atomic-swap philosophy CLAUDE.md documents
// for the still-unbuilt self-update feature's binary swap).
func SaveConfig(path string, instances []InstanceConfig, backupsDir string) error {
	cfg := fileConfig{Instances: instances, BackupsDir: backupsDir}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal instance config: %w", err)
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".pulse.instances.*.json.tmp")
	if err != nil {
		return fmt.Errorf("create temp config file: %w", err)
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write temp config file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp config file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("replace config file: %w", err)
	}
	return nil
}
