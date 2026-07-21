# Axon

Axon is a self-hosted Minecraft server management platform — Java and Bedrock,
single-game-focused (no multi-game abstraction layer), inspired by
Pterodactyl/Pelican.

It has two components, versioned and released together out of this monorepo:

- **Axon Pulse** (`pulse/`) — a small Go agent that runs on any machine
  hosting Minecraft server instances: process lifecycle, RCON bridge, host
  and per-server metrics, backups, self-update.
- **Axon Panel** (`panel/`) — a SvelteKit dashboard, built once and deployed
  three ways via SvelteKit adapters: Cloudflare Workers (hosted), plain Node
  (same-machine/VPS), or wrapped in Tauri as a native desktop app
  (home-lab). All three targets share the same routes/components/logic.

Pulse always talks to Panel over a pull-based HTTP heartbeat + command-poll
contract — Pulse initiates every request, so no inbound ports are required on
any Pulse-hosting machine. This mirrors the team's Beacon RMM agent pattern
instead of the gRPC approach used in an earlier prototype (gRPC isn't
supported on Cloudflare Workers, and none of Axon's actual requirements need
bidirectional streaming).

## Status

Early scaffold + first vertical slice: Pulse can start/stop a single
Minecraft server process and heartbeat host/instance state to Panel; Panel
can enroll a Pulse agent and queue start/stop commands. RCON console
commands, backups, self-update, file management, and multi-user auth are
not yet implemented.

## Repo layout

```
pulse/        Go agent
panel/        SvelteKit app (adapter-cloudflare / adapter-node / Tauri)
migrations/   D1/SQLite schema migrations, shared by all Panel deployment targets
scripts/      Dev/release utility scripts
```

## Prerequisites

- **Go 1.22+** (pulse)
- **Node 22.13+** — pnpm 11 itself requires this, and Panel's local dev DB
  uses the `node:sqlite` builtin (stable from Node 22.5+). Tested on Node 24.
  CI got bitten by this once already (ran on Node 20 → cryptic
  `ERR_UNKNOWN_BUILTIN_MODULE: node:sqlite` failure) — don't go below 22.13.
- **pnpm**, version-pinned via `panel/package.json`'s `packageManager`
  field — run `corepack enable` once per machine and `pnpm` will
  auto-install/use the exact pinned version, no manual pnpm install needed.
- No API keys or `.env` files are needed for local dev — the node-adapter
  target uses a local SQLite file created on first run. Only the Cloudflare
  target needs Cloudflare credentials (see `panel/README.md`).

## Development

See `pulse/README.md` and `panel/README.md` for component-specific setup.
`make help` lists top-level tasks.

### Quickstart — run the full Pulse↔Panel loop locally

```sh
git clone https://github.com/codenexus/axon.git && cd axon

# Panel
cd panel && pnpm install
pnpm run seed:dev          # prints a dev admin password + an enrollment token
ADAPTER=node pnpm run dev  # http://localhost:5173 — log in with the seeded password
```

In a second terminal, build and run Pulse against a real (or stand-in)
server command, using the enrollment token seed:dev printed:

```sh
cd pulse
go build -o pulse ./cmd/pulse
cp pulse.instances.example.json pulse.instances.json   # point "command"/"working_dir" at a real server, or any long-running command for a smoke test
./pulse --server-url http://localhost:5173 --enroll-token <token-from-seed:dev>
```

Pulse enrolls, then heartbeats every ~30s. Refresh the Panel dashboard to see
the agent and its instance appear; Start/Stop buttons queue commands Pulse
picks up on its next poll.

## License

AGPL-3.0 — see [LICENSE](./LICENSE).
