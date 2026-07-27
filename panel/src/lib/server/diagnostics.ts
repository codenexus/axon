// The fixed, hand-maintained set of diagnostic names Pulse's own
// per-platform allowlist (pulse/internal/diagnostics) supports — Panel
// doesn't need to know an agent's OS-specific allowlist in detail, it
// just always offers this same universal set and trusts Pulse to map
// each name correctly for its own platform.
export const DIAGNOSTIC_NAMES = ['uptime', 'disk_usage', 'memory', 'processes'] as const;

export type DiagnosticName = (typeof DIAGNOSTIC_NAMES)[number];

export function isDiagnosticName(value: string): value is DiagnosticName {
	return (DIAGNOSTIC_NAMES as readonly string[]).includes(value);
}
