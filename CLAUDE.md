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

This repo has a working vertical slice (start/stop/restart, RCON graceful
stop, PID-file process reconciliation) plus a fully implemented backups
subsystem (manual create/list/delete, download, restore) — see "Project
status / scope" below before assuming something is or isn't implemented.

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
"Pulse vX" per agent; without it every build looks identical ("dev"), which
made verifying what's actually deployed to a given node needlessly hard
during this feature's development. If you build Pulse by hand (not via
`make`) for anything other than a throwaway local test, pass the same
`-ldflags` — e.g.
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

Two more env vars matter for the backups feature, both Node-adapter-only,
both with working defaults if unset:
`AXON_LOCAL_DB_PATH` (default `<cwd>/axon-local.db`) and
`AXON_BACKUP_HOLDING_DIR` (default `<cwd>/.backup-holding` — see "Backups"
below for what this holds and why it's transient).

Node ≥22.13 required (pnpm 11's own requirement, plus the local dev DB uses
the `node:sqlite` builtin, stable from Node 22.5+). `corepack enable` picks
up the pnpm version pinned in `panel/package.json`'s `packageManager` field.

### Local dev/testing workflow established this feature

For any change touching Pulse↔Panel interaction, spin up an **isolated
sandbox** before touching a real dev server or a real Pulse-managed host:
a scratch dir with its own `AXON_LOCAL_DB_PATH` and (if backups are
involved) `AXON_BACKUP_HOLDING_DIR`, a Pulse binary pointed at a throwaway
`pulse.instances.json` with a `sh -c "sleep N"` stand-in instance (matches
the existing Go test philosophy — see `manager_test.go`), `HOME` overridden
so Pulse's credential file doesn't touch `~/.config/axon`, and a short
`--interval` (e.g. `3s`) so the heartbeat cycle doesn't make you wait. Drive
it with `curl` against the SvelteKit form actions (they accept normal
form-encoded POSTs, no browser needed) and query the sqlite DB directly
with `node -e "...node:sqlite..."` to assert on outcomes. This caught real
bugs before they ever reached a real host — worth the setup cost every
time, especially before anything destructive (restore).

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
(`pulse/internal/updater/`, ported from Beacon's `agent/internal/updater/*`
and `agent/tools/{keygen,sign}`; see "Self-update" below for what changed
in the port).

The wire contract lives in two parallel type definitions kept in sync by
hand, not by shared schema/codegen: `pulse/internal/protocol/types.go` (Go)
and `panel/src/lib/server/protocol.ts` (TypeScript).

- **Heartbeat** (`POST /api/v1/heartbeat`, Pulse → Panel, ~30–60s): host
  metrics + per-instance status array + the agent's own `interval_seconds`
  (so Panel can compute a "next heartbeat in ~Ns" countdown instead of
  guessing a fixed default — added for the backups UI, see below). Panel
  upserts `server_instances` rows and returns any queued commands, marking
  them `sent`, plus an optional `update` field (see "Self-update" below)
  whenever a newer Pulse release is published for this agent's os/arch.
- **Command results** are piggybacked onto the *next* heartbeat
  (`pending_command_results`) rather than pushed immediately — a resolved
  design decision (simplicity over latency, matches Beacon precedent), not a
  gap to "fix". **This has a real consequence to design around**: if Pulse
  itself restarts (e.g. mid-deploy) between finishing a command and the
  heartbeat that would report it, the result is lost and the command sits
  at `sent` forever with no result. This happened during development
  (a `delete_backup` got orphaned this way) and was first fixed by hand
  (correcting the DB to match verified on-disk reality); it's now handled
  automatically by `failStaleCommands()`
  (`panel/src/lib/server/commands.ts`) — a command stuck `sent` past 3
  missed heartbeats (reusing `isOnline()`'s "presumed offline" bar,
  `panel/src/lib/heartbeat.ts`) auto-resolves to `failed` with an honest
  "timed out waiting for a result" message, and any dependent `backups`/
  `backupDownloads` row is resolved the same way a genuine failure would
  resolve it (both paths now share `resolveCommandOutcome()`). Piggybacked
  on requests, not a timer, per the same Cloudflare-compatibility
  constraint as `pruneExpiredDownloads()` — called from the heartbeat route
  (agent-scoped), the dashboard load (unscoped, since the owning agent may
  never come back), and the instance detail page load (agent-scoped).
- **Enrollment** (`POST /api/v1/enroll`, one-time): token → device
  credential. Enrollment tokens and device credentials are never stored
  raw, only `sha256Hex()` hashes (`panel/src/lib/server/tokens.ts` /
  `pulse/internal/credential`).
- **Backup file transfer** (`POST /api/v1/backups/{id}/upload`, Pulse →
  Panel, on-demand): a separate, non-JSON, non-heartbeat call — see
  "Backups" below.

### Pulse always runs, even same-machine

When Panel and a Minecraft server are on the same box, Pulse still runs as a
separate process talking to Panel over `localhost` using the identical
heartbeat contract used remotely — there is no special-cased "direct mode".
Don't add one; the point is that adding a second machine later requires zero
re-architecture.

### Pulse forgets nothing it doesn't have to: PID-file process reconciliation

`Manager` (`pulse/internal/mcserver/manager.go`) keeps instance running-state
purely in memory. Naively, that means **every Pulse restart** (binary
upgrade, crash, host reboot) forgets whether a real Minecraft process is
still running underneath it — discovered the hard way when deploying this
feature's own binary updates kept losing track of a real, live
`bedrock_server` process. Fixed with `Manager.Reconcile()`
(`pulse/internal/mcserver/pidfile.go` + the `Reconcile`/`watchReattached`
methods in `manager.go`):

- `Start()` writes a small JSON PID file (`.pulse-pid`, inside
  `working_dir`) recording the spawned PID and command; the exit-detection
  goroutine removes it on exit.
- On startup, call `manager.Reconcile()` (after `NewManager`, before the
  heartbeat loop) — for each configured instance, read its `.pulse-pid` if
  present, and adopt the process (mark it Running, no restart) if it's
  still alive **and** its command's base executable name matches what's
  configured (`processMatches` in `pidfile.go`) — a best-effort fingerprint
  guarding against PID-reuse-after-reboot false positives. Uses
  `github.com/shirou/gopsutil/v3/process` (already a dependency via
  `internal/inventory`) for the cross-platform liveness/cmdline check
  rather than hand-rolled unix/windows syscalls.
- An adopted process has no `*exec.Cmd` (Pulse isn't its parent, so
  `cmd.Wait()` would fail with ECHILD once the *original* Pulse process
  that spawned it is gone) — `watchReattached` polls liveness every 2s
  instead. `Stop()`/`gracefulStop` were refactored to take a bare `pid int`
  rather than `*exec.Cmd` (`terminate()` in `process_unix.go` /
  `process_windows.go` likewise), so stopping works identically whether
  Pulse spawned the process this run or adopted it.
- **One-time bootstrap gap, not a recurring bug**: a process spawned by a
  *pre-reconciliation* Pulse binary never got a PID file written for it, so
  the very first restart onto the new binary still needs a **manual**
  one-time PID-file seed (`echo '{"pid":N,"command":["..."]}' > .pulse-pid`
  as the process owner) before `Reconcile()` has anything to find. Every
  Pulse restart *after* that self-heals correctly, since every process it
  spawns from then on gets a real PID file automatically.

### Backups (implemented: manual create/list/delete, download, restore, scheduling + retention)

Full design lived in a plan doc during development; the durable summary is
here. Four foundational decisions, all still load-bearing:

1. **Pulse's disk is the only durable copy.** Panel never stores backup
   bytes persistently — only transiently, for an active download (see
   below). This avoids duplicate storage and matches the
   no-inbound-connections principle: Pulse pushes bytes to Panel on
   request, Panel never reaches into Pulse.
2. **Scheduling is Panel-owned**, evaluated inline during heartbeat
   handling — no new Go dependency in Pulse, fits the request-driven (no
   persistent timers) design Cloudflare compatibility requires. See
   "Backup scheduling + retention" below for the implementation.
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

**Why backups stop the server first, always** (`pulse/internal/backup`
package doc): Java Edition's RCON `save-off`/`save-all` pause-writes
convention is not supported by Bedrock Dedicated Server's RCON. Rather than
branching backup behavior per edition, `backup_instance` and
`restore_backup` both: stop the instance if running (reusing
`gracefulStop`, which already works correctly on both editions) → do the
archive/restore work with nothing writing to disk → restart afterward
*only for backup*, restore always leaves it stopped (the admin should look
at a restored world before deciding to bring it back up). This is simpler
and edition-agnostic, at the cost of a brief stop/restart cycle for a
backup taken while running.

**A structural UX consequence of the above**: because the whole
stop→archive→restart cycle happens *between* two heartbeats, Pulse's own
`running_state` reporting frequently never has a chance to show
"stopping"/"starting" for it at all — the wire state just jumps straight
from `running` to `running` across two heartbeats with nothing observable
in between. **Don't try to fix this by making Pulse report faster; it
structurally can't** (it doesn't know about the command until after that
heartbeat's status snapshot is already built). The fix used throughout
this feature: derive "is something happening to this instance" from
**Panel's own command/backup tracking**, independent of `running_state`.
See `panel/src/routes/+page.server.ts`'s `instancesBackingUp`/
`pendingActions` and the "Backing up…"/"Restoring…"/"Starting…" badges
that replace (not stack next to) the normal status badge on the dashboard.
Same reasoning applies to plain start/stop/restart — a `restart_instance`
click can fully resolve within one heartbeat gap too.

**Push-backup transfer** (download): admin clicks Download →
`downloadBackup` action queues a `push_backup` command and inserts a
`backup_downloads` row (`status='requested'`, `requestedAt`, and an
`expiresAt` safety-net deadline set immediately in case it never becomes
ready — see `panel/src/lib/server/backupDownloads.ts`) → Pulse's next
heartbeat picks up the command → `protocol.Client.PushBackup` streams the
file to `POST /api/v1/backups/{id}/upload` (bearer device-credential
auth + an `X-Axon-Instance-Id` header Panel cross-checks) → upload route
streams straight to Panel's local holding dir
(`AXON_BACKUP_HOLDING_DIR`) → marks the row `ready` with a fresh 10-minute
TTL → client-side polling (unconditional, not just "while known
in-flight" — see "Known gaps") notices `ready` and **auto-triggers** the
actual browser download (hidden `<a download>` + `.click()`) rather than
making the admin click twice. `GET /backups/{id}/download-file` streams
the held file back with delivery-confirmed cleanup (unlink + mark
`expired` on stream `end`); a heartbeat-piggybacked sweep
(`pruneExpiredDownloads`) is the backstop for holds nobody ever collects.

**One real bug avoided, not just noted**: `protocol.Client`'s shared
`http.Client` has a blanket 15s timeout — fine for small JSON
enroll/heartbeat calls, would silently abort a multi-GB backup upload
partway through. `PushBackup` uses a **separate** `http.Client` with no
timeout (`uploadHTTPClient` in `pulse/internal/protocol/client.go`) rather
than raising the shared one, so a hung heartbeat still fails fast.

**Restore** (`executeRestore` in `pulse/cmd/pulse/main.go`, `Engine.Restore`
+ `extract()` in `pulse/internal/backup/engine.go`): stop (if running) →
take an automatic **pre-restore safety backup** first (Panel pregenerates
its id and passes it in the payload, so Pulse never invents ids — matches
the convention every other backup command already follows) → wipe
`working_dir` and extract the target archive in place (**exact rollback,
not a merge** — anything created after the target backup was taken is
gone) → leave stopped regardless of prior state. The safety backup's
size/checksum are reported in the `CommandResult` **whenever that step
itself succeeded**, independent of the overall `Success` flag — so Panel
still records a real, usable safety backup even if the *extract* step
afterward fails. A failed restore (bad id, disk error) never touches
`working_dir` — verified explicitly in both unit tests
(`TestRestoreUnknownBackupFailsWithoutTouchingWorkingDir` in
`pulse/internal/backup/engine_test.go`) and a full sandboxed HTTP-level
test before this ever touched a real host.

**Confirming a restore uses a themed modal**
(`panel/src/lib/components/ConfirmModal.svelte`), not the browser's native
`confirm()` — see STYLE.md.

**Backup scheduling + retention** (`panel/src/lib/server/backupSchedules.ts`):
one `backupSchedules` row per instance (`serverInstanceId` PK, so presence
of the row plus which of `intervalHours`/`keepCount`/`keepDays` are
non-null fully expresses the state — no separate enabled flag; deleting
the row turns everything off). `runSchedulesForAgent()` is called
agent-scoped from the heartbeat route, in the same opportunistic-cleanup
spot as `pruneExpiredDownloads`/`failStaleCommands` and for the same
reason (request-piggybacked, no timer), before the `queued`-command
select so anything it queues ships in that same response:

- `queueScheduledBackupIfDue()`: if `intervalHours` is set and
  `now >= lastRunAt + intervalHours`, queues a `backup_instance` command
  (`trigger: 'scheduled'`) exactly like the manual "Create backup" button.
  `lastRunAt` is stamped *immediately*, before the backup resolves, so a
  short interval relative to how long a backup takes can't cause pile-up —
  the next due-check compares against this timestamp, not completion. A
  freshly-configured schedule (`lastRunAt` still null) is due on the very
  next heartbeat rather than waiting a full interval — lets the admin
  confirm a schedule is actually wired up without a heartbeat-scale wait.
- `applyRetention()`: candidates are an instance's `status='complete' AND
  pendingOperation IS NULL` backups (already-in-flight rows are excluded
  from ranking entirely, not just from being re-deleted), newest first. A
  backup survives if it's the single newest, **or** within the
  `keepCount` budget, **or** within the `keepDays` window — union, not
  intersection ("keep at least N most recent AND everything from the last
  D days" is the forgiving, standard interpretation). Everything else gets
  a `delete_backup` command queued exactly like the manual Delete button.
  Runs unconditionally alongside the due-check on every heartbeat for
  every configured schedule (cheap: a select + in-memory filter, writes
  only when something's actually stale).
- Exported separately (not one combined function) so the instance page's
  **Apply Retention Now** button can call `applyRetention()` alone,
  without also triggering an unexpected extra backup.
- No live due-countdown in the UI — the instance page shows the configured
  values and a static "last automatic backup" timestamp; the existing
  unconditional 3s poll already surfaces schedule-driven backup rows and
  their in-flight badges for free via the same `backups`/`pendingOperation`
  machinery every other backup uses.

### Provisioning new servers (Java + Bedrock vanilla)

Until this feature, every instance had to be hand-configured: a working
binary/jar placed in `working_dir` and the exact launch `command` hand-written
into `pulse.instances.json` before Pulse ever touched it. This lets an admin
create a real, running server from Panel — deliberately narrow scope (vanilla
only, latest ~3 versions per edition, Linux-only Java auto-install, fixed
Java heap, one shared port range) to keep v1 achievable; see "Deliberately
deferred" below for the exact cuts.

**Version/download-URL resolution lives in Panel, not Pulse** — plain
outbound `fetch()` calls from Panel's own server code
(`panel/src/lib/server/versionCatalog.ts`), which works identically on the
Node adapter and Cloudflare Workers, so no scraping/parsing logic needs to
live in Pulse and the admin sees resolved versions instantly in the
create-server form (no heartbeat round-trip just to populate a dropdown).
Java resolves via Mojang's public `version_manifest_v2.json` — its
per-version detail JSON includes both `downloads.server.url` and
`javaVersion.majorVersion` (confirmed live against the real API while
building this), so there's no hardcoded MC-version-to-Java-version mapping
table anywhere. Bedrock has no equivalent API — Panel attempts to scrape
minecraft.net's download page, but **this is confirmed unreliable in
practice** (a live test during development timed out, likely bot-detection
or JS rendering) — treated as best-effort throughout: a scrape failure
yields an empty/stale cached result rather than an error, and the
create-server form always shows an admin-editable download-URL field for
Bedrock rather than trusting the scrape blindly. Pulse itself does zero
version-catalog resolution — it receives a fully-resolved
`create_instance` payload (concrete version, download URL, required Java
major version) and just acts on it.

**Java runtime handling is auto-install, Linux-only**
(`pulse/internal/javaruntime/`): detects an already-installed match first
(`PATH` + well-known distro glob paths), and if missing, installs via
`apt-get`/`dnf`/`yum` (whichever is present) using a small hand-maintained
package-name table (`packageNames` — update by hand as Mojang's
requirements move, same philosophy as the wire types). **This needs a new
operational prerequisite**: the Pulse service user needs a scoped
passwordless-sudo rule for package installation — see "Deploying Pulse to
a real host" below. Any non-Linux `GOOS`, or a missing package-manager, or
the install itself failing, fails the command with a clear message rather
than attempting anything unsafe — never silently falls back to a
half-working state.

**Provisioning mechanics** (`pulse/internal/provision/`, deliberately
separate from `internal/backup` — that package's doc explicitly scopes it
to archive/restore of an *existing* instance, this one only ever runs
once, before either backup or process-lifecycle code has anything to work
with): downloads the URL (Java → `server.jar` directly; Bedrock → a temp
file extracted as a zip, with the same zip-slip defensive check
`backup/engine.go`'s tar extraction already has — genuinely load-bearing
here, since this archive comes from a remote third party, not code this
repo produced itself — and an explicit `chmod 0o755` on `bedrock_server`
since zip entries don't reliably carry the exec bit), then writes
`eula.txt` (Java) and patches `server-port` into `server.properties` via a
new `mcserver.WriteProperty` (exported alongside the existing
`ReadRCONConfig` reader in `properties.go`, since a freshly-provisioned
instance's `server.properties` may not exist yet at all — Java's server
generates most of it on first boot, so partial pre-seeding is the correct
approach, not a workaround). Bedrock's launch needs
`LD_LIBRARY_PATH=.` to find its bundled `.so` files — added a new `Env
[]string` field to `mcserver.InstanceConfig` for this (`Start()` appends
it to the spawned process's inherited environment), the first instance
config field with no hand-configured-instance equivalent.

**Dynamic instance registration** (`Manager.AddInstance`,
`pulse/internal/mcserver/manager.go` + `config_persist.go`'s
`SaveConfig`): the instance list was, until now, entirely fixed at process
start (`NewManager(configs)` from one `LoadConfig` file read — there was
no `AddInstance`-shaped method anywhere in the package). `AddInstance`
inserts into the in-memory map and atomically rewrites
`pulse.instances.json` (temp file + `os.Rename`, same directory — atomic
on POSIX and Windows) in the *same* critical section (`m.mu`), rolling
back the in-memory insert if the disk write fails, so memory and disk can
never diverge and two `create_instance` commands landing in the same
heartbeat batch can't race the write. **This is the concrete precedent to
follow for the still-unbuilt self-update feature's atomic binary swap.**

**Async command execution — the one exception to `execute()`'s contract**:
every other command type is a single blocking call that returns a
terminal `CommandResult` within one heartbeat cycle. `create_instance`
structurally can't (installing Java, downloading a jar/zip — can take
minutes). `runLoop` (`pulse/cmd/pulse/main.go`) intercepts it *before*
`execute()`'s switch and runs it in a goroutine
(`pulse/cmd/pulse/create_instance.go`'s `creationJob`, tracked in an
`activeJobs` map that persists across heartbeat iterations), reporting a
coarse phase (`preparing` → `installing_java` (Java only) → `downloading`
→ `configuring` → `registering`) via a new `HeartbeatRequest.
InProgressCommands` field until it finishes, at which point its result
folds into the normal `pending_command_results` batch on a later
heartbeat. Panel's `commands.progressPhase` column mirrors this, guarded
to `status = 'sent'` only (same never-touch-a-terminal-row guard as the
`pending_command_results` loop) so a late progress report can't clobber a
row the stale-command sweep already resolved. No other command type needs
this — don't reach for it unless something is genuinely multi-cycle.

**Port + working-dir placement**: Panel auto-assigns both. Each agent gets
an admin-configured `portRangeStart`/`portRangeEnd`/`instancesRootDir`
(new columns on `pulseAgents`, set from the new `/agents/[pulseAgentId]`
page — the first agent-detail view this project has had; previously the
dashboard was the only agent-facing UI at all).
`panel/src/lib/server/portAllocator.ts`'s `allocatePort` picks the first
free port considering both already-recorded `server_instances.port` values
*and* ports already claimed by a still-in-flight `create_instance` command
(parsed from `commands.payload` for `status IN ('queued','sent')` rows) —
the second check closes a real race, since no `server_instances` row is
pre-inserted for `create_instance` (unlike backups' pregenerated-row
pattern: there's no honest `running_state` etc. to give it before Pulse
ever confirms the instance exists), so two quick create-server submissions
could otherwise allocate the same port before the first one's instance
ever showed up in `server_instances`. See "Known gaps" for the allocator's
one real limitation (blind to legacy hand-configured instances).

### Raw RCON console

`console_command`: an arbitrary admin command sent verbatim to a running
instance's RCON port (`Manager.RunConsoleCommand`,
`pulse/internal/mcserver/rcon_command.go`) and its text response returned
to Panel. This is a normal synchronous command like every other type
except `create_instance` — RCON's dial+auth+execute is bounded at a few
seconds, so it runs inside `execute()`'s switch
(`executeConsoleCommand` in `main.go`) like `start_instance`/
`backup_instance`/etc., not the async/goroutine pattern
`create_instance` needed for its multi-minute provisioning.

**`Success` vs `Output` is a deliberate split, not two names for the same
thing**: `Success` reflects whether the RCON exchange itself worked
(reachable, authenticated) — `false` only for "not running," "RCON not
configured," or a connection/auth failure. `Output` carries whatever text
came back whenever the exchange succeeded, *even if the game itself
rejected the command* (e.g. `/foo` → `"Unknown command"` is still a
successful RCON round-trip, not a Pulse-side failure) — `Message` stays
reserved for "the round-trip itself failed," matching how every other
command type already uses `Message`.

**No fallback path, unlike graceful stop.** `gracefulStop()` falls back to
a bare OS signal when RCON isn't usable, because "stop the process" has a
meaningful non-RCON way to happen. An arbitrary console command doesn't —
if RCON isn't enabled/configured/reachable, `RunConsoleCommand` fails
cleanly with a specific reason (not running / not configured / connect
failed / auth failed) rather than attempting anything else.

**Latency UX, resolved**: unlike a real terminal, a command sent now only
reaches Pulse on its next heartbeat, and the response comes back on the
heartbeat after that — up to ~2× `--interval` end-to-end in the worst
case, an open question flagged since an earlier session. Resolved as: the
instance page's transcript (`panel/src/routes/instances/[serverInstanceId]/+page.svelte`,
reusing the `commands` table directly — no new table, `type='console_command'`
rows *are* the transcript) polls at the page's normal 3s baseline, but
drops to 1s while a console command (or, since properties editing reuses
this same effect — see "Server properties editor" below — a properties
load/save) is `queued`/`sent` (a `$effect` depending on a derived
`fastPollNeeded` boolean naturally tears down and restarts the poll
interval at the new cadence when it changes) — polling faster doesn't beat
Pulse's `--interval` floor, but it does shave the perceived wait down to
noticing the result sooner once Pulse has actually reported it. The
transcript itself reuses the exact "Sent, waiting…" `badge-pulsing`
precedent already established for backups/start/stop, rather than
pretending to be a live console.

### Server properties editor

A raw-text editor for an instance's `server.properties`
(`ReadPropertiesFile`/`WritePropertiesFile`,
`pulse/internal/mcserver/properties.go`) on the same instance page as the
RCON console. **Deliberately raw text, not a structured per-key form** —
the key set differs by edition/software and changes over time; Panel never
parses the file, it's opaque content in and content out. Two more
synchronous command types, `read_properties`/`write_properties`, following
`console_command`'s exact shape:

- `read_properties` needs no payload; its result comes back in
  `CommandResult.Output` — the same field added for the RCON console,
  reused here rather than adding a new one, since "text response for this
  command" is exactly what both need.
- `write_properties` carries the full replacement content
  (`WritePropertiesCommandPayload`) and overwrites the file **atomically**
  (temp file + `os.Rename`, same directory) — same pattern as
  `mcserver.SaveConfig` (provisioning), so a crash mid-write can't corrupt
  the file the running server depends on.
- No "must be stopped" requirement — `server.properties` is only read at
  server startup, so editing while running is safe, it just doesn't take
  effect until a restart (same as hand-editing it over SSH). The UI says
  so explicitly after a save rather than implying anything changed live.
- Panel keeps zero server-side cache of "current properties" — `load`
  queries only the single most recent `read_properties`/`write_properties`
  command row (`latestPropertiesCommand`), and the instance page's
  `propertiesText` textarea state applies a completed `read_properties`
  result to itself exactly once per command id (a `loadedPropertiesCommandId`
  guard) — the same "apply an incoming result once, never clobber further
  local edits on a later poll of the same already-applied command"
  pattern the create-server page's Bedrock-URL prefill already established.

### File management (browse/upload/delete an instance's working_dir)

Own dedicated route (`/instances/[serverInstanceId]/files`, not a card on
the instance page — enough surface, matching the `new-instance`-style
convention of "a real flow gets its own URL"). Whole `working_dir` tree is
browsable, not restricted to a hardcoded `plugins`/`mods` folder — those
names aren't even consistent across server software.

**`pulse/internal/filemanager/`** — a new package, distinct from `mcserver`
(process lifecycle), `backup` (whole-tree archive/restore), and
`provision` (one-time software acquisition): `List` (non-recursive,
directories-first), `Delete` (recursive, explicitly rejects deleting
`working_dir` itself), `Save` (atomic temp-file+rename, same pattern as
`WritePropertiesFile`/`SaveConfig`). Every function funnels through a
`resolve()` helper that rejects any admin-supplied path escaping
`working_dir` — **load-bearing here**, unlike `backup`'s and `provision`'s
identical `withinRoot` helpers (duplicated per-package, not shared), since
every path this package touches is admin-controlled, not
Pulse-self-produced. Lexical check only, same limitation those two already
carry — doesn't dereference symlinks; not a new gap, documented via an
explicit test rather than silently assumed.

**Uploads need bytes to flow *into* Pulse — the second reversed-transfer
pattern, after backup downloads.** Pulse never accepts inbound
connections, so the admin uploads to Panel first (a normal browser
request, held transiently in `file_uploads` — simpler than
`backup_downloads`' shape, one status set and one TTL, since the browser
action already has the complete file on disk before the row exists, no
"requested but not ready" window to represent), then Pulse *pulls* it on
its own next heartbeat via `(*protocol.Client).PullFileUpload` (a `GET`
Pulse itself initiates, mirroring `PushBackup` reversed) against
`GET /api/v1/files/[holdingId]/download` — a genuine hybrid of the two
existing backup-transfer routes: **auth** like the backup-upload route
(bearer device-credential, since Pulse calls it), **streaming +
delivery-confirmed cleanup** like `download-file`.

**`CommandResult.Output`'s third reuse**: `list_files`'s result is a
JSON-encoded `[]filemanager.Entry` in the same free-text field already
carrying RCON output and raw properties content — see that field's doc
comment (both `types.go` and `protocol.ts`) for all three uses in one
place. `resolveCommandOutcome` needs no branch for any of the three new
command types (`list_files`/`delete_file` have no dependent row;
`upload_file`'s only dependent row, `file_uploads`, already resolves as a
side effect of the download route being hit — by the time Pulse can report
`upload_file`'s outcome, it has necessarily already called
`PullFileUpload`, so the download route's stream-end/error handlers have
already settled it in every normal case; the one gap, Pulse never calling
`PullFileUpload` at all, is covered by `pruneExpiredFileUploads`'s TTL
sweep, the same backstop `pruneExpiredDownloads` provides for the
equivalent `push_backup` gap).

**Delete is the one destructive action this session that gets
`ConfirmModal`** where several other destructive-ish actions deliberately
didn't (plain backup delete, console commands, properties save) — the
combination here is different in kind: an admin-navigable *arbitrary*
subtree, one click, *recursive* (`os.RemoveAll`), no listing of contents
shown first, no easy undo/recreate path the way a backup or a console
command has.

**A real, pre-existing bug found and fixed while building this**:
`@sveltejs/adapter-node`'s built server caps every request body at `512K`
by default (`BODY_SIZE_LIMIT`) — confirmed live (a >512K upload got a
clean `413` without the env var raised, and succeeded once
`BODY_SIZE_LIMIT=Infinity` was set). This silently affected the existing
`push_backup` upload route too, not just this feature — `vite dev` never
enforces it, only the built server does, which is why it went unnoticed
through this project's whole development-so-far. See `panel/README.md`'s
"Running the built adapter-node server directly" section.

### Self-update

`pulse/internal/updater/` — Pulse can update itself in place once a signed
release is published on Panel, ported from Beacon's
`agent/internal/updater/*` and `agent/tools/{keygen,sign}` with the
adaptations below. This only covers *updating* an already-enrolled Pulse;
first install onto a host is still the fully-manual flow in "Deploying
Pulse to a real host" below — there's no running Pulse to self-update
*from* yet on a brand-new host.

**Signing** (`verify.go`, `pulse/tools/{keygen,sign}`): releases are
signed with Ed25519, not just checksummed — `sha256sum`-matching (the
manual-deploy flow's only integrity check) proves the file wasn't
corrupted in transit, not that Panel's word about it should be trusted;
signing is the actual security boundary, since a compromised or spoofed
Panel response is exactly what this needs to be robust against. `VerifyBinary(path, sigHex)`
checks a hex Ed25519 signature over the SHA-256 digest of the downloaded
file against a hardcoded `pinnedPublicKey` — split into an unexported
`verifyBinaryWithKey(path, sigHex, pubKeyHex)` so tests can exercise the
crypto with a throwaway keypair instead of the real one.
`pulse/tools/keygen` (`go run ./tools/keygen`) generates a keypair;
`pulse/tools/sign` (`AXON_SIGNING_KEY=<hex private key> go run ./tools/sign
<binary-path>`) prints the hex signature to publish alongside a release.
**The private key is never committed or persisted anywhere in this
repo or on any filesystem it touches** — generated once, surfaced only in
chat for the user to store externally (password manager), same handling
as any other one-time secret this project generates (enrollment tokens).
Only the public key lives in source (`verify.go`'s `pinnedPublicKey`).

**Swap + restart** (`swap_unix.go`/`swap_windows.go`, build-tag split like
every other platform-specific pair in this codebase): Unix backs up the
running binary, `os.Rename`s the new one into place (atomic, same
filesystem — the kernel keeps the old inode alive via the current
process's open fd), then `syscall.Exec`s into it, replacing the process
image in place — **same PID, but a completely fresh `main()`**. Windows
can't do that same rename-over-a-running-exe trick as a *no-op* (unlike
Unix, opening a file doesn't pin its name), but Windows locks by handle,
not by name, so a rename of the running exe *away* is still legal — swap
renames the old exe aside, moves the new one into its place, spawns it,
and exits.
Simplified from Beacon's version: no Windows-service-mode branch (and no
`golang.org/x/sys/windows/svc` dependency for it), since Pulse has no
service-mode capability anywhere else in this codebase.

**Why the PID-losing-in-memory-state restart is safe, not a new risk**:
`syscall.Exec`'s fresh `main()` forgets everything Pulse knew about
already-running Minecraft processes — this is *exactly* the scenario
`Manager.Reconcile()` (see "PID-file process reconciliation" above)
already exists to solve, and it already runs early in every Pulse
startup. Self-update is a real, automatic instance of the restart case
that machinery was built for — confirmed live in the sandboxed
verification below (`reconciled instance "..." with already-running pid
N` logged across a self-triggered swap).

**Grace-period confirm/rollback state machine** (`updater.go`): before
swapping, `ApplyUpdate` writes `update-state.json` (pending version,
backup path, a deadline 10 minutes out — `gracePeriod`) into
`credential.Dir()`, then swaps. On the *next* process start (i.e.
immediately after the swap-triggered restart), `Start()` finds this file
and launches `awaitConfirmation`, which selects between `checkInC` (fed
by `NotifyCheckIn()`, called from `main.go`'s `runLoop` after every
*successful* heartbeat) and the deadline timer: confirmed → delete the
state file and the backup binary; unconfirmed (Panel unreachable, new
binary broken, etc.) → `rollback()` restores the backup and re-execs into
it. Both paths were exercised live, not just unit-tested — see below.

**Ported from Beacon, but structurally simpler — no separate poll loop**:
Beacon's updater has its own always-on version-check loop (5-minute
startup stagger, 24h interval, a dedicated `GET .../version` endpoint).
Axon doesn't need any of that — Pulse's heartbeat is already a
periodic Pulse-initiated cycle on its own `--interval`, so the update
check just rides `HeartbeatResponse.Update` (`protocol.UpdateInfo` —
`version`/`download_url`/`signature_hex`, mirrored in `protocol.ts`).
`main.go`'s `runLoop` calls `updater.ApplyUpdate(exePath, credential.Dir(),
*resp.Update)` directly after processing that heartbeat's commands, no
separate polling goroutine, timer, or endpoint exists. `downloadFile` uses
an explicit no-timeout `http.Client{}` (`downloadHTTPClient`), matching
this codebase's established explicit-client convention (see
`uploadHTTPClient` in "Backups" above) rather than Beacon's implicit
`http.Get`.

**In-flight guard, beyond what Beacon needed**: `runLoop` only calls
`ApplyUpdate` when `len(activeJobs) == 0` — an update landing mid a
multi-minute `create_instance` provisioning job would otherwise silently
kill that goroutine on re-exec. If a job's in flight, the update is simply
deferred; Panel keeps offering it on every subsequent heartbeat until
versions match (see below), so nothing is lost by waiting.

**Panel: `pulseReleases` table + `/settings` publish form** — insert-only,
no upsert, no "current" flag: the heartbeat route always takes the newest
row (`ORDER BY createdAt DESC LIMIT 1`) for a given `(os, arch)`, so
publishing a new release naturally supersedes the old one. **Panel never
verifies the signature itself** — that's Pulse's job (`VerifyBinary`, the
real security boundary); Panel's publish form is metadata relay only, the
admin is responsible for actually building, signing (`pulse/tools/sign`),
and hosting the binary somewhere Pulse can reach. **Deliberately no
downgrade protection**: Pulse's version string is a `git describe` short
hash with no total order, so "the newest published release's version
differs from what this agent just reported" is Panel's whole comparison —
matches this project's existing single-admin trust model (same reasoning
already applied to port-pool ranges, retention config, etc.), not a new
relaxation. `latestVersionsByPlatform`/`updateAvailableFor`
(`panel/src/lib/server/pulseReleases.ts`) share this same comparison
between the heartbeat route and the dashboard/agent-page "→ vNEW
available" note (cosmetic only — the update itself is fully automatic
from Panel's perspective once published; there's no separate
progress UI, the version string on the agent card just changes on a
later heartbeat).

**Verification note**: the swap/restart/reconcile/confirm/rollback
mechanics can't be meaningfully unit-tested (swapping the actual test
binary's own process isn't something `go test` can safely exercise) — they
were verified with a real sandboxed dry run instead: two real Pulse
binaries (old + new, distinct injected `-X main.version=...` strings)
against a throwaway Ed25519 keypair swapped into a temporary local build of
`verify.go` for the test only (never the real pinned key), serving the new
binary from a local static file server and seeding a `pulseReleases` row
by hand. Confirmed live: the happy-path swap-and-confirm (including
`Reconcile()` re-adopting a still-running stand-in Minecraft process
across the restart, twice, across two consecutive self-triggered
updates); the rejection path (a deliberately-wrong signature never swaps,
Pulse keeps running the original binary and keeps retrying every
heartbeat); and the rollback path (killing Panel right as the swap fires
so no heartbeat can ever confirm — the grace-period deadline fired,
`rollback()` restored the backup binary with no error, and the process
resumed heartbeating normally afterward). The in-flight `create_instance`
guard was verified by code inspection only (a one-line boolean condition,
low risk) rather than a live run, given the setup cost of a real
multi-minute provisioning job in the sandbox.

### Deploying Pulse to a real host — first install is still manual

Self-update (above) only covers updating an *already-enrolled* Pulse.
First install onto a new host has no bootstrap path yet — there's no
running Pulse to self-update *from*. The established flow for this:
cross-compile with the `-X main.version=...` ldflags
above, `scp` to `<host>:/tmp/pulse-new`, verify the `sha256sum` matches on
both ends, then hand the actual privileged swap to the human — **`sudo`
commands run over SSH to a remote host are blocked by the auto-mode
permission classifier**, by design (this is correct behavior, not a bug to
route around). The swap itself:

```sh
sudo mv /tmp/pulse-new /usr/local/bin/pulse
sudo chown root:root /usr/local/bin/pulse && sudo chmod 755 /usr/local/bin/pulse
sudo kill <old-pid>
sudo -u <service-user> bash -c 'nohup /usr/local/bin/pulse --server-url <url> --config <path> --interval 30s > <logfile> 2>&1 & disown'
```

No `--enroll-token` needed on restart — the saved credential carries over.
Verify the swap by checking `Reconcile()` logged an adoption
(`reconciled instance "X" with already-running pid N`) if a game server
process was already running, and that Panel's `last_seen_at` updates again
shortly after.

**New prerequisite for server provisioning**: if Java-edition server
creation is going to be used on a host, the Pulse service user needs a
scoped passwordless-sudo rule for package installation (Debian/Ubuntu:
`apt-get update`/`apt-get install -y openjdk-*`; RHEL/Fedora: `dnf install
-y java-*-openjdk*`/`yum` equivalent) — see "Provisioning new servers"
above. Without it, `javaruntime.EnsureInstalled` fails cleanly with a
message telling the admin to install Java manually instead; Bedrock
creation needs no such rule.

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
exists" **and** "duplicate column name" — the latter added this feature,
since it's the first schema change to use `ALTER TABLE ... ADD` rather
than only `CREATE TABLE`; without it, a long-running local dev server would
crash on its second restart after any future additive column change).
`scripts/seed-dev.mjs` has the identical tolerance list, kept in sync by
hand.

Migrations are drizzle-kit-generated (`pnpm run db:generate`, config in
`panel/drizzle.config.ts`) into the repo-root `migrations/` directory — one
source of truth shared by D1 and the local sqlite bootstrap. **A
long-running local dev server only applies migrations once, at process
startup** — after adding a migration, restart the dev server (not just
save the file) or new columns/tables won't exist in that process's
connection yet. Bit twice during this feature's development.

### Command layers (conceptually distinct — don't collapse them)

1. **Process-level** — Pulse manages the OS process directly
   (`pulse/internal/mcserver/`): install, start/stop/restart,
   stdout/stderr capture, PID-file reconciliation (see above). Command
   types: `start_instance`, `stop_instance`, `restart_instance` (stops
   first only if running, otherwise behaves like a plain start — safe to
   route every "Restart" click through this one type regardless of current
   state), plus the backup-family types below. Graceful stop
   (`gracefulStop()` in `rcon_stop.go`) issues RCON `save-all` + `stop`
   when the instance's `server.properties` has `enable-rcon=true` and a
   non-empty `rcon.password`, falling back to the bare SIGTERM/Kill signal
   (`terminate()` in `process_unix.go` / `process_windows.go`) otherwise.
2. **Backup lifecycle** — its own command family, layered on top of
   process-level: `backup_instance`, `restore_backup`, `delete_backup`,
   `push_backup`. See "Backups" above. Implemented in
   `pulse/internal/backup/` (archiving/restore mechanics, no
   process-lifecycle awareness of its own) + handler functions in
   `pulse/cmd/pulse/main.go` (`executeBackup`, `executeRestore`,
   `executePushBackup` — these own the stop/restart orchestration around
   the mechanics).
3. **Console commands (RCON)** — implemented: `console_command`, an
   arbitrary admin command sent verbatim to the instance's RCON port. See
   "Raw RCON console" below.
4. **In-game/gameplay commands** — no separate code path; anything typeable
   with `/` in-game rides the same RCON layer `console_command` uses —
   there's nothing gameplay-specific about it, it's just text sent
   verbatim.
5. **Provisioning** — `create_instance`, layered *before* process-level
   (there's no process to manage until this completes). See "Provisioning
   new servers" above. The one command type that doesn't complete
   synchronously within a single `execute()` call — everything else in
   this list does.

### Auth: single admin, no roles (v1 decision)

`panel/src/lib/server/auth.ts` — one `admin_settings` row, session cookies
(`adminSessions`, id = sha256 of the cookie token, not the raw token).
`hooks.server.ts` gates every route except `/login` and `/api/v1/*`, which
authenticate Pulse agents via their own bearer device credentials instead of
the session cookie. `/api/v1/commands` is one exception inside that
namespace — it's admin-facing (dashboard start/stop/restart buttons), so it
re-checks `isAuthenticated()` itself rather than relying on the hook.
`backups/[backupId]/download-file` is admin-session-authenticated normally
(it's outside `/api/v1/*`, so the hook covers it automatically) even though
the *bytes* originated from Pulse — only `api/v1/backups/[backupId]/upload`
(the Pulse-initiated ingest side) uses bearer device-credential auth.

Still single-admin — no per-user settings, no roles. The **Settings** page
(`/settings`) exists now (see below) but only holds global,
admin-instance-wide config (enrollment token generation, theme), not
anything resembling multi-user preferences.

### Theming

CSS custom properties, palette selected via `data-theme` on `<html>`,
defined in `panel/src/lib/theme/palettes.css`. Status colors
(success/warning/error/info) are fixed across all palettes on purpose —
don't let a palette override them; that's what keeps "red = danger"
meaningful under a red/orange palette like Nether. New palettes are pure
additive CSS blocks, no component changes needed. Palettes are named by
color character (Classic/End/Nether), not exact in-game names — a
trademark consideration for the OSS project; keep that convention for new
palettes. The theme switcher itself now lives on `/settings`, not the
dashboard header (see STYLE.md).

## Coding conventions established so far

- **Never store raw secrets.** Enrollment tokens, device credentials, and
  session cookies are hashed with `sha256Hex()` before hitting the DB
  (`panel/src/lib/server/tokens.ts`); only the hash is persisted, matching
  Beacon's pattern. Admin passwords use `scrypt` (`hashPassword`/
  `verifyPassword`, same file). Follow this for any new credential type.
- **Server-only code lives under `panel/src/lib/server/`**, one small
  module per concern (`db/`, `auth.ts`, `tokens.ts`, `http.ts`,
  `commands.ts`, `protocol.ts`, `backupDownloads.ts`) — imported only from
  `+server.ts` / `+page.server.ts` / `hooks.server.ts`, never from
  `.svelte` files. **Client-safe shared logic** (no server-only imports,
  usable from any `.svelte` file) goes directly under `panel/src/lib/`
  instead — see `heartbeat.ts` (pure timing-math functions shared by the
  dashboard and the backups page) for the pattern.
- **Shared DB mutations get extracted into a `lib/server/*.ts` helper**
  and called from both a form action and a REST route rather than
  duplicated — see `queueCommand()` in `commands.ts`, used by every
  command-queueing form action across the dashboard, instance detail page,
  and `api/v1/commands/+server.ts`. Follow this shape for future
  mutations. `queueCommand()` returns the new command's id (needed so
  callers can stamp `commandId` onto a `backups` row they're inserting in
  the same action — see `createBackup`/`restoreBackup` in
  `instances/[serverInstanceId]/+page.server.ts`).
- **Node-only server code (real filesystem access) uses dynamic
  `import('node:...')` inside the function body**, never a static
  top-level import — this is what lets the Cloudflare adapter bundle
  cleanly despite these modules existing in the shared codebase. Established
  by `db/index.ts`'s `node:sqlite` handling, followed by
  `backupDownloads.ts` and the upload/download-file routes for
  `node:fs`/`node:path`/`node:stream`. Guard the Cloudflare-unsupported
  path with `if (platform?.env?.DB) throw error(501, '...')` *before* any
  dynamic import, so the check itself has zero Node-specific surface.
- **Admin-facing mutations are SvelteKit form actions with `use:enhance`**,
  not client-side `fetch()` calls to a JSON API. The `/api/v1/*` routes are
  reserved for cross-boundary callers (Pulse agents, bearer-token
  authenticated) — `/api/v1/commands` is the one route that also accepts
  admin session auth, documented there as the deliberate exception, not the
  norm. **A destructive confirmation uses the themed `ConfirmModal`
  component**, not the browser's native `confirm()` — see STYLE.md.
- **Heartbeat-driven upserts use a composite string id**:
  `` `${pulseAgentId}:${instanceId}` `` as the `server_instances` primary
  key, via `onConflictDoUpdate`. The same shape is reused for
  `backupSchedules.id` if/when Phase 4 lands. Use the same
  "prefix:localId" shape for any future per-agent-scoped resource Panel
  needs to key on.
- **Panel-owned ids for anything Pulse creates on request**: Panel always
  generates the id (`bkp_<random>` via `newBackupId()` in `commands.ts`)
  *before* queuing a command that will create something, and Pulse uses it
  verbatim (as its on-disk filename stem, for backups) rather than
  inventing its own. Keeps Pulse "dumb" and avoids a round-trip just to
  learn an id. Follow this for any future Pulse-side creation.
- **A command's wire `payload` is a JSON-stringified column
  (`commands.payload`)**, `null` for payload-less types (start/stop).
  Parse with the matching `*CommandPayload` TS interface from
  `protocol.ts` (`BackupCommandPayload`, `RestoreCommandPayload`). This
  plumbing (DB column → `queueCommand()` param → heartbeat handler's
  `wireCommands` mapping) existed in the wire-type structs before it
  actually worked end-to-end — don't assume a wire-type field is actually
  plumbed through; check the DB schema and every hop.
- **A destructive/state-changing wire command reports enough in its
  `CommandResult` for Panel to fully resolve the corresponding DB
  row(s) without a second round-trip** — extend `CommandResult`'s optional
  fields (`size_bytes`, `checksum`) rather than adding a parallel reporting
  channel, and structure Pulse's result-building so those fields are
  populated based on which *step* succeeded, not just the command's
  overall pass/fail (see restore's safety-backup reporting above).
- **Go: one `internal/<concern>/` package per concern**
  (`protocol`, `credential`, `mcserver`, `inventory`, `backup`, `rcon`),
  platform-specific behavior split into `_unix.go` / `_windows.go` files
  with build tags rather than runtime `if runtime.GOOS` branching (see
  `mcserver/process_unix.go` vs `process_windows.go`). A command handler
  that orchestrates process-lifecycle + another package's mechanics (stop →
  do the thing → maybe restart) lives in `cmd/pulse/main.go`, not inside
  the mechanics package itself — `backup.Engine` has zero
  process-lifecycle awareness by design; `main.go`'s `executeBackup`/
  `executeRestore` own that orchestration. Keeps the mechanics package
  testable without a real process, and keeps "should this stop the
  server first" as one visible decision per command type.
- **Wire types are hand-mirrored, not codegenned**: Go structs in
  `pulse/internal/protocol/types.go` and TS interfaces in
  `panel/src/lib/server/protocol.ts` must be updated together, same
  snake_case JSON field names on both sides. Don't introduce a shared
  schema tool speculatively — it hasn't been needed yet — but do remember
  to update both files for any wire-shape change.
- **Go tests avoid needing a real Minecraft server**: `manager_test.go`
  drives `mcserver.Manager` against a trivial `sh -c "sleep N"` stand-in
  process; `pulse/internal/backup/engine_test.go` and
  `pulse/cmd/pulse/main_test.go` follow the same pattern for backup
  create/restore. Keep using cheap stand-ins for process-lifecycle tests
  rather than requiring a real server jar. For anything destructive
  (restore), pair the unit tests with a full sandboxed HTTP-level run
  (see "Local dev/testing workflow" above) before it ever touches a real
  host.
- **A "why isn't this happening" report that turns out to be timing, not a
  bug, is common with this architecture** — before changing code, check
  actual DB timestamps (`commands.sent_at`/`completed_at`,
  `backup_downloads.requested_at`/`ready_at`) against `now` and the
  agent's heartbeat interval before assuming something's broken. Several
  "it's stuck" reports during this feature turned out to be normal
  heartbeat-cycle latency, or (once) an actual multi-second-relayed
  network path — not a code defect.
- **CSS custom properties are prefixed `--axon-*`** (see `STYLE.md` for the
  full palette/spacing/component conventions, including this feature's
  additions: progress bars, pulsing badges, modals, icon buttons).

## Known gaps (real, not yet fixed — don't assume otherwise)

- **Restore has never been run against nimo's real backups** (only the
  isolated sandbox, thoroughly) — deliberately left to the user to trigger
  first, given it's destructive even with the safety-backup net.
- **The port allocator (`panel/src/lib/server/portAllocator.ts`) is blind
  to legacy hand-configured instances.** It only ever considers ports
  already recorded on `server_instances` for instances Panel itself
  created via `create_instance` — a pre-existing hand-configured instance
  never reports a port on the wire at all, so retrofitting that isn't
  possible without also teaching Pulse to parse `server.properties` for
  every instance, not just newly-provisioned ones (out of scope). The
  admin is responsible for picking a port range that doesn't collide with
  anything they've already configured outside Panel's knowledge.
- **Provisioning a new server leaves no automatic cleanup on a Pulse crash
  mid-provision.** The `commands` row self-heals via `failStaleCommands`,
  but a partially-downloaded file or half-created directory under
  `instancesRootDir` has no automatic sweep — matches this project's
  existing tolerance for similar one-off gaps (e.g. the PID-file bootstrap
  gap above).

## Project status / scope

Deliberately implemented, verified end-to-end locally and (mostly) against
a real production Bedrock server ("nimo", home LAN, Tailscale-reachable):

- **Pulse**: enrollment, heartbeat (now reporting its own `--interval`),
  start/stop/restart of configured Minecraft processes, RCON graceful stop,
  PID-file process reconciliation across Pulse's own restarts, full backup
  lifecycle (create/list/delete/download/restore) per "Backups" above,
  provisioning brand-new Java/Bedrock vanilla servers (Java-runtime
  auto-install on Linux, download+configure, dynamic instance registration)
  per "Provisioning new servers" above, a raw RCON console
  (`console_command`) per "Raw RCON console" above, a raw
  `server.properties` read/write pair (`read_properties`/
  `write_properties`) per "Server properties editor" above, file
  management (`list_files`/`upload_file`/`delete_file`,
  `pulse/internal/filemanager/`) per "File management" above, and
  self-update (`pulse/internal/updater/`, Ed25519-signed atomic binary
  swap with grace-period confirm/rollback) per "Self-update" above.
  `go build/vet/test` clean, including Windows/macOS cross-compiles.
- **Panel**: single-admin auth, enrollment token generation (now on
  `/settings`), dashboard listing agents/instances with online/offline
  status + accurate in-flight badges, start/stop/restart controls, a
  per-instance backups page (`/instances/[serverInstanceId]`) with
  create/list/delete/download/restore, backup scheduling + retention
  (interval-based automatic backups, keep-count/keep-days pruning, "Apply
  Retention Now"), a raw RCON console transcript and a raw
  `server.properties` text editor on that same page, a file browser
  (`/instances/[serverInstanceId]/files` — browse/upload/delete), a themed
  confirm modal, 3 working theme palettes, an agent detail page
  (`/agents/[pulseAgentId]`, the first agent-facing view beyond the
  dashboard) with port-range/instances-dir config and a create-server
  flow, and a "Publish Pulse release" form on `/settings` plus a
  "→ vNEW available" note on the dashboard/agent pages for self-update
  (see "Self-update" above). `svelte-check` clean; both `ADAPTER=node` and
  `ADAPTER=cloudflare` builds pass.
- Repo pushed to `codenexus/axon` (private), `main` branch.

Deliberately deferred — don't assume half-built unless you find code for it:

- Dedicated whitelist/op/ban forms (structured UI around specific RCON
  commands — the raw console can already run `/whitelist add`, `/op`,
  `/ban` etc. verbatim, this would be purpose-built forms around them),
  multi-user auth/RBAC, mDNS/Bonjour discovery, Tauri sidecar process
  spawning, and a "Systems"-style multi-node overview page (the current
  dashboard already lists multiple agents, but nothing like the old
  mockup's dedicated node-detail/diagnostic-command view exists). No
  CI/CD or automated build/sign/publish pipeline for Pulse releases —
  self-update automates the *swap*, not the build; the admin still
  builds, signs, and hosts each release binary by hand.
- Reusable server "definitions"/templates (the old UI mockup's "Create
  Definition" concept) — server creation is direct one-shot "create this
  specific server now," not a saved-template system. Deleting a
  Panel-created instance. Per-host RAM-based Java heap sizing (fixed
  `provision.DefaultJavaHeapMB` constant for every Java instance) or any
  UI control for it. Split port ranges per edition (one shared range per
  agent covers both). Paper/Forge/Fabric/etc. server software (vanilla
  only) and anything beyond the latest ~3 versions per edition.

See `PROJECT_LOG.md` for session-by-session history and next steps, and
`STYLE.md` for UI/UX conventions.

## License

AGPL-3.0.
