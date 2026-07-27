// Package diagnostics runs a fixed, hand-maintained allowlist of
// read-only host-level commands on request — distinct from mcserver's
// RCON console (which talks to the Minecraft process, not the host OS)
// and from inventory (which passively collects metrics every heartbeat
// rather than running anything on demand). The admin picks a friendly
// name from a small fixed set (see the per-OS allowlist files); Pulse
// maps that name to the real, OS-appropriate command itself, the same
// "Panel stays dumb, Pulse knows its own platform" philosophy as
// javaruntime's package-name table.
//
// Deliberately not arbitrary command execution: only a name already
// present in the platform's allowlist can ever run, and extra
// admin-supplied arguments are appended to that fixed base command via
// a real argv slice (os/exec, never a shell string) — the same
// safe-invocation convention used throughout this codebase.
package diagnostics

import (
	"fmt"
	"os/exec"
	"strings"
)

// maxOutputBytes bounds how much combined stdout+stderr a single
// diagnostic can return, so a runaway or unexpectedly chatty command
// can't bloat a heartbeat payload.
const maxOutputBytes = 64 * 1024

// Run executes the named allowlisted diagnostic, appending any
// whitespace-split extra arguments to its fixed base command. Returns an
// error only if name isn't in the allowlist — a nonzero exit code from
// the command itself is not an error, its output (whatever it printed)
// is still returned, matching console_command's "Success reflects
// whether the mechanism worked, not whether the callee liked it" split.
func Run(name, argsStr string) (string, error) {
	base, ok := allowlist[name]
	if !ok {
		return "", fmt.Errorf("unknown diagnostic %q", name)
	}

	args := append([]string{}, base[1:]...)
	if extra := strings.Fields(argsStr); len(extra) > 0 {
		args = append(args, extra...)
	}

	cmd := exec.Command(base[0], args...)
	output, _ := cmd.CombinedOutput()

	if len(output) > maxOutputBytes {
		output = append(output[:maxOutputBytes], []byte("\n... (truncated)")...)
	}
	return string(output), nil
}

// Names returns the allowlisted diagnostic names for this platform, for
// tests and any future self-describing use — Panel today just hardcodes
// the same fixed 4-name set on its own side, matching this platform's
// list is Pulse's responsibility, not something negotiated on the wire.
func Names() []string {
	names := make([]string, 0, len(allowlist))
	for name := range allowlist {
		names = append(names, name)
	}
	return names
}
