// Adapter-agnostic real-time push for the instance detail page: instead of
// only polling, tell any open browser tab the moment a channel's
// underlying data changed so it can refetch immediately. There's no raw
// stdout/console tailing in Axon -- the console is a request/response
// transcript built from `commands` rows -- so the payload here is
// deliberately trivial ("something changed, go reload"), not a
// data-bearing stream. Keeps this layer from duplicating +page.server.ts's
// own load/query logic.
//
// Mirrors the same branch point db/index.ts already uses
// (platform?.env?.DB): one shared interface, two backends (Cloudflare
// Durable Objects, a plain in-process Node WebSocket server), chosen at
// runtime, every caller elsewhere in the app uses only this interface and
// never knows which backend is live.
//
// The *connection-accept* half (actually terminating a WebSocket upgrade)
// genuinely can't be unified the same way -- SvelteKit exposes no raw
// socket the same way on Node vs. Workers. That's real architectural
// asymmetry, not a modeling shortcut: see realtime/node.ts and the
// /realtime/[serverInstanceId] route for where the two paths diverge.

export interface RealtimeEvent {
	type: 'changed';
}

export interface Realtime {
	publish(channel: string, event: RealtimeEvent): Promise<void>;
	subscriberCount(channel: string): Promise<number>;
}

export async function getRealtime(platform: App.Platform | undefined): Promise<Realtime> {
	const ns = platform?.env?.INSTANCE_HUB;
	if (ns) {
		const { getCloudflareRealtime } = await import('./cloudflare');
		return getCloudflareRealtime(ns);
	}
	const { getNodeRealtime } = await import('./node');
	return getNodeRealtime();
}
