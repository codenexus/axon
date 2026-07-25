# PROJECT_LOG.md

Session-by-session history for Axon. Newest entry first. This is a
working log for picking up context between sessions, not user-facing
documentation — see `README.md` for that, `CLAUDE.md` for architecture,
`STYLE.md` for UI/UX conventions.

---

## 2026-07-24 — Backups: create/list/delete, download, restore (Phases 1–3)

### What we finished

Took backups from "not implemented" to fully working end-to-end against a
real production Bedrock server, in three phases:

- **Phase 1 (create/list/delete)**: new `pulse/internal/backup` package
  (tar+gzip archiving, stdlib only), `commands.payload` actually plumbed
  through (it existed in the wire types but was never wired end-to-end —
  real gap, fixed as part of this), new `backups` table, new
  `/instances/[serverInstanceId]` page.
- **Phase 2 (download)**: push-backup transfer — Pulse uploads on request
  to a new `api/v1/backups/[id]/upload` route (Node-only, 501 on
  Cloudflare), Panel holds it transiently and serves
  `backups/[id]/download-file` with delivery-confirmed cleanup + a
  heartbeat-piggybacked TTL sweep for abandoned holds. Fixed a real bug
  before it shipped: the shared Pulse HTTP client's 15s timeout would have
  silently aborted large uploads — gave `PushBackup` its own untimed
  client.
- **Phase 3 (restore)**: stop → automatic pre-restore safety backup →
  wipe+extract (exact rollback, not merge) → always leaves stopped.
  Verified with unit tests *and* a full sandboxed HTTP-level run (real
  Panel + real Pulse, running-instance case, corrupted-then-restored
  world, byte-verified on disk) before any of this touched a real host.

Along the way, fixed a real architectural bug unrelated to backups
per se but surfaced by redeploying Pulse repeatedly during this work:
**Pulse forgot about already-running Minecraft processes across its own
restarts** (in-memory-only state, no reconciliation). Built PID-file
reconciliation (`Manager.Reconcile()`, `pulse/internal/mcserver/pidfile.go`,
using `gopsutil/v3/process` for cross-platform liveness/fingerprint
checks) so a Pulse restart adopts a still-running process instead of
losing track of it. Confirmed on the real host: a live `bedrock_server`
survived several Pulse binary updates undisturbed.

Also shipped along the way, in response to live usage against nimo:
online/offline agent indicator, `restart_instance` command (dashboard
button relabels Start→Restart when running rather than a 3rd button),
real Pulse version reporting (`-X main.version=$(git describe
--always --dirty)`, previously always "dev" — made verifying what's
actually deployed possible), a `/settings` page (enrollment token
generation + theme switcher moved off the main dashboard), a themed
`ConfirmModal` component (replacing the browser's native `confirm()` for
the restore confirmation), and several rounds of dashboard/backups-page
polling and progress-indicator fixes (see "Key technical decisions").

### Key technical decisions (with why)

- **Backups stop the server first, always — no live-server RCON
  save-off/save-all path.** Bedrock Dedicated Server's RCON doesn't support
  that Java-only convention; branching per edition wasn't worth it when
  "stop, do the work, restart" is simpler and edition-agnostic. Cost: a
  brief stop/restart cycle for a backup taken while running.
- **Panel derives "is something happening to this instance" from its own
  command/backup tracking, not from Pulse's `running_state`.** The
  stop→archive→restart cycle happens entirely between two heartbeats, so
  `running_state` frequently jumps straight from running to running with
  nothing observable in between — confirmed live, not hypothetical. The
  "Backing up…"/"Restoring…"/"Starting…" badges replace (don't stack next
  to) the normal status badge for exactly this reason.
- **Dashboard polling had to become unconditional, twice.** First
  attempt only continued polling once something was *already known*
  in-flight from the page's own initial data — missed a backup queued
  from a different page/tab while the dashboard sat idle. Fixed by making
  the poll run on a steady interval regardless of state, on both the
  dashboard and the backups page. Lesson: "poll while X is true" is the
  wrong shape when X can become true from outside the page's own view of
  the world.
- **The redundant-progress-bar bug (and its fix, then its over-fix)**:
  during a restore, the auto-created safety-backup row and the target
  backup row both showed a heartbeat countdown — same information twice.
  First fix suppressed the bar for *any* `pre_restore`-triggered row,
  which then incorrectly hid it when independently deleting an
  already-resolved one afterward. Correct condition: suppress only while
  `trigger === 'pre_restore' && status === 'pending'` — the exact window
  where it's actually redundant with the target row's own bar, nothing
  broader.
- **Download auto-triggers once ready** (hidden `<a download>` + `.click()`
  the moment polling detects `status: 'ready'`) rather than requiring a
  second manual click after "Preparing…" resolves — this was a direct,
  reasonable user objection to the original two-click design.
- **Safety-backup outcome is reported independent of overall restore
  success.** `CommandResult.SizeBytes`/`Checksum` are populated as soon as
  the safety-backup step itself succeeds, even if the subsequent extract
  fails — so Panel always records a real, usable safety backup rather than
  losing track of it when only the second half of the operation fails.
- **`sudo` over SSH to a remote host is blocked by the auto-mode permission
  classifier**, every time, by design. Established the working pattern:
  build + `scp` to `/tmp` + verify checksum on both ends (all doable
  directly), then hand the actual privileged swap to the user as
  copy-paste commands. Don't try to route around this.
- **Idempotent local-dev migration replay needed a second tolerance
  case.** The self-apply-on-startup logic (`db/index.ts`,
  `scripts/seed-dev.mjs`) only tolerated re-running `CREATE TABLE`
  ("already exists"). The first `ALTER TABLE ... ADD` migration (adding
  `commands.payload`) would have crashed a long-running local dev server
  on its second restart — added "duplicate column name" to the tolerance
  list in both places before it could bite in practice.
- **A long-running local dev server only applies new migrations once, at
  process startup** — not on file save. Bit us more than once mid-session;
  the fix is always "restart the dev server," not a code change.

### Real-world deployment notes (nimo)

nimo is a home-LAN Bedrock server host, Tailscale-reachable
(`100.105.127.116`), SSH access via `~/.ssh/config` with a dedicated
automation key, passwordless `sudo`. Pulse there runs as a bare `nohup`'d
process under a dedicated `axon` user (not a systemd service — there's a
half-finished systemd install attempt sitting in `/tmp` on that host, unit
expects a binary at `/usr/local/bin/axon-pulse` but what's actually
deployed is `/usr/local/bin/pulse`; never touched it, worth finishing
properly at some point rather than continuing with bare `nohup`).

nimo's Tailscale connection runs through a DERP relay (`sfo`) instead of
direct — confirmed via `tailscale netcheck`, root cause is WSL2's inner
NAT (`PortMapping:` empty, no UPnP/NAT-PMP) blocking direct connection
negotiation for this WSL-hosted dev Panel's Tailscale node specifically.
Explained to the user (mirrored networking mode would fix it) but
deliberately **not changed** — global WSL networking change, user declined
given production won't run over Tailscale anyway. Backup downloads over
this path take ~60-90s for an ~85MB world (relay-limited, ~11.5 Mbps) —
not a code problem, don't try to optimize the transfer path for it.

### Next 2–3 logical steps

1. **Backup scheduling + retention (Phase 4)** — `backupSchedules` table
   (interval, keep-count, keep-days), a due-check evaluated inline during
   heartbeat handling (no new timer, matches the request-driven design),
   retention pruning reusing the existing `delete_backup` path, schedule
   config UI on the instance page. The mockup's "Apply Retention Now"
   manual button is a cheap addition once the routine exists — don't
   build it standalone first.
2. **Stale-command timeout** — no automatic recovery today if Pulse dies
   between finishing a command and reporting it (see CLAUDE.md's "Known
   gaps"); happened for real once this session, fixed by hand. A command
   `sent` for longer than a few heartbeat intervals with no result should
   probably auto-fail with a clear message instead of hanging forever.
3. **Raw RCON console** — expose `rcon.Client.Execute` through Pulse's
   command-poll loop and a Panel UI console box; the heartbeat-latency UX
   question flagged back in the 2026-07-21 entry is still open, and now
   there's a real "Backing up…"-style badge precedent to reuse for
   "command sent, waiting for result" instead of a literal live console.
4. Test-backup cleanup on nimo is optional/deferred — user is fine
   leaving ~7 accumulated test backups (~600MB) and doesn't need them
   pruned proactively.

---

## 2026-07-21 — RCON graceful stop

### What we finished

- Added `pulse/internal/rcon/` — a small hand-rolled Source RCON client
  (Dial/Authenticate/Execute/Close), no third-party dependency, tested
  against a fake in-process RCON server (same cheap-stand-in philosophy as
  `mcserver`'s process tests).
- Replaced the SIGTERM/Kill placeholder in `Manager.Stop()`
  (`pulse/internal/mcserver/manager.go`) with `gracefulStop()`
  (`rcon_stop.go`): reads `rcon.port`/`rcon.password` from the instance's
  `server.properties` (`properties.go`) and, when RCON is enabled and
  configured, sends `save-all` then `stop`; falls back to the existing
  `terminate()` signal whenever RCON isn't usable (disabled, unconfigured,
  unreachable, or — as in every existing test — a non-Minecraft stand-in
  process with no `server.properties` at all).
- Restructured `Stop()` to release the per-instance mutex before the
  network round-trip, so a slow/hanging RCON exchange for one instance
  can't stall `Statuses()` reads during the heartbeat loop.
- Added tests for both paths: RCON commands actually sent + fallback
  signal *not* sent when RCON is configured; fallback signal *is* sent
  when it isn't. Existing `TestStartStopLifecycle` continues to pass
  unchanged, since its `sh -c sleep` stand-in has no `server.properties`
  and naturally exercises the fallback path.
- `go build/vet/test` clean.

### Key technical decisions (with why)

- **Read RCON credentials from `server.properties` rather than adding new
  fields to `pulse.instances.json`** — every Minecraft server
  implementation (Vanilla/Paper/Forge/Fabric) already reads RCON config
  from this file, so there's no new config surface for users to keep in
  sync, and it matches the spec's "works uniformly across server
  softwares" goal for the RCON layer.
- **Empty `rcon.password` treated as RCON-unusable** — real Minecraft
  servers refuse to actually start RCON with an empty password even if
  `enable-rcon=true`, so treating that case as "fall back to signal"
  avoids a confident-looking connection attempt against a port that isn't
  actually listening.
- **No escalation to a hard kill if graceful stop hangs** — out of scope
  for this pass (kept to "graceful stop only"); risk profile is unchanged
  from before (a SIGTERM-ignoring process could already hang forever
  pre-this-change).
- **Scope explicitly excludes the raw admin console and whitelist/op/ban
  forms** — reviewed an old UI mockup (built against the earlier gRPC
  prototype, not the current pull-based HTTP design) for feature
  inspiration but chose to land graceful stop alone first; raw console UX
  under the pull-based heartbeat model (command results only surface on
  the next heartbeat) is an open design question for that later slice.

### Next 2–3 logical steps

1. **Get a real Pulse agent running** on the user's home-network Ubuntu
   26.04 box (already running a Minecraft server) — in progress.
2. **Raw RCON console** — expose `rcon.Client.Execute` through Pulse's
   command-poll loop and a Panel UI console box; decide there whether
   heartbeat-interval latency is acceptable or a faster poll cadence is
   worth the extra complexity while a console view is open.
3. **Backups** — on-demand + cron-scheduled (`robfig/cron`) stop →
   snapshot → restart flow in Pulse, with Panel proxying the download.

---

## 2026-07-21 — Initial scaffold + vertical slice

### What we finished

- Took the Axon architecture spec (a from-scratch handoff brief replacing
  an earlier gRPC prototype) and scaffolded the full monorepo: `pulse/`
  (Go agent), `panel/` (SvelteKit), `migrations/`, `scripts/`, root
  tooling (`Makefile`, CI).
- Built and manually verified a thin end-to-end vertical slice: Pulse
  enrolls with Panel via a one-time token, heartbeats host + instance
  metrics on an interval, and executes `start_instance`/`stop_instance`
  commands against a real subprocess. Panel's dashboard lists
  agents/instances and can queue those commands. Walked the full loop
  locally by hand: seed admin → log in → generate token → run pulse →
  watch it enroll/heartbeat → click Start → confirm state flips to
  running → click Stop → confirm it flips back.
- Implemented the 3 v1 theme palettes (Classic/End/Nether) as CSS custom
  properties with a runtime switcher; contrast-checked text/background
  pairs and the fixed status colors against WCAG AA by hand (all passed
  with large margins).
- Stood up CI and fixed two real bugs it caught: a pnpm version mismatch
  (`pnpm/action-setup` pinned to v9 while the committed lockfile used
  11.5.1 — v9 rejects `pnpm-workspace.yaml` files without a `packages:`
  list, which ours doesn't have since it's not a real multi-package
  workspace) and a Node version too old for pnpm 11's own requirements
  (bumped CI from Node 20 to 24).
- Wrote root/pulse/panel READMEs (prerequisites, local quickstart,
  Cloudflare-target setup) and the initial `CLAUDE.md`.
- Created the GitHub repo (`codenexus/axon`, private) and pushed
  everything; `gh auth` is local to this machine and won't carry over to
  a new one.

### Key technical decisions (with why)

- **Heartbeat/poll HTTP instead of gRPC** — Cloudflare Workers (the hosted
  Panel target) doesn't support gRPC, and nothing in Axon's requirements
  needs bidirectional streaming. Deliberately mirrors Beacon RMM's agent
  pattern (`/home/jeremys/projects/beacon`).
- **Single-admin auth for v1**, no roles/multi-user — smaller surface for
  the home-lab/small-VPS target audience; can add RBAC later without a
  data-model break (the spec itself left this as an open question).
- **D1 + Drizzle** for the data layer, with Node's built-in `node:sqlite`
  (not `better-sqlite3`) driving the identical schema locally via
  `drizzle-orm/sqlite-proxy` — avoids a native-module build step that
  needs network access to nodejs.org, which isn't guaranteed on every
  Pulse/Panel host.
- **AGPL-3.0 license** — matches Beacon and the common choice for
  self-hosted panels (discourages un-contributed SaaS resale).
- **Command layers kept conceptually separate**: process-level (Pulse/OS,
  implemented), RCON console (not implemented), in-game commands (rides
  RCON once it exists, no separate path). Graceful stop is currently a
  placeholder SIGTERM/Kill until RCON lands.
- Repo lives at `codenexus/axon`, private, `main` branch.

### Next 2–3 logical steps

1. **RCON integration** — connect Pulse to the running server's RCON
   port; wire "save-all" + "stop" for graceful shutdown (replacing the
   SIGTERM placeholder in `pulse/internal/mcserver/process_unix.go` /
   `process_windows.go`); expose a raw-command console plus the dedicated
   whitelist/op/ban forms described in the spec.
2. **Backups** — on-demand + cron-scheduled (`robfig/cron`) stop → snapshot
   → restart flow in Pulse, with Panel proxying the download (Pulse →
   Panel → browser) rather than exposing Pulse directly.
3. **Self-update** — port Beacon's `agent/internal/updater/*` and
   `agent/tools/{keygen,sign}` wholesale into Pulse (Ed25519 sign/verify,
   atomic binary swap with rollback on failed startup), plus a
   version-check on the heartbeat response.

Also worth doing when there's access to a machine with the Rust
toolchain: make the Tauri desktop shell actually functional (spawn
`build/index.js` as a sidecar instead of just scaffolding `src-tauri/`) —
this sandbox has no `cargo`/`rustc` so it's never been compiled or run.
