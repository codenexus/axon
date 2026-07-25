import { json, error } from '@sveltejs/kit';
import { and, eq } from 'drizzle-orm';
import type { RequestHandler } from './$types';
import { backupDownloads, backups, commands, pulseAgents, serverInstances } from '$lib/server/db/schema';
import { pruneExpiredDownloads } from '$lib/server/backupDownloads';
import { bearerToken } from '$lib/server/http';
import { sha256Hex } from '$lib/server/tokens';
import type {
	BackupCommandPayload,
	HeartbeatRequestBody,
	HeartbeatResponseBody,
	RestoreCommandPayload,
	WireCommand
} from '$lib/server/protocol';

export const POST: RequestHandler = async ({ request, locals }) => {
	const token = bearerToken(request);
	if (!token) throw error(401, 'missing device credential');

	const [agent] = await locals.db
		.select()
		.from(pulseAgents)
		.where(eq(pulseAgents.deviceCredentialHash, sha256Hex(token)));
	if (!agent) throw error(401, 'unknown device credential');

	const body = (await request.json()) as HeartbeatRequestBody;
	const now = Date.now();

	await locals.db
		.update(pulseAgents)
		.set({
			lastSeenAt: now,
			intervalSeconds: body.interval_seconds,
			pulseVersion: body.pulse_version,
			cpuUsagePercent: body.host?.cpu_usage_percent,
			cpuCores: body.host?.cpu_cores,
			ramTotalBytes: body.host?.ram_total_bytes,
			ramUsedBytes: body.host?.ram_used_bytes
		})
		.where(eq(pulseAgents.id, agent.id));

	for (const instance of body.instances ?? []) {
		const rowId = `${agent.id}:${instance.id}`;
		const values = {
			id: rowId,
			pulseAgentId: agent.id,
			instanceId: instance.id,
			name: instance.name,
			gamePlatform: instance.game_platform,
			version: instance.version,
			softwareType: instance.software_type,
			runningState: instance.running_state,
			playerCount: instance.player_count ?? 0,
			uptimeSeconds: instance.uptime_seconds ?? 0,
			updatedAt: now
		};
		await locals.db
			.insert(serverInstances)
			.values(values)
			.onConflictDoUpdate({ target: serverInstances.id, set: values });
	}

	for (const result of body.pending_command_results ?? []) {
		const [cmd] = await locals.db.select().from(commands).where(eq(commands.id, result.command_id));

		await locals.db
			.update(commands)
			.set({
				status: result.success ? 'completed' : 'failed',
				resultMessage: result.message,
				completedAt: now
			})
			.where(eq(commands.id, result.command_id));

		if (!cmd) continue;

		if (cmd.type === 'backup_instance' || cmd.type === 'delete_backup') {
			const payload = cmd.payload ? (JSON.parse(cmd.payload) as BackupCommandPayload) : null;
			if (!payload?.backup_id) continue;

			if (cmd.type === 'backup_instance') {
				await locals.db
					.update(backups)
					.set({
						status: result.success ? 'complete' : 'failed',
						sizeBytes: result.size_bytes,
						checksumSha256: result.checksum,
						errorMessage: result.success ? null : result.message,
						completedAt: now
					})
					.where(eq(backups.id, payload.backup_id));
			} else if (result.success) {
				await locals.db.delete(backups).where(eq(backups.id, payload.backup_id));
			} else {
				await locals.db
					.update(backups)
					.set({ pendingOperation: null, errorMessage: result.message })
					.where(eq(backups.id, payload.backup_id));
			}
		} else if (cmd.type === 'restore_backup') {
			const restorePayload = cmd.payload ? (JSON.parse(cmd.payload) as RestoreCommandPayload) : null;
			if (!restorePayload?.backup_id || !restorePayload?.safety_backup_id) continue;

			// Pulse reports the safety backup's size/checksum whenever that
			// step itself succeeded, independent of whether the restore that
			// followed it succeeded — so a real, usable safety backup gets
			// recorded even if the extract step afterward failed.
			if (result.size_bytes && result.checksum) {
				await locals.db
					.update(backups)
					.set({
						status: 'complete',
						sizeBytes: result.size_bytes,
						checksumSha256: result.checksum,
						completedAt: now
					})
					.where(eq(backups.id, restorePayload.safety_backup_id));
			} else {
				await locals.db
					.update(backups)
					.set({ status: 'failed', errorMessage: result.message, completedAt: now })
					.where(eq(backups.id, restorePayload.safety_backup_id));
			}

			await locals.db
				.update(backups)
				.set({ pendingOperation: null })
				.where(eq(backups.id, restorePayload.backup_id));
		} else if (cmd.type === 'push_backup' && !result.success) {
			// On success there's nothing to do here — the upload endpoint
			// itself already marked backupDownloads 'ready' by the time
			// Pulse can report this command as completed (it only returns
			// success after the upload HTTP call itself succeeds). On
			// failure, mark it so a "Preparing download…" row doesn't hang
			// forever waiting for a file that's never coming.
			const pushPayload = cmd.payload ? (JSON.parse(cmd.payload) as BackupCommandPayload) : null;
			if (pushPayload?.backup_id) {
				await locals.db
					.update(backupDownloads)
					.set({ status: 'failed', errorMessage: result.message })
					.where(eq(backupDownloads.backupId, pushPayload.backup_id));
			}
		}
	}

	// Opportunistic cleanup for abandoned download holds (admin requested a
	// download, then never clicked it, or it never became ready) — piggybacks
	// on this agent's regular heartbeat cadence rather than a real timer,
	// consistent with the project's request-driven design.
	await pruneExpiredDownloads(locals.db, agent.id);

	const queued = await locals.db
		.select()
		.from(commands)
		.where(and(eq(commands.pulseAgentId, agent.id), eq(commands.status, 'queued')));

	const wireCommands: WireCommand[] = queued.map((c) => ({
		id: c.id,
		type: c.type,
		instance_id: c.instanceId,
		payload: c.payload ? JSON.parse(c.payload) : undefined
	}));

	if (queued.length > 0) {
		for (const c of queued) {
			await locals.db.update(commands).set({ status: 'sent', sentAt: now }).where(eq(commands.id, c.id));
		}
	}

	const response: HeartbeatResponseBody = { commands: wireCommands };
	return json(response);
};
