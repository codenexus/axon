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
}

export interface CommandResult {
	command_id: string;
	success: boolean;
	message?: string;
	// Set on a successful backup_instance result.
	size_bytes?: number;
	checksum?: string; // sha256 hex
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
