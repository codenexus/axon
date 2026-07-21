# Axon Panel — desktop shell

Scaffolded via `pnpm exec tauri init` and wired to the `ADAPTER=node` build
(`beforeBuildCommand`/`beforeDevCommand` in `tauri.conf.json`).

**Known gap, not yet solved:** Panel isn't a static SPA — it's a SvelteKit
app with server routes (auth, `/api/v1/*`, DB access), so `frontendDist`
pointing at the adapter-node `build/` output isn't sufficient on its own at
runtime; that directory contains a Node server entrypoint (`build/index.js`),
not static files a webview can load directly. The desktop build needs a
Tauri sidecar that spawns `node build/index.js` on a local port before the
window opens, then points the webview at that port — not implemented yet.
`tauri dev` works today only because `devUrl` points at the already-running
`vite dev` server.

This scaffold has not been compiled or run — this sandbox has no Rust
toolchain (`cargo`/`rustc` not installed) and no display server. Before
relying on it, run `cargo check` (or `pnpm exec tauri build`) on a machine
with Rust installed.
