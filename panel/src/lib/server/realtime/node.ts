// Node/Tauri backend for the shared Realtime interface (see ./index.ts) --
// process-local, no external service. A single module-level Map holds the
// live WebSocket connections per channel; publish()/subscriberCount() act
// on it directly. No EventEmitter -- one would be redundant indirection
// when we're already holding the sockets themselves, and nothing else in
// this codebase needs to observe these events.
//
// The actual WebSocket *upgrade* handling (accepting a raw HTTP upgrade
// request and adding the resulting socket to `channels` below) happens
// outside this module, in server.mjs (production) and a small vite.config
// dev-server plugin (development) -- see those files' comments. Neither
// SvelteKit route handlers nor the adapter-node "build/index.js" output
// expose a hook for the raw Node http.Server's 'upgrade' event, so it has
// to be wired at that layer instead.

import type { WebSocket } from 'ws';
import type { Realtime, RealtimeEvent } from './index';

// server.mjs (the production entrypoint that actually accepts WebSocket
// upgrades -- see its own comments) runs as plain Node, outside Vite's
// bundling, so it can't import this TS module directly without its own
// separate compiled copy, which would give it a *different* module
// instance and thus a different, disconnected channels Map. Anchoring on
// globalThis instead of a module-scoped const guarantees both sides
// share literally the same object within one OS process, regardless of
// how each side's code got loaded. The dev-server Vite plugin (see
// vite.config.ts) uses the same key for the same reason.
const KEY = '__axonRealtimeChannels';
const g = globalThis as unknown as Record<string, Map<string, Set<WebSocket>> | undefined>;
const channels = (g[KEY] ??= new Map<string, Set<WebSocket>>());

// Exported so server.mjs/the dev-server plugin can add a newly-upgraded
// socket to the right channel without reaching into this module's
// private state directly.
export function addSubscriber(channel: string, socket: WebSocket): void {
	let set = channels.get(channel);
	if (!set) {
		set = new Set();
		channels.set(channel, set);
	}
	set.add(socket);
	socket.once('close', () => {
		set?.delete(socket);
		if (set && set.size === 0) channels.delete(channel);
	});
}

class NodeRealtime implements Realtime {
	async publish(channel: string, event: RealtimeEvent): Promise<void> {
		const set = channels.get(channel);
		if (!set || set.size === 0) return;
		const payload = JSON.stringify(event);
		for (const socket of set) {
			// OPEN === 1, avoid importing the ws value export just for this
			// constant.
			if (socket.readyState === 1) socket.send(payload);
		}
	}

	async subscriberCount(channel: string): Promise<number> {
		return channels.get(channel)?.size ?? 0;
	}
}

const instance = new NodeRealtime();

export function getNodeRealtime(): Realtime {
	return instance;
}
