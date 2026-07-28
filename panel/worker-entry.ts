// Hand-written worker entrypoint, replacing the generated
// .svelte-kit/cloudflare/_worker.js as wrangler.toml's `main`.
// adapter-cloudflare generates a worker module that only exports the
// SvelteKit fetch handler (its default export) -- there's no established
// path in this project for also exporting an additional named class (a
// Durable Object) from that same generated module, so this file re-wraps
// it, adding the DO export alongside. Only exists after ADAPTER=cloudflare
// pnpm run build has produced the generated file -- same build-then-deploy
// ordering the original main path already implied.
//
// *** UNVERIFIED IN THIS ENVIRONMENT *** -- whether this wrapper-export
// approach actually works with the pinned adapter-cloudflare/wrangler
// versions has never been confirmed against a real deploy. See
// instance-hub-do.ts's header comment; a standalone spike should confirm
// this before trusting it in production.

export { default } from './.svelte-kit/cloudflare/_worker.js';
export { InstanceHub } from './src/lib/server/realtime/instance-hub-do';
