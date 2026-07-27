import { json, error } from '@sveltejs/kit';
import { and, desc, eq } from 'drizzle-orm';
import type { RequestHandler } from './$types';
import { commands, pulseAgents, pulseReleases, serverInstances } from '$lib/server/db/schema';
import { failStaleCommands, resolveCommandOutcome } from '$lib/server/commands';
import { pruneExpiredDownloads } from '$lib/server/backupDownloads';
import { pruneExpiredFileUploads } from '$lib/server/fileUploads';
import { runSchedulesForAgent } from '$lib/server/backupSchedules';
import { bearerToken } from '$lib/server/http';
import { sha256Hex } from '$lib/server/tokens';
import type {
	HeartbeatRequestBody,
	HeartbeatResponseBody,
	UpdateInfo,
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
			intervalSeconds: body.interval_seconds,
			pulseVersion: body.pulse_version,
			cpuUsagePercent: body.host?.cpu_usage_percent,
			cpuCores: body.host?.cpu_cores,
			ramTotalBytes: body.host?.ram_total_bytes,
			ramUsedBytes: body.host?.ram_used_bytes,
			diskUsageJson: JSON.stringify(body.host?.disks ?? []),
			hostUptimeSeconds: body.host?.uptime_seconds
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
			updatedAt: now,
			port: instance.port ?? null
		};
		await locals.db
			.insert(serverInstances)
			.values(values)
			.onConflictDoUpdate({ target: serverInstances.id, set: values });
	}

	for (const result of body.pending_command_results ?? []) {
		const [cmd] = await locals.db.select().from(commands).where(eq(commands.id, result.command_id));
		// Unknown id, or already resolved (e.g. by the stale-command sweep
		// below on a previous heartbeat) — a late genuine result must never
		// overwrite an already-terminal row.
		if (!cmd || cmd.status !== 'sent') continue;

		await resolveCommandOutcome(
			locals.db,
			cmd,
			{
				success: result.success,
				message: result.message,
				sizeBytes: result.size_bytes,
				checksum: result.checksum,
				output: result.output
			},
			now
		);
	}

	for (const progress of body.in_progress_commands ?? []) {
		// Guarded to status='sent' only, same reasoning as the
		// pending_command_results guard above — never touch an
		// already-terminal row (e.g. one the stale-command sweep just
		// failed on a previous heartbeat, racing a late progress report).
		await locals.db
			.update(commands)
			.set({ progressPhase: progress.phase })
			.where(and(eq(commands.id, progress.command_id), eq(commands.status, 'sent')));
	}

	// Opportunistic work, all piggybacked on this agent's regular heartbeat
	// cadence rather than a real timer, consistent with the project's
	// request-driven design: abandoned download holds (admin requested a
	// download, then never clicked it, or it never became ready), commands
	// stuck 'sent' because Pulse died/restarted before reporting a result
	// (see CLAUDE.md's former "Known gaps" entry on this), and due backup
	// schedules / retention pruning for this agent's instances. Runs before
	// the queued-command select below so anything just queued here (a
	// scheduled backup_instance, or a retention delete_backup) is picked up
	// and sent in this same response.
	await pruneExpiredDownloads(locals.db, agent.id);
	await pruneExpiredFileUploads(locals.db, agent.id);
	await failStaleCommands(locals.db, agent.id);
	await runSchedulesForAgent(locals.db, agent.id, now);

	const queued = await locals.db
		.select()
		.from(commands)
		.where(and(eq(commands.pulseAgentId, agent.id), eq(commands.status, 'queued')));

	const wireCommands: WireCommand[] = queued.map((c) => ({
		id: c.id,
		type: c.type,
		instance_id: c.instanceId,
		payload: c.payload ? JSON.parse(c.payload) : undefined
	}));

	if (queued.length > 0) {
		for (const c of queued) {
			await locals.db.update(commands).set({ status: 'sent', sentAt: now }).where(eq(commands.id, c.id));
		}
	}

	// Self-update: offer the newest published release for this agent's
	// os/arch whenever its version differs from what it just reported.
	// Self-resolving — once Pulse swaps and restarts, its next heartbeat
	// reports the new version and this stops matching, no cleanup needed.
	// No downgrade protection: pulse_version is a git-describe short hash
	// with no total order, so "differs" is the whole check — see CLAUDE.md.
	let update: UpdateInfo | undefined;
	const [latestRelease] = await locals.db
		.select()
		.from(pulseReleases)
		.where(and(eq(pulseReleases.os, agent.os), eq(pulseReleases.arch, agent.arch)))
		.orderBy(desc(pulseReleases.createdAt))
		.limit(1);
	if (latestRelease && latestRelease.version !== body.pulse_version) {
		update = {
			version: latestRelease.version,
			download_url: latestRelease.downloadUrl,
			signature_hex: latestRelease.signatureHex
		};
	}

	const response: HeartbeatResponseBody = { commands: wireCommands, update };
	return json(response);
};
