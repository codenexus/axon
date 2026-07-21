// Package credential persists the device credential issued at enrollment.
package credential

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
)

type Credential struct {
	DeviceID         string `json:"device_id"`
	DeviceCredential string `json:"device_credential"`
	ServerURL        string `json:"server_url"`
}

// Dir returns the platform-appropriate directory for Pulse's persisted
// state (credential.json, and later update-state.json / agent.log),
// mirroring Beacon agent's credential.Dir() convention.
func Dir() string {
	switch runtime.GOOS {
	case "windows":
		if pd := os.Getenv("PROGRAMDATA"); pd != "" {
			return filepath.Join(pd, "Axon")
		}
	case "darwin":
		if os.Geteuid() == 0 {
			return "/Library/Application Support/Axon"
		}
	default: // linux and other unix
		if os.Geteuid() == 0 {
			return "/etc/axon"
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".config", "axon")
}

func path() string {
	return filepath.Join(Dir(), "credential.json")
}

func Load() (*Credential, error) {
	data, err := os.ReadFile(path())
	if err != nil {
		return nil, err
	}
	var c Credential
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func Save(c *Credential) error {
	dir := Dir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path(), data, 0o600)
}

// IsNotEnrolled reports whether err indicates no credential has been saved yet.
func IsNotEnrolled(err error) bool {
	return errors.Is(err, os.ErrNotExist)
}
