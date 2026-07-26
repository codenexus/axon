import { error } from '@sveltejs/kit';
import { eq } from 'drizzle-orm';
import type { RequestHandler } from './$types';
import { fileUploads, pulseAgents } from '$lib/server/db/schema';
import { bearerToken } from '$lib/server/http';
import { sha256Hex } from '$lib/server/tokens';

// Pulse-initiated pull of an admin-uploaded file's bytes, in response to an
// upload_file command — the reversed direction of
// api/v1/backups/[backupId]/upload (Pulse pushes backup bytes there; here
// Pulse pulls upload bytes from here), keeping Pulse as the only side that
// ever dials out either way. Auth mirrors the backup-upload route (bearer
// device-credential + agent/instance ownership checks, since Pulse is the
// caller); streaming + delivery-confirmed cleanup mirrors
// backups/[backupId]/download-file. Node-only, same reason as the backup
// transfer routes: Cloudflare Workers has no local filesystem to hold the
// file on.
export const GET: RequestHandler = async ({ params, request, locals, platform }) => {
	if (platform?.env?.DB) {
		throw error(501, 'file uploads are not supported on the Cloudflare Panel target yet');
	}

	const token = bearerToken(request);
	if (!token) throw error(401, 'missing device credential');

	const [agent] = await locals.db
		.select()
		.from(pulseAgents)
		.where(eq(pulseAgents.deviceCredentialHash, sha256Hex(token)));
	if (!agent) throw error(401, 'unknown device credential');

	const holdingId = params.holdingId;
	const [row] = await locals.db.select().from(fileUploads).where(eq(fileUploads.id, holdingId));
	if (!row || row.status !== 'ready') throw error(404, 'upload not available');
	if (row.pulseAgentId !== agent.id) throw error(403, 'upload does not belong to this agent');

	const instanceIdHeader = request.headers.get('x-axon-instance-id');
	if (instanceIdHeader !== row.instanceId) throw error(403, 'instance id mismatch');

	const fs = await import('node:fs');
	const { Readable } = await import('node:stream');

	if (!fs.existsSync(row.filePath)) throw error(404, 'file no longer available');

	const filePath = row.filePath;
	const nodeStream = fs.createReadStream(filePath);

	// Distinct terminal statuses (unlike download-file's single 'expired'
	// for both end/error) — costs nothing and is useful for troubleshooting
	// later: 'fetched' means Pulse definitely received the bytes, 'failed'
	// means the transfer itself broke.
	let settled = false;
	const settle = (status: 'fetched' | 'failed', errorMessage?: string) => {
		if (settled) return;
		settled = true;
		locals.db
			.update(fileUploads)
			.set({ status, errorMessage: errorMessage ?? null })
			.where(eq(fileUploads.id, holdingId))
			.then(() => fs.promises.rm(filePath, { force: true }))
			.catch(() => {});
	};
	nodeStream.on('end', () => settle('fetched'));
	nodeStream.on('error', (err) => settle('failed', err.message));

	const headers: Record<string, string> = { 'Content-Type': 'application/octet-stream' };
	if (row.sizeBytes) headers['Content-Length'] = String(row.sizeBytes);

	return new Response(Readable.toWeb(nodeStream) as ReadableStream, { headers });
};
