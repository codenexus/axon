import { error, fail, redirect } from '@sveltejs/kit';
import { and, desc, eq, inArray } from 'drizzle-orm';
import type { Actions, PageServerLoad } from './$types';
import { backupDownloads, backups, backupSchedules, commands, pulseAgents, serverInstances } from '$lib/server/db/schema';
import { DOWNLOAD_REQUEST_TTL_MS, pruneExpiredDownloads } from '$lib/server/backupDownloads';
import { applyRetention } from '$lib/server/backupSchedules';
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

	const [schedule] = await locals.db
		.select()
		.from(backupSchedules)
		.where(eq(backupSchedules.serverInstanceId, params.serverInstanceId));

	// Last 20, newest-last (chronological top-to-bottom like a real
	// terminal scrollback) — unlike the backups list's newest-first
	// convention, which reads better for independent items rather than an
	// ongoing exchange.
	const recentConsoleCommands = await locals.db
		.select()
		.from(commands)
		.where(
			and(
				eq(commands.pulseAgentId, instance.pulseAgentId),
				eq(commands.instanceId, instance.instanceId),
				eq(commands.type, 'console_command')
			)
		)
		.orderBy(desc(commands.createdAt))
		.limit(20);
	recentConsoleCommands.reverse();

	const [latestPropertiesCommand] = await locals.db
		.select()
		.from(commands)
		.where(
			and(
				eq(commands.pulseAgentId, instance.pulseAgentId),
				eq(commands.instanceId, instance.instanceId),
				inArray(commands.type, ['read_properties', 'write_properties'])
			)
		)
		.orderBy(desc(commands.createdAt))
		.limit(1);

	return {
		instance,
		backups: instanceBackups,
		downloadsByBackupId,
		agentHeartbeat: agent ?? null,
		schedule: schedule ?? null,
		consoleHistory: recentConsoleCommands,
		latestPropertiesCommand: latestPropertiesCommand ?? null
	};
};

// Empty string (field left blank) means "disable this dimension", not
// "invalid input" — matches saveSchedule's leave-blank-to-disable UX.
function parsePositiveIntOrNull(raw: FormDataEntryValue | null): number | null | 'invalid' {
	const value = String(raw ?? '').trim();
	if (!value) return null;
	const parsed = Number(value);
	if (!Number.isInteger(parsed) || parsed <= 0) return 'invalid';
	return parsed;
}

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
	},

	saveSchedule: async ({ request, params, locals }) => {
		const [instance] = await locals.db
			.select()
			.from(serverInstances)
			.where(eq(serverInstances.id, params.serverInstanceId));
		if (!instance) return fail(404, { error: 'instance not found' });

		const form = await request.formData();
		const intervalHours = parsePositiveIntOrNull(form.get('interval_hours'));
		const keepCount = parsePositiveIntOrNull(form.get('keep_count'));
		const keepDays = parsePositiveIntOrNull(form.get('keep_days'));
		if (intervalHours === 'invalid' || keepCount === 'invalid' || keepDays === 'invalid') {
			return fail(400, { error: 'interval hours, keep count, and keep days must be positive whole numbers' });
		}
		if (intervalHours === null && keepCount === null && keepDays === null) {
			return fail(400, { error: 'set at least one of interval hours, keep count, or keep days' });
		}

		// onConflictDoUpdate's `set` deliberately omits lastRunAt/createdAt —
		// an existing row's due-check anchor and creation time survive an
		// edit to the interval/retention numbers untouched. The `values`
		// below only take effect for a genuinely new row.
		await locals.db
			.insert(backupSchedules)
			.values({
				serverInstanceId: instance.id,
				pulseAgentId: instance.pulseAgentId,
				instanceId: instance.instanceId,
				intervalHours,
				keepCount,
				keepDays,
				lastRunAt: null,
				createdAt: Date.now()
			})
			.onConflictDoUpdate({
				target: backupSchedules.serverInstanceId,
				set: { intervalHours, keepCount, keepDays }
			});

		return { ok: true };
	},

	disableSchedule: async ({ params, locals }) => {
		await locals.db.delete(backupSchedules).where(eq(backupSchedules.serverInstanceId, params.serverInstanceId));
		return { ok: true };
	},

	applyRetentionNow: async ({ params, locals }) => {
		const [schedule] = await locals.db
			.select()
			.from(backupSchedules)
			.where(eq(backupSchedules.serverInstanceId, params.serverInstanceId));
		if (!schedule || (!schedule.keepCount && !schedule.keepDays)) {
			return fail(400, { error: 'no retention configured for this instance' });
		}

		await applyRetention(locals.db, schedule, Date.now());

		return { ok: true };
	},

	runConsoleCommand: async ({ request, params, locals }) => {
		const form = await request.formData();
		const command = String(form.get('command') ?? '').trim();
		if (!command) return fail(400, { error: 'enter a command' });

		const [instance] = await locals.db
			.select()
			.from(serverInstances)
			.where(eq(serverInstances.id, params.serverInstanceId));
		if (!instance) return fail(404, { error: 'instance not found' });

		await queueCommand(locals.db, {
			pulseAgentId: instance.pulseAgentId,
			instanceId: instance.instanceId,
			type: 'console_command',
			payload: { command }
		});

		return { ok: true };
	},

	loadProperties: async ({ params, locals }) => {
		const [instance] = await locals.db
			.select()
			.from(serverInstances)
			.where(eq(serverInstances.id, params.serverInstanceId));
		if (!instance) return fail(404, { error: 'instance not found' });

		await queueCommand(locals.db, {
			pulseAgentId: instance.pulseAgentId,
			instanceId: instance.instanceId,
			type: 'read_properties'
		});

		return { ok: true };
	},

	saveProperties: async ({ request, params, locals }) => {
		const form = await request.formData();
		const content = form.get('content');
		if (content === null) return fail(400, { error: 'missing content' });

		const [instance] = await locals.db
			.select()
			.from(serverInstances)
			.where(eq(serverInstances.id, params.serverInstanceId));
		if (!instance) return fail(404, { error: 'instance not found' });

		await queueCommand(locals.db, {
			pulseAgentId: instance.pulseAgentId,
			instanceId: instance.instanceId,
			type: 'write_properties',
			payload: { content: String(content) }
		});

		return { ok: true };
	},

	deleteInstance: async ({ params, locals }) => {
		const [instance] = await locals.db
			.select()
			.from(serverInstances)
			.where(eq(serverInstances.id, params.serverInstanceId));
		if (!instance) return fail(404, { error: 'instance not found' });

		await queueCommand(locals.db, {
			pulseAgentId: instance.pulseAgentId,
			instanceId: instance.instanceId,
			type: 'delete_instance'
		});

		// The delete only completes asynchronously (Pulse reports it on a
		// later heartbeat) — redirect back to the dashboard immediately
		// rather than staying on a page whose own row is about to vanish.
		// The dashboard's existing poll + "Deleting…" badge (see
		// pendingActions) carries the rest of the story.
		throw redirect(303, '/');
	}
};
