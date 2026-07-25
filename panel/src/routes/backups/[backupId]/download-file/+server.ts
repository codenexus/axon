import { error } from '@sveltejs/kit';
import { eq } from 'drizzle-orm';
import type { RequestHandler } from './$types';
import { backupDownloads } from '$lib/server/db/schema';

// Admin-session authenticated automatically by hooks.server.ts — this route
// lives outside /api/v1/*, which is the one namespace that opts out of the
// normal session check (Pulse agents authenticate there with bearer device
// credentials instead).
export const GET: RequestHandler = async ({ params, locals, platform }) => {
	if (platform?.env?.DB) {
		throw error(501, 'backup downloads are not supported on the Cloudflare Panel target yet');
	}

	const backupId = params.backupId;
	const [row] = await locals.db.select().from(backupDownloads).where(eq(backupDownloads.backupId, backupId));
	if (!row || row.status !== 'ready' || !row.filePath) {
		throw error(404, 'download not ready');
	}

	const fs = await import('node:fs');
	const { Readable } = await import('node:stream');

	if (!fs.existsSync(row.filePath)) {
		throw error(404, 'file no longer available');
	}

	const filePath = row.filePath;
	const nodeStream = fs.createReadStream(filePath);

	// Delivery-confirmed cleanup — once the file's been fully read off disk
	// (the client has it, or the connection dropped), there's no reason to
	// keep Panel's transient copy around. The heartbeat-driven TTL sweep is
	// the backstop for holds nobody ever collects.
	let cleaned = false;
	const cleanup = () => {
		if (cleaned) return;
		cleaned = true;
		locals.db
			.update(backupDownloads)
			.set({ status: 'expired' })
			.where(eq(backupDownloads.backupId, backupId))
			.then(() => fs.promises.rm(filePath, { force: true }))
			.catch(() => {});
	};
	nodeStream.on('end', cleanup);
	nodeStream.on('error', cleanup);

	const headers: Record<string, string> = {
		'Content-Type': 'application/gzip',
		'Content-Disposition': `attachment; filename="${backupId}.tar.gz"`
	};
	if (row.sizeBytes) headers['Content-Length'] = String(row.sizeBytes);

	return new Response(Readable.toWeb(nodeStream) as ReadableStream, { headers });
};
