import type { Db } from './db';
import { commands } from './db/schema';
import { randomToken } from './tokens';

export type CommandType = 'start_instance' | 'stop_instance';

export async function queueCommand(
	db: Db,
	params: { pulseAgentId: string; instanceId: string; type: CommandType }
): Promise<void> {
	await db.insert(commands).values({
		id: `cmd_${randomToken(8)}`,
		pulseAgentId: params.pulseAgentId,
		instanceId: params.instanceId,
		type: params.type,
		status: 'queued',
		createdAt: Date.now()
	});
}
