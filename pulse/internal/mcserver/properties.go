package mcserver

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// RCONConfig holds the connection details needed to reach a running
// instance's RCON port.
type RCONConfig struct {
	Port     int
	Password string
}

// ReadRCONConfig reads server.properties in workingDir and returns the RCON
// port/password if RCON is enabled and configured with a non-empty
// password. An empty password makes real Minecraft servers refuse to
// actually start RCON even with enable-rcon=true, so that case is treated
// as unusable too. Returns ok=false whenever RCON isn't usable, so callers
// can fall back to a plain process signal — expected for the sh/sleep
// stand-ins used in tests and for any server run with RCON turned off.
//
// Exported so pulse/internal/backup can reuse the exact same
// credential-reading logic gracefulStop uses below, rather than
// duplicating it.
func ReadRCONConfig(workingDir string) (cfg RCONConfig, ok bool) {
	f, err := os.Open(filepath.Join(workingDir, "server.properties"))
	if err != nil {
		return RCONConfig{}, false
	}
	defer f.Close()

	props := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		props[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}

	if props["enable-rcon"] != "true" {
		return RCONConfig{}, false
	}
	password := props["rcon.password"]
	if password == "" {
		return RCONConfig{}, false
	}
	port, err := strconv.Atoi(props["rcon.port"])
	if err != nil || port <= 0 {
		return RCONConfig{}, false
	}
	return RCONConfig{Port: port, Password: password}, true
}

// WriteProperty creates server.properties at path if it doesn't exist yet
// (common right after provisioning — Java's server generates most of this
// file itself on first boot, Bedrock's zip ships a default one), or patches
// the single line for key if present, or appends a new key=value line if
// the key isn't already there. Comment lines and unrelated keys are left
// untouched.
//
// Exported so pulse/internal/provision can reuse it when configuring a
// freshly-downloaded server, rather than duplicating this line-based
// parse/rewrite logic.
func WriteProperty(path, key, value string) error {
	var lines []string
	found := false

	if data, err := os.ReadFile(path); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			trimmed := strings.TrimSpace(line)
			if !found && !strings.HasPrefix(trimmed, "#") {
				if k, _, ok := strings.Cut(trimmed, "="); ok && strings.TrimSpace(k) == key {
					line = key + "=" + value
					found = true
				}
			}
			lines = append(lines, line)
		}
		// Splitting a trailing-newline-terminated file leaves one empty
		// trailing element; drop it so we don't accumulate a blank line on
		// every rewrite.
		if len(lines) > 0 && lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	if !found {
		lines = append(lines, key+"="+value)
	}

	return os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

// ReadPropertiesFile returns the raw contents of server.properties in
// workingDir. Panel never parses this — a raw text editor, not a
// structured per-key form, so there's no schema here to keep in sync with
// Mojang's ever-changing property set.
func ReadPropertiesFile(workingDir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(workingDir, "server.properties"))
	if err != nil {
		return "", fmt.Errorf("read server.properties: %w", err)
	}
	return string(data), nil
}

// WritePropertiesFile atomically overwrites server.properties in workingDir
// with content exactly as given — unvalidated, same as ReadPropertiesFile
// not parsing it; Pulse doesn't check Minecraft property syntax either.
// Atomic (temp file + os.Rename, same directory) so a crash mid-write
// can't corrupt the file the running server depends on — same approach as
// SaveConfig (config_persist.go).
func WritePropertiesFile(workingDir, content string) error {
	path := filepath.Join(workingDir, "server.properties")
	tmp, err := os.CreateTemp(workingDir, ".server.properties.*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()

	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("replace server.properties: %w", err)
	}
	return nil
}
