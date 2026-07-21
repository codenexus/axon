# Axon Panel

SvelteKit dashboard, one codebase built three ways:

- `ADAPTER=node pnpm run build` — plain Node process (same-machine/VPS)
- `ADAPTER=cloudflare pnpm run build` — Cloudflare Workers + D1 (hosted)
- Tauri (`src-tauri/`) wraps the `ADAPTER=node` build as a desktop app (home-lab)

## Dev

```
pnpm install
pnpm run seed:dev      # creates a local admin + a throwaway enrollment token
ADAPTER=node pnpm run dev
```

Local dev (and the plain-Node/Tauri targets in general) use Node's built-in
`node:sqlite`, not a native module — see `src/lib/server/db/index.ts`. The
Cloudflare target uses D1 through the exact same Drizzle schema
(`src/lib/server/db/schema.ts`); migrations are generated with
`pnpm run db:generate` into the repo-root `migrations/` directory and applied
with `wrangler d1 migrations apply` for the hosted target.

### Running the built adapter-node server directly

`ADAPTER=node pnpm run build && node build/index.js` needs an `ORIGIN` env
var set (e.g. `ORIGIN=http://localhost:5173`) — without it, SvelteKit's CSRF
check can't verify same-origin form POSTs (login, enrollment token
generation, start/stop) against the `Host` header it sees, and rejects them
with 403. `pnpm run dev` doesn't need this (Vite's dev server sets it for
you); only the built server does. Tauri's sidecar (once implemented) will
need to set the same env var when it spawns `build/index.js`.

### Cloudflare target

Not needed for local dev — only when actually testing/deploying the hosted
target:

```sh
pnpm exec wrangler login                 # one-time Cloudflare auth
pnpm exec wrangler d1 create axon        # prints a database_id
cp wrangler.toml.example wrangler.toml   # gitignored; paste the database_id in
pnpm exec wrangler d1 migrations apply axon --local    # or --remote once the DB exists on Cloudflare
ADAPTER=cloudflare pnpm run build
pnpm exec wrangler dev                   # local smoke test against the D1 binding
```

## Status

Vertical slice: single-admin login, enrollment token generation, `/api/v1/enroll`
and `/api/v1/heartbeat` for Pulse agents, and a dashboard that lists agents/instances
and can queue `start_instance`/`stop_instance` commands. Not yet implemented: RCON
console, backups, file management, multi-user auth, mDNS discovery.
