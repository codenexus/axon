/// <reference types="@cloudflare/workers-types/experimental" />
// Cloudflare Durable Object backing the Realtime interface (see
// ./index.ts) on the Cloudflare target. One DO instance *is* one channel
// -- cloudflare.ts resolves it via idFromName(channel), so every request
// for the same channel lands on the same DO instance, which just holds a
// Set of open WebSockets for that channel.
//
// Non-hibernating WebSocket accept for v1 (simpler; the hibernating API
// would reduce billed "active" DO time while a tab sits idle with a
// connection open, a real optimization worth revisiting once this is
// proven live, not attempted now).
//
// *** UNVERIFIED IN THIS ENVIRONMENT *** -- there is no live Cloudflare
// account/deploy access available here, so this class, its worker-entry
// wrapper, and the wrangler.toml bindings have never actually been
// deployed or exercised. Treat this the same as CLAUDE.md's other
// explicitly-flagged unverified-in-this-environment gaps (Tauri live-run
// before this session, the Fabric/Forge installer). A standalone spike
// (this class alone, deployed to a real account) should be the first
// thing that touches Cloudflare before trusting this in production.

import { DurableObject } from 'cloudflare:workers';

export class InstanceHub extends DurableObject {
	sockets = new Set<WebSocket>();

	async fetch(request: Request): Promise<Response> {
		const url = new URL(request.url);

		if (request.headers.get('Upgrade') === 'websocket') {
			const pair = new WebSocketPair();
			const [client, server] = Object.values(pair);
			server.accept();
			this.sockets.add(server);
			server.addEventListener('close', () => this.sockets.delete(server));
			server.addEventListener('error', () => this.sockets.delete(server));
			return new Response(null, { status: 101, webSocket: client });
		}

		if (url.pathname === '/publish' && request.method === 'POST') {
			const body = await request.text();
			for (const socket of this.sockets) {
				try {
					socket.send(body);
				} catch {
					this.sockets.delete(socket);
				}
			}
			return new Response(null, { status: 204 });
		}

		if (url.pathname === '/count' && request.method === 'GET') {
			return new Response(JSON.stringify({ count: this.sockets.size }), {
				headers: { 'content-type': 'application/json' }
			});
		}

		return new Response('not found', { status: 404 });
	}
}
