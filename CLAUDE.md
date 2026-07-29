# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

Axon is a self-hosted Minecraft server management platform (Java + Bedrock),
single-game-focused, inspired by Pterodactyl/Pelican but without a
multi-game abstraction layer. Two components released together from one
monorepo:

- **Axon Pulse** (`pulse/`) — headless Go agent, one per host, manages
  Minecraft server process(es), RCON, metrics, backups, provisioning,
  self-update.
- **Axon Panel** (`panel/`) — SvelteKit dashboard, one codebase built three
  ways (Cloudflare Workers / plain Node / Tauri desktop).

See "Project status / scope" near the end of this file before assuming
something is or isn't implemented — don't infer scope from this intro.

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
`make build-pulse-linux|windows|darwin` (cross-compile). All `build-pulse*`
targets inject a real version string via
`-ldflags -X main.version=$(git describe --always --dirty)` — there are no
release tags yet, so this is a short commit hash (+`-dirty` if the working
tree has uncommitted changes). This is what Panel's dashboard shows as
"Pulse vX" per agent; without it every build looks identical ("dev"),
making it hard to tell what's actually deployed to a given node. If you
build Pulse by hand (not via `make`) for anything other than a throwaway
local test, pass the same `-ldflags` — e.g.
`GOOS=linux GOARCH=amd64 go build -ldflags="-s -w -X main.version=$(git describe --always --dirty)" -o pulse ./cmd/pulse`.

To publish a signed release for self-update (see "Self-update" below):
`go run ./tools/keygen` once to generate a keypair (never reuse this for a
second key — see that section), then for each release binary
`AXON_SIGNING_KEY=<hex private key> go run ./tools/sign <binary-path>` to
get the hex signature to paste into Panel's `/settings` publish form.

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

Two more env vars, both Node-adapter-only, both with working defaults if
unset: `AXON_LOCAL_DB_PATH` (default `<cwd>/axon-local.db`) and
`AXON_BACKUP_HOLDING_DIR` (default `<cwd>/.backup-holding` — see "Backups"
below for what this holds and why it's transient).

Node ≥22.13 required (pnpm 11's own requirement, plus the local dev DB uses
the `node:sqlite` builtin, stable from Node 22.5+). `corepack enable` picks
up the pnpm version pinned in `panel/package.json`'s `packageManager` field.

### Local dev/testing workflow

For any change touching Pulse↔Panel interaction, spin up an **isolated
sandbox** before touching a real dev server or a real Pulse-managed host:
a scratch dir with its own `AXON_LOCAL_DB_PATH` and (if backups/uploads are
involved) `AXON_BACKUP_HOLDING_DIR`/`AXON_FILE_UPLOAD_HOLDING_DIR`, a Pulse
binary pointed at a throwaway `pulse.instances.json` with a `sh -c "sleep
N"` stand-in instance (matches the Go test philosophy — see
`manager_test.go`), `HOME` overridden so Pulse's credential file doesn't
touch `~/.config/axon`, and a short `--interval` (e.g. `3s`). Drive it with
`curl` against the SvelteKit form actions (they accept normal
form-encoded POSTs, no browser needed) and query the sqlite DB directly
with `node -e "...node:sqlite..."` to assert on outcomes. Do this before
anything destructive (restore, delete, self-update swap) — it has
repeatedly caught real bugs before they reached a real host, see
`PROJECT_LOG.md`.

`vite dev` ignores `-- --port N` when invoked via `pnpm run dev`; call the
binary directly instead: `./node_modules/.bin/vite dev --port N`.

## Architecture

### Communication: pull-based HTTP, not gRPC

Pulse always initiates contact with Panel — heartbeat + command-poll over
plain HTTP/JSON, no inbound ports needed on any Pulse host, no persistent
connections. This replaced an earlier gRPC-based prototype: Cloudflare
Workers (the hosted Panel target) doesn't support gRPC, and none of Axon's
actual requirements (console tailing, commands, metrics) need bidirectional
streaming. It deliberately mirrors the team's Beacon RMM agent pattern, and
several pieces are directly ported/adapted from it — including self-update
(`pulse/internal/updater/`; see "Self-update" below).

The wire contract lives in two parallel type definitions kept in sync by
hand, not by shared schema/codegen: `pulse/internal/protocol/types.go` (Go)
and `panel/src/lib/server/protocol.ts` (TypeScript).

- **Heartbeat** (`POST /api/v1/heartbeat`, Pulse → Panel, interval varies —
  see "Adaptive heartbeat interval" below): host metrics + per-instance
  status array + the agent's own `interval_seconds` (its *current*,
  possibly-fast interval) and `base_interval_seconds` (the immutable
  `--interval` CLI flag) so Panel can compute a "next heartbeat in ~Ns"
  countdown instead of guessing a fixed default. Panel upserts
  `server_instances` rows and returns any queued commands, marking them
  `sent`, plus an optional `update` field (see "Self-update" below)
  whenever a newer Pulse release is published for this agent's os/arch,
  plus `next_interval_seconds` (always set) suggesting what interval Pulse
  should use next.
- **Command results** are piggybacked onto the *next* heartbeat
  (`pending_command_results`) rather than pushed immediately — simplicity
  over latency, matches Beacon precedent. **Real consequence**: if Pulse
  itself restarts between finishing a command and the heartbeat that would
  report it, the result is lost and the command sits at `sent` forever.
  `failStaleCommands()` (`panel/src/lib/server/commands.ts`) handles this —
  a command stuck `sent` past 3 missed heartbeats (reusing `isOnline()`'s
  "presumed offline" bar, `panel/src/lib/heartbeat.ts`) auto-resolves to
  `failed` with an honest timeout message, and any dependent row resolves
  the same way a genuine failure would (`resolveCommandOutcome()`, shared
  by both paths). Called from the heartbeat route (agent-scoped), the
  dashboard load (unscoped, since the owning agent may never come back),
  and the instance detail page load (agent-scoped).
- **Enrollment** (`POST /api/v1/enroll`, one-time): token → device
  credential. Enrollment tokens and device credentials are never stored
  raw, only `sha256Hex()` hashes (`panel/src/lib/server/tokens.ts` /
  `pulse/internal/credential`).
- **Backup file transfer** (`POST /api/v1/backups/{id}/upload`, Pulse →
  Panel, on-demand): a separate, non-JSON, non-heartbeat call — see
  "Backups" below.

### Adaptive heartbeat interval + real-time push layer

Two-part fix for a real, previously-unaddressed latency complaint: a
console command sent from the instance page only reached Pulse on its
*next* heartbeat, and the result only came back on the heartbeat *after
that* — up to ~2x Pulse's `--interval` (default 30s) round-trip,
independent of network distance (same-machine, LAN, or across the
internet all cost the same, since Pulse always polls on a fixed timer
regardless of locality — a real point of confusion this session,
resolved by explaining the architecture rather than changing it).

**Part A — adaptive interval (agent-scoped, not instance-scoped)**:
Panel suggests a fast interval (3s) via `HeartbeatResponse.
NextIntervalSeconds` (`next_interval_seconds`, always set, a pointer on
the Go side so an older Panel that never sets it is distinguishable from
a deliberate instruction) whenever there's a command `queued`/`sent` for
the agent, or `server_instances.lastViewedAt` shows an admin loaded that
agent's instance page within the last 15s (`PRESENCE_WINDOW_MS`) —
falling back to the agent's own `base_interval_seconds` otherwise. Pulse
applies it by mutating its local `interval` var in `runLoop`
(`pulse/cmd/pulse/main.go`); the CLI-flag value itself is captured once
at startup into a separate, never-mutated `baseInterval` and reported
every heartbeat as `base_interval_seconds`, specifically so a temporary
fast window can never change what "stale"/"offline" means. **This
distinction is load-bearing**: `isOnline()`/`heartbeatProgress()`/
`nextHeartbeatLabel()` (`panel/src/lib/heartbeat.ts`) and
`failStaleCommands()` (`panel/src/lib/server/commands.ts`) all compute
their deadlines from `baseIntervalSeconds`, never the ephemeral
`intervalSeconds` — using the fast value there would shrink the "3
missed heartbeats" deadline to ~9s and spuriously fail/offline-flip
things during ordinary jitter while a fast burst is in progress. Verified
live in an isolated sandbox (throwaway `sh -c "sleep N"` Pulse instance):
idle heartbeats hold at the base interval; queueing a command flips the
*next* heartbeat's response to `next_interval_seconds: 3` and subsequent
heartbeats land ~3s apart until the command resolves, then decay back;
presence alone (no command) does the same and decays once the 15s window
expires; a manually-inserted 10s-old `sent` command survives a
simultaneous fast burst untouched, confirming the base-interval-only
staleness math actually holds under load, not just in isolation.

**Part B — adapter-agnostic real-time push** (`panel/src/lib/server/
realtime/`): replaces (well, supplements — see below) the instance
page's poll with an actual push channel, so a change lands in the
browser the instant it happens rather than waiting for that tab's own
next poll tick. One shared interface (`index.ts`: `publish(channel,
event)` / `subscriberCount(channel)`, `channel` = the same
`pulseAgentId:instanceId` composite id `server_instances.id` already
uses), two backends chosen at the exact same runtime branch point the DB
layer already uses (`platform?.env?.DB` in `db/index.ts`) — Cloudflare
Durable Objects (`cloudflare.ts` + `instance-hub-do.ts`, one DO instance
*is* one channel via `idFromName`) or a plain in-process Node
`Map<channel, Set<WebSocket>>` (`node.ts`, `ws` package — a genuinely new
runtime dependency, this project was dependency-light before). There's
no raw stdout/console tailing anywhere in Axon — the console is a
request/response transcript built from `commands` rows — so the pushed
payload is deliberately trivial (`{type:'changed'}`, "something changed,
go reload"), not a data-bearing stream; this keeps the realtime layer
from duplicating `+page.server.ts`'s own query logic.

**The connection-*accept* half (terminating a raw WebSocket upgrade)
genuinely cannot be unified the way publish/subscriberCount are** —
SvelteKit exposes no raw socket the same way on Node vs. Workers. Real
architectural asymmetry, not a modeling shortcut:
- **Cloudflare**: `/realtime/[serverInstanceId]/+server.ts`'s `GET`
  forwards the request straight to the channel's Durable Object
  (`ns.get(id).fetch(request)`), which returns the real `101` response
  itself. (`@cloudflare/workers-types`' own `Request`/`Response` types
  are structurally near-identical but nominally distinct from the DOM lib
  types SvelteKit's `RequestHandler` expects — bridged with a cast at
  that one boundary, not threaded further.)
- **Node**: the real upgrade happens entirely outside SvelteKit, in a new
  `panel/server.mjs` production entrypoint (replacing `node
  build/index.js` — see "Panel: one codebase, three adapters" below) via
  the raw `http.Server`'s `'upgrade'` event, which never reaches
  `hooks.server.ts` or any SvelteKit route at all. The dev-mode
  equivalent is a small Vite plugin in `vite.config.ts`
  (`configureServer` → `server.httpServer.on('upgrade', ...)`), so this
  is actually developable under `pnpm run dev`, not just in a production
  build.

**Two real bugs caught by testing this live, not just type-checking it**
(a `ws`-based Node test client, two browser-tab-equivalent WebSocket
connections against a real `server.mjs` instance):
- **Cross-module-boundary state**: `server.mjs` runs as plain Node,
  outside Vite's bundling of `realtime/node.ts` — importing that TS file
  directly from `server.mjs` would produce a *second*, disconnected
  module instance with its own empty `channels` Map, meaning a socket
  accepted in `server.mjs` would never be visible to `publish()` calls
  made from the SvelteKit-bundled route code. Fixed by anchoring the
  Map on `globalThis` (`__axonRealtimeChannels`) instead of a
  module-scoped `const`, in both `node.ts` and `server.mjs` (and the dev
  plugin) — genuinely the same object within one OS process regardless
  of how each side's code got loaded, not a workaround.
- **Auth bypass, caught live**: raw `'upgrade'` events bypass
  `hooks.server.ts`'s admin-session gate entirely on Node (that pipeline
  never runs for an upgrade request), so `server.mjs`/the dev plugin make
  an internal loopback `GET` to `/realtime/[id]` first, forwarding the
  browser's original cookies, to reuse the real session check before
  accepting the upgrade — reaching the route at all (a 2xx) means it
  passed. **First attempt was actually broken**: `fetch()` follows
  redirects by default, so an unauthenticated loopback request silently
  followed `hooks.server.ts`'s `303` to `/login`, got that page's own
  `200 OK`, and the check saw `.ok === true` for a request that should
  have been rejected — a real unauthenticated WebSocket connected
  successfully in testing before `redirect: 'manual'` was added to the
  loopback `fetch()` call. Re-tested after the fix: an unauthenticated
  connection now gets destroyed immediately, an authenticated one
  connects and receives pushed events normally.

**Coexists with, doesn't replace, the instance page's poll** — dropped
from a 3s baseline to 8s (still 1s while something's known in-flight),
not removed: a v1 WebSocket can silently drop (network blip, a Durable
Object eviction, tab backgrounding) with no guaranteed reconnect before
the next poll tick, so the poll stays the eventual-consistency safety
net. The client reconnects with capped exponential backoff (1s→15s) on
`onclose`.

**Cloudflare half is unverified in this environment** — no live account/
deploy access here, flagged explicitly in `cloudflare.ts`/
`instance-hub-do.ts`'s own header comments, same honesty this project
already applies to Tauri live-run (before it was verified) and the
Fabric/Forge installer. In particular, `wrangler.toml.example`'s `main`
now points at a hand-written `panel/worker-entry.ts` wrapper (re-exporting
both the generated `_worker.js` default export and the `InstanceHub`
Durable Object class) instead of the generated file directly, since
`adapter-cloudflare` has no established path for exporting an additional
named class from its own generated worker module — whether this wrapper
approach actually works against the pinned adapter/wrangler versions has
never been confirmed by a real deploy. A standalone spike (just the DO,
deployed to a real account) should be the first thing that touches
Cloudflare here, before trusting the full feature in production.

### Pulse always runs, even same-machine

When Panel and a Minecraft server are on the same box, Pulse still runs as a
separate process talking to Panel over `localhost` using the identical
heartbeat contract used remotely — there is no special-cased "direct mode".
Don't add one; the point is that adding a second machine later requires zero
re-architecture.

### PID-file process reconciliation

`Manager` (`pulse/internal/mcserver/manager.go`) keeps instance running-state
purely in memory, so naively **every Pulse restart** (binary upgrade,
crash, host reboot) forgets whether a real Minecraft process is still
running underneath it. Fixed with `Manager.Reconcile()`
(`pulse/internal/mcserver/pidfile.go` + `Reconcile`/`watchReattached` in
`manager.go`):

- `Start()` writes a small JSON PID file (`.pulse-pid`, inside
  `working_dir`) recording the spawned PID and command; the exit-detection
  goroutine removes it on exit.
- On startup, `manager.Reconcile()` (call after `NewManager`, before the
  heartbeat loop) reads each instance's `.pulse-pid` if present, and adopts
  the process (marks it Running, no restart) if it's still alive **and**
  its command's base executable name matches what's configured
  (`processMatches` in `pidfile.go`) — a best-effort fingerprint against
  PID-reuse-after-reboot false positives. Uses
  `github.com/shirou/gopsutil/v3/process` for the cross-platform
  liveness/cmdline check.
- An adopted process has no `*exec.Cmd` (Pulse isn't its parent, so
  `cmd.Wait()` would fail with ECHILD) — `watchReattached` polls liveness
  every 2s instead. `Stop()`/`gracefulStop` take a bare `pid int` rather
  than `*exec.Cmd` (`terminate()` in `process_unix.go`/`process_windows.go`
  likewise), so stopping works identically whether Pulse spawned the
  process this run or adopted it.
- **One-time bootstrap gap, not a recurring bug**: a process spawned by a
  *pre-reconciliation* Pulse binary never got a PID file, so the very
  first restart onto the new binary needs a **manual** one-time PID-file
  seed (`echo '{"pid":N,"command":["..."]}' > .pulse-pid` as the process
  owner). Every restart after that self-heals, since every process Pulse
  spawns from then on gets a real PID file automatically.
- This is also what makes self-update's process-replacing restart safe —
  see "Self-update" below.

### Backups (manual create/list/delete, download, restore, scheduling + retention)

Four foundational decisions, all load-bearing:

1. **Pulse's disk is the only durable copy.** Panel never stores backup
   bytes persistently — only transiently, for an active download. Pulse
   pushes bytes to Panel on request, Panel never reaches into Pulse.
2. **Scheduling is Panel-owned**, evaluated inline during heartbeat
   handling — no new Go dependency in Pulse, fits the request-driven (no
   persistent timers) design Cloudflare compatibility requires.
3. **Backup scope is the whole `working_dir`** (world + server.properties +
   plugins/mods/configs), tar+gzip, stdlib only
   (`archive/tar`+`compress/gzip`), excluding `logs/`, `crash-reports/`,
   `session.lock`, `pulse.log` (matched by base name at any depth — see
   `excludeNames` in `pulse/internal/backup/engine.go`).
4. **Cloudflare gets a clean 501, not a half-working feature.** The two
   routes that need a real filesystem
   (`api/v1/backups/[backupId]/upload`, `backups/[backupId]/download-file`)
   check `platform?.env?.DB` first and refuse cleanly; backup *metadata*
   (the `backups`/`backup_downloads` tables) is fine on D1 either way.

**Why backups stop the server first, always**: **Bedrock Dedicated
Server has no RCON support at all** — confirmed live (see "Raw RCON
console" below for how this was discovered against a real Bedrock
instance) against the official `server.properties` key reference, which
lists `enable-rcon`/`rcon.port`/`rcon.password` only under Java Edition,
and Mojang's own Bedrock feedback docs. (This project's earlier
documentation here said Bedrock RCON existed but just didn't support
Java's `save-off`/`save-all` pause-writes convention specifically — that
was wrong; there's no Bedrock RCON to support any convention at all.)
Rather than branching per edition,
`backup_instance`/`restore_backup` both stop the instance if running
(reusing `gracefulStop`) → do the archive/restore work with nothing
writing to disk → restart afterward *only for backup* (restore always
leaves it stopped — the admin should look at a restored world before
deciding to bring it back up).

**A structural UX consequence, reused by every later feature with the
same shape**: because a stop→work→restart cycle happens *between* two
heartbeats, Pulse's own `running_state` reporting frequently never shows
"stopping"/"starting" for it at all. **Don't try to fix this by making
Pulse report faster; it structurally can't** — it doesn't know about the
command until after that heartbeat's status snapshot is already built.
The fix: derive "is something happening to this instance" from **Panel's
own command/backup tracking**, independent of `running_state` — see
`panel/src/routes/+page.server.ts`'s `instancesBackingUp`/`pendingActions`
and the "Backing up…"/"Restoring…"/"Starting…"/"Deleting…" badges that
replace (not stack next to) the normal status badge on the dashboard. The
same reasoning applies to plain start/stop/restart, and to
`delete_instance` (see "Deleting a provisioned instance" below).

**Push-backup transfer** (download): `downloadBackup` action queues a
`push_backup` command and inserts a `backup_downloads` row
(`status='requested'`, an `expiresAt` safety-net deadline set immediately)
→ Pulse's next heartbeat picks up the command → `protocol.Client.PushBackup`
streams the file to `POST /api/v1/backups/{id}/upload` (bearer
device-credential auth + an `X-Axon-Instance-Id` header Panel cross-checks)
→ streams straight to Panel's local holding dir → marks the row `ready`
with a fresh 10-minute TTL → client-side polling notices `ready` and
**auto-triggers** the actual browser download rather than making the admin
click twice. `GET /backups/{id}/download-file` streams the held file back
with delivery-confirmed cleanup; `pruneExpiredDownloads` is the backstop
for holds nobody ever collects.

`protocol.Client`'s shared `http.Client` has a blanket 15s timeout — fine
for small JSON calls, would abort a multi-GB backup upload partway
through. `PushBackup` uses a **separate**, untimed `http.Client`
(`uploadHTTPClient` in `pulse/internal/protocol/client.go`).

**Restore** (`executeRestore` in `pulse/cmd/pulse/main.go`, `Engine.Restore`
in `pulse/internal/backup/engine.go`): stop (if running) → take an
automatic **pre-restore safety backup** first (Panel pregenerates its id)
→ wipe `working_dir` and extract the target archive in place (**exact
rollback, not a merge**) → leave stopped regardless of prior state. The
safety backup's size/checksum are reported **whenever that step itself
succeeded**, independent of the overall restore's success — so Panel
still records a usable safety backup even if the extract step fails
afterward. A failed restore never touches `working_dir` — verified in
`TestRestoreUnknownBackupFailsWithoutTouchingWorkingDir`. Confirming a
restore uses the themed `ConfirmModal` (`panel/src/lib/components/`), not
the browser's native `confirm()` — see STYLE.md.

**Backup scheduling + retention** (`panel/src/lib/server/backupSchedules.ts`):
one `backupSchedules` row per instance (presence of the row plus which of
`intervalHours`/`keepCount`/`keepDays` are non-null fully expresses the
state, no separate enabled flag). `runSchedulesForAgent()` runs
agent-scoped from the heartbeat route, same opportunistic-cleanup spot as
`pruneExpiredDownloads`/`failStaleCommands`, before the `queued`-command
select so anything it queues ships in that same response.
`queueScheduledBackupIfDue()` stamps `lastRunAt` *before* the backup
resolves so a short interval can't cause pile-up; a freshly-configured
schedule is due on the very next heartbeat, not a full interval later.
`applyRetention()` keeps a backup if it's the single newest, **or** within
`keepCount`, **or** within `keepDays` — union, not intersection — everything
else gets a `delete_backup` command queued exactly like the manual Delete
button. Exported separately from the due-check so the instance page's
**Apply Retention Now** button can call it alone.

### Provisioning new servers (Java: vanilla/Paper/Fabric/Forge; Bedrock: vanilla)

Lets an admin create a real, running server from Panel — deliberately
narrow scope (latest ~3 versions per edition/software, Linux-only Java
auto-install, fixed Java heap, one shared port range); see "Deliberately
deferred" below for the exact cuts.

**Version/download-URL resolution lives in Panel, not Pulse**
(`panel/src/lib/server/versionCatalog.ts`) — plain outbound `fetch()`
calls, works identically on Node and Cloudflare Workers. Java vanilla
resolves via Mojang's public `version_manifest_v2.json` (per-version JSON
has both the download URL and required Java major version, no hardcoded
mapping table needed). Bedrock has no equivalent API — Panel's best-effort
scrape of minecraft.net's download page is confirmed unreliable in
practice (bot-detection/JS rendering), so a scrape failure yields a stale
cached result rather than an error, and the create-server form always
shows an admin-editable download-URL field for Bedrock. Pulse itself does
zero version-catalog resolution — it receives a fully-resolved
`create_instance` payload and just acts on it.

**`versionCatalogEntries` is keyed by `` `${gamePlatform}:${softwareType}:${version}` ``,**
not just `${gamePlatform}:${version}` — a single MC version now has up to
four distinct cached entries (vanilla/paper/fabric/forge), each with its
own download URL, so every cache read/write
(`freshEntries`/`staleEntries`/`replaceEntries`) filters on **both**
`gamePlatform` and `softwareType`. Filtering on `gamePlatform` alone was
a real bug caught during this feature's development — resolving a fresh
vanilla catalog would have wiped Paper/Fabric/Forge's cached entries too,
and vice versa.

**Paper, Fabric, and Forge — one resolver each, all live-verified against
the real APIs during development** (not just documentation):
- **Paper** (`resolvePaperVersions`) is structurally identical to
  vanilla — `fill.papermc.io`'s API returns a direct downloadable server
  jar URL, so **Pulse needs zero changes at all** to run one; it's just
  another `server.jar`. Paper's own version list is queried rather than
  assumed identical to vanilla's latest-3 (Paper sometimes lags a fresh MC
  release); per candidate version, the newest `channel: "STABLE"` build's
  `downloads["server:default"].url` is taken.
- **Fabric** (`resolveFabricVersions`) resolves the installer jar once
  from `meta.fabricmc.net/v2/versions/installer` (first `stable: true`
  entry — installer version is MC-version-independent) and, per candidate
  MC version, the loader version from
  `meta.fabricmc.net/v2/versions/loader/{v}` (first `stable: true`
  entry). The *installer* URL is stored as `downloadUrl`; the resolved
  loader version goes in a new nullable `loaderVersion` column (Fabric is
  the only software type that needs it — the loader version is
  independent of the MC version and doesn't fit any existing field).
- **Forge** (`resolveForgeVersions`) fetches
  `files.minecraftforge.net/net/minecraftforge/forge/promotions_slim.json`
  once, and per candidate MC version looks up `"{v}-recommended"`
  (falling back to `"{v}-latest"`), constructing the installer URL
  deterministically from Forge's own maven layout:
  `https://maven.minecraftforge.net/net/minecraftforge/forge/{v}-{forgeVersion}/forge-{v}-{forgeVersion}-installer.jar`
  — verified live during development (a real ~6MB jar came back at that
  exact constructed URL).

All three reuse `latestVanillaMcVersions()` (a thin wrapper over the
vanilla Java resolver) for "which MC versions are current" — one source
of truth, not three independently-derived lists. `resolveVersionSelection`
takes a `softwareType` parameter now and returns `loaderVersion` alongside
the existing fields; `CreateInstanceCommandPayload` (both `types.go` and
`protocol.ts`) gained a matching `LoaderVersion`/`loader_version` field,
empty except for Fabric.

**Java runtime handling is auto-install, Linux-only**
(`pulse/internal/javaruntime/`): detects an existing match first, installs
via `apt-get`/`dnf`/`yum` otherwise using a small hand-maintained
package-name table. **New operational prerequisite**: the Pulse service
user needs scoped passwordless sudo for package installation — see
"Deploying Pulse to a real host" below. Any failure (non-Linux, missing
package manager, install failure) fails the command with a clear message
rather than attempting anything unsafe.

**Provisioning mechanics** (`pulse/internal/provision/`, deliberately
separate from `internal/backup`, which only ever archives/restores an
*existing* instance): downloads the URL (Java vanilla/Paper → `server.jar`
directly; Java Fabric/Forge → `installer.jar`, since the download there is
an installer *program*, not a runnable server — see below; Bedrock → a
zip, with the same zip-slip defensive check `backup/engine.go`'s tar
extraction has — genuinely load-bearing here since the archive comes from
a remote third party, plus an explicit `chmod 0o755` on `bedrock_server`
since zip entries don't reliably carry the exec bit), writes `eula.txt`
(Java) and patches `server-port` into `server.properties` via
`mcserver.WriteProperty`. Bedrock's launch needs `LD_LIBRARY_PATH=.` — a
new `Env []string` field on `mcserver.InstanceConfig`.

**Fabric/Forge need an installer run before anything is launchable** —
the one genuinely new mechanic Paper didn't need (Paper's jar runs exactly
like vanilla's). `provision.RunInstaller(softwareType, workingDir,
javaBin, mcVersion, loaderVersion)` invokes the downloaded
`installer.jar`, split into a pure `installerArgs(...)` (unit-tested, no
Java needed) and a thin `exec.Command` wrapper (not meaningfully testable
without a real installer):
- **Fabric**: `java -jar installer.jar server -mcversion {mcVersion}
  -loader {loaderVersion} -downloadMinecraft -dir {workingDir}` —
  `-downloadMinecraft` has the installer fetch the vanilla jar itself too,
  so Pulse needs no separate Mojang download step. Produces a fixed,
  predictable `fabric-server-launch.jar` regardless of MC version.
- **Forge**: `java -jar installer.jar --installServer` (cwd =
  `workingDir`). Produces `run.sh`/`run.bat` plus a per-version
  `user_jvm_args.txt`/`libraries/.../*_args.txt` — deliberately **not**
  parsed or reconstructed by Pulse (that internal path has genuinely
  varied across Forge/MC version eras); `Configure()` just invokes the
  generated `run.sh` (Unix) / `run.bat` (Windows) directly instead,
  sidestepping needing to know Forge's internal file-naming convention at
  all.

`create_instance.go` calls `RunInstaller` after `Download()` and before
`Configure()`, under a new `"installing_loader"` progress phase, only when
`SoftwareType` is `fabric`/`forge`. `provision.Configure()` branches its
launch-command construction on `SoftwareType` (not just `GamePlatform`):
vanilla/Paper unchanged (`java -jar server.jar nogui`), Fabric launches
`fabric-server-launch.jar`, Forge invokes the generated run script.

**Explicit, un-glossed-over verification gap**: this was all written and
developed in an environment with **no Java runtime at all**. Every
resolver above was live-verified against the real Paper/Fabric/Forge
infrastructure (including a real constructed Forge installer URL
returning a genuine ~6MB jar), but the installer **execution** step
itself — the exact CLI flags `installerArgs` passes — is reasoned through
from each project's current public documentation, not from a real
invocation. Treat this the same as self-update's Windows swap path and
Tauri's Rust code: verify Fabric and Forge server creation against a real
Java environment before trusting it on a real host.

**Dynamic instance registration** (`Manager.AddInstance` +
`config_persist.go`'s `SaveConfig`): the instance list was previously
fixed at process start. `AddInstance` inserts into the in-memory map and
atomically rewrites `pulse.instances.json` (temp file + `os.Rename`) in
the *same* critical section (`m.mu`), rolling back the in-memory insert if
the disk write fails, so memory and disk can never diverge.

**Async command execution — the one exception to `execute()`'s
contract**: every other command type is a single blocking call returning
a terminal `CommandResult` within one heartbeat cycle. `create_instance`
can't (Java install, jar/zip download, and now a Fabric/Forge installer
run, can all take minutes). `runLoop` (`pulse/cmd/pulse/main.go`)
intercepts it *before* `execute()`'s switch and runs it in a goroutine
(`creationJob`, `pulse/cmd/pulse/create_instance.go`, tracked in an
`activeJobs` map), reporting a coarse phase via
`HeartbeatRequest.InProgressCommands`
(`"preparing" | "installing_java" | "downloading" | "installing_loader" | "configuring" | "registering"`)
until it finishes. No other command type needs this — don't reach for it
unless something is genuinely multi-cycle.

**Port + working-dir placement**: Panel auto-assigns both from an
admin-configured `portRangeStart`/`portRangeEnd`/`instancesRootDir` (set
from `/agents/[pulseAgentId]`, the first agent-detail view this project
has had). `panel/src/lib/server/portAllocator.ts`'s `allocatePort` considers
both already-recorded `server_instances.port` values *and* ports already
claimed by a still-in-flight `create_instance` command — closes a real
race, since no `server_instances` row is pre-inserted for
`create_instance` (there's no honest `running_state` to give it before
Pulse confirms the instance exists). See "Known gaps" for the allocator's
one real limitation (blind to legacy hand-configured instances).

### Reusable server definitions (templates)

`serverDefinitions` — a saved preset (name, edition, version, download
URL, Java major version) an admin creates once and reuses to create
instances repeatedly without re-picking a version each time. **Global,
not per-agent** — a definition describes *what* to install, not *where*,
so it lives on `/settings` (already the home for global,
admin-instance-wide config) rather than any per-agent page, and the
create-server page (`/agents/[pulseAgentId]/new-instance`) offers every
definition regardless of which agent it's creating on.

**Pinned at creation time, not a live "always latest" reference**: saving
a definition resolves a concrete version/download URL/Java major version
right then (same `versionCatalogEntries` resolution the create-server
form already does) and stores that permanently — using the definition
later never re-resolves the catalog. Re-resolving on every use would
reintroduce the catalog's own TTL/staleness handling for no real benefit;
Mojang's old version download URLs stay valid indefinitely, so pinning is
simpler and just as durable. If an admin wants a newer version, they make
a new definition (or use the unaffected from-scratch flow).

**Zero wire/Pulse changes** — selecting a definition on the create-server
form just changes which Panel-side code path resolves the
`create_instance` payload's `version`/`download_url`/`java_major_version`
fields (the definition's own pinned values instead of a fresh
`catalog_id` lookup); the command Pulse receives is identical either way.
The version-resolution logic itself (`resolveVersionSelection` in
`versionCatalog.ts`) is shared between the create-server action and the
new-definition action — extracted once rather than duplicated, since both
needed the exact same "Java is always re-looked-up by catalog id, Bedrock
falls back to an admin-supplied URL override" branching.

No `ConfirmModal` on delete — matches the existing convention that only
genuinely high-blast-radius actions (restore, recursive file delete,
delete-instance) get that treatment; deleting a template has zero effect
on any running server.

### Configurable Java heap size + server.properties defaults at creation

Closes two real, previously-hardcoded gaps in provisioning: Java heap
size was a fixed `provision.DefaultJavaHeapMB = 2048` constant with zero
admin control anywhere, and `server.properties` only ever got
`server-port` written at creation time (everything else was left for the
server software's own first-launch defaults to silently fill in later).

**Wire**: `CreateInstanceCommandPayload` (both `types.go` and
`protocol.ts`) gained `java_heap_mb` (java only, 0/omitted falls back to
`DefaultJavaHeapMB`) and `gamemode`/`difficulty`/`max_players`/`motd`,
all optional — an omitted value means "don't write this key," leaving
the software's own default untouched, exactly like every other key this
payload doesn't mention. `provision.Configure()` writes whichever of
these the payload sets via a new `writePropertyOverrides()` helper,
before the per-platform launch-command switch. `gamemode`/`difficulty`/
`max-players` share the same server.properties key on both editions;
`motd` is edition-mapped by Pulse itself — Bedrock has no `motd` key, its
equivalent display-name field is `server-name` — so Panel only ever has
to think in terms of one concept.

**Panel**: the create-server form gained a "Server settings" section,
always shown regardless of whether a saved definition supplied the
software/version choice — **deliberately not part of `server_definitions`
itself**: a definition pins *what* to install, these are *how it's
configured*, chosen fresh at every creation. Pre-filled with the same
defaults the server software would pick on its own first launch
(survival/easy/20 players/"A Minecraft Server"), so they're visible and
editable upfront instead of hidden until an admin goes looking in the
properties editor after the fact. Gamemode options are edition-aware
(Bedrock's server.properties has no valid `spectator` value — the form
hides that option once the edition resolves to bedrock, and the create
action rejects it server-side too as defense in depth against a
hand-crafted request).

### Deleting a provisioned instance

The inverse of "Provisioning new servers" above — `delete_instance`, a
synchronous command like `start_instance`/`stop_instance` (no payload,
acts on `cmd.InstanceID` alone).

**Pulse**: `Manager.RemoveInstance(id, configPath, backupsDir string)
error` (`pulse/internal/mcserver/manager.go`) is the literal inverse of
`AddInstance` — same critical section, same rebuild-then-`SaveConfig`
shape, same rollback-on-write-failure guarantee. `executeDeleteInstance`
(`pulse/cmd/pulse/main.go`) orchestrates: stop if running →
`RemoveInstance` → `os.RemoveAll` the entire `working_dir`. Like
`create_instance`, this needs `configPath`/`backupsDir` that `execute()`'s
switch doesn't receive, so `runLoop` intercepts it the same way, before
the switch. **Backup archives already taken for this instance are never
touched** — they live in the shared `backups_dir`, not under
`working_dir`.

**Panel**: cascade-delete on success, in `resolveCommandOutcome`
(`panel/src/lib/server/commands.ts`) — the `serverInstances` row, its
`backups` rows, and its `backupSchedules` row are all deleted.
**Deliberately not cascaded**: `backupDownloads`/`fileUploads`/`commands`
history for the same instance — these already self-expire via existing
TTL sweeps and nothing ever queries them unscoped once the instance's own
page is gone (same tolerance this project extends to similar one-off
gaps, e.g. the PID-file bootstrap gap above). On failure, nothing is
cleaned up — the row stays so the admin can retry.

The dashboard's "Starting…"/"Stopping…"/"Restarting…" in-flight badges
gained a fourth, "Deleting…", via the identical `pendingActions`
mechanism (see "Backups" above). The instance page's "Danger Zone" card
uses the same `ConfirmModal` + hidden-form pattern as Restore/file-delete,
and states plainly that backup archives are not deleted.

### Node detail: host stats + allowlisted diagnostic commands

The agent-detail page (`/agents/[pulseAgentId]`) gained two things the
dashboard's per-agent cards never had room for:

**Host stats** — CPU/RAM were already flowing through the heartbeat and
stored on `pulseAgents`, just never displayed here (a pure display gap).
Disk usage (`HostMetrics.Disks`) was already on the wire too but silently
dropped by the heartbeat route — now persisted as
`pulseAgents.diskUsageJson` (a JSON-blob column, same convention as
`commands.payload`, since it's structured data that's never queried, only
displayed). Host uptime didn't exist at all — added
`HostMetrics.UptimeSeconds` via `gopsutil/v3/host.Uptime()` in
`pulse/internal/inventory`, distinct from `InstanceStatus.UptimeSeconds`
(a specific Minecraft instance's uptime, not the host's).

**Allowlisted diagnostic commands** (`run_diagnostic`) — a **host-level**
command, unrelated to `console_command`'s RCON round-trip into the
Minecraft process itself. `pulse/internal/diagnostics` (a new package,
distinct concern from `inventory`'s passive per-heartbeat collection)
holds a **fixed, hand-maintained allowlist** per OS
(`allowlist_linux.go`/`allowlist_darwin.go`/`allowlist_windows.go`,
build-tagged like the `_unix.go`/`_windows.go` process-signal split) —
four friendly names (`uptime`, `disk_usage`, `memory`, `processes`) map
to real OS-appropriate base commands. **Deliberately not arbitrary
command execution**: only a name already in the allowlist can ever run;
extra admin-supplied arguments are appended to that fixed base command
via a real argv slice (`os/exec`, never a shell string) — the admin can
add flags to `df`, not swap out `df` for something else. Panel offers
this same fixed 4-name set regardless of the target agent's OS
(`panel/src/lib/server/diagnostics.ts`), trusting Pulse to map each name
correctly for its own platform — same "Panel stays dumb" split already
established for Java-runtime package names. `CommandResult.Output` picks
up a fourth documented reuse (combined stdout+stderr); no dependent DB
row, host-level commands are stored with `commands.instanceId = ''`
(NOT NULL, not "non-empty") since they don't target any Minecraft
instance. UI reuses the exact RCON console transcript pattern
(`badge-pulsing` "Sent, waiting…", fast-poll while in flight).

**A real bug this caught**: `export const` from a `+page.server.ts` file
that isn't one of SvelteKit's recognized page exports (`load`, `actions`,
etc.) fails the production build's stricter export validation —
`svelte-check` doesn't catch this, only `pnpm run build` does. Moved the
diagnostic-name allowlist into a proper `$lib/server/` module instead of
exporting it from the route file directly. Reconfirms why this project
always checks both `ADAPTER=node` and `ADAPTER=cloudflare` builds, not
just `svelte-check`, before calling a Panel change verified.

### Raw RCON console

`console_command`: an arbitrary admin command sent verbatim to a running
instance's RCON port (`Manager.RunConsoleCommand`,
`pulse/internal/mcserver/rcon_command.go`), text response returned to
Panel. Synchronous like every command type except `create_instance`.

**Java only — Bedrock Dedicated Server has no RCON support at all**,
confirmed live against a real production Bedrock instance (nimo's
"Survival"): `enable-rcon`/`rcon.port`/`rcon.password` in
`server.properties` do nothing on Bedrock — `bedrock_server`'s own
startup log never mentions RCON at all, success or failure, even with
those keys correctly set (this was chased for a while as a suspected
config-key-naming mismatch, e.g. Java's `rcon.port` vs. a hypothetical
Bedrock `rcon-port`, before confirming via the official
`server.properties` key reference that RCON is a Java-only concept with
no Bedrock equivalent whatsoever, not a naming or version issue). The
instance page reflects this directly: the Console card and the
"Player Management" whitelist/op/ban card (below) are both gated behind
`data.instance.gamePlatform === 'java'`, replaced by a short explanatory
note for Bedrock instances rather than showing controls that can only
ever fail. The properties editor (below) is unaffected — it's a plain
file read/write via Pulse, nothing RCON-dependent about it, so it's the
right place to point a Bedrock admin who needs something configured.

**`Success` vs `Output` is deliberate**: `Success` reflects whether the
RCON exchange itself worked (reachable, authenticated) — `false` only for
"not running"/"not configured"/connection or auth failure. `Output`
carries whatever text came back whenever the exchange succeeded, *even if
the game rejected the command* (e.g. `/foo` → "Unknown command" is still
a successful round-trip) — `Message` stays reserved for "the round-trip
itself failed," matching every other command type. **No fallback path,
unlike graceful stop**: if RCON isn't usable, `RunConsoleCommand` fails
cleanly rather than attempting anything else — "stop the process" has a
meaningful non-RCON fallback, an arbitrary console command doesn't.

**Latency UX**: a command sent now only reaches Pulse on its next
heartbeat, response comes back the heartbeat after that. The instance
page's transcript (reusing the `commands` table directly, no new table)
polls at 3s baseline, dropping to 1s while a console command (or
properties load/save, see below) is `queued`/`sent` — a derived
`fastPollNeeded` boolean. Doesn't beat Pulse's `--interval` floor, just
shaves the perceived wait once Pulse has actually reported a result.

**Whitelist/op/ban forms** (the "Player Management" card, same instance
page): purpose-built forms — whitelist add/remove/list/on/off, op/deop,
ban/pardon/kick (with optional reasons)/banlist — that construct the
equivalent command string and queue it as a plain `console_command`,
identical in shape to what the raw console box already does with
free-typed text. **No new wire type, no new Go code** — Pulse and the
game don't know or care whether the text came from a button or typing,
so these show up in the same transcript as raw-typed commands. Panel
only rejects a whitespace-containing username before queuing (a
malformed multi-arg command would otherwise go out silently) — no other
validation of game-side acceptance is possible or attempted, same
passthrough philosophy as the raw console itself.

### Server properties editor

A raw-text editor for an instance's `server.properties`
(`ReadPropertiesFile`/`WritePropertiesFile`,
`pulse/internal/mcserver/properties.go`). **Deliberately raw text, not a
structured per-key form** — the key set differs by edition/software and
changes over time; Panel never parses the file. Two synchronous command
types, `read_properties`/`write_properties`, following `console_command`'s
shape:

- `read_properties` needs no payload; its result comes back in
  `CommandResult.Output` — reused from the RCON console.
- `write_properties` carries the full replacement content and overwrites
  the file **atomically** (temp file + `os.Rename`) — same pattern as
  `mcserver.SaveConfig`.
- No "must be stopped" requirement — `server.properties` is only read at
  startup, so editing while running is safe, it just doesn't take effect
  until a restart.
- Panel keeps zero server-side cache of "current properties" — `load`
  queries only the most recent `read_properties`/`write_properties`
  command row, and the instance page applies a completed result to the
  textarea exactly once per command id (a `loadedPropertiesCommandId`
  guard), never clobbering further local edits on a later poll of the
  same already-applied command.

### File management (browse/upload/delete an instance's working_dir)

Own dedicated route (`/instances/[serverInstanceId]/files`). Whole
`working_dir` tree is browsable, not restricted to a hardcoded
`plugins`/`mods` folder.

**`pulse/internal/filemanager/`** — distinct from `mcserver` (process
lifecycle), `backup` (whole-tree archive/restore), and `provision`
(one-time acquisition): `List` (non-recursive, directories-first),
`Delete` (recursive, explicitly rejects deleting `working_dir` itself),
`Save` (atomic temp-file+rename). Every function funnels through a
`resolve()` helper rejecting any path escaping `working_dir` — load-bearing
here since every path this package touches is admin-controlled, not
Pulse-self-produced. Lexical check only — doesn't dereference symlinks,
documented via an explicit test rather than silently assumed.

**Uploads need bytes to flow *into* Pulse — the second reversed-transfer
pattern, after backup downloads.** The admin uploads to Panel first (held
transiently in `file_uploads`), then Pulse *pulls* it on its own next
heartbeat via `PullFileUpload` against `GET /api/v1/files/[holdingId]/download`
— a hybrid of the two existing backup-transfer routes: bearer
device-credential auth like the backup-upload route, streaming +
delivery-confirmed cleanup like `download-file`.

`CommandResult.Output`'s third reuse: `list_files`'s result is a
JSON-encoded `[]filemanager.Entry` in the same free-text field already
carrying RCON output and raw properties content.

**Delete is one of the few destructive actions that gets `ConfirmModal`**
(along with restore and delete-instance) — an admin-navigable arbitrary
subtree, recursive, no listing shown first, no easy undo, unlike plain
backup delete or a console command.

**A real, pre-existing bug found while building this**:
`@sveltejs/adapter-node`'s built server caps every request body at `512K`
by default (`BODY_SIZE_LIMIT`) — silently affected the existing
`push_backup` upload route too (`vite dev` never enforces it, only the
built server does). See `panel/README.md`'s "Running the built
adapter-node server directly" section for the fix
(`BODY_SIZE_LIMIT=Infinity`).

### Self-update

`pulse/internal/updater/` — Pulse can update itself in place once a signed
release is published on Panel, ported from Beacon's
`agent/internal/updater/*` and `agent/tools/{keygen,sign}`. This only
covers *updating* an already-enrolled Pulse; first install onto a host is
still the fully-manual flow in "Deploying Pulse to a real host" below.

**Signing** (`verify.go`, `pulse/tools/{keygen,sign}`): releases are
signed with Ed25519, not just checksummed — checksumming only proves the
file wasn't corrupted in transit, not that Panel's word about it should be
trusted; signing is the actual security boundary against a compromised or
spoofed Panel response. `VerifyBinary(path, sigHex)` checks a hex Ed25519
signature over the SHA-256 digest of the downloaded file against a
hardcoded `pinnedPublicKey` — split into an unexported
`verifyBinaryWithKey(path, sigHex, pubKeyHex)` so tests can exercise the
crypto with a throwaway keypair. **The private key is never committed or
persisted anywhere in this repo or on any filesystem it touches** —
generated once, surfaced only in chat for the user to store externally,
same handling as any other one-time secret this project generates
(enrollment tokens).

**Swap + restart** (`swap_unix.go`/`swap_windows.go`): Unix backs up the
running binary, `os.Rename`s the new one into place (atomic, same
filesystem), then `syscall.Exec`s into it — **same PID, fresh `main()`**.
Windows renames the old exe aside (Windows locks by handle, not by name),
moves the new one into place, spawns it, exits. Simplified from Beacon's
version: no Windows-service-mode branch, since Pulse has no service-mode
capability anywhere else in this codebase.

`syscall.Exec`'s fresh `main()` forgetting already-running Minecraft
processes is safe, not a new risk — it's exactly the scenario
`Manager.Reconcile()` (see above) already exists to solve.

**Grace-period confirm/rollback state machine** (`updater.go`): before
swapping, `ApplyUpdate` writes `update-state.json` (pending version,
backup path, a 10-minute deadline) into `credential.Dir()`, then swaps. On
the next process start, `Start()` finds this file and launches
`awaitConfirmation`, which selects between `checkInC` (fed by
`NotifyCheckIn()`, called after every successful heartbeat) and the
deadline timer: confirmed → delete the state file and backup; unconfirmed
→ `rollback()` restores the backup and re-execs into it.

**Real production incident — infinite self-update loop** (found live on
nimo, the first time self-update ever actually completed end-to-end on a
real host): CI's `git describe` (via the `PULSE_VERSION` Makefile var)
doesn't reliably see the tag that *triggers* a tag-push release build —
a well-known `actions/checkout` gotcha, not fixed by `fetch-depth: 0`
alone — so a binary built at tag `v0.1.3` reported its own version as
`v0.1.2-1-g3e93406` (the nearest *older* tag plus a commit suffix), never
literally `v0.1.3`. Since self-update's whole trigger is "differs from
what was last reported" (previous paragraph), and Panel's published
release row was the literal string `v0.1.3` (what the admin typed into
the publish form), the comparison stayed permanently true — Pulse kept
re-downloading, re-verifying, and re-swapping the *exact same* binary
every single heartbeat, indefinitely, confirmed live at ~8-13s intervals
until manually stopped by rewriting the published release's version
string in the DB to match what the binary actually reported. Two fixes,
both real: `.github/workflows/release.yml` now runs `git fetch --tags
--force` right after checkout (the actual root cause); `updater.go`
independently persists the last version *this process itself* confirmed
(`last-confirmed-version.txt` in `credential.Dir()`, survives the
restart a successful swap always causes, so an in-memory guard couldn't
work here) and refuses to re-apply it — defense in depth against this
exact failure mode recurring for any reason, not just this one CI bug.

**No separate poll loop, unlike Beacon**: Beacon's updater has its own
always-on version-check loop; Axon's heartbeat is already periodic and
Pulse-initiated, so the update check rides `HeartbeatResponse.Update`
(`protocol.UpdateInfo` — version/download_url/signature_hex). `main.go`'s
`runLoop` calls `updater.ApplyUpdate` directly after processing a
heartbeat's commands. `downloadFile` uses an explicit no-timeout
`http.Client{}`, matching this codebase's established explicit-client
convention (see `uploadHTTPClient` above).

**In-flight guard**: `runLoop` only calls `ApplyUpdate` when
`len(activeJobs) == 0` — an update landing mid a multi-minute
`create_instance` job would otherwise kill that goroutine on re-exec.
Panel keeps offering the update every heartbeat until versions match, so
nothing is lost by deferring.

**Panel: `pulseReleases` table + `/settings` publish form** — insert-only,
no upsert: the heartbeat route always takes the newest row for a given
`(os, arch)`. Panel never verifies the signature itself; the admin builds,
signs, and hosts the binary. **Deliberately no downgrade protection**:
Pulse's version string is a `git describe` short hash with no total
order, so "differs from what's reported" is the whole check — same
single-admin trust model already applied elsewhere (port ranges,
retention config). `latestVersionsByPlatform`/`updateAvailableFor`
(`panel/src/lib/server/pulseReleases.ts`) share this comparison between the
heartbeat route and the dashboard/agent-page "→ vNEW available" note
(cosmetic only — the update is fully automatic, no separate progress UI).

See `PROJECT_LOG.md`'s 2026-07-26 entry for how the swap/rollback/rejection
paths were verified live (a throwaway Ed25519 keypair, never the real
pinned key).

### CI/CD for Pulse releases (+ a Windows Tauri desktop build)

`.github/workflows/release.yml`, triggered by pushing a version tag
(`git tag vX.Y.Z && git push --tags`, or manually via
`workflow_dispatch`), has two jobs against the same tag:

- **`release`** (the original job): runs `go vet`/`go test`,
  cross-compiles the same three `make build-pulse-linux/windows/darwin`
  targets used for manual builds (`PULSE_VERSION := $(shell git describe
  ...)` resolves to the clean tag name at an exact tag ref), signs each
  binary with the `AXON_SIGNING_KEY` repo secret (the same Ed25519 key
  generated for self-update — never printed, referenced only as an env
  var passed straight into `go run ./tools/sign`), and creates the
  GitHub Release with all three binaries plus a `dist-manifest.csv`
  (platform, filename, sha256, signature_hex per binary) attached.
- **`build-desktop-windows`** (`needs: release`, so it appends to the
  release the first job already created rather than racing it): builds
  the Tauri desktop shell (`panel/src-tauri/`) natively on a
  `windows-latest` runner — Tauri apps aren't meaningfully
  cross-compilable for Windows from the Linux boxes this project
  otherwise develops on, unlike Pulse's plain `GOOS=windows` Go
  cross-compile. Same `pnpm`/Node setup as `ci.yml`
  (`pnpm/action-setup` + `actions/setup-node` with the `packageManager`-
  pinned pnpm version), plus `dtolnay/rust-toolchain` for Rust (not
  preinstalled on the hosted runner) and `Swatinem/rust-cache` scoped to
  `panel/src-tauri` for build speed. Runs `pnpm run tauri:build`
  (`tauri.conf.json`'s `bundle.targets: "all"` produces both an MSI and
  an NSIS `.exe` installer on Windows) and uploads both to the same
  release tag via `gh release upload --clobber`. **Unsigned** — no
  Windows code-signing certificate configured, so SmartScreen will warn
  on first run; acceptable for now (single-admin self-hosted tool,
  installing your own build), revisit if this ever needs to look
  trustworthy to a wider audience.

**Automates build+sign, not hosting or publishing to Panel** — a
deliberate scope boundary, not a gap: the admin still copies the release
asset's download URL and the matching signature from the manifest CSV
into Panel's `/settings` publish form. Auto-publishing would need a new
authenticated Panel API endpoint, a shared secret, and Panel being
reachable from GitHub Actions — real new architecture for a step that
already takes 30 seconds. (This boundary only applies to Pulse's
self-update wiring — the desktop shell has no self-update mechanism at
all; a new build is just a new release asset the admin downloads and
reinstalls by hand.)

**The repo is now public** (`codenexus/axon`) — this is *why* self-update
downloads work at all here: Pulse's downloader
(`downloadHTTPClient.Get()` in `updater.go`) is a plain unauthenticated
HTTP GET, and a private repo's release assets aren't fetchable without a
token. Rather than teach Pulse to carry a GitHub token (a new credential
type for one narrow use), the repo went public instead — the LICENSE
(AGPL-3.0) was already there and already recognized by GitHub, so this
wasn't blocked on anything real.

### Deploying Pulse to a real host — first install is still manual

Self-update only covers updating an *already-enrolled* Pulse. First
install has no bootstrap path — there's no running Pulse to self-update
*from*. Flow: cross-compile with the `-X main.version=...` ldflags above,
`scp` to `<host>:/tmp/pulse-new`, verify the `sha256sum` matches on both
ends, then hand the actual privileged swap to the human — **`sudo`
commands run over SSH to a remote host are blocked by the auto-mode
permission classifier**, by design (correct behavior, not a bug to route
around). The swap itself:

```sh
sudo mv /tmp/pulse-new /usr/local/bin/pulse
sudo chown <service-user>:<service-user> /usr/local/bin/pulse && sudo chmod 755 /usr/local/bin/pulse
sudo chmod +t /usr/local/bin   # if not already set -- see prerequisite below
sudo kill <old-pid>
sudo -u <service-user> bash -c 'nohup /usr/local/bin/pulse --server-url <url> --config <path> --interval 30s > <logfile> 2>&1 & disown'
```

No `--enroll-token` needed on restart — the saved credential carries over.
Verify by checking `Reconcile()` logged an adoption
(`reconciled instance "X" with already-running pid N`) if a game server
process was already running, and that Panel's `last_seen_at` updates again
shortly after.

**New prerequisite for self-update to actually work**: found live on
nimo — self-update's binary swap (`os.Rename` over the running
executable, see "Self-update" below) needs *write permission on the
directory containing the binary*, not just the binary itself. A plain
`chown root:root` on the binary (an earlier, reasonable-looking version
of this exact snippet) silently defeats every future self-update
attempt if Pulse runs as an unprivileged service user, as documented
here — the swap fails every single heartbeat with no user-visible
symptom beyond "the version never changes." Fix once per host:

```sh
sudo chgrp <service-user> /usr/local/bin
sudo chmod g+w /usr/local/bin
sudo chmod +t /usr/local/bin   # sticky bit: even with group write, only a file's own owner (or root) can rename/delete it -- keeps <service-user> from touching *other* root-owned binaries that might share this directory
sudo chown <service-user>:<service-user> /usr/local/bin/pulse
```

**New prerequisite for server provisioning**: if Java-edition server
creation is going to be used on a host, the Pulse service user needs a
scoped passwordless-sudo rule for package installation. Without it,
`javaruntime.EnsureInstalled` fails cleanly with a message telling the
admin to install Java manually; Bedrock creation needs no such rule.
**A wildcard rule (`apt-get install -y openjdk-*`) doesn't work on every
system** — confirmed live on Ubuntu 26.04/sudo 1.9: "wildcards are not
allowed in command arguments" is a real, non-bypassable hardening some
sudo builds enable. The reliable form enumerates every exact package
name `javaruntime.go`'s `packageNames` map can ever request (one
`NOPASSWD:` line per major version) rather than globbing — tighter than
a wildcard anyway, since it's a real allowlist instead of a pattern:

```
axon ALL=(root) NOPASSWD: /usr/bin/apt-get update
axon ALL=(root) NOPASSWD: /usr/bin/apt-get install -y openjdk-17-jre-headless
axon ALL=(root) NOPASSWD: /usr/bin/apt-get install -y openjdk-21-jre-headless
axon ALL=(root) NOPASSWD: /usr/bin/apt-get install -y openjdk-25-jre-headless
```

RHEL/Fedora needs the equivalent `dnf`/`yum` package names
(`java-*-openjdk*`) enumerated the same way, one line per exact package
this project's `packageNames` map can request — untested live so far
(only Ubuntu has been deployed to), so treat the exact command name
(`dnf` vs `yum`) and package-name shape as needing the same live
verification before trusting it. Whichever distro, **always validate
with `visudo -c` after writing the drop-in file** — a malformed
`/etc/sudoers.d/` entry is silently ignored by some sudo builds and
loudly rejected by others; catching it immediately is cheap, debugging
"sudo rule exists but isn't working" after the fact is not.

### Panel: one codebase, three adapters, one DB abstraction

`panel/vite.config.ts` picks `@sveltejs/adapter-node` or
`@sveltejs/adapter-cloudflare` based on the `ADAPTER` env var — same
routes/components/logic either way.

**Tauri (`panel/src-tauri/`) is a thin desktop client, not a fourth
deployment target for Panel's own backend.** It has no local server, no
local DB, and no build-time dependency on Panel's SvelteKit build at
all — the only "frontend" it bundles is a tiny static config page
(`src-tauri/ui/index.html`). On first run it asks for an already-running
Panel's URL (the same address you'd type into a browser), saves it, and
points the native webview at it; later runs go straight there, and a
"Change Panel URL…" menu item lets the admin repoint it. This
deliberately replaced an earlier "spawn `node build/index.js` as a local
sidecar" plan — that assumed Panel only exists while the desktop app is
open, which doesn't fit wanting one Panel reachable from multiple
machines/networks (e.g. a home LAN today, a VPS added later). Axon
doesn't need to know or care how that Panel is reachable (LAN, Tailscale,
a public domain) — same as browser access, unchanged. **Compiled and
live-run-verified** on WSL2 Ubuntu 24.04 (`cargo build`, then the built
binary run directly against WSLg's Wayland compositor, confirmed via
`weston.log` registering a real `axon-panel-desktop` app window) — see
`src-tauri/README.md`. One real fix needed: `build.rs`'s plain
`tauri_build::build()` doesn't auto-generate ACL permissions for
app-level `#[tauri::command]` functions (only plugin commands get that
for free) — the build failed with "Permission allow-get-panel-url not
found" until `build.rs` was changed to `tauri_build::try_build(...)`
with an explicit `AppManifest::new().commands(&["get_panel_url",
"set_panel_url"])`. This generated `permissions/autogenerated/*.toml`
(committed, like Tauri's other ACL files) and resolved the capability
lookup already declared in `capabilities/default.json`. No other
API-shape mismatches found — the rest of the scaffold (menu, window
management, config persistence) compiled as originally written.

**Update, later session**: after live testing on Windows, this
thin-client design (webview pointed at a remotely-hosted Panel) was
judged not to work as wanted in practice and is being pulled out of
active scope — rescoped as a v2 initiative: Panel running as a proper
local Windows service/webserver the desktop app bundles and manages
directly, closer to the original pre-thin-client sidecar concept this
section describes above as "deliberately replaced." That v2 rework
hasn't been designed yet (a separate planning pass, not sketched here);
the code in `panel/src-tauri/` as described above is unchanged and still
compiles, just no longer the intended direction — don't extend it
further without confirming this decision hasn't been revisited.

The DB layer (`panel/src/lib/server/db/index.ts`) branches on
`platform?.env?.DB`: if present (Cloudflare), uses `drizzle-orm/d1`
directly; otherwise (Node/Tauri/local dev) lazily opens a Node built-in
`node:sqlite` database via `drizzle-orm/sqlite-proxy` — deliberately not
`better-sqlite3`, which needs a native build step. Both paths share the
same Drizzle schema (`panel/src/lib/server/db/schema.ts`). Local dev
self-applies `migrations/*.sql` on startup (idempotent, tolerates "already
exists" **and** "duplicate column name" for `ALTER TABLE ... ADD`
migrations). `scripts/seed-dev.mjs` has the identical tolerance list, kept
in sync by hand.

Migrations are drizzle-kit-generated (`pnpm run db:generate`) into the
repo-root `migrations/` directory — one source of truth shared by D1 and
the local sqlite bootstrap. **A long-running local dev server only
applies migrations once, at process startup** — after adding a migration,
restart the dev server, not just save the file.

The real-time push layer (`panel/src/lib/server/realtime/`, see
"Adaptive heartbeat interval + real-time push layer" above) mirrors this
exact same `platform?.env?.DB`-branch-then-shared-interface shape for a
second Cloudflare-specific primitive (Durable Objects) — worth reaching
for again the next time a Cloudflare-only capability needs a Node-side
equivalent, rather than inventing a new abstraction pattern. Its Node
implementation needs a custom production entrypoint,
`panel/server.mjs` (**not** the adapter-generated `build/index.js` — run
`node server.mjs` instead, see `panel/README.md`), since raw WebSocket
upgrade handling needs the underlying `http.Server`'s `'upgrade'` event,
which neither SvelteKit route handlers nor `build/index.js`'s
self-contained server expose a hook for.

### Command layers (conceptually distinct — don't collapse them)

1. **Process-level** — Pulse manages the OS process directly
   (`pulse/internal/mcserver/`): install, start/stop/restart,
   stdout/stderr capture, PID-file reconciliation. Command types:
   `start_instance`, `stop_instance`, `restart_instance` (stops first only
   if running, otherwise a plain start — safe to route every "Restart"
   click through this one type). Graceful stop (`gracefulStop()` in
   `rcon_stop.go`) issues RCON `save-all` + `stop` when the instance's
   `server.properties` has `enable-rcon=true` and a non-empty
   `rcon.password`, falling back to the bare SIGTERM/Kill signal
   otherwise.
2. **Backup lifecycle** — layered on top of process-level:
   `backup_instance`, `restore_backup`, `delete_backup`, `push_backup`.
   Implemented in `pulse/internal/backup/` (archiving/restore mechanics,
   no process-lifecycle awareness) + handler functions in
   `pulse/cmd/pulse/main.go` (`executeBackup`, `executeRestore`,
   `executePushBackup` — these own the stop/restart orchestration).
3. **Console commands (RCON)** — `console_command`. See "Raw RCON
   console" above.
4. **In-game/gameplay commands** — no separate code path; anything
   typeable with `/` in-game rides the same RCON layer `console_command`
   uses.
5. **Provisioning/deprovisioning** — `create_instance`/`delete_instance`,
   layered before/after process-level respectively. `create_instance` is
   the one command type that doesn't complete synchronously within a
   single `execute()` call — everything else in this list does.

### Auth: single admin, no roles (v1 decision)

`panel/src/lib/server/auth.ts` — one `admin_settings` row, session cookies
(`adminSessions`, id = sha256 of the cookie token). `hooks.server.ts`
gates every route except `/login` and `/api/v1/*`, which authenticate
Pulse agents via their own bearer device credentials instead of the
session cookie. `/api/v1/commands` is the one exception inside that
namespace — admin-facing (dashboard buttons), so it re-checks
`isAuthenticated()` itself. `backups/[backupId]/download-file` is
admin-session-authenticated normally even though the bytes originated
from Pulse — only the Pulse-initiated ingest side
(`api/v1/backups/[backupId]/upload`) uses bearer device-credential auth.

Still single-admin — no per-user settings, no roles. The **Settings** page
(`/settings`) holds global, admin-instance-wide config (enrollment token
generation, theme, Pulse release publishing), not multi-user preferences.

### Theming

CSS custom properties, palette selected via `data-theme` on `<html>`,
defined in `panel/src/lib/theme/palettes.css`. Status colors
(success/warning/error/info) are fixed across all palettes on purpose —
don't let a palette override them; that's what keeps "red = danger"
meaningful under a red/orange palette like Nether. New palettes are pure
additive CSS blocks, no component changes needed. Palettes are named by
color character (Classic/End/Nether), not exact in-game names — a
trademark consideration for the OSS project. The theme switcher lives on
`/settings`, not the dashboard header (see STYLE.md).

## Coding conventions established so far

- **Never store raw secrets.** Enrollment tokens, device credentials, and
  session cookies are hashed with `sha256Hex()` before hitting the DB
  (`panel/src/lib/server/tokens.ts`); only the hash is persisted, matching
  Beacon's pattern. Admin passwords use `scrypt` (`hashPassword`/
  `verifyPassword`, same file). Follow this for any new credential type.
- **Server-only code lives under `panel/src/lib/server/`**, one small
  module per concern (`db/`, `auth.ts`, `tokens.ts`, `http.ts`,
  `commands.ts`, `protocol.ts`, `backupDownloads.ts`, `pulseReleases.ts`,
  ...) — imported only from `+server.ts` / `+page.server.ts` /
  `hooks.server.ts`, never from `.svelte` files. **Client-safe shared
  logic** (no server-only imports, usable from any `.svelte` file) goes
  directly under `panel/src/lib/` instead — see `heartbeat.ts` (pure
  timing-math functions shared by the dashboard and backups page).
- **Shared DB mutations get extracted into a `lib/server/*.ts` helper**
  and called from both a form action and a REST route rather than
  duplicated — see `queueCommand()` in `commands.ts`, used by every
  command-queueing form action. `queueCommand()` returns the new
  command's id (needed so callers can stamp `commandId` onto a dependent
  row inserted in the same action).
- **Node-only server code (real filesystem access) uses dynamic
  `import('node:...')` inside the function body**, never a static
  top-level import — lets the Cloudflare adapter bundle cleanly despite
  these modules existing in the shared codebase. Guard the
  Cloudflare-unsupported path with `if (platform?.env?.DB) throw error(501,
  '...')` *before* any dynamic import, so the check itself has zero
  Node-specific surface.
- **Admin-facing mutations are SvelteKit form actions with `use:enhance`**,
  not client-side `fetch()` calls to a JSON API. The `/api/v1/*` routes are
  reserved for cross-boundary callers (Pulse agents, bearer-token
  authenticated) — `/api/v1/commands` is the one route that also accepts
  admin session auth, documented there as the deliberate exception. **A
  destructive confirmation uses the themed `ConfirmModal` component**, not
  the browser's native `confirm()` — see STYLE.md.
- **Heartbeat-driven upserts use a composite string id**:
  `` `${pulseAgentId}:${instanceId}` `` as the `server_instances` primary
  key, via `onConflictDoUpdate`. Same shape reused for
  `backupSchedules.id`. Use "prefix:localId" for any future
  per-agent-scoped resource Panel needs to key on.
- **Panel-owned ids for anything Pulse creates on request**: Panel always
  generates the id (e.g. `bkp_<random>` via `newBackupId()` in
  `commands.ts`) *before* queuing a command that will create something,
  and Pulse uses it verbatim rather than inventing its own. Follow this
  for any future Pulse-side creation.
- **A command's wire `payload` is a JSON-stringified column
  (`commands.payload`)**, `null` for payload-less types. Parse with the
  matching `*CommandPayload` TS interface from `protocol.ts`. Don't assume
  a wire-type field is actually plumbed through end-to-end — check the DB
  schema and every hop.
- **A destructive/state-changing wire command reports enough in its
  `CommandResult` for Panel to fully resolve the corresponding DB row(s)
  without a second round-trip** — extend `CommandResult`'s optional fields
  rather than adding a parallel reporting channel, and populate them based
  on which *step* succeeded, not just overall pass/fail (see restore's
  safety-backup reporting above).
- **A TTL-cached lookup table must scope every read/write (fresh, stale,
  replace) by every dimension of its own cache key, not just some of
  them.** `versionCatalogEntries` is keyed by
  `` `${gamePlatform}:${softwareType}:${version}` `` — a real bug this
  session came from `replaceEntries` filtering only on `gamePlatform`,
  which would have silently wiped every other software type's cached
  entries on a single-software-type refresh. If a cache key has N parts,
  every query touching that cache needs all N in its `WHERE`.
- **Go: one `internal/<concern>/` package per concern**
  (`protocol`, `credential`, `mcserver`, `inventory`, `backup`, `rcon`,
  `filemanager`, `provision`, `javaruntime`, `updater`), platform-specific
  behavior split into `_unix.go`/`_windows.go` files with build tags
  rather than runtime `if runtime.GOOS` branching. A command handler that
  orchestrates process-lifecycle + another package's mechanics (stop → do
  the thing → maybe restart) lives in `cmd/pulse/main.go`, not inside the
  mechanics package itself — keeps the mechanics package testable without
  a real process, and keeps "should this stop the server first" as one
  visible decision per command type.
- **Wire types are hand-mirrored, not codegenned**: Go structs in
  `pulse/internal/protocol/types.go` and TS interfaces in
  `panel/src/lib/server/protocol.ts` must be updated together, same
  snake_case JSON field names on both sides. Don't introduce a shared
  schema tool speculatively.
- **Go tests avoid needing a real Minecraft server**: drive
  `mcserver.Manager` against a trivial `sh -c "sleep N"` stand-in process.
  For anything destructive (restore, self-update swap, delete-instance),
  pair unit tests with a full sandboxed HTTP-level run (see "Local
  dev/testing workflow" above) before it ever touches a real host.
- **A "why isn't this happening" report is often timing, not a bug** —
  check actual DB timestamps (`commands.sent_at`/`completed_at`,
  `backup_downloads.requested_at`/`ready_at`) against `now` and the
  agent's heartbeat interval before assuming something's broken.
- **CSS custom properties are prefixed `--axon-*`** — see `STYLE.md` for
  the full palette/spacing/component conventions.
- **State shared across two separate Node entrypoints (the SvelteKit
  bundle plus a hand-written one like `server.mjs`) needs a `globalThis`
  anchor, not a module-level `const`.** Each entrypoint gets its own
  bundled copy of any shared module, so a plain `const channels = new
  Map()` in `realtime/node.ts` would silently become two different,
  disconnected `Map` instances depending on which entrypoint touched it
  first — real bug, caught live (see "Adaptive heartbeat interval +
  real-time push layer" above). Anchor with
  `(globalThis as any).__someKey ??= new Map()` instead, so every
  caller in the same OS process shares the literal same object
  regardless of which bundle loaded it.
- **`fetch()` follows redirects by default — pass `redirect: 'manual'`
  whenever a fetch result is used as a pass/fail authorization check
  against a route that might redirect on failure**, not just read as
  data. A GET to a route gated by `hooks.server.ts`'s admin-session
  check returns a `303` to `/login` when unauthenticated; a plain
  `fetch()` transparently follows that redirect and returns the login
  page's own `200 OK`, making `.ok` true for a request that actually
  failed auth. Caused a real, live auth bypass (an unauthenticated
  WebSocket connected successfully) before this was caught — see the
  real-time push layer's loopback auth check in `server.mjs`.
- **Any custom-width `<input>`/`<select>` needs an explicit
  `box-sizing: border-box`, every time, no exceptions.** This codebase
  has no global CSS reset, so the browser default (`content-box`) adds
  padding/border *on top of* a specified `width` instead of within it —
  bit this project twice in the same session, independently, in two
  different files (`.settings-form`/`.release-form`), both times as a
  field visibly overlapping a sibling button. Set it directly on the
  input rule itself, don't rely on inheriting it from anywhere.

## Known gaps (real, not yet fixed — don't assume otherwise)

- **Restore has never been run against nimo's real backups** (only the
  isolated sandbox, thoroughly) — deliberately left to the user to trigger
  first, given it's destructive even with the safety-backup net.
- **The port allocator (`panel/src/lib/server/portAllocator.ts`) is blind
  to legacy hand-configured instances.** It only considers ports already
  recorded on `server_instances` for instances Panel itself created — a
  pre-existing hand-configured instance never reports a port on the wire
  at all, and teaching Pulse to parse `server.properties` for every
  instance is out of scope. The admin is responsible for picking a port
  range that doesn't collide with anything configured outside Panel's
  knowledge.
- **Provisioning a new server leaves no automatic cleanup on a Pulse crash
  mid-provision.** The `commands` row self-heals via `failStaleCommands`,
  but a partially-downloaded file or half-created directory has no
  automatic sweep — matches this project's existing tolerance for similar
  one-off gaps (e.g. the PID-file bootstrap gap above).
- **Bedrock instances have no remote admin surface at all.** Confirmed
  live: Bedrock Dedicated Server has no RCON support, full stop — not a
  version or config-key issue (see "Raw RCON console" above). The
  console/moderation UI is correctly hidden for Bedrock instances now,
  but nothing replaces it. `mcserver.Manager` never wires up `cmd.Stdin`
  for spawned processes (`pulse/internal/mcserver/manager.go`) — the
  *actual* Mojang-intended interface (typing commands directly into
  `bedrock_server`'s own console) isn't available today, and an
  *adopted* process (one `Reconcile()` found already running, not
  spawned by this Pulse process) couldn't get a stdin pipe retroactively
  even if this were built — only a future Pulse-initiated start would
  get one. Discussed with the user, not yet decided whether to build it.
- **The real-time push layer's Cloudflare Durable Object half is
  unverified** — no live Cloudflare account/deploy access in this
  environment. The Node/`ws` half is fully verified live, including
  against nimo's real production Panel. A standalone DO spike (just
  `InstanceHub` + the `worker-entry.ts` wrapper) should be the first
  thing that touches a real Cloudflare account before trusting the full
  feature there — see "Adaptive heartbeat interval + real-time push
  layer" above.

## Project status / scope

Deliberately implemented, verified end-to-end locally and (mostly) against
a real production Bedrock server ("nimo", home LAN, Tailscale-reachable) —
see `PROJECT_LOG.md` for session-by-session detail on each:

- **Pulse**: enrollment; heartbeat (now interval-adaptive — see "Adaptive
  heartbeat interval" below); start/stop/restart; RCON graceful stop
  (Java only — Bedrock has no RCON at all, see below); PID-file process
  reconciliation; full backup lifecycle
  (create/list/delete/download/restore/schedule/retention); provisioning
  new Java (vanilla/Paper/Fabric/Forge, with configurable heap size and
  gamemode/difficulty/max-players/motd defaults — see "Configurable Java
  heap size" below) and Bedrock (vanilla) servers, including running
  Fabric/Forge's installer program (`pulse/internal/provision`'s
  `RunInstaller` — **installer execution itself unverified, no Java
  runtime in the environment this was built in**, see "Provisioning new
  servers" above); deleting a provisioned instance; a raw RCON console
  (**Java only — confirmed live that Bedrock Dedicated Server has no
  RCON support at all**, see "Raw RCON console" above); a raw
  `server.properties` read/write pair (works on both editions, no RCON
  involved); file management (list/upload/delete); self-update
  (Ed25519-signed atomic binary swap with grace-period confirm/rollback,
  now also guarding against re-applying an already-confirmed version —
  see "Self-update" above for the real infinite-loop incident that
  guard exists because of); allowlisted host diagnostic commands
  (`pulse/internal/diagnostics`). `go build/vet/test` clean, including
  Windows/macOS cross-compiles.
- **Panel**: single-admin auth; enrollment token generation; dashboard
  with online/offline status + accurate in-flight badges
  (starting/stopping/restarting/deleting/backing-up/restoring), player
  count/uptime shown per instance row, and a small icon marking which
  rows are actual Minecraft servers vs. the node/agent card they sit
  inside; a per-instance page with backups, scheduling/retention, a
  properties editor, and a "Danger Zone" delete card — plus, **Java
  instances only**, an RCON console transcript and whitelist/op/ban
  moderation forms (Bedrock instances show a short explanatory note in
  their place instead, since there's nothing RCON-based to offer); a
  file browser; a themed confirm modal; 3 theme palettes; an agent
  detail page with port-range/instances-dir config, a create-server flow
  (Java software choice of vanilla/Paper/Fabric/Forge, Bedrock
  vanilla-only, with reusable saved server definitions pinning the same
  choice, plus a "Server settings" section for Java heap size and
  gamemode/difficulty/max-players/motd defaults — see "Configurable Java
  heap size" above), host stats (CPU/RAM/disk/uptime), and an
  allowlisted diagnostic-command runner; a "Publish Pulse release" form +
  "→ vNEW available" note for self-update; an adaptive heartbeat interval
  and adapter-agnostic real-time push layer for the instance console page
  (see "Adaptive heartbeat interval + real-time push layer" above —
  Node side fully verified live, including against nimo's real
  production Panel; Cloudflare half unverified in this environment,
  flagged explicitly there). `svelte-check` clean; both `ADAPTER=node`
  and `ADAPTER=cloudflare` builds pass. A Tauri desktop thin client
  (`panel/src-tauri/`) is compiled and was live-run-verified (WSL2
  Ubuntu 24.04) but is **being pulled out of active scope after live
  Windows testing** — rescoped as a v2 local-Windows-service initiative,
  not yet designed — see "Panel: one codebase, three adapters" above.
- Repo pushed to `codenexus/axon` (**public**, AGPL-3.0), `main` branch. A
  tag-triggered CI/CD pipeline (`.github/workflows/release.yml`)
  cross-compiles, signs, and publishes a GitHub Release for Pulse, plus a
  second job that builds an (unsigned) Windows Tauri desktop installer
  onto the same release — see "CI/CD for Pulse releases" above. Real
  incident + fix: the release build's `git describe` didn't reliably see
  the tag that triggered it (a well-known `actions/checkout` gotcha),
  which combined with self-update's version-diff trigger to cause an
  infinite self-update restart loop on nimo — see "Self-update" above.
  nimo (home LAN, Tailscale-reachable) is the real production host this
  project verifies against; Panel runs there persistently as a systemd
  **user** service on `panel/server.mjs` (not the generated `node
  build/index.js` — needed once the real-time push layer landed, see
  "Panel: one codebase, three adapters" above).

Deliberately deferred — don't assume half-built unless you find code for it:

- Multi-user auth/RBAC and mDNS/Bonjour discovery. A separate top-level
  multi-node list page — not needed, the dashboard already lists every
  agent; "Node detail" above closed the actual gap (host stats +
  diagnostics on the existing agent-detail page). CI auto-publishing a
  release straight to Panel (today the admin still pastes the download
  URL/signature into `/settings` — see "CI/CD for Pulse releases" above
  for why that's a
  deliberate boundary, not a gap). A hosted/integrated tunnel for exposing
  a Minecraft server's game port without port-forwarding (playit.gg-style)
  — deferred to a later "v2", not started.
- **Per-host RAM-based *automatic* Java heap sizing** — an admin can now
  set heap size explicitly per server at creation time (see "Configurable
  Java heap size" above), but nothing auto-computes a suggested value
  from the host's own free RAM; deliberately not attempted, since
  auto-sizing is a footgun on a host running multiple servers side by
  side that an explicit admin-set number avoids. Split port ranges per
  edition (one shared range per agent covers both). Any software beyond
  Paper/Fabric/Forge
  (e.g. Sponge, Quilt) and anything beyond the latest ~3 versions per
  edition/software. Editing an existing server definition (delete and
  recreate instead). Mod/plugin management (installing actual mods/
  plugins onto a provisioned server) — this feature is server-*software*
  provisioning only, same as vanilla only ever installed the bare server.
  A Bedrock loader equivalent — Bedrock has no comparable ecosystem.

See `PROJECT_LOG.md` for session-by-session history and next steps, and
`STYLE.md` for UI/UX conventions.

## License

AGPL-3.0.
