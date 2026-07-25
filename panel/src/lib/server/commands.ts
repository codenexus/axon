import { and, eq, inArray } from 'drizzle-orm';
import type { Db } from './db';
import { effectiveInterval } from '../heartbeat';
import { backupDownloads, backups, commands, pulseAgents } from './db/schema';
import type { BackupCommandPayload, RestoreCommandPayload } from './protocol';
import { randomToken } from './tokens';

export type CommandType =
	| 'start_instance'
	| 'stop_instance'
	| 'restart_instance'
	| 'backup_instance'
	| 'restore_backup'
	| 'delete_backup'
	| 'push_backup';

export async function queueCommand(
	db: Db,
	params: { pulseAgentId: string; instanceId: string; type: CommandType; payload?: unknown }
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
	return id;
}

/** Generates a Panel-owned backup id — Pulse always uses this verbatim as its on-disk filename stem, never inventing its own. */
export function newBackupId(): string {
	return `bkp_${randomToken(8)}`;
}

export interface CommandOutcome {
	success: boolean;
	message?: string;
	sizeBytes?: number;
	checksum?: string;
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
			completedAt: now
		})
		.where(eq(commands.id, cmd.id));

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
		.select({ id: pulseAgents.id, intervalSeconds: pulseAgents.intervalSeconds })
		.from(pulseAgents)
		.where(inArray(pulseAgents.id, agentIds));
	const intervalByAgent = new Map(agents.map((a) => [a.id, a.intervalSeconds]));

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
