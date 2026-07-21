import { json, error } from '@sveltejs/kit';
import { and, eq } from 'drizzle-orm';
import type { RequestHandler } from './$types';
import { commands, pulseAgents, serverInstances } from '$lib/server/db/schema';
import { bearerToken } from '$lib/server/http';
import { sha256Hex } from '$lib/server/tokens';
import type {
	HeartbeatRequestBody,
	HeartbeatResponseBody,
	WireCommand
} from '$lib/server/protocol';

export const POST: RequestHandler = async ({ request, locals }) => {
	const token = bearerToken(request);
	if (!token) throw error(401, 'missing device credential');

	const [agent] = await locals.db
		.select()
		.from(pulseAgents)
		.where(eq(pulseAgents.deviceCredentialHash, sha256Hex(token)));
	if (!agent) throw error(401, 'unknown device credential');

	const body = (await request.json()) as HeartbeatRequestBody;
	const now = Date.now();

	await locals.db
		.update(pulseAgents)
		.set({
			lastSeenAt: now,
			pulseVersion: body.pulse_version,
			cpuUsagePercent: body.host?.cpu_usage_percent,
			cpuCores: body.host?.cpu_cores,
			ramTotalBytes: body.host?.ram_total_bytes,
			ramUsedBytes: body.host?.ram_used_bytes
		})
		.where(eq(pulseAgents.id, agent.id));

	for (const instance of body.instances ?? []) {
		const rowId = `${agent.id}:${instance.id}`;
		const values = {
			id: rowId,
			pulseAgentId: agent.id,
			instanceId: instance.id,
			name: instance.name,
			gamePlatform: instance.game_platform,
			version: instance.version,
			softwareType: instance.software_type,
			runningState: instance.running_state,
			playerCount: instance.player_count ?? 0,
			uptimeSeconds: instance.uptime_seconds ?? 0,
			updatedAt: now
		};
		await locals.db
			.insert(serverInstances)
			.values(values)
			.onConflictDoUpdate({ target: serverInstances.id, set: values });
	}

	for (const result of body.pending_command_results ?? []) {
		await locals.db
			.update(commands)
			.set({
				status: result.success ? 'completed' : 'failed',
				resultMessage: result.message,
				completedAt: now
			})
			.where(eq(commands.id, result.command_id));
	}

	const queued = await locals.db
		.select()
		.from(commands)
		.where(and(eq(commands.pulseAgentId, agent.id), eq(commands.status, 'queued')));

	const wireCommands: WireCommand[] = queued.map((c) => ({
		id: c.id,
		type: c.type,
		instance_id: c.instanceId
	}));

	if (queued.length > 0) {
		for (const c of queued) {
			await locals.db.update(commands).set({ status: 'sent', sentAt: now }).where(eq(commands.id, c.id));
		}
	}

	const response: HeartbeatResponseBody = { commands: wireCommands };
	return json(response);
};
