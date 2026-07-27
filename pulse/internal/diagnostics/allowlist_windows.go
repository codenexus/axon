//go:build windows

package diagnostics

// allowlist maps a friendly, cross-platform diagnostic name to its base
// argv on Windows. Extra admin-supplied arguments (Run's argsStr) are
// appended after these — for the PowerShell-based entries that mostly
// just tacks extra text onto an already-complete -Command string, a
// rough edge specific to Windows this codebase can't exercise directly
// (no Windows machine in the environment this was written in — same
// caveat as self-update's Windows swap logic).
var allowlist = map[string][]string{
	"uptime":     {"powershell", "-NoProfile", "-Command", "(Get-CimInstance Win32_OperatingSystem).LastBootUpTime"},
	"disk_usage": {"powershell", "-NoProfile", "-Command", "Get-PSDrive -PSProvider FileSystem | Format-Table -AutoSize"},
	"memory":     {"powershell", "-NoProfile", "-Command", "Get-CimInstance Win32_OperatingSystem | Select-Object FreePhysicalMemory,TotalVisibleMemorySize"},
	"processes":  {"tasklist"},
}
