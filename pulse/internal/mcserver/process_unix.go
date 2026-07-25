//go:build !windows

package mcserver

import (
	"os/exec"
	"syscall"
)

// setProcAttrs puts the child in its own process group so a future
// "stop everything" doesn't also signal Pulse itself.
func setProcAttrs(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// terminate sends SIGTERM to the process group. This is the fallback used
// when RCON graceful stop (rcon_stop.go) isn't available — e.g. RCON
// disabled, unconfigured, or unreachable. Targets the process group (-pid)
// rather than just pid because setProcAttrs makes every Pulse-spawned
// process its own group leader at creation time — true whether we're
// signaling a process we just spawned or one adopted via Reconcile, since
// group membership doesn't change when the original spawning Pulse process
// exits and the child gets reparented to init.
func terminate(pid int) error {
	return syscall.Kill(-pid, syscall.SIGTERM)
}
