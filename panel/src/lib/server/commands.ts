import { and, eq, inArray } from 'drizzle-orm';
import type { Db } from './db';
import { effectiveInterval } from '../heartbeat';
import { backupDownloads, backups, backupSchedules, commands, pulseAgents, serverInstances } from './db/schema';
import type { BackupCommandPayload, RestoreCommandPayload } from './protocol';
import { randomToken } from './tokens';
import { getRealtime } from './realtime';

export type CommandType =
	| 'start_instance'
	| 'stop_instance'
	| 'restart_instance'
	| 'backup_instance'
	| 'restore_backup'
	| 'delete_backup'
	| 'push_backup'
	| 'create_instance'
	| 'delete_instance'
	| 'console_command'
	| 'read_properties'
	| 'write_properties'
	| 'list_files'
	| 'upload_file'
	| 'delete_file'
	| 'run_diagnostic';

// `platform` is optional and, when passed, publishes a 'changed' event on
// this instance's real-time channel right after queueing -- lets the
// admin's *own* action (e.g. submitting the console box) reflect
// instantly, before Pulse ever reports back. Most callers (backups,
// provisioning, etc.) don't pass it: those don't feed the instance
// console's real-time channel, and the bigger win (Pulse's own result
// landing) is already covered by the heartbeat route's own publish calls
// regardless of whether the queueing action passed platform here.
export async function queueCommand(
	db: Db,
	params: { pulseAgentId: string; instanceId: string; type: CommandType; payload?: unknown },
	platform?: App.Platform
): Promise<string> {
	const id = `cmd_${randomToken(8)}`;
	await db.insert(commands).values({
		id,
		pulseAgentId: params.pulseAgentId,
		instanceId: params.instanceId,
		type: params.type,
		payload: params.payload !== undefined ? JSON.stringify(params.payload) : null,
		status: 'queued',
		createdAt: Date.now()
	});
	if (platform !== undefined) {
		const realtime = await getRealtime(platform);
		await realtime.publish(`${params.pulseAgentId}:${params.instanceId}`, { type: 'changed' });
	}
	return id;
}

/** Generates a Panel-owned backup id — Pulse always uses this verbatim as its on-disk filename stem, never inventing its own. */
export function newBackupId(): string {
	return `bkp_${randomToken(8)}`;
}

/** Generates a Panel-owned instance id for create_instance — Pulse uses this verbatim as the new instance's id, never inventing its own. */
export function newInstanceId(): string {
	return `inst_${randomToken(8)}`;
}

/** Generates a Panel-owned id for a held file upload — see fileUploads.ts. */
export function newFileUploadId(): string {
	return `fup_${randomToken(8)}`;
}

export interface CommandOutcome {
	success: boolean;
	message?: string;
	sizeBytes?: number;
	checksum?: string;
	// RCON response text for a console_command result.
	output?: string;
}

/**
 * Resolves a command to a terminal status and applies whatever downstream
 * effect its type implies (backup/delete/restore/push-backup row updates).
 * Shared by the real pending_command_results path (heartbeat route) and the
 * stale-command timeout sweep below, so both agree on exactly what happens
 * to a command's dependent rows once it resolves.
 */
export async function resolveCommandOutcome(
	db: Db,
	cmd: typeof commands.$inferSelect,
	outcome: CommandOutcome,
	now: number
): Promise<void> {
	await db
		.update(commands)
		.set({
			status: outcome.success ? 'completed' : 'failed',
			resultMessage: outcome.message,
			completedAt: now,
			progressPhase: null,
			output: outcome.output ?? null
		})
		.where(eq(commands.id, cmd.id));

	if (cmd.type === 'create_instance') {
		// Nothing else to do here on success — the new instance surfaces
		// naturally via the existing heartbeat instance-upsert loop once
		// Pulse's next heartbeat reports it in body.instances. On failure
		// there's no serverInstances row to clean up either, since none was
		// pre-inserted (unlike backups' pregenerated-row pattern) — Panel
		// had no honest running_state/etc. to give it before Pulse ever
		// confirmed the instance exists.
		return;
	}

	if (cmd.type === 'delete_instance') {
		if (!outcome.success) return; // stays around so the admin can retry

		// Cascade-delete Panel's own metadata for the now-gone instance.
		// Deliberately NOT cascaded: backupDownloads (keyed by backupId,
		// already self-expiring via pruneExpiredDownloads' TTL sweep once
		// its parent backups row is gone), fileUploads and commands history
		// for this (pulseAgentId, instanceId) pair (same reasoning — inert,
		// never queried unscoped, and the instance's own page is gone so
		// nothing surfaces them again). Backup archives on Pulse's disk are
		// never deleted by this — only Panel's metadata about them; they
		// live in the shared backups_dir, not under working_dir, and Pulse
		// never touches them as part of delete_instance.
		const serverInstanceId = `${cmd.pulseAgentId}:${cmd.instanceId}`;
		await db.delete(backupSchedules).where(eq(backupSchedules.serverInstanceId, serverInstanceId));
		await db.delete(backups).where(eq(backups.serverInstanceId, serverInstanceId));
		await db.delete(serverInstances).where(eq(serverInstances.id, serverInstanceId));
		return;
	}

	if (cmd.type === 'backup_instance' || cmd.type === 'delete_backup') {
		const payload = cmd.payload ? (JSON.parse(cmd.payload) as BackupCommandPayload) : null;
		if (!payload?.backup_id) return;

		if (cmd.type === 'backup_instance') {
			await db
				.update(backups)
				.set({
					status: outcome.success ? 'complete' : 'failed',
					sizeBytes: outcome.sizeBytes,
					checksumSha256: outcome.checksum,
					errorMessage: outcome.success ? null : outcome.message,
					completedAt: now
				})
				.where(eq(backups.id, payload.backup_id));
		} else if (outcome.success) {
			await db.delete(backups).where(eq(backups.id, payload.backup_id));
		} else {
			await db
				.update(backups)
				.set({ pendingOperation: null, errorMessage: outcome.message })
				.where(eq(backups.id, payload.backup_id));
		}
	} else if (cmd.type === 'restore_backup') {
		const restorePayload = cmd.payload ? (JSON.parse(cmd.payload) as RestoreCommandPayload) : null;
		if (!restorePayload?.backup_id || !restorePayload?.safety_backup_id) return;

		// Pulse reports the safety backup's size/checksum whenever that
		// step itself succeeded, independent of whether the restore that
		// followed it succeeded — so a real, usable safety backup gets
		// recorded even if the extract step afterward failed. A timeout
		// (no size/checksum at all) naturally falls into the "failed"
		// branch here, which is the honest answer: Panel doesn't know.
		if (outcome.sizeBytes && outcome.checksum) {
			await db
				.update(backups)
				.set({
					status: 'complete',
					sizeBytes: outcome.sizeBytes,
					checksumSha256: outcome.checksum,
					completedAt: now
				})
				.where(eq(backups.id, restorePayload.safety_backup_id));
		} else {
			await db
				.update(backups)
				.set({ status: 'failed', errorMessage: outcome.message, completedAt: now })
				.where(eq(backups.id, restorePayload.safety_backup_id));
		}

		await db.update(backups).set({ pendingOperation: null }).where(eq(backups.id, restorePayload.backup_id));
	} else if (cmd.type === 'push_backup' && !outcome.success) {
		// On success there's nothing to do here — the upload endpoint
		// itself already marked backupDownloads 'ready' by the time
		// Pulse can report this command as completed (it only returns
		// success after the upload HTTP call itself succeeds). On
		// failure, mark it so a "Preparing download…" row doesn't hang
		// forever waiting for a file that's never coming.
		const pushPayload = cmd.payload ? (JSON.parse(cmd.payload) as BackupCommandPayload) : null;
		if (pushPayload?.backup_id) {
			await db
				.update(backupDownloads)
				.set({ status: 'failed', errorMessage: outcome.message })
				.where(eq(backupDownloads.backupId, pushPayload.backup_id));
		}
	}
}

// A command's result only ever arrives piggybacked on a later heartbeat —
// if Pulse dies/restarts between finishing a command and reporting it, the
// command is orphaned at 'sent' forever with no automatic recovery (see
// CLAUDE.md's "Known gaps"). This sweep auto-fails anything stuck 'sent'
// past a "presumed lost" deadline, reusing the same 3-missed-heartbeats bar
// isOnline() already uses for "presumed offline" (panel/src/lib/heartbeat.ts).
// Modeled structurally on pruneExpiredDownloads: select first, act per-row,
// optional pulseAgentId scoping.
export async function failStaleCommands(db: Db, pulseAgentId?: string): Promise<void> {
	const now = Date.now();
	const condition = pulseAgentId ? and(eq(commands.status, 'sent'), eq(commands.pulseAgentId, pulseAgentId)) : eq(commands.status, 'sent');

	const sent = await db.select().from(commands).where(condition);
	if (sent.length === 0) return;

	const agentIds = [...new Set(sent.map((c) => c.pulseAgentId))];
	const agents = await db
		.select({ id: pulseAgents.id, baseIntervalSeconds: pulseAgents.baseIntervalSeconds })
		.from(pulseAgents)
		.where(inArray(pulseAgents.id, agentIds));
	const intervalByAgent = new Map(agents.map((a) => [a.id, a.baseIntervalSeconds]));

	for (const cmd of sent) {
		if (!cmd.sentAt) continue;
		const deadlineMs = effectiveInterval(intervalByAgent.get(cmd.pulseAgentId) ?? null) * 3000;
		if (now - cmd.sentAt < deadlineMs) continue;

		await resolveCommandOutcome(
			db,
			cmd,
			{
				success: false,
				message: `timed out waiting for Pulse to report a result (${Math.round((now - cmd.sentAt) / 1000)}s since sent) — the agent may have restarted before reporting completion`
			},
			now
		);
	}
}
