//go:build darwin

package diagnostics

// allowlist maps a friendly, cross-platform diagnostic name to its base
// argv on macOS. Extra admin-supplied arguments (Run's argsStr) are
// appended after these. macOS has no `free` equivalent — `vm_stat`
// reports page-level memory stats instead, less directly readable but
// the closest stdlib-free option without adding a new dependency.
var allowlist = map[string][]string{
	"uptime":     {"uptime"},
	"disk_usage": {"df", "-h"},
	"memory":     {"vm_stat"},
	"processes":  {"ps", "aux"},
}
