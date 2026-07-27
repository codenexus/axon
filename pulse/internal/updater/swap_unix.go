//go:build !windows

package updater

import (
	"os"
	"syscall"
)

// atomicSwap replaces the running executable with newPath and restarts.
// On Unix, os.Rename over the running binary is safe — the kernel keeps
// the old inode alive for the current process via its open file
// descriptor.
func atomicSwap(exe, newPath, backupPath string) error {
	// Backup current binary so rollback is possible.
	if err := copyFile(exe, backupPath); err != nil {
		return err
	}

	// Atomic replace: rename(2) is atomic on the same filesystem.
	if err := os.Rename(newPath, exe); err != nil {
		// Clean up backup to avoid confusion.
		os.Remove(backupPath)
		return err
	}

	// Ensure the new binary is executable.
	if err := os.Chmod(exe, 0o755); err != nil {
		return err
	}

	// Replace the current process image with the new binary. os.Args and
	// os.Environ() carry the original flags and environment forward — the
	// new process runs main() from scratch (same PID), which is exactly
	// what Manager.Reconcile() already exists to recover from: it re-adopts
	// any still-running Minecraft server process this Pulse process lost
	// track of by restarting.
	return syscall.Exec(exe, os.Args, os.Environ())
}

// rollback swaps the backup binary back into place and restarts.
func rollback(exe, backupPath string) error {
	if err := os.Rename(backupPath, exe); err != nil {
		return err
	}
	if err := os.Chmod(exe, 0o755); err != nil {
		return err
	}
	return syscall.Exec(exe, os.Args, os.Environ())
}
