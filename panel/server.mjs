// Production entrypoint for the ADAPTER=node target, replacing the
// generated `node build/index.js`. Needed because raw WebSocket upgrade
// handling (for the real-time push layer -- see
// src/lib/server/realtime/) requires the underlying Node http.Server's
// 'upgrade' event, which neither SvelteKit route handlers nor
// build/index.js's self-contained server expose a hook for. This file
// imports the same `handler` middleware build/index.js does (from
// build/handler.js, not the whole index.js, which starts its own
// server), builds the HTTP server directly, and adds WebSocket handling
// on top.
//
// ORIGIN/BODY_SIZE_LIMIT/etc. are read internally by the handler chunk
// itself at import time (see build/server/chunks/handler-*.js), so they
// still work exactly as documented in README.md -- only HOST/PORT are
// replicated here, matching build/index.js's own simple `env(...,
// default)` reads.
//
// Run exactly like the old command: `node server.mjs` (env vars
// unchanged). Update any existing systemd unit's ExecStart= accordingly.

import http from 'node:http';
import { handler } from './build/handler.js';
import { WebSocketServer } from 'ws';

const host = process.env.HOST ?? '0.0.0.0';
const port = process.env.PORT ?? '3000';

const server = http.createServer(handler);

// Same globalThis anchor as src/lib/server/realtime/node.ts -- this file
// runs as plain Node, outside Vite's bundling of that module, so it
// can't import it directly without ending up with a second, disconnected
// module instance (and thus a separate, empty channels Map that
// publish() calls from the SvelteKit-bundled route code would never see
// subscribers on). See that file's comment for the full reasoning.
const KEY = '__axonRealtimeChannels';
globalThis[KEY] ??= new Map();
const channels = globalThis[KEY];

const wss = new WebSocketServer({ noServer: true });

server.on('upgrade', async (req, socket, head) => {
	const url = new URL(req.url ?? '', 'http://internal');
	const match = url.pathname.match(/^\/realtime\/([^/]+)$/);
	if (!match) {
		socket.destroy();
		return;
	}
	const channel = decodeURIComponent(match[1]);

	// Upgrade requests bypass hooks.server.ts's admin-session gate
	// entirely -- this event fires on the raw http.Server before
	// SvelteKit's own request pipeline ever sees anything. Reuse the
	// real check via an actual loopback HTTP round-trip to this same
	// process's own /realtime/[id] route (see that file's comment),
	// forwarding the browser's original cookies, rather than
	// re-implementing session validation here in plain JS.
	try {
		// redirect: 'manual' is load-bearing -- fetch() follows redirects by
		// default, which would follow hooks.server.ts's 303-to-/login for an
		// unauthenticated request straight to the login page's own 200 OK,
		// making authCheck.ok true for a request that was actually rejected.
		// Caught live: an unauthenticated WebSocket connected successfully
		// until this was added.
		const authCheck = await fetch(`http://${req.headers.host}/realtime/${encodeURIComponent(channel)}`, {
			headers: { cookie: req.headers.cookie ?? '' },
			redirect: 'manual'
		});
		if (!authCheck.ok) {
			socket.destroy();
			return;
		}
	} catch {
		socket.destroy();
		return;
	}

	wss.handleUpgrade(req, socket, head, (ws) => {
		let set = channels.get(channel);
		if (!set) {
			set = new Set();
			channels.set(channel, set);
		}
		set.add(ws);
		ws.once('close', () => {
			set.delete(ws);
			if (set.size === 0) channels.delete(channel);
		});
	});
});

server.listen({ host, port }, () => {
	console.log(`Listening on http://${host}:${port}`);
});

process.on('SIGTERM', () => server.close());
process.on('SIGINT', () => server.close());
