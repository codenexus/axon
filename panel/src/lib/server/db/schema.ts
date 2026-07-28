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
	// The interval Pulse is *currently* sleeping for -- may be a
	// Panel-suggested fast interval, not the configured --interval. Only
	// for the dashboard's "next heartbeat" countdown display.
	intervalSeconds: integer('interval_seconds'),
	// The immutable --interval CLI flag value. Staleness math
	// (isOnline/failStaleCommands) must key off this, never
	// intervalSeconds -- see HeartbeatRequest.BaseIntervalSeconds in
	// pulse/internal/protocol/types.go for why.
	baseIntervalSeconds: integer('base_interval_seconds'),
	cpuUsagePercent: real('cpu_usage_percent'),
	cpuCores: integer('cpu_cores'),
	ramTotalBytes: integer('ram_total_bytes'),
	ramUsedBytes: integer('ram_used_bytes'),
	// JSON-stringified DiskUsage[] from the wire — structured but never
	// queried, same "JSON blob column" convention as commands.payload.
	diskUsageJson: text('disk_usage_json'),
	hostUptimeSeconds: integer('host_uptime_seconds'),
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
	port: integer('port'),
	// Stamped by the instance detail page's load() on every SSR reload
	// while an admin has that page open -- the presence signal the
	// heartbeat route uses to suggest a fast interval to Pulse. Null means
	// "never viewed" or stale enough to not matter.
	lastViewedAt: integer('last_viewed_at')
});

export const commands = sqliteTable('commands', {
	id: text('id').primaryKey(),
	pulseAgentId: text('pulse_agent_id').notNull(),
	instanceId: text('instance_id').notNull(),
	// "start_instance" | "stop_instance" | "restart_instance" | "backup_instance" | "restore_backup" | "delete_backup" | "push_backup" | "create_instance" | "console_command" | "read_properties" | "write_properties" | "list_files" | "upload_file" | "delete_file"
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
	// Free-text carrying a command's actual response, reused across three
	// unrelated command types: RCON response text for console_command, raw
	// server.properties content for read_properties, a JSON-encoded
	// FileEntry[] for list_files. Null for every other command type.
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
	id: text('id').primaryKey(), // `${gamePlatform}:${softwareType}:${version}`
	gamePlatform: text('game_platform').notNull(), // "java" | "bedrock"
	softwareType: text('software_type').notNull(), // "vanilla" | "paper" | "fabric" | "forge"
	version: text('version').notNull(),
	downloadUrl: text('download_url').notNull(),
	javaMajorVersion: integer('java_major_version'), // null for bedrock
	// Fabric's loader version is independent of the Minecraft version and
	// doesn't fit any other field — null for every other software type.
	loaderVersion: text('loader_version'),
	sortOrder: integer('sort_order').notNull(), // 0 = newest, for dropdown ordering
	fetchedAt: integer('fetched_at').notNull(),
	expiresAt: integer('expires_at').notNull()
});

// Tracks the last time a *fetch attempt* was made per (gamePlatform,
// softwareType), independent of whether it produced any entries in
// versionCatalogEntries above. Needed because a failed/empty fetch
// inserts zero rows there, so without this a resolver with no
// successful history (e.g. minecraft.net's Bedrock scrape, confirmed
// unreliable — see versionCatalog.ts) has nothing to treat as "fresh"
// and re-attempts the live fetch on every single page load, each paying
// the full fetch timeout. This lets resolveCached() skip retrying for
// NEGATIVE_CACHE_TTL_MS after a recent failed attempt, distinct from
// (and much shorter than) CATALOG_TTL_MS for a successful one.
export const versionCatalogFetchAttempts = sqliteTable('version_catalog_fetch_attempts', {
	id: text('id').primaryKey(), // `${gamePlatform}:${softwareType}`
	attemptedAt: integer('attempted_at').notNull()
});

// A reusable server-creation template — global, not per-agent (describes
// *what* to install, not *where*). version/downloadUrl/javaMajorVersion
// are pinned at creation time from a real versionCatalogEntries
// resolution, not a live "always latest" reference — re-resolving on
// every use would reintroduce the catalog's own staleness/TTL complexity,
// and Mojang's old version download URLs stay valid indefinitely.
export const serverDefinitions = sqliteTable('server_definitions', {
	id: text('id').primaryKey(), // def_<random>
	name: text('name').notNull(),
	gamePlatform: text('game_platform').notNull(), // "java" | "bedrock"
	softwareType: text('software_type').notNull(), // "vanilla" | "paper" | "fabric" | "forge"
	version: text('version').notNull(),
	downloadUrl: text('download_url').notNull(),
	javaMajorVersion: integer('java_major_version'), // null for bedrock
	loaderVersion: text('loader_version'), // fabric only
	createdAt: integer('created_at').notNull()
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

// Transient — the reversed direction of backupDownloads: the admin
// uploads a file to Panel first (a normal browser request), Panel holds
// it here, then Pulse pulls it on its own next heartbeat (see
// api/v1/files/[holdingId]/download). Simpler status set than
// backupDownloads' — no "requested"/"pending" state, since the
// browser-upload action already has the complete file on disk before this
// row is ever created, unlike push_backup's genuinely asynchronous
// readiness.
export const fileUploads = sqliteTable('file_uploads', {
	id: text('id').primaryKey(), // fup_<random>
	pulseAgentId: text('pulse_agent_id').notNull(), // denormalized for the download route's auth check
	instanceId: text('instance_id').notNull(), // Pulse-local instance id, cross-checked against X-Axon-Instance-Id like the backup upload route does
	targetPath: text('target_path').notNull(), // working_dir-relative destination Pulse will Save() to
	filePath: text('file_path').notNull(), // absolute path in Panel's local holding dir
	status: text('status').notNull(), // "ready" | "fetched" | "failed" | "expired"
	sizeBytes: integer('size_bytes'),
	errorMessage: text('error_message'),
	createdAt: integer('created_at').notNull(),
	expiresAt: integer('expires_at') // TTL backstop for a hold nobody's Pulse agent ever came to collect
});

// Admin-published Pulse releases, for self-update. Insert-only — no
// upsert, no "current" flag — the heartbeat route always takes the newest
// row (by createdAt) for a given (os, arch), so publishing a new version
// naturally supersedes the old one. Panel never verifies signatureHex
// itself (that's Pulse's job via updater.VerifyBinary, the real security
// boundary); this table is metadata relay only — the admin is responsible
// for actually building, signing, and hosting the binary somewhere Pulse
// can reach. Deliberately no downgrade protection: Pulse's version string
// is a git-describe short hash with no total order, so "differs from what
// the agent reports" is Panel's whole comparison — see CLAUDE.md.
export const pulseReleases = sqliteTable('pulse_releases', {
	id: text('id').primaryKey(), // rel_<random>
	version: text('version').notNull(),
	os: text('os').notNull(), // "linux" | "darwin" | "windows"
	arch: text('arch').notNull(), // "amd64" | "arm64"
	downloadUrl: text('download_url').notNull(),
	signatureHex: text('signature_hex').notNull(),
	createdAt: integer('created_at').notNull()
});
