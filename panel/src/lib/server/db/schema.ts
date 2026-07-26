import { sqliteTable, text, integer, real } from 'drizzle-orm/sqlite-core';

// Single-admin v1: one row, created by scripts/seed-dev.mjs or the first-run
// setup flow. No user table / RBAC (spec's own open question, resolved as
// single-admin for v1 — see Axon architecture spec section 11).
export const adminSettings = sqliteTable('admin_settings', {
	id: integer('id').primaryKey({ autoIncrement: true }),
	passwordHash: text('password_hash').notNull(),
	createdAt: integer('created_at').notNull()
});

export const adminSessions = sqliteTable('admin_sessions', {
	id: text('id').primaryKey(), // sha256(session cookie token) — raw token never stored, mirrors enrollment/device credential handling
	createdAt: integer('created_at').notNull(),
	expiresAt: integer('expires_at').notNull()
});

export const enrollmentTokens = sqliteTable('enrollment_tokens', {
	id: text('id').primaryKey(),
	tokenHash: text('token_hash').notNull(),
	createdAt: integer('created_at').notNull(),
	usedAt: integer('used_at'),
	expiresAt: integer('expires_at').notNull()
});

export const pulseAgents = sqliteTable('pulse_agents', {
	id: text('id').primaryKey(),
	hostname: text('hostname').notNull(),
	os: text('os').notNull(),
	arch: text('arch').notNull(),
	deviceCredentialHash: text('device_credential_hash').notNull(),
	pulseVersion: text('pulse_version').notNull(),
	lastSeenAt: integer('last_seen_at'),
	intervalSeconds: integer('interval_seconds'),
	cpuUsagePercent: real('cpu_usage_percent'),
	cpuCores: integer('cpu_cores'),
	ramTotalBytes: integer('ram_total_bytes'),
	ramUsedBytes: integer('ram_used_bytes'),
	createdAt: integer('created_at').notNull(),
	// All three nullable — admin must configure before creating servers on
	// this agent. The port allocator (portAllocator.ts) only ever considers
	// ports already recorded on server_instances for this agent; it has no
	// visibility into ports used by pre-existing hand-configured instances,
	// since those never report a port on the wire at all.
	portRangeStart: integer('port_range_start'),
	portRangeEnd: integer('port_range_end'),
	instancesRootDir: text('instances_root_dir')
});

export const serverInstances = sqliteTable('server_instances', {
	// composite natural key: Pulse assigns the instance id locally, unique
	// per pulse agent, not globally — so the DB primary key combines both.
	id: text('id').primaryKey(), // `${pulseAgentId}:${instanceId}`
	pulseAgentId: text('pulse_agent_id').notNull(),
	instanceId: text('instance_id').notNull(),
	name: text('name').notNull(),
	gamePlatform: text('game_platform').notNull(),
	version: text('version').notNull(),
	softwareType: text('software_type').notNull(),
	runningState: text('running_state').notNull(),
	playerCount: integer('player_count').notNull().default(0),
	uptimeSeconds: integer('uptime_seconds').notNull().default(0),
	updatedAt: integer('updated_at').notNull(),
	// Only populated for instances Pulse itself provisioned via
	// create_instance — legacy hand-configured instances never report one.
	port: integer('port')
});

export const commands = sqliteTable('commands', {
	id: text('id').primaryKey(),
	pulseAgentId: text('pulse_agent_id').notNull(),
	instanceId: text('instance_id').notNull(),
	// "start_instance" | "stop_instance" | "restart_instance" | "backup_instance" | "restore_backup" | "delete_backup" | "push_backup" | "create_instance" | "console_command"
	type: text('type').notNull(),
	// JSON-stringified command-specific payload (e.g. BackupCommandPayload); null for start/stop.
	payload: text('payload'),
	status: text('status').notNull(), // "queued" | "sent" | "completed" | "failed"
	resultMessage: text('result_message'),
	createdAt: integer('created_at').notNull(),
	sentAt: integer('sent_at'),
	completedAt: integer('completed_at'),
	// Coarse in-flight phase for a command that can't complete within one
	// heartbeat (currently only create_instance) — "preparing" |
	// "installing_java" | "downloading" | "configuring" | "registering".
	// Null once terminal (completed/failed) or for command types that
	// never report progress.
	progressPhase: text('progress_phase'),
	// The RCON response text for a console_command result — populated
	// whenever the RCON round-trip itself succeeded, even if the game
	// rejected the command. Null for every other command type.
	output: text('output')
});

export const backups = sqliteTable('backups', {
	id: text('id').primaryKey(), // bkp_<random> — also the Pulse-side filename stem
	pulseAgentId: text('pulse_agent_id').notNull(),
	instanceId: text('instance_id').notNull(), // Pulse-local instance id
	serverInstanceId: text('server_instance_id').notNull(), // `${pulseAgentId}:${instanceId}`, for per-instance list queries
	status: text('status').notNull(), // "pending" | "running" | "complete" | "failed"
	trigger: text('trigger').notNull(), // "manual" | "scheduled" | "pre_restore"
	pendingOperation: text('pending_operation'), // null | "restore" | "delete" — set while a command is in flight for this backup
	commandId: text('command_id'), // id of the commands row currently driving this backup's lifecycle
	sizeBytes: integer('size_bytes'),
	checksumSha256: text('checksum_sha256'),
	errorMessage: text('error_message'),
	createdAt: integer('created_at').notNull(),
	completedAt: integer('completed_at')
});

export const backupSchedules = sqliteTable('backup_schedules', {
	serverInstanceId: text('server_instance_id').primaryKey(),
	pulseAgentId: text('pulse_agent_id').notNull(), // denormalized, same reason as backupDownloads.pulseAgentId — agent-scoped heartbeat queries without a join
	instanceId: text('instance_id').notNull(), // Pulse-local id, needed verbatim by queueCommand
	intervalHours: integer('interval_hours'), // null = no automatic backups; retention below still applies independently
	keepCount: integer('keep_count'), // null = no count-based retention
	keepDays: integer('keep_days'), // null = no age-based retention
	lastRunAt: integer('last_run_at'), // last time the due-check queued a scheduled backup; null = never
	createdAt: integer('created_at').notNull()
});

// Cached results of resolving available software versions for the
// create-server flow — Java via Mojang's public version manifest, Bedrock
// via a best-effort scrape of minecraft.net (see versionCatalog.ts). Cache
// exists because Cloudflare Workers are stateless/ephemeral (no reliable
// in-memory cache across requests) and to keep the create-server page fast
// without hitting either external source on every load.
export const versionCatalogEntries = sqliteTable('version_catalog_entries', {
	id: text('id').primaryKey(), // `${gamePlatform}:${version}`
	gamePlatform: text('game_platform').notNull(), // "java" | "bedrock"
	version: text('version').notNull(),
	downloadUrl: text('download_url').notNull(),
	javaMajorVersion: integer('java_major_version'), // null for bedrock
	sortOrder: integer('sort_order').notNull(), // 0 = newest, for dropdown ordering
	fetchedAt: integer('fetched_at').notNull(),
	expiresAt: integer('expires_at').notNull()
});

// Transient — Pulse pushes the backup file here on request rather than
// Panel storing a durable second copy (see CLAUDE.md's push-backup design:
// Pulse's own disk is the source of truth). A second download request for
// the same backup reuses/overwrites this row, hence backupId as the PK
// rather than a separate generated id.
export const backupDownloads = sqliteTable('backup_downloads', {
	backupId: text('backup_id').primaryKey(),
	pulseAgentId: text('pulse_agent_id').notNull(), // denormalized for the upload endpoint's auth check
	status: text('status').notNull(), // "requested" | "ready" | "expired" | "failed"
	filePath: text('file_path'), // absolute path in Panel's local holding dir, set once ready
	sizeBytes: integer('size_bytes'),
	errorMessage: text('error_message'),
	requestedAt: integer('requested_at').notNull(),
	readyAt: integer('ready_at'),
	expiresAt: integer('expires_at')
});
