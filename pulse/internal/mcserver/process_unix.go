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

// terminate sends SIGTERM to the process group. This is a placeholder for
// graceful shutdown — once RCON lands, stopping a real Minecraft server
// should issue "save-all" + "stop" instead of a bare signal.
func terminate(cmd *exec.Cmd) error {
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
}
