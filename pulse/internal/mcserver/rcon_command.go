package mcserver

import (
	"fmt"
	"time"

	"github.com/codenexus/axon/pulse/internal/protocol"
	"github.com/codenexus/axon/pulse/internal/rcon"
)

// RunConsoleCommand sends an arbitrary command to a running instance's RCON
// port and returns its text response. Unlike gracefulStop, there is no
// meaningful fallback for an arbitrary command if RCON isn't usable — this
// fails cleanly instead of attempting anything else. A non-empty response
// body on a command the game itself rejected (e.g. "Unknown command") is
// still a successful RCON round-trip: the returned error is only non-nil
// when the RCON exchange itself couldn't happen (not running, not
// configured, unreachable, bad auth).
func (m *Manager) RunConsoleCommand(id, command string) (string, error) {
	m.mu.RLock()
	inst, ok := m.instances[id]
	m.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("unknown instance %q", id)
	}

	inst.mu.Lock()
	running := inst.state == protocol.StateRunning
	workingDir := inst.cfg.WorkingDir
	inst.mu.Unlock()
	if !running {
		return "", fmt.Errorf("instance %q is not running", id)
	}

	cfg, ok := ReadRCONConfig(workingDir)
	if !ok {
		return "", fmt.Errorf("RCON is not enabled/configured for instance %q", id)
	}

	client, err := rcon.Dial(fmt.Sprintf("127.0.0.1:%d", cfg.Port), rconDialTimeout)
	if err != nil {
		return "", fmt.Errorf("connect to RCON: %w", err)
	}
	defer client.Close()

	client.SetDeadline(time.Now().Add(rconIOTimeout))
	if err := client.Authenticate(cfg.Password); err != nil {
		return "", fmt.Errorf("RCON authentication failed: %w", err)
	}

	output, err := client.Execute(command)
	if err != nil {
		return "", fmt.Errorf("execute command: %w", err)
	}
	return output, nil
}
