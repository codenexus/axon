import { and, desc, eq, isNull } from 'drizzle-orm';
import type { Db } from './db';
import { backups, backupSchedules } from './db/schema';
import { newBackupId, queueCommand } from './commands';

export type BackupSchedule = typeof backupSchedules.$inferSelect;

const MS_PER_HOUR = 60 * 60 * 1000;
const MS_PER_DAY = 24 * MS_PER_HOUR;

// Queues a backup_instance command + pending backups row (trigger:
// 'scheduled') if this schedule's interval has elapsed since lastRunAt.
// lastRunAt is stamped immediately, before the backup actually resolves,
// so a short interval relative to how long a backup takes can't cause
// pile-up on the next heartbeat — the next due-check compares against
// this timestamp, not against the backup's own completion.
export async function queueScheduledBackupIfDue(db: Db, schedule: BackupSchedule, now: number): Promise<void> {
	if (!schedule.intervalHours) return;
	const dueAt = (schedule.lastRunAt ?? 0) + schedule.intervalHours * MS_PER_HOUR;
	if (now < dueAt) return;

	await db
		.update(backupSchedules)
		.set({ lastRunAt: now })
		.where(eq(backupSchedules.serverInstanceId, schedule.serverInstanceId));

	const backupId = newBackupId();
	const commandId = await queueCommand(db, {
		pulseAgentId: schedule.pulseAgentId,
		instanceId: schedule.instanceId,
		type: 'backup_instance',
		payload: { backup_id: backupId }
	});

	await db.insert(backups).values({
		id: backupId,
		pulseAgentId: schedule.pulseAgentId,
		instanceId: schedule.instanceId,
		serverInstanceId: schedule.serverInstanceId,
		status: 'pending',
		trigger: 'scheduled',
		commandId,
		createdAt: now
	});
}

// Retention: candidates are this instance's complete, not-already-in-flight
// backups, newest first. A backup survives if it's the single newest, or
// within the keepCount budget, or within the keepDays window (union, not
// intersection — keep at least N most recent AND everything from the last
// D days, the common/forgiving interpretation) — everything else gets
// queued for delete_backup exactly like the manual Delete button. No-ops
// if neither keepCount nor keepDays is configured.
export async function applyRetention(db: Db, schedule: BackupSchedule, now: number): Promise<void> {
	if (!schedule.keepCount && !schedule.keepDays) return;

	const candidates = await db
		.select()
		.from(backups)
		.where(
			and(
				eq(backups.serverInstanceId, schedule.serverInstanceId),
				eq(backups.status, 'complete'),
				isNull(backups.pendingOperation)
			)
		)
		.orderBy(desc(backups.createdAt));

	const keepDaysMs = schedule.keepDays ? schedule.keepDays * MS_PER_DAY : null;

	for (let i = 0; i < candidates.length; i++) {
		const backup = candidates[i];
		const isNewest = i === 0;
		const withinCount = schedule.keepCount != null && i < schedule.keepCount;
		const withinDays = keepDaysMs != null && now - backup.createdAt <= keepDaysMs;
		if (isNewest || withinCount || withinDays) continue;

		const commandId = await queueCommand(db, {
			pulseAgentId: schedule.pulseAgentId,
			instanceId: schedule.instanceId,
			type: 'delete_backup',
			payload: { backup_id: backup.id }
		});
		await db.update(backups).set({ pendingOperation: 'delete', commandId }).where(eq(backups.id, backup.id));
	}
}

// Sweep entry point for the heartbeat route: run the due-check and
// retention pass for every schedule configured for this agent.
export async function runSchedulesForAgent(db: Db, pulseAgentId: string, now: number): Promise<void> {
	const schedules = await db.select().from(backupSchedules).where(eq(backupSchedules.pulseAgentId, pulseAgentId));
	for (const schedule of schedules) {
		await queueScheduledBackupIfDue(db, schedule, now);
		await applyRetention(db, schedule, now);
	}
}
