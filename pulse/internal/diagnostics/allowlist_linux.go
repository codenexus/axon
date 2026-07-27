//go:build linux

package diagnostics

// allowlist maps a friendly, cross-platform diagnostic name to its base
// argv on Linux. Extra admin-supplied arguments (Run's argsStr) are
// appended after these.
var allowlist = map[string][]string{
	"uptime":     {"uptime"},
	"disk_usage": {"df", "-h"},
	"memory":     {"free", "-h"},
	"processes":  {"ps", "aux"},
}
