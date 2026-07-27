import { error, fail } from '@sveltejs/kit';
import { desc, eq } from 'drizzle-orm';
import type { Actions, PageServerLoad } from './$types';
import { commands, pulseAgents, serverDefinitions } from '$lib/server/db/schema';
import { newInstanceId, queueCommand } from '$lib/server/commands';
import { allocatePort } from '$lib/server/portAllocator';
import { resolveBedrockVersions, resolveJavaVersions, resolveVersionSelection } from '$lib/server/versionCatalog';
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

	// Definitions are global, not per-agent (they describe what to install,
	// not where), so this is unfiltered — every definition is offered
	// regardless of which agent's create-server page this is.
	const definitions = await locals.db.select().from(serverDefinitions).orderBy(desc(serverDefinitions.createdAt));

	if (!configured) {
		return { agent, configured, javaVersions: [], bedrockVersions: [], definitions, pendingCommand };
	}

	const [javaVersions, bedrockVersions] = await Promise.all([
		resolveJavaVersions(locals.db),
		resolveBedrockVersions(locals.db)
	]);

	return { agent, configured, javaVersions, bedrockVersions, definitions, pendingCommand };
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
		const definitionId = String(form.get('definition_id') ?? '').trim();

		if (!name) return fail(400, { error: 'name is required' });

		let gamePlatform: string;
		let version: string;
		let downloadUrl: string;
		let javaMajorVersion: number | undefined;

		if (definitionId) {
			// Pinned at the definition's own creation time — skip catalog
			// resolution entirely, this is not a live/"always latest" lookup.
			const [definition] = await locals.db
				.select()
				.from(serverDefinitions)
				.where(eq(serverDefinitions.id, definitionId));
			if (!definition) return fail(400, { error: 'selected definition no longer exists' });

			gamePlatform = definition.gamePlatform;
			version = definition.version;
			downloadUrl = definition.downloadUrl;
			javaMajorVersion = definition.javaMajorVersion ?? undefined;
		} else {
			gamePlatform = String(form.get('game_platform') ?? '');
			if (gamePlatform !== 'java' && gamePlatform !== 'bedrock') {
				return fail(400, { error: 'invalid edition' });
			}

			const catalogId = String(form.get('catalog_id') ?? '');
			const bedrockUrlOverride = String(form.get('download_url') ?? '').trim();
			const resolved = await resolveVersionSelection(locals.db, gamePlatform, catalogId, bedrockUrlOverride);
			if ('error' in resolved) return fail(400, { error: resolved.error });

			version = resolved.version;
			downloadUrl = resolved.downloadUrl;
			javaMajorVersion = resolved.javaMajorVersion;
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
