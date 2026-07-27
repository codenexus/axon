import { fail, redirect } from '@sveltejs/kit';
import { and, eq, inArray } from 'drizzle-orm';
import type { Actions, PageServerLoad } from './$types';
import { backups, commands, pulseAgents, serverInstances } from '$lib/server/db/schema';
import { destroySession } from '$lib/server/auth';
import { failStaleCommands, queueCommand } from '$lib/server/commands';
import { latestVersionsByPlatform, updateAvailableFor } from '$lib/server/pulseReleases';

export const load: PageServerLoad = async ({ locals }) => {
	const agents = await locals.db.select().from(pulseAgents);
	const instances = await locals.db.select().from(serverInstances);

	// Unscoped (no agent filter) — this is the one place that also has to
	// catch commands whose owning agent never heartbeats again, so a stuck
	// "Starting…/Backing up…" badge doesn't hang forever waiting on an agent
	// that isn't coming back. Run before both derived queries below so it
	// self-heals both instancesBackingUp and pendingActions in one pass.
	await failStaleCommands(locals.db);

	// A backup's (or a restore's pre-restore backup step's) stop->archive
	// cycle happens entirely between two heartbeats — Pulse's own
	// running_state reporting never has a chance to show "stopping" for it
	// (see the dashboard's "Backing up…"/"Restoring…" badge, which covers
	// exactly this gap using Panel's own command-tracking instead of waiting
	// on Pulse to report a state it structurally can't report in time).
	// trigger='pre_restore' distinguishes an in-progress restore (which also
	// creates a 'pending' backups row, for its safety backup) from a plain
	// manual/scheduled backup, so the two get different, accurate labels.
	const pendingBackups = await locals.db
		.select({ serverInstanceId: backups.serverInstanceId, trigger: backups.trigger })
		.from(backups)
		.where(eq(backups.status, 'pending'));
	const instancesBackingUp: Record<string, 'backup' | 'restore'> = {};
	for (const b of pendingBackups) {
		instancesBackingUp[b.serverInstanceId] = b.trigger === 'pre_restore' ? 'restore' : 'backup';
	}

	// Same gap applies to plain start/stop/restart: if the process finishes
	// stopping before the next heartbeat fires, running_state jumps straight
	// from running to stopped/running and the transitional state is never
	// observed on the wire at all. Track it from the command queue instead.
	const pendingStartStop = await locals.db
		.select({ pulseAgentId: commands.pulseAgentId, instanceId: commands.instanceId, type: commands.type })
		.from(commands)
		.where(
			and(
				inArray(commands.status, ['queued', 'sent']),
				inArray(commands.type, ['start_instance', 'stop_instance', 'restart_instance', 'delete_instance'])
			)
		);
	const pendingActions: Record<string, 'starting' | 'stopping' | 'restarting' | 'deleting'> = {};
	for (const c of pendingStartStop) {
		const label =
			c.type === 'start_instance'
				? 'starting'
				: c.type === 'stop_instance'
					? 'stopping'
					: c.type === 'delete_instance'
						? 'deleting'
						: 'restarting';
		pendingActions[`${c.pulseAgentId}:${c.instanceId}`] = label;
	}

	const latestVersions = await latestVersionsByPlatform(locals.db);

	return {
		agents: agents.map((agent) => ({
			...agent,
			instances: instances.filter((i) => i.pulseAgentId === agent.id),
			updateAvailable: updateAvailableFor(latestVersions, agent)
		})),
		instancesBackingUp,
		pendingActions
	};
};

export const actions: Actions = {
	queueCommand: async ({ request, locals }) => {
		const form = await request.formData();
		const pulseAgentId = String(form.get('pulse_agent_id') ?? '');
		const instanceId = String(form.get('instance_id') ?? '');
		const type = String(form.get('type') ?? '');
		if (
			!pulseAgentId ||
			!instanceId ||
			(type !== 'start_instance' && type !== 'stop_instance' && type !== 'restart_instance')
		) {
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
