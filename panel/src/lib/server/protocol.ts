// Mirrors pulse/internal/protocol/types.go — the wire shape of Pulse's
// heartbeat/poll HTTP contract, from Panel's side.

export interface EnrollRequestBody {
	hostname: string;
	os: string;
	arch: string;
	pulse_version: string;
}

export interface EnrollResponseBody {
	device_id: string;
	device_credential: string;
}

export interface DiskUsage {
	mount: string;
	total_bytes: number;
	used_bytes: number;
}

export interface HostMetrics {
	cpu_usage_percent: number;
	cpu_cores: number;
	ram_total_bytes: number;
	ram_used_bytes: number;
	disks: DiskUsage[] | null;
	os: string;
	platform: string;
}

export type RunningState = 'stopped' | 'starting' | 'running' | 'stopping' | 'crashed';

export interface InstanceStatus {
	id: string;
	name: string;
	game_platform: string;
	version: string;
	software_type: string;
	running_state: RunningState;
	tps?: number;
	player_count: number;
	players: string[] | null;
	world_size_bytes: number;
	uptime_seconds: number;
	// Only populated for instances Pulse itself provisioned via
	// create_instance — legacy hand-configured instances never report one.
	port?: number;
}

export interface CommandResult {
	command_id: string;
	success: boolean;
	message?: string;
	// Set on a successful backup_instance result; also set on a successful
	// upload_file result (bytes written).
	size_bytes?: number;
	checksum?: string; // sha256 hex
	// Free-text carrying a command's actual response, reused across three
	// unrelated command types: RCON response text for console_command
	// (populated whenever the RCON round-trip itself succeeded, even if the
	// game rejected the command); raw server.properties content for
	// read_properties; a JSON-encoded FileEntry[] for list_files. message
	// stays reserved for "the command itself failed" in all three cases.
	output?: string;
}

// Wire shape of a filemanager.Entry — one row in a list_files result.
export interface FileEntry {
	name: string;
	path: string;
	is_dir: boolean;
	size_bytes: number;
	mod_time_ms: number;
}

// Payload shape for backup_instance / delete_backup / push_backup commands.
// Panel always generates backup_id before queuing.
export interface BackupCommandPayload {
	backup_id: string;
}

// Payload shape for restore_backup. Panel also pregenerates
// safety_backup_id for the automatic pre-restore backup.
export interface RestoreCommandPayload {
	backup_id: string;
	safety_backup_id: string;
}

// Payload shape for create_instance. Panel fully resolves the version/
// download URL/required Java version before ever queuing this — Pulse
// does zero version or software-catalog resolution itself.
export interface CreateInstanceCommandPayload {
	name: string;
	game_platform: string;
	version: string;
	software_type: string;
	download_url: string;
	// Omitted for bedrock — no JVM involved.
	java_major_version?: number;
	port: number;
	working_dir: string;
}

// Reports that a long-running command (currently only create_instance) is
// still in flight and roughly what it's doing — distinct from
// CommandResult, which is always terminal.
export interface CommandProgress {
	command_id: string;
	phase: string;
}

// Payload shape for console_command — an arbitrary admin command sent
// verbatim to the instance's RCON port.
export interface ConsoleCommandPayload {
	command: string;
}

// Payload shape for write_properties — the full, verbatim replacement
// contents for server.properties. read_properties needs no payload; its
// result comes back in CommandResult.output.
export interface WritePropertiesCommandPayload {
	content: string;
}

// Payload shape for list_files — the working_dir-relative directory to
// list ('' or '.' for the root).
export interface ListFilesCommandPayload {
	path: string;
}

// Payload shape for upload_file. target_path is the full working_dir-
// relative destination (directory + filename); holding_id is the
// fileUploads row id Pulse fetches the bytes from.
export interface UploadFileCommandPayload {
	target_path: string;
	holding_id: string;
}

// Payload shape for delete_file — the working_dir-relative file or
// directory to remove (recursively, if a directory).
export interface DeleteFileCommandPayload {
	path: string;
}

export interface HeartbeatRequestBody {
	device_id: string;
	timestamp: number;
	pulse_version: string;
	host: HostMetrics;
	instances: InstanceStatus[] | null;
	// Agent's configured --interval, so Panel can show a "next heartbeat"
	// countdown instead of guessing a fixed default.
	interval_seconds: number;
	pending_command_results?: CommandResult[] | null;
	in_progress_commands?: CommandProgress[] | null;
}

export interface WireCommand {
	id: string;
	type: string;
	instance_id: string;
	payload?: unknown;
}

export interface HeartbeatResponseBody {
	commands?: WireCommand[];
}
