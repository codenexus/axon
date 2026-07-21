import { json, error } from '@sveltejs/kit';
import type { RequestHandler } from './$types';
import { isAuthenticated } from '$lib/server/auth';
import { queueCommand } from '$lib/server/commands';

// Admin-facing (dashboard start/stop buttons) — session-cookie authenticated,
// unlike the sibling /enroll and /heartbeat routes which authenticate Pulse
// agents via bearer device credentials. hooks.server.ts intentionally skips
// its own session check for all of /api/v1/*, so this route checks itself.
export const POST: RequestHandler = async ({ request, locals, cookies }) => {
	if (!(await isAuthenticated(locals.db, cookies))) throw error(401, 'not authenticated');

	const body = (await request.json()) as {
		pulse_agent_id: string;
		instance_id: string;
		type: 'start_instance' | 'stop_instance';
	};
	if (!body.pulse_agent_id || !body.instance_id || !body.type) {
		throw error(400, 'pulse_agent_id, instance_id, and type are required');
	}

	await queueCommand(locals.db, {
		pulseAgentId: body.pulse_agent_id,
		instanceId: body.instance_id,
		type: body.type
	});

	return json({ ok: true });
};
