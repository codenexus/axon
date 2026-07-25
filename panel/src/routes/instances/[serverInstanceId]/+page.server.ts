import { error, fail } from '@sveltejs/kit';
import { and, desc, eq, inArray } from 'drizzle-orm';
import type { Actions, PageServerLoad } from './$types';
import { backupDownloads, backups, pulseAgents, serverInstances } from '$lib/server/db/schema';
import { DOWNLOAD_REQUEST_TTL_MS, pruneExpiredDownloads } from '$lib/server/backupDownloads';
import { failStaleCommands, newBackupId, queueCommand } from '$lib/server/commands';

export const load: PageServerLoad = async ({ params, locals }) => {
	const [instance] = await locals.db
		.select()
		.from(serverInstances)
		.where(eq(serverInstances.id, params.serverInstanceId));
	if (!instance) throw error(404, 'instance not found');

	await pruneExpiredDownloads(locals.db, instance.pulseAgentId);
	await failStaleCommands(locals.db, instance.pulseAgentId);

	const instanceBackups = await locals.db
		.select()
		.from(backups)
		.where(eq(backups.serverInstanceId, params.serverInstanceId))
		.orderBy(desc(backups.createdAt));

	const downloads =
		instanceBackups.length === 0
			? []
			: await locals.db
					.select()
					.from(backupDownloads)
					.where(
						inArray(
							backupDownloads.backupId,
							instanceBackups.map((b) => b.id)
						)
					);
	const downloadsByBackupId: Record<string, (typeof downloads)[number]> = {};
	for (const d of downloads) downloadsByBackupId[d.backupId] = d;

	// For the "next check-in ~Ns" countdown next to pending operations —
	// pending backups only resolve on this agent's next heartbeat.
	const [agent] = await locals.db
		.select({ lastSeenAt: pulseAgents.lastSeenAt, intervalSeconds: pulseAgents.intervalSeconds })
		.from(pulseAgents)
		.where(eq(pulseAgents.id, instance.pulseAgentId));

	return { instance, backups: instanceBackups, downloadsByBackupId, agentHeartbeat: agent ?? null };
};

export const actions: Actions = {
	createBackup: async ({ params, locals }) => {
		const [instance] = await locals.db
			.select()
			.from(serverInstances)
			.where(eq(serverInstances.id, params.serverInstanceId));
		if (!instance) return fail(404, { error: 'instance not found' });

		const backupId = newBackupId();
		const now = Date.now();

		const commandId = await queueCommand(locals.db, {
			pulseAgentId: instance.pulseAgentId,
			instanceId: instance.instanceId,
			type: 'backup_instance',
			payload: { backup_id: backupId }
		});

		await locals.db.insert(backups).values({
			id: backupId,
			pulseAgentId: instance.pulseAgentId,
			instanceId: instance.instanceId,
			serverInstanceId: instance.id,
			status: 'pending',
			trigger: 'manual',
			commandId,
			createdAt: now
		});

		return { ok: true };
	},

	deleteBackup: async ({ request, params, locals }) => {
		const form = await request.formData();
		const backupId = String(form.get('backup_id') ?? '');
		if (!backupId) return fail(400, { error: 'missing backup_id' });

		const [instance] = await locals.db
			.select()
			.from(serverInstances)
			.where(eq(serverInstances.id, params.serverInstanceId));
		if (!instance) return fail(404, { error: 'instance not found' });

		const [backup] = await locals.db
			.select()
			.from(backups)
			.where(and(eq(backups.id, backupId), eq(backups.serverInstanceId, params.serverInstanceId)));
		if (!backup) return fail(404, { error: 'backup not found' });

		const commandId = await queueCommand(locals.db, {
			pulseAgentId: instance.pulseAgentId,
			instanceId: instance.instanceId,
			type: 'delete_backup',
			payload: { backup_id: backupId }
		});

		await locals.db
			.update(backups)
			.set({ pendingOperation: 'delete', commandId })
			.where(eq(backups.id, backupId));

		return { ok: true };
	},

	downloadBackup: async ({ request, params, locals }) => {
		const form = await request.formData();
		const backupId = String(form.get('backup_id') ?? '');
		if (!backupId) return fail(400, { error: 'missing backup_id' });

		const [instance] = await locals.db
			.select()
			.from(serverInstances)
			.where(eq(serverInstances.id, params.serverInstanceId));
		if (!instance) return fail(404, { error: 'instance not found' });

		const [backup] = await locals.db
			.select()
			.from(backups)
			.where(and(eq(backups.id, backupId), eq(backups.serverInstanceId, params.serverInstanceId)));
		if (!backup || backup.status !== 'complete') return fail(404, { error: 'backup not available' });

		await queueCommand(locals.db, {
			pulseAgentId: instance.pulseAgentId,
			instanceId: instance.instanceId,
			type: 'push_backup',
			payload: { backup_id: backupId }
		});

		const now = Date.now();
		const values = {
			backupId,
			pulseAgentId: instance.pulseAgentId,
			status: 'requested' as const,
			filePath: null,
			sizeBytes: null,
			errorMessage: null,
			requestedAt: now,
			readyAt: null,
			expiresAt: now + DOWNLOAD_REQUEST_TTL_MS
		};
		// A second download click while one's already pending/ready just
		// re-requests it, reusing the same row (backupId is the PK).
		await locals.db
			.insert(backupDownloads)
			.values(values)
			.onConflictDoUpdate({ target: backupDownloads.backupId, set: values });

		return { ok: true };
	},

	restoreBackup: async ({ request, params, locals }) => {
		const form = await request.formData();
		const backupId = String(form.get('backup_id') ?? '');
		if (!backupId) return fail(400, { error: 'missing backup_id' });

		const [instance] = await locals.db
			.select()
			.from(serverInstances)
			.where(eq(serverInstances.id, params.serverInstanceId));
		if (!instance) return fail(404, { error: 'instance not found' });

		const [backup] = await locals.db
			.select()
			.from(backups)
			.where(and(eq(backups.id, backupId), eq(backups.serverInstanceId, params.serverInstanceId)));
		if (!backup || backup.status !== 'complete' || backup.pendingOperation) {
			return fail(404, { error: 'backup not available' });
		}

		const safetyBackupId = newBackupId();
		const now = Date.now();

		const commandId = await queueCommand(locals.db, {
			pulseAgentId: instance.pulseAgentId,
			instanceId: instance.instanceId,
			type: 'restore_backup',
			payload: { backup_id: backupId, safety_backup_id: safetyBackupId }
		});

		// Panel pregenerates the safety-backup id and records it up front
		// (status='pending') so it shows up in the list immediately, the same
		// way a manually-triggered backup does — Pulse takes it automatically
		// as the first step of the restore, before touching anything.
		await locals.db.insert(backups).values({
			id: safetyBackupId,
			pulseAgentId: instance.pulseAgentId,
			instanceId: instance.instanceId,
			serverInstanceId: instance.id,
			status: 'pending',
			trigger: 'pre_restore',
			commandId,
			createdAt: now
		});

		await locals.db.update(backups).set({ pendingOperation: 'restore', commandId }).where(eq(backups.id, backupId));

		return { ok: true };
	}
};
