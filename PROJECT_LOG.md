# PROJECT_LOG.md

Session-by-session history for Axon. Newest entry first. This is a
working log for picking up context between sessions, not user-facing
documentation — see `README.md` for that, `CLAUDE.md` for architecture,
`STYLE.md` for UI/UX conventions.

---

## 2026-07-28 (cont.) — Discovered: Bedrock Dedicated Server has no RCON at all

Tried to enable RCON on nimo's real "Survival" Bedrock instance to test
the console feature end-to-end (prompted by the earlier self-update
incident leaving a stale "Failed" transcript entry visible in the UI).
Went through several real dead ends before landing on the actual answer:
a first attempt collided `rcon.port` with `server-portv6` (both 19133);
fixed, but the connection was then refused outright — nothing was
listening. Checked the actual `bedrock_server` startup log directly
(`pulse.log` inside the instance's own working directory, distinct from
Pulse's own log) — it initializes IPv4/IPv6 game ports and completes
startup normally, but never mentions RCON at all, success or failure,
despite `enable-rcon=true` being correctly present in `server.properties`
on disk. Chased a "maybe Bedrock uses different key names" theory
(`rcon-port` vs `rcon.port`, `on`/`off` vs `true`/`false`) based on
inconsistent hosting-provider pages, before confirming against the
actual `server.properties` key reference and Mojang's own Bedrock
feedback docs: **RCON is a Java Edition-only concept. Bedrock Dedicated
Server has never had any RCON support, full stop** — not a version gap,
not a key-naming mismatch.

This corrects a wrong assumption baked into this project since the
backups feature was first built: CLAUDE.md previously said Bedrock RCON
existed but didn't support Java's `save-off`/`save-all` convention
specifically — actually there's no Bedrock RCON to support any
convention. The conclusion that reasoning was used for (backups always
stop the server first) was never wrong, just the stated justification.

**Fixed**: CLAUDE.md corrected in two places (the backups reasoning, and
a new note on "Raw RCON console"). The instance page now gates the
Console and Player Management (whitelist/op/ban) cards behind
`gamePlatform === 'java'`, showing a short explanatory note for Bedrock
instances instead of controls that could only ever fail — the properties
editor is unaffected (plain file read/write, no RCON involved) and is
now the explicitly-pointed-to place for a Bedrock admin to configure
anything. nimo's Survival instance had its test RCON config reverted
back to just the two port lines it started with.

### Next 2–3 logical steps

1. Bedrock's actual admin surface is `server.properties` plus whatever
   the game itself exposes in-game — worth a future pass considering
   whether Bedrock needs its own lightweight admin story (there's no
   RCON to build one on), or whether this is simply out of scope given
   Bedrock's own ecosystem constraints.
2. Still open from prior sessions: the Cloudflare Durable Object spike,
   Fabric/Forge provisioning verification against a real Java
   environment, and a real restore against nimo's actual backups.

---

## 2026-07-28 — nimo Panel deployment, bug-fix sprint, adaptive interval + real-time push

### What we finished

A long session covering three phases: getting Panel running persistently
on nimo for real (not just tested), a string of real bugs found by
actually using it, and a two-part latency fix (adaptive heartbeat
interval + a real-time push layer) plus configurable Java heap/
server.properties defaults.

**nimo Panel deployment**: Panel now runs as a systemd **user** service
on nimo (`~/.config/systemd/user/axon-panel.service`, `linger` enabled
so it survives logout/reboot), built with `ADAPTER=node`, reachable over
the LAN at `10.0.0.73:3000`. Installed Node 22.22/pnpm the hard way —
`corepack`'s bundled pnpm hit `ERR_VM_DYNAMIC_IMPORT_CALLBACK_MISSING`
on this Node build, worked around by installing `pnpm` directly via
`npm install -g --prefix ~/.local`. nimo's Pulse binary turned out to
predate both `create_instance` and self-update entirely (it couldn't
self-update to fix itself), so it needed one manual binary swap using
the already-built `v0.1.1` GitHub release asset; every release after
that self-updates normally. Also discovered mid-session that `sudo`
wildcard command args (`apt-get install -y openjdk-*`) are rejected
outright on this system's sudo build ("wildcards are not allowed in
command arguments") — fixed with an explicit per-package-name allowlist
instead (see CLAUDE.md's "Deploying Pulse to a real host").

**Real bugs found by actually using Panel on nimo** (all fixed, verified
live against nimo, not just locally):
- Session cookie `Secure` flag was `!dev` (true for every production
  build), silently breaking login on any plain-HTTP LAN deploy — browsers
  accept a `Secure` cookie over `http://` but never send it back, causing
  a silent login→redirect→login loop. Never caught before because every
  prior test ran against `localhost`, which Chrome/Firefox treat as a
  secure context regardless of scheme. Now derived from the actual
  request's `url.protocol`.
- Tauri's `set_panel_url` navigated the existing window from the small
  config-page size (420×420) to the real Panel URL via `.navigate()`,
  which doesn't resize the window — looked like a UI glitch (background
  color filling the excess space) and a dead Settings click (pushed out
  of the visible area). Fixed by resizing to the same dimensions
  `show_panel()` already uses.
- The provisioning save form returned `{ok: true}` but the template only
  ever rendered `form?.error` — a successful save looked like it silently
  did nothing.
- `.settings-form`/`.release-form` inputs had no `box-sizing: border-box`
  — the instances-directory field visibly overlapped the Save button next
  to it (browser default content-box sizing adds padding/border on top
  of a `width: 100%`, not within it).
- `/settings`'s `load()` hit all 5 version-catalog resolvers with **no
  fetch timeout anywhere** — on a fresh empty cache, minecraft.net's
  flaky Bedrock scrape took *minutes* to fail, looking identical to a
  dead Settings button. Fixed with an 8s `AbortSignal.timeout` on every
  fetch, **and** a new negative-cache table (`versionCatalogFetchAttempts`)
  so a reliably-failing endpoint isn't retried on every single page load
  (confirmed: first load after the fix still cost 8s, second load 43ms).
- The "Backups →" link/page title were a naming holdover from before the
  instance page grew a console, properties editor, player management,
  and a danger zone — renamed to "Manage".
- Instance rows only ever showed name/badge/edition/version, wasting most
  of the row's width even though `playerCount`/`uptimeSeconds` already
  flow through the heartbeat — same "data exists, never displayed" shape
  as the earlier host-stats gap. Restructured into a row layout (info
  left, stats+actions right) on both the dashboard and agent-detail
  pages, and added a small inline server-icon next to each instance name
  so agent cards and the actual Minecraft servers inside them don't look
  visually interchangeable.

**Configurable Java heap size + server.properties defaults** and
**Adaptive heartbeat interval + real-time push layer**: see CLAUDE.md's
new "Configurable Java heap size + server.properties defaults at
creation" and "Adaptive heartbeat interval + real-time push layer"
sections for the full detail — too much to duplicate here. Short version:
Java heap was a hardcoded 2048MB constant with zero admin control, now
configurable per server with sensible pre-filled defaults; a console
command's round trip could take up to ~2x Pulse's heartbeat interval
regardless of network distance (a real point of user confusion this
session — same-machine doesn't mean faster, the bottleneck is the poll
interval itself, not transit time), fixed with Panel suggesting a fast
(3s) interval while something's in-flight or being watched, plus an
adapter-agnostic WebSocket push layer (Cloudflare Durable Objects / Node
`ws`) so the instance console updates instantly instead of waiting for
the next poll tick.

**Tauri decision**: the thin-client design (webview pointed at a
remote Panel) was judged not to work as wanted after live Windows
testing and is being pulled out of active scope — rescoped as a v2
initiative (Panel as a proper local Windows service/webserver the
desktop app bundles directly), not yet designed. The existing
`panel/src-tauri/` code is unchanged and still compiles, just no longer
the intended direction.

Verified: `go build/vet/test`, `svelte-check`, both `ADAPTER` builds
clean throughout. Two real bugs caught specifically by *live* testing
rather than type-checking (see CLAUDE.md's new architecture section for
detail): a cross-module-boundary state bug (`server.mjs` and the
SvelteKit-bundled route code getting disconnected `Map` instances) and
an actual auth bypass (`fetch()`'s default redirect-following silently
defeated the loopback admin-session check for the WebSocket upgrade
path) — a real unauthenticated WebSocket connected successfully before
`redirect: 'manual'` was added.

### Key technical decisions (with why)

- **Same-machine Pulse↔Panel is not faster** — the architecture always
  routes through the fixed heartbeat poll interval regardless of network
  distance; co-locating them doesn't skip that. The actual lever for
  perceived latency is the interval itself (now adaptive) or a genuine
  push channel, not deployment topology.
- **Java heap/property defaults are per-create-server-submission, not
  part of `server_definitions`** — a definition pins *what* to install,
  these are *how it's configured*, deliberately kept as a smaller,
  separately-reasoned-about change rather than growing the definitions
  schema too.
- **Part A (adaptive interval) and Part B (real-time push) were built
  as one continuous session rather than pausing after A** — despite B
  being materially riskier (first-ever Durable Object usage in this
  repo, a new Node production entrypoint, a new runtime dependency) —
  because the value of B (instant browser updates) meaningfully extends
  A's win (fast Pulse polling) rather than duplicating it.
- **Cloudflare's half of the real-time layer is unverified in this
  environment** — no live account/deploy access — flagged explicitly in
  the code itself, matching this project's existing tolerance for
  similar gaps (Tauri before it was verified, the Fabric/Forge
  installer). A standalone Durable Object spike against a real account
  should be the first thing that touches Cloudflare here.

### Real production incident: self-update infinite loop on nimo

nimo's Panel was redeployed to `server.mjs`, `v0.1.3` was tagged, and —
for the first time since this project began — self-update actually
completed successfully on a real host (a real permissions fix earlier
this same session, see below, was what finally unblocked it). It then
immediately started re-applying the same "update" every ~8-13 seconds,
indefinitely. Root cause: a CI `git describe` quirk (a well-known
`actions/checkout` gotcha — the tag that triggers a tag-push workflow
isn't reliably visible to `git describe` right after checkout, even with
`fetch-depth: 0`) meant the built binary's own reported version
(`v0.1.2-1-g3e93406`) never exactly matched the literal tag string
(`v0.1.3`) published to Panel — so "differs from what was last reported"
stayed permanently true. Caught by accident while investigating why the
adaptive interval didn't look right in a live timing test; the timing
anomaly turned out to be the restart loop itself, not an interval bug.
Stopped immediately by hand-correcting the published release's version
string in nimo's DB to match what the binary actually reports; properly
fixed at the root (`git fetch --tags --force` added to
`release.yml`) and given defense in depth (`updater.go` now persists the
last version it itself confirmed and refuses to re-apply it, regardless
of why Panel offers it again — a guard that survives the process restart
a successful swap always causes, unlike an in-memory check). See
CLAUDE.md's "Self-update" section for full detail.

Also fixed in the same investigation: `/usr/local/bin/pulse` (and its
containing directory) were `root:root`, but Pulse runs as the
unprivileged `axon` service account per this project's own documented
deploy flow — self-update's binary-swap `os.Rename` had been silently
failing on every heartbeat this whole time, on every host, since the
feature was written. Fixed on nimo with a sticky bit + group-write on
`/usr/local/bin` plus handing `axon` ownership of just its own `pulse`
binary (not the whole directory) — narrow enough that `axon` still can't
touch any other root-owned binary that might live there.

### Next 2–3 logical steps

1. **Cloudflare Durable Object spike** — deploy just `InstanceHub` (a
   trivial echo DO) plus the `worker-entry.ts` wrapper to a real
   Cloudflare account, confirming the wrapper-export approach actually
   works against the pinned `adapter-cloudflare`/wrangler versions,
   before trusting the full real-time feature there.
2. Still open from prior sessions: verify Fabric/Forge provisioning
   against a real Java environment, and run a real restore against
   nimo's actual backups.

---

## 2026-07-27 — Tauri desktop shell: first real compile

### What we finished

- Compiled the Tauri desktop shell (`panel/src-tauri/`) for the first
  time — previous sessions had only scaffolded it, with no Rust
  toolchain available to verify. Done on the user's WSL2 Ubuntu 24.04
  machine: installed Rust via `rustup`, then Tauri v2's Ubuntu 24.04
  system libraries (`libwebkit2gtk-4.1-dev`,
  `libjavascriptcoregtk-4.1-dev`, `libgtk-3-dev`,
  `libayatana-appindicator3-dev`, `librsvg2-dev`, `libssl-dev`,
  `libxdo-dev`, `build-essential`).
- **One real bug found and fixed**: `build.rs`'s plain
  `tauri_build::build()` doesn't auto-generate ACL permissions for
  app-level `#[tauri::command]` functions (`get_panel_url`/
  `set_panel_url`) — only *plugin* commands get that for free. `cargo
  check` failed with "Permission allow-get-panel-url not found" even
  though `capabilities/default.json` already listed it (a reasonable
  guess at the time it was written, just not how Tauri v2's ACL
  resolution actually works). Fixed by switching `build.rs` to
  `tauri_build::try_build(...)` with an explicit
  `AppManifest::new().commands(&["get_panel_url", "set_panel_url"])`,
  which generates `permissions/autogenerated/*.toml` (now committed,
  same convention as Tauri's other ACL files).
- No other API-shape mismatches — the rest of the scaffold (menu,
  window management, config persistence via `app_config_dir()`)
  compiled exactly as originally written.
- Went beyond a bare compile: ran the built binary directly against
  WSLg's Wayland compositor (`$DISPLAY`/`$WAYLAND_DISPLAY` both present
  under WSL2). No panic; `/mnt/wslg/weston.log` shows the compositor
  registering a real `axon-panel-desktop` app window
  (`associateWindowId`, a cursor role added) — confirms the first-run
  config-page path (no `desktop-config.json` yet) actually renders, not
  just that it links.
- `cargo check` and `cargo build` both clean.

### Key technical decisions (with why)

- **Tauri v2's command-permission autogeneration is opt-in per
  crate-type.** Plugins get `allow-$command`/`deny-$command` permissions
  generated automatically from their command list; the main app crate
  needs the same thing requested explicitly via `AppManifest::commands`
  in `build.rs`. Worth remembering for any *future* `#[tauri::command]`
  added to this shell — it needs a matching entry in that `commands`
  list, or the capability lookup fails the same way.

### Next 2–3 logical steps

1. **Test the "navigate to a saved Panel URL" path** — only the local
   config page (no saved URL yet) was exercised live. Write a
   `desktop-config.json` with a real Panel URL (e.g. a local `pnpm run
   dev` instance) and confirm the webview actually loads and is usable.
2. **Run a full `pnpm run tauri:build`** (bundled AppImage/installer),
   not just `cargo build` — the bundling step (icons, `.desktop` file,
   etc.) is still unverified.
3. Remaining known gaps from the prior session: verify Fabric/Forge
   provisioning against a real Java environment, and run a real restore
   against nimo's actual backups.

---

## 2026-07-27 — Paper/Fabric/Forge server software provisioning

### What we finished

- Extended provisioning (previously vanilla-only) to Java's three most
  common server softwares. Researched live against the real
  infrastructure before writing any code: `fill.papermc.io` (Paper),
  `meta.fabricmc.net` (Fabric loader + installer), and
  `files.minecraftforge.net/.../promotions_slim.json` + Forge's maven
  layout (Forge) — including a real constructed Forge installer URL that
  returned a genuine ~6MB jar during planning.
- **Paper** needed zero Pulse changes — its API returns a direct
  server-jar URL, structurally identical to vanilla's Mojang manifest
  resolution.
- **Fabric and Forge** both needed a genuinely new mechanic: download an
  *installer* program (not a runnable server), then run it before
  anything is launchable. `provision.RunInstaller` (split into a pure,
  unit-tested `installerArgs()` and a thin `exec.Command` wrapper) runs
  under a new `"installing_loader"` progress phase, between the existing
  `"downloading"` and `"configuring"` phases of the async
  `create_instance` job. `Configure()` now branches its launch command on
  `SoftwareType`, not just `GamePlatform`: Fabric launches the fixed
  `fabric-server-launch.jar` the installer produces; Forge invokes its own
  generated `run.sh`/`run.bat` directly rather than reconstructing its
  internal (version-era-variable) args-file invocation.
- `versionCatalogEntries`/`serverDefinitions` gained `softwareType`/
  `loaderVersion` columns; the cache key became
  `` `${gamePlatform}:${softwareType}:${version}` ``. Fixed a real bug
  caught during development: the cache's `replaceEntries` originally
  filtered only on `gamePlatform`, so refreshing vanilla's catalog would
  have silently wiped Paper/Fabric/Forge's cached entries too.
- Panel UI: a Software dropdown (Vanilla/Paper/Fabric/Forge) on both the
  create-server page and the Settings server-definitions form, each
  driving the Version dropdown via a cascading-select pattern (see
  STYLE.md) — the backend already supported non-vanilla software from the
  wire-payload work, this closed the last "no way to actually pick it"
  gap.
- Verified: `go build/vet/test` (incl. cross-compiles), `svelte-check`,
  both `ADAPTER` builds clean; new Go tests for `Configure()`'s per-
  software-type command construction and `installerArgs()` (pure logic,
  no Java needed); a sandboxed dry run confirmed all three resolvers
  return real live data and that a `create_instance` payload queued
  through the actual UI (Fabric) and a saved definition (Forge) both
  carry the correct `software_type`/`loader_version`/`download_url`.

### Key technical decisions (with why)

- **The installer *execution* step itself is unverified against a real
  Java environment** — this whole feature was built in a sandbox with no
  Java runtime at all. Every resolver was live-verified against the real
  internet; the exact CLI flags `installerArgs` passes to Fabric's and
  Forge's installer jars are reasoned from each project's current public
  documentation, not from a real invocation. Same treatment as
  self-update's Windows swap path and Tauri's Rust code — flagged
  explicitly in CLAUDE.md, not silently assumed correct.
- **Forge's launch command invokes its own generated `run.sh`/`run.bat`
  rather than reconstructing the internal `@user_jvm_args.txt`/
  `@libraries/.../*_args.txt` invocation** — that internal path has
  genuinely varied across Forge/MC version eras; the run script already
  encodes whatever that specific installer run produced, so Pulse never
  needs to know Forge's internal file-naming convention at all.
- **A cache table's read/write/delete queries must filter on every
  dimension of its own key, not just some of them** — the
  `replaceEntries` bug above, now called out generally in CLAUDE.md's
  "Coding conventions" so it's not re-learned the hard way on the next
  multi-dimensional cache table.

### Next 2–3 logical steps

1. **Verify Fabric and Forge server creation against a real Java
   environment** — the one explicitly-flagged, un-glossed-over gap left
   by this feature. Needs a real host with Java installed; confirm the
   installer actually runs to completion and the resulting server
   actually starts under the constructed launch command.
2. **Compile the Tauri desktop shell** on a machine with the Rust
   toolchain — still unverified by compiling since it was reworked as a
   thin client (see the entry below).
3. **Run a real restore against nimo's actual backups** — still
   deliberately left to the user to trigger first (destructive even with
   the safety-backup net), noted as a known gap since the backups
   feature landed.

---

## 2026-07-27 — Reusable server definitions (templates)

### What we finished

- `serverDefinitions` — a saved preset (name, edition, version, download
  URL, Java major version) an admin creates once on `/settings` and
  reuses from any agent's create-server page instead of re-picking a
  version every time. Global, not per-agent, since a definition describes
  *what* to install, not *where*.
- **Pinned at creation time, not a live reference**: saving a definition
  resolves a concrete version/download URL/Java major version right then
  (the same `versionCatalogEntries` resolution the create-server form
  already used) and stores it permanently — using the definition later
  never re-resolves the catalog. Extracted the shared resolution logic
  (`resolveVersionSelection`) once rather than duplicating it between the
  create-server action and the new create-definition action.
- Zero wire/Pulse changes — picking a definition just changes which
  Panel-side code path resolves the `create_instance` payload's
  version/URL fields; the command Pulse receives is identical either way.
- `go build/vet/test`, `svelte-check`, both `ADAPTER` builds clean.

### Key technical decisions (with why)

- **No `ConfirmModal` on delete** — matches the existing convention that
  only genuinely high-blast-radius actions (restore, recursive file
  delete, delete-instance) get that treatment; deleting a template has
  zero effect on any running server.

---

## 2026-07-27 — Node detail: host stats and allowlisted diagnostics

### What we finished

- Host stats (CPU/RAM/disk/uptime) on the agent-detail page. CPU/RAM were
  already flowing through the heartbeat but never displayed here (a pure
  display gap); disk usage was already on the wire but silently dropped
  by the heartbeat route; host uptime didn't exist at all
  (`gopsutil/v3/host.Uptime()`, distinct from a Minecraft instance's own
  uptime).
- `run_diagnostic` — a new host-level command, unrelated to
  `console_command`'s RCON round-trip into the Minecraft process itself.
  `pulse/internal/diagnostics` holds a fixed, hand-maintained allowlist
  per OS (four friendly names: uptime/disk_usage/memory/processes) —
  deliberately not arbitrary command execution; extra admin-supplied
  arguments are appended to the fixed base command via a real argv slice,
  never a shell string.
- Real bug caught by this feature: `export const` from a
  `+page.server.ts` file that isn't a recognized page export
  (`load`/`actions`) passes `svelte-check` but fails the actual
  `pnpm run build` — reconfirmed why this project always checks both
  `ADAPTER` builds, not just `svelte-check`.
- `go build/vet/test`, `svelte-check`, both `ADAPTER` builds clean.

### Key technical decisions (with why)

- **Same fixed 4-name allowlist offered regardless of the target agent's
  OS** — Panel trusts Pulse to map each name correctly for its own
  platform, same "Panel stays dumb" split already established for
  Java-runtime package names.

---

## 2026-07-27 — Whitelist/op/ban forms, CI/CD for Pulse releases, Tauri rework

### What we finished

- **Whitelist/op/ban moderation forms** (instance page's "Player
  Management" card): purpose-built forms that construct the equivalent
  RCON command string and queue it through the exact same
  `console_command` pipe the raw console already uses — no new wire type,
  no new Go code, since the game doesn't care whether the text came from
  a button or typing. Same transcript as the raw console.
- **CI/CD for Pulse releases**
  (`.github/workflows/release.yml`): a tag-triggered workflow
  cross-compiles, signs (reusing self-update's Ed25519 signing key), and
  publishes a GitHub Release with all three binaries plus a manifest CSV.
  Automates build+sign only, not hosting/publishing to Panel — the admin
  still pastes the download URL/signature into `/settings` by hand, a
  deliberate scope boundary. **Made the repo public** (`codenexus/axon`,
  still AGPL-3.0) so Pulse's unauthenticated self-update downloader can
  fetch release assets without carrying a GitHub token as a new
  credential type.
- **Tauri desktop shell reworked as a thin client**: the original
  scaffolded plan (spawn `node build/index.js` as a local sidecar)
  assumed Panel only exists while the desktop app is open — doesn't fit
  wanting one Panel reachable from multiple networks over time. Reworked
  to remember an already-running Panel's URL and point a native webview
  at it, same as opening it in a browser — no local backend, no local DB.
  **Not yet verified by compiling** — no Rust toolchain in this
  environment.

### Key technical decisions (with why)

- **No new validation beyond a whitespace check on usernames** for the
  moderation forms — a malformed multi-arg command would otherwise go out
  silently, but no other game-side acceptance check is possible or
  attempted, matching the raw console's own passthrough philosophy.
- **CI/CD stops at build+sign, deliberately** — auto-publishing straight
  to Panel would need a new authenticated Panel API endpoint, a shared
  secret, and Panel reachable from GitHub Actions; real new architecture
  for a step that already takes 30 seconds by hand.

---

## 2026-07-27 — Deleting a Panel-created instance

### What we finished

- `Manager.RemoveInstance(id, configPath, backupsDir string) error`
  (`pulse/internal/mcserver/manager.go`) — the literal inverse of
  `AddInstance`: same critical section, same rebuild-the-full-list-then-
  `SaveConfig` shape, same rollback-in-memory-if-the-write-fails
  guarantee.
- `delete_instance` command + `executeDeleteInstance`
  (`pulse/cmd/pulse/main.go`): stop if running → `RemoveInstance` →
  `os.RemoveAll(working_dir)`. Dispatched in `runLoop` before `execute()`'s
  switch, the same way `create_instance` already is, since both need
  `configPath`/`backupsDir` that `execute()`'s signature doesn't carry.
- Panel cascade-deletes `serverInstances`/`backups`/`backupSchedules` on
  success, in `resolveCommandOutcome`. A "Deleting…" dashboard badge
  reuses the existing `pendingActions` in-flight-badge mechanism
  (`start`/`stop`/`restart` already worked this way — no new column, no
  new component). A "Danger Zone" card on the instance page reuses the
  `ConfirmModal` + hidden-form pattern already established for restore and
  file/folder delete.
- Verified in a sandbox: stopped a running stand-in instance, deleted it,
  confirmed the process died, `working_dir` and its `pulse.instances.json`
  entry vanished, and all three Panel DB rows cascade-deleted; also
  verified the failure path (deleting an already-gone instance resolves
  cleanly to `failed`, no hang).
- `go build/vet/test` (incl. cross-compiles) and `svelte-check` (both
  adapters) clean.

### Key technical decisions (with why)

- **Backup archives are never deleted by this feature** — they live in
  the shared `backups_dir`, not under the instance's `working_dir`, so
  deleting an instance only deletes Panel's *metadata* about its backups,
  never the bytes on Pulse's disk. Stated explicitly in the confirmation
  dialog, not left implicit — an easy consequence to miss.
- **Not everything gets cascade-deleted.** `backupDownloads`/
  `fileUploads`/`commands` history for the deleted instance are left as
  harmless orphans rather than explicitly cleaned up — they already
  self-expire via existing TTL sweeps and nothing ever queries them
  unscoped once the instance's own page is gone. Matches this project's
  existing tolerance for similar one-off gaps (e.g. the PID-file bootstrap
  gap).
- **No confirm-by-typing-the-name.** `ConfirmModal` is this codebase's
  only destructive-confirmation precedent; didn't invent a stronger one
  for this despite the higher blast radius.

---

## 2026-07-26 — Self-update: Ed25519-signed atomic binary swap with rollback

### What we finished

- `pulse/internal/updater/` ported from Beacon's `agent/internal/updater/*`
  and `agent/tools/{keygen,sign}`, adapted to Axon's architecture:
  `verify.go` (`VerifyBinary`, hardcoded `pinnedPublicKey`, split into an
  unexported `verifyBinaryWithKey` for testability), `swap_unix.go`
  (`os.Rename` + `syscall.Exec`, same PID fresh `main()`) /
  `swap_windows.go` (rename-aside + spawn + exit — simplified from
  Beacon's version, no Windows-service-mode branch since Pulse has none),
  `updater.go` (the `ApplyUpdate`/grace-period confirm-or-rollback state
  machine).
- `pulse/tools/{keygen,sign}` — generate a keypair, sign a release binary.
  Generated a real production keypair; the private key was surfaced only
  in chat, never written to any file or committed.
- Wire: `HeartbeatResponse.Update` (`protocol.UpdateInfo` —
  version/download_url/signature_hex), Panel's `pulseReleases` table
  (insert-only, newest-row-wins, no downgrade protection) and a "Publish
  Pulse release" form on `/settings`.
- Verified with a real sandboxed dry run: two Pulse binaries built with
  distinct injected versions, signed with a throwaway keypair swapped into
  a temporary local build of `verify.go` for the test only (never the real
  pinned key). Confirmed live: the happy-path swap-and-confirm (including
  `Reconcile()` re-adopting a still-running stand-in process across the
  restart, twice, across two consecutive self-triggered updates); the
  rejection path (a deliberately-wrong signature never swaps, Pulse keeps
  running and keeps retrying); the rollback path (killing Panel right as
  the swap fires so no heartbeat can ever confirm — the grace-period
  deadline fired and `rollback()` restored the backup binary cleanly).
- `go build/vet/test` (incl. cross-compiles), `svelte-check`, both
  `ADAPTER` builds clean.

### Key technical decisions (with why)

- **No separate poll loop, unlike Beacon.** Beacon's updater has its own
  always-on version-check cycle (5-minute stagger, 24h interval, a
  dedicated endpoint). Axon's heartbeat is already a periodic
  Pulse-initiated cycle, so the update check just rides
  `HeartbeatResponse.Update` — a genuine simplification over the reference
  implementation, not just a port.
- **Signing, not just checksumming, is the real security boundary.**
  `sha256sum`-matching (the manual-deploy flow's only integrity check)
  proves a file wasn't corrupted in transit, not that Panel's word about
  it should be trusted — Panel's heartbeat response only ever *proposes*
  an update, `VerifyBinary` is the actual authority.
- **`syscall.Exec`'s fresh `main()` losing in-memory instance state is
  safe, not a new risk** — it's exactly the scenario `Manager.Reconcile()`
  (PID-file reconciliation, 2026-07-24 entry) already exists to solve, and
  it already runs on every Pulse startup.
- **In-flight `create_instance` guard**: `runLoop` only applies an update
  when `activeJobs` is empty, so a multi-minute provisioning goroutine
  can't get silently killed by a re-exec mid-job. Panel keeps offering the
  update every heartbeat until it's finally applied, so nothing is lost by
  deferring.
- **No CI/CD** — self-update automates the swap, not the build; the admin
  still builds, signs, and hosts each release binary by hand.

---

## 2026-07-26 — RCON console, properties editor, file management

### What we finished

- **Raw RCON console**: `console_command`
  (`Manager.RunConsoleCommand`, `pulse/internal/mcserver/rcon_command.go`)
  — an arbitrary admin command sent verbatim to a running instance's RCON
  port, its text response returned via a new `CommandResult.Output` field.
  Synchronous like every command type except `create_instance`.
- **Server properties editor**: `read_properties`/`write_properties`
  (`ReadPropertiesFile`/`WritePropertiesFile`,
  `pulse/internal/mcserver/properties.go`) — deliberately raw text, not a
  structured per-key form; `write_properties` overwrites atomically
  (temp file + rename).
- **File management**: new `pulse/internal/filemanager/` package —
  `List`/`Delete`/`Save` over an instance's whole `working_dir` (not just a
  hardcoded plugins/mods folder), gated by a `resolve()` helper rejecting
  any path escaping `working_dir`. Uploads needed a second reversed-
  transfer pattern (after backup downloads): the admin uploads to Panel
  first (`file_uploads` table), then Pulse *pulls* it on its own next
  heartbeat via `PullFileUpload`.
- Found and fixed a real, pre-existing bug while building the upload path:
  `@sveltejs/adapter-node`'s built server caps every request body at 512K
  by default (`BODY_SIZE_LIMIT`) — confirmed live (a >512K upload got a
  clean 413 without the env var raised). This silently affected the
  existing `push_backup` upload route too, not just this feature — `vite
  dev` never enforces it, only the built server does, which is why it went
  unnoticed until now. Documented in `panel/README.md`.
- `go build/vet/test` and `svelte-check` clean throughout.

### Key technical decisions (with why)

- **`CommandResult.Output` reused three ways** (RCON response text, raw
  `server.properties` content, JSON-encoded file listing) rather than
  growing a new single-purpose field each time — one free-text field,
  documented once, in both `types.go` and `protocol.ts`.
- **`Success` vs `Output` is a deliberate split for RCON**: `Success`
  reflects whether the RCON exchange itself worked; `Output` carries
  whatever text came back even if the game itself rejected the command
  (e.g. `/foo` → "Unknown command" is still a successful round-trip).
- **Fast-poll UX pattern, reused across both console and properties**: the
  instance page's poll drops from 3s to 1s while a command is
  `queued`/`sent`, via a `fastPollNeeded` derived boolean — doesn't beat
  Pulse's `--interval` floor, just shaves the perceived wait once Pulse
  has actually reported a result.
- **File delete is the one destructive action (besides restore) that gets
  `ConfirmModal`** — an admin-navigable arbitrary subtree, recursive, no
  listing shown first, no easy undo, unlike plain backup delete or a
  console command.

---

## 2026-07-25 — Server provisioning: deploy new Java and Bedrock servers

### What we finished

- **Version/download-URL resolution lives in Panel, not Pulse**
  (`panel/src/lib/server/versionCatalog.ts`) — plain outbound `fetch()`
  calls that work identically on Node and Cloudflare Workers. Java
  resolves via Mojang's public `version_manifest_v2.json` (its per-version
  detail JSON has both the download URL and the required Java major
  version — no hardcoded mapping table needed). Bedrock has no equivalent
  API; Panel attempts a best-effort scrape of minecraft.net that's
  confirmed unreliable in practice (timed out live during development,
  likely bot-detection), so the create-server form always shows an
  admin-editable download-URL field for Bedrock rather than trusting the
  scrape blindly.
- **Java runtime auto-install, Linux-only** (`pulse/internal/javaruntime/`):
  detects an existing match first, installs via `apt-get`/`dnf`/`yum`
  otherwise. Needs a new operational prerequisite — the Pulse service user
  needs scoped passwordless sudo for package installation.
- **`pulse/internal/provision/`**: downloads the release (Java →
  `server.jar` directly; Bedrock → a zip, with the same zip-slip
  defensive check `backup/engine.go`'s tar extraction already has, load-
  bearing here since the archive comes from a remote third party), writes
  `eula.txt`, patches `server-port` into `server.properties`. Bedrock's
  launch needed a new `Env []string` field on `InstanceConfig` for
  `LD_LIBRARY_PATH=.`.
- **`Manager.AddInstance`** (`pulse/internal/mcserver/manager.go`) —
  dynamic instance registration; the instance list was previously fixed at
  process start. Atomically rewrites `pulse.instances.json` in the same
  critical section as the in-memory insert, rolling back on a failed
  write.
- **Async command execution** — the one exception to `execute()`'s
  synchronous contract. `create_instance` can take minutes (Java install,
  download), so `runLoop` intercepts it before `execute()`'s switch and
  runs it in a goroutine (`creationJob`, `pulse/cmd/pulse/create_instance.go`),
  reporting a coarse phase via a new `HeartbeatRequest.InProgressCommands`
  field until it finishes.
- **Port + working-dir placement**: Panel auto-assigns both from an
  admin-configured port range + instances root dir (new
  `/agents/[pulseAgentId]` page — the first agent-detail view this project
  has had). The port allocator considers both already-recorded ports and
  ports claimed by a still-in-flight `create_instance` command, closing a
  real race between two quick create-server submissions.
- `go build/vet/test` and `svelte-check` clean.

### Key technical decisions (with why)

- **Deliberately narrow v1 scope**: vanilla only, latest ~3 versions per
  edition, Linux-only Java auto-install, fixed Java heap, one shared port
  range per agent. Reusable server "definitions"/templates, per-host RAM-
  based heap sizing, and split port ranges per edition were all
  consciously deferred rather than half-built.
- **Port allocator is blind to legacy hand-configured instances** — it
  only considers ports Panel itself knows about (`server_instances` +
  in-flight `create_instance` payloads); a pre-existing hand-configured
  instance never reports a port on the wire at all. Documented as a known
  gap, not silently assumed away — the admin is responsible for picking a
  non-colliding range.
- **No automatic cleanup on a Pulse crash mid-provision** — the `commands`
  row self-heals via `failStaleCommands`, but a partially-downloaded file
  or half-created directory has no automatic sweep. Matches this
  project's existing tolerance for similar one-off gaps.

---

## 2026-07-25 — Stale-command timeout, backup scheduling + retention

### What we finished

- **`failStaleCommands()`** (`panel/src/lib/server/commands.ts`) — a
  command stuck `sent` past 3 missed heartbeats (reusing `isOnline()`'s
  "presumed offline" bar) auto-resolves to `failed` with an honest
  "timed out waiting for a result" message, and any dependent
  `backups`/`backupDownloads` row resolves the same way a genuine failure
  would. This closes a real gap hit during the backups work: if Pulse
  restarts between finishing a command and the heartbeat that would
  report it, the result is lost and the command previously sat at `sent`
  forever (a `delete_backup` was orphaned this way and had to be fixed by
  hand against verified on-disk reality before this existed).
- **Backup scheduling + retention** (`panel/src/lib/server/backupSchedules.ts`):
  one `backupSchedules` row per instance (presence of the row plus which
  of `intervalHours`/`keepCount`/`keepDays` are non-null fully expresses
  the state, no separate enabled flag). `queueScheduledBackupIfDue()`
  stamps `lastRunAt` *before* the backup resolves so a short interval
  can't cause pile-up. `applyRetention()` keeps a backup if it's the
  newest, **or** within `keepCount`, **or** within `keepDays` — union, not
  intersection. Both run inline during heartbeat handling, no new timer.
  "Apply Retention Now" button exposes `applyRetention()` standalone.
- `go build/vet/test` and `svelte-check` clean.

### Key technical decisions (with why)

- **Piggybacked on heartbeat requests, not a timer** — same
  Cloudflare-compatibility constraint as `pruneExpiredDownloads()`; called
  from the heartbeat route (agent-scoped), the dashboard load (unscoped,
  since the owning agent may never come back), and the instance detail
  page load (agent-scoped).
- **A freshly-configured schedule is due on the very next heartbeat**
  (not a full interval later) — lets the admin confirm a schedule is
  actually wired up without a heartbeat-scale wait.

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
