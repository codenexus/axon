// Shared client-side heartbeat-timing math — used by the dashboard (agent
// online/offline) and the backups page (countdown to the next heartbeat
// while something's pending). Pure functions, no server import, safe to use
// from any .svelte component.

export interface HeartbeatTiming {
	lastSeenAt: number | null;
	// The agent's immutable --interval CLI flag value, NOT the possibly-
	// fast interval it may currently be using (Panel can suggest a much
	// shorter interval while a command is in flight or an admin is
	// watching an instance page — see the heartbeat route's
	// next_interval_seconds). Staleness/online-ness math must always use
	// this base value, or a temporary fast window would shrink the
	// "presumed offline" bar and cause false positives once things settle
	// back to normal cadence.
	baseIntervalSeconds: number | null;
}

// Agents report their own base --interval on each heartbeat; older/pre-
// upgrade agents may not have sent it yet, so fall back to Pulse's own 30s
// default rather than guessing wrong.
export function effectiveInterval(baseIntervalSeconds: number | null): number {
	return baseIntervalSeconds && baseIntervalSeconds > 0 ? baseIntervalSeconds : 30;
}

// Three missed heartbeats (using the agent's real interval when known) is a
// reasonable "presumed offline" bar.
export function isOnline(agent: HeartbeatTiming, now: number): boolean {
	if (!agent.lastSeenAt) return false;
	return now - agent.lastSeenAt < effectiveInterval(agent.baseIntervalSeconds) * 3000;
}

// Fraction of the way through the current heartbeat window, clamped to
// [0,1] so an overdue heartbeat just shows a full bar rather than
// overflowing.
export function heartbeatProgress(agent: HeartbeatTiming, now: number): number {
	if (!agent.lastSeenAt) return 0;
	const intervalMs = effectiveInterval(agent.baseIntervalSeconds) * 1000;
	return Math.min(1, Math.max(0, (now - agent.lastSeenAt) / intervalMs));
}

export function nextHeartbeatLabel(agent: HeartbeatTiming, now: number): string {
	if (!agent.lastSeenAt) return '';
	const intervalMs = effectiveInterval(agent.baseIntervalSeconds) * 1000;
	const remainingMs = agent.lastSeenAt + intervalMs - now;
	return remainingMs <= 0 ? 'due any moment' : `next check-in ~${Math.ceil(remainingMs / 1000)}s`;
}
