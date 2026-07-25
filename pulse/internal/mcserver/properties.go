package mcserver

import (
	"bufio"
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
