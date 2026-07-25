import type { Db } from './db';
import { commands } from './db/schema';
import { randomToken } from './tokens';

export type CommandType =
	| 'start_instance'
	| 'stop_instance'
	| 'restart_instance'
	| 'backup_instance'
	| 'restore_backup'
	| 'delete_backup'
	| 'push_backup';

export async function queueCommand(
	db: Db,
	params: { pulseAgentId: string; instanceId: string; type: CommandType; payload?: unknown }
): Promise<string> {
	const id = `cmd_${randomToken(8)}`;
	await db.insert(commands).values({
		id,
		pulseAgentId: params.pulseAgentId,
		instanceId: params.instanceId,
		type: params.type,
		payload: params.payload !== undefined ? JSON.stringify(params.payload) : null,
		status: 'queued',
		createdAt: Date.now()
	});
	return id;
}

/** Generates a Panel-owned backup id — Pulse always uses this verbatim as its on-disk filename stem, never inventing its own. */
export function newBackupId(): string {
	return `bkp_${randomToken(8)}`;
}
