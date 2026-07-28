// Cloudflare backend for the shared Realtime interface (see ./index.ts).
// Resolves the Durable Object instance for a channel via idFromName (so
// the same channel always lands on the same DO instance) and talks to it
// with plain internal fetch() calls -- fetch-based DO RPC rather than the
// newer method-based RPC, since it's the most version-stable option
// against the pinned wrangler ^4.112.0 and doesn't require the DO class
// itself to be imported here (only its namespace binding).
//
// *** UNVERIFIED IN THIS ENVIRONMENT *** -- see instance-hub-do.ts's
// header comment. No live Cloudflare account/deploy access here.

import type { DurableObjectNamespace } from '@cloudflare/workers-types';
import type { Realtime, RealtimeEvent } from './index';

class CloudflareRealtime implements Realtime {
	constructor(private ns: DurableObjectNamespace) {}

	private stub(channel: string) {
		const id = this.ns.idFromName(channel);
		return this.ns.get(id);
	}

	async publish(channel: string, event: RealtimeEvent): Promise<void> {
		await this.stub(channel).fetch('http://internal/publish', {
			method: 'POST',
			body: JSON.stringify(event)
		});
	}

	async subscriberCount(channel: string): Promise<number> {
		const res = await this.stub(channel).fetch('http://internal/count');
		const body = (await res.json()) as { count: number };
		return body.count;
	}
}

export function getCloudflareRealtime(ns: DurableObjectNamespace): Realtime {
	return new CloudflareRealtime(ns);
}
