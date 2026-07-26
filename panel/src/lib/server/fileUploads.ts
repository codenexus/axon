import { and, eq, isNotNull, lt } from 'drizzle-orm';
import type { Db } from './db';
import { fileUploads } from './db/schema';

// How long a held upload waits for Pulse to collect it. One TTL, unlike
// backupDownloads' READY/REQUEST pair — there's no "requested but not
// ready yet" window here, since the browser-upload action already has the
// complete file on disk by the time this row (and its expiresAt) is
// created.
export const FILE_UPLOAD_READY_TTL_MS = 10 * 60 * 1000;

function holdingDir(): string {
	return process.env.AXON_FILE_UPLOAD_HOLDING_DIR ?? process.cwd() + '/.file-upload-holding';
}

export async function resolveFileUploadHoldingPath(uploadId: string): Promise<string> {
	const path = await import('node:path');
	return path.join(holdingDir(), uploadId);
}

// Removes expired file_uploads rows and their on-disk files (if any) —
// structurally identical to pruneExpiredDownloads.
export async function pruneExpiredFileUploads(db: Db, pulseAgentId?: string): Promise<void> {
	const now = Date.now();
	const condition = pulseAgentId
		? and(eq(fileUploads.pulseAgentId, pulseAgentId), isNotNull(fileUploads.expiresAt), lt(fileUploads.expiresAt, now))
		: and(isNotNull(fileUploads.expiresAt), lt(fileUploads.expiresAt, now));

	const expired = await db.select().from(fileUploads).where(condition);
	if (expired.length === 0) return;

	const fs = await import('node:fs/promises');
	for (const row of expired) {
		await fs.rm(row.filePath, { force: true }).catch(() => {});
	}
	await db.delete(fileUploads).where(condition);
}
