import { desc } from 'drizzle-orm';
import type { Db } from './db';
import { pulseReleases } from './db/schema';

// The newest published version per (os, arch), for the dashboard/agent-page
// "update available" note — same comparison the heartbeat route itself does
// (see api/v1/heartbeat/+server.ts), just surfaced for visibility rather
// than acted on. Keyed as "${os}:${arch}".
export type LatestVersionsByPlatform = Map<string, string>;

export async function latestVersionsByPlatform(db: Db): Promise<LatestVersionsByPlatform> {
	const rows = await db.select().from(pulseReleases).orderBy(desc(pulseReleases.createdAt));
	const latest: LatestVersionsByPlatform = new Map();
	for (const row of rows) {
		const key = `${row.os}:${row.arch}`;
		if (!latest.has(key)) latest.set(key, row.version);
	}
	return latest;
}

export function updateAvailableFor(
	latest: LatestVersionsByPlatform,
	agent: { os: string; arch: string; pulseVersion: string }
): string | null {
	const latestVersion = latest.get(`${agent.os}:${agent.arch}`);
	if (!latestVersion || latestVersion === agent.pulseVersion) return null;
	return latestVersion;
}
