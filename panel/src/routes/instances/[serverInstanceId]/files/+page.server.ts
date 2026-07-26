import { error, fail } from '@sveltejs/kit';
import { and, desc, eq, inArray } from 'drizzle-orm';
import type { Actions, PageServerLoad } from './$types';
import { commands, fileUploads, serverInstances } from '$lib/server/db/schema';
import { failStaleCommands, newFileUploadId, queueCommand } from '$lib/server/commands';
import { FILE_UPLOAD_READY_TTL_MS, pruneExpiredFileUploads, resolveFileUploadHoldingPath } from '$lib/server/fileUploads';

export const load: PageServerLoad = async ({ params, url, locals }) => {
	const [instance] = await locals.db
		.select()
		.from(serverInstances)
		.where(eq(serverInstances.id, params.serverInstanceId));
	if (!instance) throw error(404, 'instance not found');

	await pruneExpiredFileUploads(locals.db, instance.pulseAgentId);
	await failStaleCommands(locals.db, instance.pulseAgentId);

	const path = url.searchParams.get('path') ?? '';

	const [latestFilesCommand] = await locals.db
		.select()
		.from(commands)
		.where(
			and(
				eq(commands.pulseAgentId, instance.pulseAgentId),
				eq(commands.instanceId, instance.instanceId),
				inArray(commands.type, ['list_files', 'upload_file', 'delete_file'])
			)
		)
		.orderBy(desc(commands.createdAt))
		.limit(1);

	return { instance, path, latestFilesCommand: latestFilesCommand ?? null };
};

export const actions: Actions = {
	listPath: async ({ request, params, locals }) => {
		const form = await request.formData();
		const path = String(form.get('path') ?? '').trim();

		const [instance] = await locals.db
			.select()
			.from(serverInstances)
			.where(eq(serverInstances.id, params.serverInstanceId));
		if (!instance) return fail(404, { error: 'instance not found' });

		await queueCommand(locals.db, {
			pulseAgentId: instance.pulseAgentId,
			instanceId: instance.instanceId,
			type: 'list_files',
			payload: { path }
		});

		return { ok: true, path };
	},

	uploadFile: async ({ request, params, locals, platform }) => {
		if (platform?.env?.DB) {
			return fail(501, { error: 'file uploads are not supported on the Cloudflare Panel target yet' });
		}

		const [instance] = await locals.db
			.select()
			.from(serverInstances)
			.where(eq(serverInstances.id, params.serverInstanceId));
		if (!instance) return fail(404, { error: 'instance not found' });

		const form = await request.formData();
		const path = String(form.get('path') ?? '').trim();
		const file = form.get('file');
		if (!(file instanceof File) || file.size === 0) return fail(400, { error: 'choose a file' });

		// file.name is browser-controlled — basename it defensively even
		// though filemanager.Save's sandboxing on Pulse's side is the real
		// authority, same defense-in-depth spirit as duplicating withinRoot
		// per Go package rather than trusting one choke point.
		const fs = await import('node:fs');
		const nodePath = await import('node:path');
		const { Readable } = await import('node:stream');
		const { pipeline } = await import('node:stream/promises');

		const filename = nodePath.basename(file.name);
		if (!filename) return fail(400, { error: 'invalid file name' });
		const targetPath = path ? `${path}/${filename}` : filename;

		const uploadId = newFileUploadId();
		const destPath = await resolveFileUploadHoldingPath(uploadId);
		await fs.promises.mkdir(nodePath.dirname(destPath), { recursive: true });

		try {
			await pipeline(Readable.fromWeb(file.stream() as never), fs.createWriteStream(destPath));
		} catch (err) {
			await fs.promises.rm(destPath, { force: true });
			return fail(500, { error: `failed to store upload: ${err instanceof Error ? err.message : String(err)}` });
		}

		const now = Date.now();
		await locals.db.insert(fileUploads).values({
			id: uploadId,
			pulseAgentId: instance.pulseAgentId,
			instanceId: instance.instanceId,
			targetPath,
			filePath: destPath,
			status: 'ready',
			sizeBytes: file.size,
			createdAt: now,
			expiresAt: now + FILE_UPLOAD_READY_TTL_MS
		});

		await queueCommand(locals.db, {
			pulseAgentId: instance.pulseAgentId,
			instanceId: instance.instanceId,
			type: 'upload_file',
			payload: { target_path: targetPath, holding_id: uploadId }
		});

		return { ok: true, path };
	},

	deleteEntry: async ({ request, params, locals }) => {
		const form = await request.formData();
		const path = String(form.get('path') ?? '').trim();
		if (!path) return fail(400, { error: 'missing path' });

		const [instance] = await locals.db
			.select()
			.from(serverInstances)
			.where(eq(serverInstances.id, params.serverInstanceId));
		if (!instance) return fail(404, { error: 'instance not found' });

		await queueCommand(locals.db, {
			pulseAgentId: instance.pulseAgentId,
			instanceId: instance.instanceId,
			type: 'delete_file',
			payload: { path }
		});

		return { ok: true };
	}
};
