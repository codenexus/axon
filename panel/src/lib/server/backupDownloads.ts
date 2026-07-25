import { and, eq, isNotNull, lt } from 'drizzle-orm';
import type { Db } from './db';
import { backupDownloads } from './db/schema';

// How long an admin has to actually click the download link once the file
// is ready and waiting.
export const DOWNLOAD_READY_TTL_MS = 10 * 60 * 1000;
// Safety-net deadline set the moment a download is requested, covering the
// case where it never becomes ready at all (Pulse never picks up the
// push_backup command, the agent is offline, push-backup isn't supported on
// this Panel target) — without this, a row stuck at "requested" would have
// no expiresAt at all and never get swept.
export const DOWNLOAD_REQUEST_TTL_MS = 15 * 60 * 1000;

function holdingDir(): string {
	return process.env.AXON_BACKUP_HOLDING_DIR ?? process.cwd() + '/.backup-holding';
}

export async function resolveHoldingPath(backupId: string): Promise<string> {
	const path = await import('node:path');
	return path.join(holdingDir(), `${backupId}.tar.gz`);
}

// Removes expired backup_downloads rows and their on-disk files (if any).
// Node-only in effect (uses node:fs dynamically so the Cloudflare bundle
// never needs to resolve it) — on Cloudflare this still deletes the DB
// rows, just never finds a file to remove, consistent with push-backup
// transfer being unsupported on that target.
export async function pruneExpiredDownloads(db: Db, pulseAgentId?: string): Promise<void> {
	const now = Date.now();
	const condition = pulseAgentId
		? and(
				eq(backupDownloads.pulseAgentId, pulseAgentId),
				isNotNull(backupDownloads.expiresAt),
				lt(backupDownloads.expiresAt, now)
			)
		: and(isNotNull(backupDownloads.expiresAt), lt(backupDownloads.expiresAt, now));

	const expired = await db.select().from(backupDownloads).where(condition);
	if (expired.length === 0) return;

	const fs = await import('node:fs/promises');
	for (const row of expired) {
		if (row.filePath) {
			await fs.rm(row.filePath, { force: true }).catch(() => {});
		}
	}
	await db.delete(backupDownloads).where(condition);
}
