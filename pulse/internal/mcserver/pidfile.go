package mcserver

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/shirou/gopsutil/v3/process"
)

// pidFileData is written to disk whenever Start() spawns a process, and
// read back on Reconcile so a freshly (re)started Pulse process can adopt
// an instance that's still actually running from before — see Reconcile's
// doc comment for why this matters.
type pidFileData struct {
	PID     int      `json:"pid"`
	Command []string `json:"command"`
}

func pidFilePath(workingDir string) string {
	return filepath.Join(workingDir, ".pulse-pid")
}

func writePIDFile(workingDir string, pid int, command []string) {
	data, err := json.Marshal(pidFileData{PID: pid, Command: command})
	if err != nil {
		return
	}
	// Best-effort: a failure here only means a future Pulse restart won't
	// be able to reconcile this instance, not that Start() itself failed.
	_ = os.WriteFile(pidFilePath(workingDir), data, 0o644)
}

func readPIDFile(workingDir string) (pidFileData, bool) {
	data, err := os.ReadFile(pidFilePath(workingDir))
	if err != nil {
		return pidFileData{}, false
	}
	var pf pidFileData
	if err := json.Unmarshal(data, &pf); err != nil {
		return pidFileData{}, false
	}
	return pf, pf.PID > 0
}

func removePIDFile(workingDir string) {
	os.Remove(pidFilePath(workingDir))
}

// processAlive reports whether pid currently identifies a live process.
// Uses gopsutil (already a dependency via internal/inventory) rather than
// hand-rolled platform-specific syscalls, since it already abstracts this
// correctly across the same OSes Pulse targets (process_unix.go /
// process_windows.go).
func processAlive(pid int) bool {
	proc, err := process.NewProcess(int32(pid))
	if err != nil {
		return false
	}
	running, err := proc.IsRunning()
	return err == nil && running
}

// processMatches reports whether pid is alive and its command plausibly
// matches wantCommand — a best-effort fingerprint (comparing just the
// executable's base name) to guard against adopting an unrelated process
// that happens to have reused the same PID after a reboot. If the
// process's command line can't be read (permissions, platform quirks),
// liveness alone is treated as a match rather than refusing to reconcile a
// real instance.
func processMatches(pid int, wantCommand []string) bool {
	if len(wantCommand) == 0 {
		return false
	}
	proc, err := process.NewProcess(int32(pid))
	if err != nil {
		return false
	}
	running, err := proc.IsRunning()
	if err != nil || !running {
		return false
	}
	cmdline, err := proc.CmdlineSlice()
	if err != nil || len(cmdline) == 0 {
		return true
	}
	return filepath.Base(cmdline[0]) == filepath.Base(wantCommand[0])
}
