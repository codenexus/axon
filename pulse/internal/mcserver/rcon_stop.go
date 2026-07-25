package mcserver

import (
	"fmt"
	"time"

	"github.com/codenexus/axon/pulse/internal/rcon"
)

const (
	rconDialTimeout = 2 * time.Second
	rconIOTimeout   = 2 * time.Second
)

// gracefulStop asks a running instance to shut down cleanly via RCON
// ("save-all" then "stop"), matching the spec's requirement to work
// uniformly across Vanilla/Paper/Forge/Fabric rather than piping stdin.
// Falls back to the OS-level terminate() signal whenever RCON isn't usable
// (disabled, unconfigured, or unreachable) — e.g. every non-Minecraft
// stand-in process used in tests. Takes a bare pid rather than an
// *exec.Cmd so it works identically for a process this Manager spawned
// itself and one adopted via Reconcile (which only ever has a pid, never a
// Cmd — see Reconcile's doc comment).
func gracefulStop(workingDir string, pid int) error {
	cfg, ok := ReadRCONConfig(workingDir)
	if !ok {
		return terminate(pid)
	}

	client, err := rcon.Dial(fmt.Sprintf("127.0.0.1:%d", cfg.Port), rconDialTimeout)
	if err != nil {
		return terminate(pid)
	}
	defer client.Close()

	client.SetDeadline(time.Now().Add(rconIOTimeout))
	if err := client.Authenticate(cfg.Password); err != nil {
		return terminate(pid)
	}

	// Best-effort: the server should exit on its own once "stop" lands;
	// Manager's existing cmd.Wait() goroutine picks up the resulting exit.
	client.Execute("save-all")
	client.Execute("stop")
	return nil
}
