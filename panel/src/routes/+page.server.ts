import { fail, redirect } from '@sveltejs/kit';
import type { Actions, PageServerLoad } from './$types';
import { enrollmentTokens, pulseAgents, serverInstances } from '$lib/server/db/schema';
import { destroySession } from '$lib/server/auth';
import { queueCommand } from '$lib/server/commands';
import { randomToken, sha256Hex } from '$lib/server/tokens';

const ENROLLMENT_TOKEN_TTL_MS = 30 * 60 * 1000; // 30 minutes

export const load: PageServerLoad = async ({ locals }) => {
	const agents = await locals.db.select().from(pulseAgents);
	const instances = await locals.db.select().from(serverInstances);

	return {
		agents: agents.map((agent) => ({
			...agent,
			instances: instances.filter((i) => i.pulseAgentId === agent.id)
		}))
	};
};

export const actions: Actions = {
	generateEnrollmentToken: async ({ locals }) => {
		const token = randomToken(16);
		const now = Date.now();
		await locals.db.insert(enrollmentTokens).values({
			id: `tok_${randomToken(8)}`,
			tokenHash: sha256Hex(token),
			createdAt: now,
			expiresAt: now + ENROLLMENT_TOKEN_TTL_MS
		});
		return { enrollmentToken: token };
	},

	queueCommand: async ({ request, locals }) => {
		const form = await request.formData();
		const pulseAgentId = String(form.get('pulse_agent_id') ?? '');
		const instanceId = String(form.get('instance_id') ?? '');
		const type = String(form.get('type') ?? '');
		if (!pulseAgentId || !instanceId || (type !== 'start_instance' && type !== 'stop_instance')) {
			return fail(400, { error: 'invalid command' });
		}
		await queueCommand(locals.db, { pulseAgentId, instanceId, type });
		return { ok: true };
	},

	logout: async ({ locals, cookies }) => {
		await destroySession(locals.db, cookies);
		throw redirect(303, '/login');
	}
};
