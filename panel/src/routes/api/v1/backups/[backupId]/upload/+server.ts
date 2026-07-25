import { error, json } from '@sveltejs/kit';
import { eq } from 'drizzle-orm';
import type { RequestHandler } from './$types';
import { backupDownloads, backups, pulseAgents } from '$lib/server/db/schema';
import { DOWNLOAD_READY_TTL_MS, resolveHoldingPath } from '$lib/server/backupDownloads';
import { bearerToken } from '$lib/server/http';
import { sha256Hex } from '$lib/server/tokens';

// Pulse-initiated push of a backup archive's bytes, in response to a
// push_backup command — see CLAUDE.md's push-backup design. Node-only:
// Cloudflare Workers has no local filesystem to hold the file transiently
// on, and this is the one route in the backups feature that actually needs
// one (metadata/scheduling elsewhere is fine on D1).
export const POST: RequestHandler = async ({ request, params, locals, platform }) => {
	if (platform?.env?.DB) {
		throw error(501, 'backup file transfer is not supported on the Cloudflare Panel target yet');
	}

	const token = bearerToken(request);
	if (!token) throw error(401, 'missing device credential');

	const [agent] = await locals.db
		.select()
		.from(pulseAgents)
		.where(eq(pulseAgents.deviceCredentialHash, sha256Hex(token)));
	if (!agent) throw error(401, 'unknown device credential');

	const backupId = params.backupId;
	const [backupRow] = await locals.db.select().from(backups).where(eq(backups.id, backupId));
	if (!backupRow) throw error(404, 'unknown backup');
	if (backupRow.pulseAgentId !== agent.id) throw error(403, 'backup does not belong to this agent');

	const instanceIdHeader = request.headers.get('x-axon-instance-id');
	if (instanceIdHeader !== backupRow.instanceId) throw error(403, 'instance id mismatch');

	if (!request.body) throw error(400, 'missing request body');

	const destPath = await resolveHoldingPath(backupId);
	const fs = await import('node:fs');
	const path = await import('node:path');
	const { Readable } = await import('node:stream');
	const { pipeline } = await import('node:stream/promises');

	await fs.promises.mkdir(path.dirname(destPath), { recursive: true });

	let sizeBytes = 0;
	const source = Readable.fromWeb(request.body as import('node:stream/web').ReadableStream);
	source.on('data', (chunk: Buffer) => {
		sizeBytes += chunk.length;
	});

	try {
		await pipeline(source, fs.createWriteStream(destPath));
	} catch (err) {
		await fs.promises.rm(destPath, { force: true });
		throw error(500, `failed to store upload: ${err instanceof Error ? err.message : String(err)}`);
	}

	const now = Date.now();
	await locals.db
		.update(backupDownloads)
		.set({
			status: 'ready',
			filePath: destPath,
			sizeBytes,
			readyAt: now,
			expiresAt: now + DOWNLOAD_READY_TTL_MS,
			errorMessage: null
		})
		.where(eq(backupDownloads.backupId, backupId));

	return json({ ok: true });
};
