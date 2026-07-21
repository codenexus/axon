# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

Axon is a self-hosted Minecraft server management platform (Java + Bedrock),
single-game-focused, inspired by Pterodactyl/Pelican but without a
multi-game abstraction layer. Two components released together from one
monorepo:

- **Axon Pulse** (`pulse/`) — headless Go agent, one per host, manages
  Minecraft server process(es), RCON, metrics, backups, self-update.
- **Axon Panel** (`panel/`) — SvelteKit dashboard, one codebase built three
  ways (Cloudflare Workers / plain Node / Tauri desktop).

This repo is currently a scaffold + first vertical slice, not the full v1
target — see "Project status / scope" below before assuming something is
implemented.

## Commands

### Pulse (Go agent)

```sh
cd pulse
go build ./...
go vet ./...
go test ./...
go test ./internal/mcserver -run TestStartStopLifecycle -v   # single test
```

Or from repo root: `make build-pulse`, `make pulse-test`,
`make build-pulse-linux|windows|darwin` (cross-compile).

### Panel (SvelteKit)

```sh
cd panel
pnpm install
pnpm run check                        # svelte-check
ADAPTER=node pnpm run dev             # dev server, http://localhost:5173
ADAPTER=node pnpm run build           # plain Node adapter build
ADAPTER=cloudflare pnpm run build     # Cloudflare Workers adapter build
pnpm run db:generate                  # regenerate migrations/*.sql from schema.ts via drizzle-kit
pnpm run seed:dev                     # seed local admin + enrollment token (node:sqlite dev DB)
```

Or from repo root: `make panel-install`, `make panel-dev`, `make panel-check`,
`make panel-build-node`, `make panel-build-cloudflare`, `make migrate-local`,
`make migrate-remote`, `make seed-dev`.

`ADAPTER` env var selects the SvelteKit adapter in `vite.config.ts` (defaults
to node if unset). Running the *built* node-adapter server directly (not
`pnpm run dev`) requires `ORIGIN` set (e.g. `ORIGIN=http://localhost:5173`),
or SvelteKit's CSRF check rejects all form POSTs (login, enrollment,
start/stop) with 403 — `pnpm run dev` sets this for you via Vite.

Node ≥22.13 required (pnpm 11's own requirement, plus the local dev DB uses
the `node:sqlite` builtin, stable from Node 22.5+). `corepack enable` picks
up the pnpm version pinned in `panel/package.json`'s `packageManager` field.

## Architecture

### Communication: pull-based HTTP, not gRPC

Pulse always initiates contact with Panel — heartbeat + command-poll over
plain HTTP/JSON, no inbound ports needed on any Pulse host, no persistent
connections. This replaced an earlier gRPC-based prototype: Cloudflare
Workers (the hosted Panel target) doesn't support gRPC, and none of Axon's
actual requirements (console tailing, commands, metrics) need bidirectional
streaming. It deliberately mirrors the team's Beacon RMM agent pattern, and
several pieces are directly ported/adapted from it (self-update, when it
lands, should be ported wholesale from Beacon's `agent/internal/updater/*`
and `agent/tools/{keygen,sign}` — Ed25519 sign/verify + atomic binary swap
with rollback on failed startup).

The wire contract lives in two parallel type definitions kept in sync by
hand, not by shared schema/codegen: `pulse/internal/protocol/types.go` (Go)
and `panel/src/lib/server/protocol.ts` (TypeScript).

- **Heartbeat** (`POST /api/v1/heartbeat`, Pulse → Panel, ~30–60s): host
  metrics + per-instance status array. Panel upserts `server_instances` rows
  and returns any queued commands, marking them `sent`.
- **Command results** are piggybacked onto the *next* heartbeat
  (`pending_command_results`) rather than pushed immediately — a resolved
  design decision (simplicity over latency, matches Beacon precedent), not a
  gap to "fix".
- **Enrollment** (`POST /api/v1/enroll`, one-time): token → device
  credential. Enrollment tokens and device credentials are never stored
  raw, only `sha256Hex()` hashes (`panel/src/lib/server/tokens.ts` /
  `pulse/internal/credential`).

### Pulse always runs, even same-machine

When Panel and a Minecraft server are on the same box, Pulse still runs as a
separate process talking to Panel over `localhost` using the identical
heartbeat contract used remotely — there is no special-cased "direct mode".
Don't add one; the point is that adding a second machine later requires zero
re-architecture.

### Panel: one codebase, three adapters, one DB abstraction

`panel/vite.config.ts` picks `@sveltejs/adapter-node` or
`@sveltejs/adapter-cloudflare` based on the `ADAPTER` env var — same
routes/components/logic either way. Tauri (`panel/src-tauri/`) wraps the
node-adapter build as a desktop shell; it's scaffolded but not functional
yet (see `src-tauri/README.md` — no sidecar spawning `build/index.js` yet,
so `tauri dev` only works today because `devUrl` points at a running `vite
dev`).

The DB layer (`panel/src/lib/server/db/index.ts`) branches on
`platform?.env?.DB`: if present (Cloudflare), it uses `drizzle-orm/d1`
directly against the D1 binding; otherwise (Node/Tauri/local dev) it lazily
opens a Node built-in `node:sqlite` database and drives it through
`drizzle-orm/sqlite-proxy` — deliberately not `better-sqlite3`, which needs
a native build step requiring network access to download prebuilt headers
that isn't guaranteed on every target machine. Both paths share the same
Drizzle schema (`panel/src/lib/server/db/schema.ts`). Local dev also
self-applies `migrations/*.sql` on startup (idempotent, tolerates "already
exists"); the Cloudflare target uses real `wrangler d1 migrations apply`.

Migrations are drizzle-kit-generated (`pnpm run db:generate`, config in
`panel/drizzle.config.ts`) into the repo-root `migrations/` directory — one
source of truth shared by D1 and the local sqlite bootstrap.

### Command layers (conceptually distinct — don't collapse them)

1. **Process-level** — Pulse manages the OS process directly
   (`pulse/internal/mcserver/`): install, start/stop/restart,
   stdout/stderr capture. No Minecraft protocol involved. Only
   `start_instance`/`stop_instance` exist today; graceful stop is a bare
   SIGTERM/Kill placeholder (`terminate()` in `process_unix.go` /
   `process_windows.go`) until RCON lands — it should become "save-all" +
   "stop" over RCON, not stdin piping.
2. **Console commands (RCON)** — not implemented yet. Will connect to the
   running server's RCON port rather than piping stdin, so it works
   uniformly across Vanilla/Paper/Forge/Fabric.
3. **In-game/gameplay commands** — no separate code path; anything typeable
   with `/` in-game rides the same RCON layer once that exists.

### Auth: single admin, no roles (v1 decision)

`panel/src/lib/server/auth.ts` — one `admin_settings` row, session cookies
(`adminSessions`, id = sha256 of the cookie token, not the raw token).
`hooks.server.ts` gates every route except `/login` and `/api/v1/*`, which
authenticate Pulse agents via their own bearer device credentials instead of
the session cookie. `/api/v1/commands` is the one exception inside that
namespace — it's admin-facing (dashboard start/stop buttons), so it
re-checks `isAuthenticated()` itself rather than relying on the hook.

### Theming

CSS custom properties, palette selected via `data-theme` on `<html>`,
defined in `panel/src/lib/theme/palettes.css`. Status colors
(success/warning/error/info) are fixed across all palettes on purpose —
don't let a palette override them; that's what keeps "red = danger"
meaningful under a red/orange palette like Nether. New palettes are pure
additive CSS blocks, no component changes needed. Palettes are named by
color character (Classic/End/Nether), not exact in-game names — a
trademark consideration for the OSS project; keep that convention for new
palettes.

## Project status / scope

Deliberately implemented in this vertical slice:

- Pulse: enrollment, heartbeat, start/stop of one or more configured
  Minecraft processes.
- Panel: single-admin auth, enrollment token generation, dashboard listing
  agents/instances, start/stop command queueing.

Deliberately deferred — don't assume half-built unless you find code for it:

- RCON console commands, backups, self-update, file management
  (plugins/mods browsing, uploads), multi-user auth/RBAC, mDNS/Bonjour
  discovery, Java-prerequisite install flow, Tauri sidecar process spawning.

## License

AGPL-3.0.
