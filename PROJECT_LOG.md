# PROJECT_LOG.md

Session-by-session history for Axon. Newest entry first. This is a
working log for picking up context between sessions, not user-facing
documentation — see `README.md` for that, `CLAUDE.md` for architecture,
`STYLE.md` for UI/UX conventions.

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
