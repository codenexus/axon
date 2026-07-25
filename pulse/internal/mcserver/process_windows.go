//go:build windows

package mcserver

import (
	"os"
	"os/exec"
)

func setProcAttrs(cmd *exec.Cmd) {}

// terminate kills the process outright. Windows has no SIGTERM equivalent
// without additional console-event plumbing. Like the Unix path, this is
// only the fallback for when RCON graceful stop (rcon_stop.go) isn't
// available. Takes a bare pid (via os.FindProcess) rather than an
// *exec.Cmd so it works identically for a process adopted via Reconcile,
// which only ever has a pid.
func terminate(pid int) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return proc.Kill()
}
