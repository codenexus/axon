import type { RequestHandler } from './$types';

// Deliberately outside /api/v1/* -- this needs the normal admin-session
// auth hooks.server.ts already applies to every non-/api/v1/ route, not
// the bearer-token/device-credential model /api/v1/* uses for Pulse.
// Reaching either branch below at all already means that check passed.
//
// The two deployment targets diverge structurally here, not just in
// implementation -- see src/lib/server/realtime/index.ts's header
// comment on why the connection-accept step can't be unified the way
// publish()/subscriberCount() are:
//
// - Cloudflare: this route genuinely performs the WebSocket upgrade, by
//   forwarding the request straight to the channel's Durable Object
//   (instance-hub-do.ts), which returns the real 101 response.
// - Node: the actual upgrade happens entirely outside SvelteKit, in
//   server.mjs's raw http.Server 'upgrade' handler -- upgrade requests
//   never reach hooks.server.ts or any SvelteKit route on that path at
//   all. server.mjs instead makes an internal loopback GET here (with
//   the browser's original cookies forwarded) purely to reuse this
//   project's real admin-session check before deciding whether to accept
//   the upgrade; this branch just needs to return a 2xx to signal "yes,
//   proceed" once reached.
export const GET: RequestHandler = async ({ params, platform, request }) => {
	const ns = platform?.env?.INSTANCE_HUB;
	if (ns) {
		const id = ns.idFromName(params.serverInstanceId);
		// @cloudflare/workers-types declares its own Request/Response types,
		// structurally near-identical but nominally distinct from the DOM
		// lib types SvelteKit's RequestHandler is typed against -- runtime
		// compatible, cast at this one boundary rather than threading the
		// Workers types further through this file.
		const response = await ns.get(id).fetch(request as any);
		return response as unknown as Response;
	}
	return new Response('ok');
};
