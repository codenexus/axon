//go:build windows

package mcserver

import "os/exec"

func setProcAttrs(cmd *exec.Cmd) {}

// terminate kills the process outright. Windows has no SIGTERM equivalent
// without additional console-event plumbing; like the Unix path, this is a
// placeholder until RCON-based graceful shutdown ("save-all" + "stop") lands.
func terminate(cmd *exec.Cmd) error {
	return cmd.Process.Kill()
}
