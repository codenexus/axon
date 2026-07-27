import { error, fail } from '@sveltejs/kit';
import { and, desc, eq } from 'drizzle-orm';
import type { Actions, PageServerLoad } from './$types';
import { commands, pulseAgents, serverInstances } from '$lib/server/db/schema';
import { failStaleCommands, queueCommand } from '$lib/server/commands';
import { isDiagnosticName } from '$lib/server/diagnostics';
import { latestVersionsByPlatform, updateAvailableFor } from '$lib/server/pulseReleases';

export const load: PageServerLoad = async ({ params, locals }) => {
	const [agent] = await locals.db.select().from(pulseAgents).where(eq(pulseAgents.id, params.pulseAgentId));
	if (!agent) throw error(404, 'agent not found');

	await failStaleCommands(locals.db, agent.id);

	const instances = await locals.db
		.select()
		.from(serverInstances)
		.where(eq(serverInstances.pulseAgentId, agent.id));

	const latestVersions = await latestVersionsByPlatform(locals.db);
	const updateAvailable = updateAvailableFor(latestVersions, agent);

	// Host-level commands (no instance target) are stored with
	// instanceId='' — see the runDiagnostic action below. Last 20,
	// newest-last, same chronological-scrollback convention as the
	// instance page's RCON console transcript.
	const recentDiagnostics = await locals.db
		.select()
		.from(commands)
		.where(and(eq(commands.pulseAgentId, agent.id), eq(commands.type, 'run_diagnostic')))
		.orderBy(desc(commands.createdAt))
		.limit(20);
	recentDiagnostics.reverse();

	return { agent, instances, updateAvailable, recentDiagnostics };
};

// Empty string means "not set" — distinct from an invalid non-numeric or
// non-positive value, which is a real input error.
function parsePositiveIntOrNull(raw: FormDataEntryValue | null): number | null | 'invalid' {
	const value = String(raw ?? '').trim();
	if (!value) return null;
	const parsed = Number(value);
	if (!Number.isInteger(parsed) || parsed <= 0) return 'invalid';
	return parsed;
}

export const actions: Actions = {
	saveAgentSettings: async ({ request, params, locals }) => {
		const [agent] = await locals.db.select().from(pulseAgents).where(eq(pulseAgents.id, params.pulseAgentId));
		if (!agent) return fail(404, { error: 'agent not found' });

		const form = await request.formData();
		const portRangeStart = parsePositiveIntOrNull(form.get('port_range_start'));
		const portRangeEnd = parsePositiveIntOrNull(form.get('port_range_end'));
		const instancesRootDir = String(form.get('instances_root_dir') ?? '').trim();

		if (portRangeStart === 'invalid' || portRangeEnd === 'invalid') {
			return fail(400, { error: 'port range must be positive whole numbers' });
		}
		if (portRangeStart === null || portRangeEnd === null) {
			return fail(400, { error: 'both a start and end port are required' });
		}
		if (portRangeStart >= portRangeEnd) {
			return fail(400, { error: 'port range start must be less than end' });
		}
		if (!instancesRootDir) {
			return fail(400, { error: 'an instances directory is required' });
		}

		await locals.db
			.update(pulseAgents)
			.set({ portRangeStart, portRangeEnd, instancesRootDir })
			.where(eq(pulseAgents.id, agent.id));

		return { ok: true };
	},

	runDiagnostic: async ({ request, params, locals }) => {
		const form = await request.formData();
		const name = String(form.get('name') ?? '');
		if (!isDiagnosticName(name)) {
			return fail(400, { error: 'unknown diagnostic' });
		}
		const args = String(form.get('args') ?? '').trim();

		const [agent] = await locals.db.select().from(pulseAgents).where(eq(pulseAgents.id, params.pulseAgentId));
		if (!agent) return fail(404, { error: 'agent not found' });

		// Host-level command, not scoped to any instance — instanceId is
		// intentionally empty (the commands.instanceId column is NOT NULL,
		// not "non-empty"), and the recentDiagnostics query above filters on
		// type='run_diagnostic' rather than any instance id.
		await queueCommand(locals.db, {
			pulseAgentId: agent.id,
			instanceId: '',
			type: 'run_diagnostic',
			payload: args ? { name, args } : { name }
		});

		return { ok: true };
	}
};
