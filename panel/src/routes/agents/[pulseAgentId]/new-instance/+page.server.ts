import { error, fail } from '@sveltejs/kit';
import { eq } from 'drizzle-orm';
import type { Actions, PageServerLoad } from './$types';
import { commands, pulseAgents, versionCatalogEntries } from '$lib/server/db/schema';
import { newInstanceId, queueCommand } from '$lib/server/commands';
import { allocatePort } from '$lib/server/portAllocator';
import { resolveBedrockVersions, resolveJavaVersions } from '$lib/server/versionCatalog';
import type { CreateInstanceCommandPayload } from '$lib/server/protocol';

export const load: PageServerLoad = async ({ params, url, locals }) => {
	const [agent] = await locals.db.select().from(pulseAgents).where(eq(pulseAgents.id, params.pulseAgentId));
	if (!agent) throw error(404, 'agent not found');

	const configured = agent.portRangeStart != null && agent.portRangeEnd != null && !!agent.instancesRootDir;

	// A commandId in the URL (set by the client after a successful submit —
	// see +page.svelte) switches this page into a progress/result view
	// instead of the create form, without needing a separate route.
	let pendingCommand: {
		id: string;
		status: string;
		progressPhase: string | null;
		resultMessage: string | null;
		instanceId: string;
	} | null = null;

	const commandId = url.searchParams.get('commandId');
	if (commandId) {
		const [cmd] = await locals.db.select().from(commands).where(eq(commands.id, commandId));
		if (cmd && cmd.pulseAgentId === agent.id && cmd.type === 'create_instance') {
			pendingCommand = {
				id: cmd.id,
				status: cmd.status,
				progressPhase: cmd.progressPhase,
				resultMessage: cmd.resultMessage,
				instanceId: cmd.instanceId
			};
		}
	}

	if (!configured) {
		return { agent, configured, javaVersions: [], bedrockVersions: [], pendingCommand };
	}

	const [javaVersions, bedrockVersions] = await Promise.all([
		resolveJavaVersions(locals.db),
		resolveBedrockVersions(locals.db)
	]);

	return { agent, configured, javaVersions, bedrockVersions, pendingCommand };
};

export const actions: Actions = {
	create: async ({ request, params, locals }) => {
		const [agent] = await locals.db.select().from(pulseAgents).where(eq(pulseAgents.id, params.pulseAgentId));
		if (!agent) return fail(404, { error: 'agent not found' });
		if (agent.portRangeStart == null || agent.portRangeEnd == null || !agent.instancesRootDir) {
			return fail(400, { error: 'configure a port range and instances directory first' });
		}

		const form = await request.formData();
		const name = String(form.get('name') ?? '').trim();
		const gamePlatform = String(form.get('game_platform') ?? '');
		const catalogId = String(form.get('catalog_id') ?? '');
		const bedrockUrlOverride = String(form.get('download_url') ?? '').trim();

		if (!name) return fail(400, { error: 'name is required' });
		if (gamePlatform !== 'java' && gamePlatform !== 'bedrock') {
			return fail(400, { error: 'invalid edition' });
		}

		let version: string;
		let downloadUrl: string;
		let javaMajorVersion: number | undefined;

		if (gamePlatform === 'java') {
			// Always re-looked-up server-side by catalog id, never trusted
			// from client input — Mojang's manifest is authoritative and
			// there's no reason to let it be overridden (unlike Bedrock,
			// which has no authoritative source to defer to).
			const [entry] = await locals.db
				.select()
				.from(versionCatalogEntries)
				.where(eq(versionCatalogEntries.id, catalogId));
			if (!entry || entry.gamePlatform !== 'java') {
				return fail(400, { error: 'select a valid Java version' });
			}
			version = entry.version;
			downloadUrl = entry.downloadUrl;
			javaMajorVersion = entry.javaMajorVersion ?? undefined;
		} else {
			let catalogVersion = '';
			if (catalogId) {
				const [entry] = await locals.db
					.select()
					.from(versionCatalogEntries)
					.where(eq(versionCatalogEntries.id, catalogId));
				if (entry && entry.gamePlatform === 'bedrock') catalogVersion = entry.version;
			}
			downloadUrl = bedrockUrlOverride;
			version = catalogVersion || 'unknown';
			if (!downloadUrl) {
				return fail(400, { error: 'a download URL is required (the automatic lookup may have failed — see the field above)' });
			}
		}

		const port = await allocatePort(locals.db, agent.id);
		if (port == null) {
			return fail(400, { error: "no free ports remaining in this agent's configured range" });
		}

		const instanceId = newInstanceId();
		const workingDir = `${agent.instancesRootDir.replace(/\/+$/, '')}/${instanceId}`;

		const payload: CreateInstanceCommandPayload = {
			name,
			game_platform: gamePlatform,
			version,
			software_type: 'vanilla',
			download_url: downloadUrl,
			java_major_version: javaMajorVersion,
			port,
			working_dir: workingDir
		};

		const commandId = await queueCommand(locals.db, {
			pulseAgentId: agent.id,
			instanceId,
			type: 'create_instance',
			payload
		});

		return { ok: true, commandId };
	}
};
