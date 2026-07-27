# Axon Panel — desktop shell

A **thin client**, not a bundled Panel backend: this app has no local
server, no local database, and no build-time dependency on Panel's own
SvelteKit build at all. It's a native window pointed at whatever
already-running Axon Panel instance the admin configures — the same URL
you'd type into a browser (Cloudflare Workers, a self-hosted Node
deployment, a LAN address, whatever). Axon doesn't need to know or care
how that Panel is reachable (LAN, Tailscale, a public domain); that's the
admin's own networking choice, unchanged from opening Panel in a normal
browser tab.

**First run**: shows a small local config page (`ui/index.html`, the only
"frontend" this app bundles) asking for the Panel URL, saves it to
`app_config_dir()/desktop-config.json`, then navigates the window there.
**Later runs**: goes straight to the saved URL. The "Change Panel URL…"
menu item returns to the config page at any time.

This replaces an earlier plan (spawning `node build/index.js` as a local
sidecar and pointing the webview at that local port) that turned out not
to fit — that design assumes Panel only exists while the desktop app is
open, which doesn't work if you want one Panel reachable from multiple
machines/networks over time.

## Commands

```sh
cd panel
pnpm run tauri:dev     # or: pnpm exec tauri dev
pnpm run tauri:build   # or: pnpm exec tauri build
```

**This scaffold has not been compiled or run** — the environment this was
written in has no Rust toolchain (`cargo`/`rustc`) and no display server.
Before relying on it, run `cargo check` (or `pnpm run tauri:dev`) on a
machine with Rust installed, and expect to need to fix small API-shape
mismatches (exact Tauri v2 method signatures, auto-generated permission
identifiers in `capabilities/default.json`) that couldn't be verified by
compiling ahead of time.
