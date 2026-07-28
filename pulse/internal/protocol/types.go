// Package protocol defines the wire types shared between Pulse and Panel's
// HTTP heartbeat/poll contract.
package protocol

import "encoding/json"

type EnrollRequest struct {
	Hostname     string `json:"hostname"`
	OS           string `json:"os"`
	Arch         string `json:"arch"`
	PulseVersion string `json:"pulse_version"`
}

type EnrollResponse struct {
	DeviceID         string `json:"device_id"`
	DeviceCredential string `json:"device_credential"`
}

type DiskUsage struct {
	Mount      string `json:"mount"`
	TotalBytes uint64 `json:"total_bytes"`
	UsedBytes  uint64 `json:"used_bytes"`
}

type HostMetrics struct {
	CPUUsagePercent float64     `json:"cpu_usage_percent"`
	CPUCores        int         `json:"cpu_cores"`
	RAMTotalBytes   uint64      `json:"ram_total_bytes"`
	RAMUsedBytes    uint64      `json:"ram_used_bytes"`
	Disks           []DiskUsage `json:"disks"`
	OS              string      `json:"os"`
	Platform        string      `json:"platform"`
	// UptimeSeconds is the host's own uptime (time since boot), not a
	// Minecraft instance's — see InstanceStatus.UptimeSeconds for that.
	UptimeSeconds uint64 `json:"uptime_seconds"`
}

type RunningState string

const (
	StateStopped  RunningState = "stopped"
	StateStarting RunningState = "starting"
	StateRunning  RunningState = "running"
	StateStopping RunningState = "stopping"
	StateCrashed  RunningState = "crashed"
)

// InstanceStatus is the per-Minecraft-server-instance payload reported on
// every heartbeat. Fields beyond identity/state (TPS, players, world size)
// are best-effort and only populated once RCON/log-parsing lands; they are
// present in the wire shape now so Panel's schema doesn't need to change
// when that happens.
type InstanceStatus struct {
	ID             string       `json:"id"`
	Name           string       `json:"name"`
	GamePlatform   string       `json:"game_platform"` // "java" | "bedrock"
	Version        string       `json:"version"`
	SoftwareType   string       `json:"software_type"` // "vanilla" for v1
	RunningState   RunningState `json:"running_state"`
	TPS            *float64     `json:"tps,omitempty"`
	PlayerCount    int          `json:"player_count"`
	Players        []string     `json:"players"`
	WorldSizeBytes int64        `json:"world_size_bytes"`
	UptimeSeconds  int64        `json:"uptime_seconds"`
	// Port is only populated for instances Pulse itself provisioned via
	// create_instance — legacy hand-configured instances never report one,
	// since their launch command/server.properties were never parsed for it.
	Port int `json:"port,omitempty"`
}

type HeartbeatRequest struct {
	DeviceID     string           `json:"device_id"`
	Timestamp    int64            `json:"timestamp"`
	PulseVersion string           `json:"pulse_version"`
	Host         HostMetrics      `json:"host"`
	Instances    []InstanceStatus `json:"instances"`
	// IntervalSeconds is this agent's configured --interval (the sleep
	// between heartbeats), so Panel can show a "next heartbeat in ~Ns"
	// countdown rather than guessing a fixed default — it's a per-agent
	// CLI flag, otherwise invisible to Panel.
	IntervalSeconds int `json:"interval_seconds"`
	// PendingCommandResults are results from previously-polled commands,
	// piggybacked onto the next heartbeat rather than pushed immediately
	// (see spec open question #3 — resolved as next-poll-cycle for v1).
	PendingCommandResults []CommandResult `json:"pending_command_results,omitempty"`
	// InProgressCommands reports a coarse phase for commands still running
	// across multiple heartbeat cycles (currently only create_instance,
	// which can take minutes — installing Java, downloading a server jar/
	// zip). Every other command type completes within a single heartbeat
	// and never appears here.
	InProgressCommands []CommandProgress `json:"in_progress_commands,omitempty"`
}

// CommandProgress reports that a long-running command is still in flight
// and roughly what it's doing — distinct from CommandResult, which is
// always terminal. Panel must never treat a progress report as resolving
// the command it names.
type CommandProgress struct {
	CommandID string `json:"command_id"`
	Phase     string `json:"phase"` // "preparing" | "installing_java" | "downloading" | "installing_loader" | "configuring" | "registering"
}

type CommandResult struct {
	CommandID string `json:"command_id"`
	Success   bool   `json:"success"`
	Message   string `json:"message,omitempty"`
	// SizeBytes and Checksum are set on a successful backup_instance result;
	// SizeBytes is also set on a successful upload_file result (the number
	// of bytes Save() wrote).
	SizeBytes int64  `json:"size_bytes,omitempty"`
	Checksum  string `json:"checksum,omitempty"` // sha256 hex
	// Output is free-text carrying a command's actual response, reused
	// across four unrelated command types rather than each growing its own
	// single-purpose field: RCON response text for console_command
	// (populated whenever the RCON round-trip itself succeeded, even if the
	// game rejected the command — e.g. "Unknown command" is still a
	// successful exchange); raw server.properties content for
	// read_properties; a JSON-encoded []filemanager.Entry for list_files;
	// combined stdout+stderr for run_diagnostic (populated whenever the
	// diagnostic name was recognized, even if the underlying command itself
	// exited nonzero). Message stays reserved for "the command itself
	// failed" in all four cases.
	Output string `json:"output,omitempty"`
}

type Command struct {
	ID   string `json:"id"`
	Type string `json:"type"` // "start_instance" | "stop_instance" | "restart_instance" | "backup_instance" | "restore_backup" | "delete_backup" | "push_backup" | "create_instance" | "delete_instance" | "console_command" | "read_properties" | "write_properties" | "list_files" | "upload_file" | "delete_file" | "run_diagnostic"
	// InstanceID is the id of an existing instance the command targets, for
	// every type except create_instance — there, Panel pregenerates the id
	// of the instance being created (which doesn't exist yet), following
	// the same Panel-owned-id convention as BackupID.
	InstanceID string          `json:"instance_id"`
	Payload    json.RawMessage `json:"payload,omitempty"`
}

// BackupCommandPayload is the Payload shape for "backup_instance",
// "delete_backup", and "push_backup". Panel always generates BackupID
// before queuing the command; Pulse uses it verbatim as the on-disk
// filename stem, never inventing its own id.
type BackupCommandPayload struct {
	BackupID string `json:"backup_id"`
}

// RestoreCommandPayload is the Payload shape for "restore_backup". Panel
// also pregenerates SafetyBackupID for the automatic pre-restore backup, so
// Pulse never invents ids itself.
type RestoreCommandPayload struct {
	BackupID       string `json:"backup_id"`
	SafetyBackupID string `json:"safety_backup_id"`
}

// CreateInstanceCommandPayload is the Payload shape for "create_instance".
// Panel fully resolves the version/download URL/required Java version
// before ever queuing this — Pulse does zero version or software-catalog
// resolution itself, it just downloads and configures whatever it's told.
type CreateInstanceCommandPayload struct {
	Name         string `json:"name"`
	GamePlatform string `json:"game_platform"` // "java" | "bedrock"
	Version      string `json:"version"`
	SoftwareType string `json:"software_type"` // "vanilla" | "paper" | "fabric" | "forge" (java); "vanilla" (bedrock)
	DownloadURL  string `json:"download_url"`
	// JavaMajorVersion is 0/omitted for bedrock — no JVM involved.
	JavaMajorVersion int `json:"java_major_version,omitempty"`
	// LoaderVersion is the Fabric loader version (independent of the
	// Minecraft version) — only set when SoftwareType is "fabric", empty
	// otherwise. Forge's installer needs no such argument (its version is
	// already baked into the installer jar's own download URL).
	LoaderVersion string `json:"loader_version,omitempty"`
	Port          int    `json:"port"`
	WorkingDir    string `json:"working_dir"`
	// JavaHeapMB is the -Xmx applied at launch, java only. 0/omitted falls
	// back to provision.DefaultJavaHeapMB — kept optional rather than
	// required so an older Panel (or a payload built by hand/in a test)
	// still produces a working launch command.
	JavaHeapMB int `json:"java_heap_mb,omitempty"`
	// Gamemode/Difficulty/MaxPlayers share the same server.properties key
	// on both editions. Motd is edition-mapped by Configure() itself
	// ("motd" for java, "server-name" for bedrock — Bedrock has no "motd"
	// key) so Panel only ever has to think in terms of one concept. All
	// four are optional: an empty/zero value here means "don't write this
	// key at all", leaving the server's own built-in default from its
	// first real launch untouched, exactly like today's behavior for
	// every key this payload doesn't mention.
	Gamemode   string `json:"gamemode,omitempty"`
	Difficulty string `json:"difficulty,omitempty"`
	MaxPlayers int    `json:"max_players,omitempty"`
	Motd       string `json:"motd,omitempty"`
}

// ConsoleCommandPayload is the Payload shape for "console_command" — an
// arbitrary admin command sent verbatim to the instance's RCON port.
type ConsoleCommandPayload struct {
	Command string `json:"command"`
}

// WritePropertiesCommandPayload is the Payload shape for
// "write_properties" — the full, verbatim replacement contents for
// server.properties. "read_properties" needs no payload (like
// start_instance/stop_instance); its result comes back in
// CommandResult.Output.
type WritePropertiesCommandPayload struct {
	Content string `json:"content"`
}

// ListFilesCommandPayload is the Payload shape for "list_files" — the
// working_dir-relative directory to list ("" or "." for the root).
type ListFilesCommandPayload struct {
	Path string `json:"path"`
}

// UploadFileCommandPayload is the Payload shape for "upload_file".
// TargetPath is the full working_dir-relative destination (directory +
// filename); HoldingID is the fileUploads row id Pulse fetches the bytes
// from via PullFileUpload.
type UploadFileCommandPayload struct {
	TargetPath string `json:"target_path"`
	HoldingID  string `json:"holding_id"`
}

// DeleteFileCommandPayload is the Payload shape for "delete_file" — the
// working_dir-relative file or directory to remove (recursively, if a
// directory).
type DeleteFileCommandPayload struct {
	Path string `json:"path"`
}

// RunDiagnosticCommandPayload is the Payload shape for "run_diagnostic" —
// Name must match one of the fixed, hand-maintained allowlist entries in
// pulse/internal/diagnostics (a friendly, cross-platform name like
// "uptime" or "disk_usage", never a raw command); Args is optional
// whitespace-split extra arguments appended to that fixed base command.
// This is a host-level command, unrelated to console_command's RCON
// round-trip into the Minecraft process.
type RunDiagnosticCommandPayload struct {
	Name string `json:"name"`
	Args string `json:"args,omitempty"`
}

// UpdateInfo is set on a HeartbeatResponse whenever Panel has a published
// release for this agent's os/arch that differs from what it just
// reported — Pulse verifies and applies it, never trusting Panel's say-so
// alone (updater.VerifyBinary is the actual authority).
type UpdateInfo struct {
	Version      string `json:"version"`
	DownloadURL  string `json:"download_url"`
	SignatureHex string `json:"signature_hex"`
}

type HeartbeatResponse struct {
	Commands []Command   `json:"commands,omitempty"`
	Update   *UpdateInfo `json:"update,omitempty"`
}
