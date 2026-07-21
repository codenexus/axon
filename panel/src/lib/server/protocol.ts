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
}

export interface HeartbeatRequestBody {
	device_id: string;
	timestamp: number;
	pulse_version: string;
	host: HostMetrics;
	instances: InstanceStatus[] | null;
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
