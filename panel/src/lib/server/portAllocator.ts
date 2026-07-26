import { and, eq, inArray, isNotNull } from 'drizzle-orm';
import type { Db } from './db';
import { commands, pulseAgents, serverInstances } from './db/schema';

// Returns the first free port in the agent's configured range, considering
// ports already recorded on server_instances for this agent (instances
// Panel created via create_instance — never legacy hand-configured ones,
// which never report a port on the wire at all) AND ports already claimed
// by a still-in-flight create_instance command that hasn't resolved into a
// server_instances row yet (no row is pre-inserted for create_instance —
// see the instance-page pattern for backups' pregenerated-row contrast).
// Without that second check, two create-server submissions in quick
// succession could race and allocate the same port. Throws if the agent
// has no port range configured; returns null if the range is exhausted.
export async function allocatePort(db: Db, pulseAgentId: string): Promise<number | null> {
	const [agent] = await db
		.select({ portRangeStart: pulseAgents.portRangeStart, portRangeEnd: pulseAgents.portRangeEnd })
		.from(pulseAgents)
		.where(eq(pulseAgents.id, pulseAgentId));

	if (!agent || agent.portRangeStart == null || agent.portRangeEnd == null) {
		throw new Error('no port range configured for this agent');
	}

	const usedPorts = new Set<number>();

	const existing = await db
		.select({ port: serverInstances.port })
		.from(serverInstances)
		.where(and(eq(serverInstances.pulseAgentId, pulseAgentId), isNotNull(serverInstances.port)));
	for (const row of existing) {
		if (row.port != null) usedPorts.add(row.port);
	}

	const pending = await db
		.select({ payload: commands.payload })
		.from(commands)
		.where(
			and(
				eq(commands.pulseAgentId, pulseAgentId),
				eq(commands.type, 'create_instance'),
				inArray(commands.status, ['queued', 'sent'])
			)
		);
	for (const row of pending) {
		if (!row.payload) continue;
		try {
			const payload = JSON.parse(row.payload) as { port?: number };
			if (typeof payload.port === 'number') usedPorts.add(payload.port);
		} catch {
			// Malformed payload shouldn't be possible — ignore defensively.
		}
	}

	for (let port = agent.portRangeStart; port <= agent.portRangeEnd; port++) {
		if (!usedPorts.has(port)) return port;
	}
	return null;
}
