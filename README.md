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

## Development

See `pulse/README.md` and `panel/README.md` (once present) for
component-specific setup. `make help` lists top-level tasks.

## License

AGPL-3.0 — see [LICENSE](./LICENSE).
