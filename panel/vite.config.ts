import adapterNode from '@sveltejs/adapter-node';
import adapterCloudflare from '@sveltejs/adapter-cloudflare';
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig, type Plugin } from 'vite';
import { WebSocketServer } from 'ws';

// Three Panel deployment targets share this one config; only the adapter
// changes, selected via ADAPTER=cloudflare|node (see Makefile / package.json
// scripts). Defaults to node so `pnpm run dev` / a bare `pnpm run build`
// works without the env var set.
const adapter = process.env.ADAPTER === 'cloudflare' ? adapterCloudflare() : adapterNode();

// Dev-mode equivalent of server.mjs's WebSocket upgrade handling, so the
// real-time push layer (src/lib/server/realtime/) is actually developable
// under `pnpm run dev`, not just in a production build. Same globalThis
// anchor as realtime/node.ts and server.mjs -- see those files' comments.
// Only acts on /realtime/* upgrade requests; anything else is left alone
// so Vite's own HMR WebSocket (registered as another 'upgrade' listener
// on the same http.Server) still gets to handle its own requests.
function realtimeDevPlugin(): Plugin {
	return {
		name: 'axon-realtime-dev',
		configureServer(server) {
			const wss = new WebSocketServer({ noServer: true });
			server.httpServer?.on('upgrade', async (req, socket, head) => {
				const url = new URL(req.url ?? '', 'http://internal');
				const match = url.pathname.match(/^\/realtime\/([^/]+)$/);
				if (!match) return;
				const channel = decodeURIComponent(match[1]);

				try {
					// redirect: 'manual' is load-bearing -- see server.mjs's
					// identical check for why (fetch() follows redirects by
					// default, which would silently defeat this check).
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
					const key = '__axonRealtimeChannels';
					(globalThis as unknown as Record<string, Map<string, Set<unknown>>>)[key] ??= new Map();
					const channels = (globalThis as unknown as Record<string, Map<string, Set<unknown>>>)[key];
					let set = channels.get(channel);
					if (!set) {
						set = new Set();
						channels.set(channel, set);
					}
					set.add(ws);
					ws.once('close', () => {
						set?.delete(ws);
						if (set?.size === 0) channels.delete(channel);
					});
				});
			});
		}
	};
}

export default defineConfig({
	plugins: [
		sveltekit({
			compilerOptions: {
				// Force runes mode for the project, except for libraries. Can be removed in svelte 6.
				runes: ({ filename }) =>
					filename.split(/[/\\]/).includes('node_modules') ? undefined : true
			},
			adapter
		}),
		realtimeDevPlugin()
	]
});
