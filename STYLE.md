# STYLE.md

UI/UX and styling conventions for Axon Panel. Established from the first
vertical slice (`panel/src/routes/+page.svelte`, `login/+page.svelte`,
`lib/theme/`), extended by the backups feature
(`instances/[serverInstanceId]/+page.svelte`, `settings/+page.svelte`,
`lib/components/ConfirmModal.svelte`), and further extended by the RCON
console/diagnostics-runner, file management, and provisioning/server-
definitions features (see "Command transcript", "Compact inline admin
form + table", and "Cascading/dependent selects" below) — follow these
rather than introducing new patterns per component.

## Theming

- All color values are CSS custom properties, prefixed `--axon-*`, defined
  per-palette in `panel/src/lib/theme/palettes.css` and selected via
  `data-theme="<id>"` on `<html>`. **Never hardcode a hex color in a
  component** — always reference a variable, so the component works under
  every palette automatically.
- Palette variable set: `--axon-background`, `--axon-surface`,
  `--axon-primary`, `--axon-secondary`, `--axon-accent`, `--axon-text`.
- Status colors are declared once in `:root` and are **fixed across every
  palette** — `--axon-status-success`, `--axon-status-warning`,
  `--axon-status-error`, `--axon-status-info`. Don't let a palette block
  override them; that's what keeps "red = danger" meaningful under a
  red/orange-toned palette like Nether.
- New palettes: add a new `[data-theme='id']` block to `palettes.css` with
  all six palette variables — no component changes needed. Name palettes by
  color character (e.g. "The End", "The Nether"), not exact in-game terms —
  trademark consideration for the OSS project.
- Theme persistence: `localStorage.setItem('axon-theme', id)`, applied
  before hydration via the inline script in `app.html` (avoids a flash of
  the default palette). `ThemeSwitcher.svelte` is the only place that
  writes the `data-theme` attribute at runtime.

## Layout

- Page container: `max-width: 960px`, centered (`margin: 0 auto`), page
  padding `1.5rem 2rem 4rem` — see `.page` in `+page.svelte`.
- "Card" pattern (used for the enrollment box and each agent card):
  `background: var(--axon-surface)`, `border: 1px solid var(--axon-accent)`,
  `border-radius: 0.75rem`, padding `1rem 1.25rem` to `1.25rem` depending on
  content density.
- Auth pages (login) center a single card in the viewport
  (`min-height: 100vh; display: flex; align-items: center; justify-content:
  center`), card width `min(360px, 90vw)`.
- **Detail pages** (a page one level below the dashboard, e.g.
  `/instances/[serverInstanceId]`, `/settings`) open with a breadcrumb back
  to the dashboard: `<p class="breadcrumb"><a href="/">← Dashboard</a></p>`
  above the page `<h1>`, `.breadcrumb a` styled `color: var(--axon-text);
  opacity: 0.7` (no underline emphasis — it's a secondary nav affordance,
  not a primary action). Content below the header goes in one or more
  `.card` sections, same card recipe as the dashboard's agent cards.

## Spacing & radius scale

Stick to these values rather than inventing new ones:

- Border radius: `0.375rem` (inputs, buttons, small controls), `0.75rem`
  (cards), `999px` (pill badges).
- Gaps/padding follow a `0.25rem` step scale in practice: `0.25rem`,
  `0.375rem`, `0.5rem`, `0.75rem`, `1rem`, `1.25rem`, `1.5rem`, `2rem`.
- Font sizes: `0.75rem` (badges), `0.8rem`–`0.875rem` (meta text, form
  labels, theme switcher), body default (unset, inherits `system-ui`).

## Components

- **Buttons**: primary action = filled, `background: var(--axon-primary)`,
  `color: var(--axon-background)` (inverted-on-primary, not white — keeps
  it correct across light and dark palettes), `border-radius: 0.375rem`,
  padding `0.4rem 0.9rem` (list actions) or `0.625rem` full-width (auth
  forms). Secondary/destructive-leaning actions use the `.ghost` variant:
  `background: transparent`, `border: 1px solid var(--axon-accent)`,
  `color: var(--axon-text)`. Disabled state: `opacity: 0.5; cursor:
  not-allowed`.
- **Status badges**: pill-shaped (`border-radius: 999px`), colored
  `background` from the fixed `--axon-status-*` set, `color: white` fixed
  (not `var(--axon-text)`) — the white-on-color contrast was checked by
  hand for all four status colors (≥5:1, comfortably above AA's 4.5:1). Add
  new status meanings by mapping to one of the existing four colors, don't
  invent a fifth without re-checking contrast.
- **Forms**: SvelteKit form actions (`method="POST" action="?/actionName"
  use:enhance`), not client-side `fetch()`. Structured data goes in hidden
  inputs (`<input type="hidden" name="..." value={...} />`), not JSON
  bodies. Inline error display is a `{#if form?.error}<p class="error">`
  directly below the relevant input, styled with
  `color: var(--axon-status-error)`. A successful save/action uses the same
  shape with `.success` instead — `{#if form?.success}<p class="success">`,
  `color: var(--axon-status-success)` — the positive counterpart, added
  once a form (server-properties save) needed to confirm success, not just
  surface failure. Don't invent a third variant; map any other outcome onto
  one of these two.
- **Inputs**: padding `0.5rem 0.625rem`, `border-radius: 0.375rem`,
  `border: 1px solid var(--axon-accent)`, `background: var(--axon-background)`.
  Any input/select given a custom `width` (not left to its natural/flex
  sizing) **must** also get `box-sizing: border-box` — without it the
  padding/border add on top of the declared width instead of inside it,
  which is exactly narrow enough to go unnoticed until the input sits next
  to something else (a button, another field) and visibly overlaps it.
  Found live on the new-instance directory field overlapping its submit
  button; check this any time a width is added to an existing input.
- **Ghost link** (`.ghost-link`): the anchor-tag equivalent of a `.ghost`
  button, for navigation/actions that render as a link rather than a form
  submit (e.g. a "Backups →" link, a ready "Download ⬇" link). Same visual
  recipe as `.ghost`: `padding: 0.4rem 0.9rem`, `border-radius: 0.375rem`,
  `border: 1px solid var(--axon-accent)`, `color: var(--axon-text)`,
  `text-decoration: none`, `font-size: 0.875rem`, `display: inline-flex;
  align-items: center`. Duplicated per-file (each route owns its own
  `<style>` block, see below) rather than shared — keep it that way unless
  a third file needs it and the duplication starts actively hurting.
- **Icon button** (`.icon-link`, e.g. the dashboard header's Settings cog):
  a square `.ghost`-styled button/link sized to just the icon —
  `width/height: 2.1rem`, same border/radius as `.ghost`, `display:
  inline-flex; align-items: center; justify-content: center`. Icons are
  **inline SVG, hand-written in the markup** (`stroke="currentColor"` so
  they follow `--axon-text` automatically across palettes) — no icon font
  or icon library dependency. Always pair with `title`/`aria-label` since
  there's no visible text.
- **Pulsing badge** (`.badge-pulsing`, added to a status badge's class list
  while that state is actively in-flight — "Pending", "Deleting…",
  "Backing up…", "Starting…", etc.): `animation: badge-pulse 1.4s
  ease-in-out infinite` cycling `opacity` between `1` and `0.45`, respecting
  `@media (prefers-reduced-motion: reduce)` (animation: none). Duplicated
  per-file alongside the badge classes themselves, same reasoning as
  `.ghost-link` above.
- **Heartbeat/progress bar** (`.heartbeat-bar` / `.heartbeat-bar-fill`): a
  thin (`height: 0.3rem`) pill-shaped track (`background:
  var(--axon-accent)`) with a fill (`background: var(--axon-status-info)`,
  switching to `var(--axon-status-warning)` via a `.heartbeat-bar-overdue`
  modifier once the countdown hits zero) whose `width` is driven by
  `heartbeatProgress()` from `$lib/heartbeat.ts` and transitions smoothly
  (`transition: width 1s linear`) rather than snapping. **Only render this
  contextually, next to a specific pending operation** (a backup row that's
  `pending`, an instance mid start/stop/restart, a download that's
  `preparing`) — never as an always-visible ambient status element. An
  earlier version showed it unconditionally per-agent on the dashboard;
  that was explicitly walked back as unwanted 24/7 chrome. If the same
  operation would show it on two rows at once (e.g. a restore's
  auto-created safety-backup row *and* the backup being restored), suppress
  it on the secondary row rather than showing the same countdown twice —
  see `suppressRedundantBar()` in the instances detail page for the exact
  (narrow!) condition.
- **Modal** (`$lib/components/ConfirmModal.svelte`, the first component
  under `lib/components/` — that directory not existing was true when this
  file was first written, it's since been established and should be reused
  for the next shared piece of UI, not treated as still nonexistent):
  full-screen `.backdrop` (`position: fixed; inset: 0; background:
  rgba(0,0,0,0.5)`) centering a `.modal` card (same card recipe:
  `var(--axon-surface)` background, `var(--axon-accent)` border,
  `0.75rem` radius, `max-width: 420px`). Used for destructive confirmations
  in place of the browser's native `confirm()` — a native `confirm()` was
  the first implementation and was explicitly called out as needing to be
  themed instead. Props: `open`, `title`, `message`, `confirmLabel`,
  `cancelLabel`, `danger` (styles the confirm button with
  `--axon-status-error` instead of `--axon-primary`), `onConfirm`,
  `onCancel`. The calling page owns *when* to show it (e.g. a button's
  `onclick` sets a `$state` id rather than the form submitting directly)
  and submits the real form programmatically
  (`formEl.requestSubmit()`) from `onConfirm` — see the Restore button in
  `instances/[serverInstanceId]/+page.svelte` for the reference
  implementation.

- **Command transcript** (`.console-log` / `.console-entry-header` /
  `.console-output`, first built for the raw RCON console on the instance
  page): a `<ul>` of `<li>` entries, each showing the sent command as
  `<code>&gt; {command}</code>` plus a status badge (`badge-pulsing`
  "Sent, waiting…" while `queued`/`sent`, `badge-error` "Failed"), and —
  once `completed` — a `<pre class="console-output">` block with the raw
  result text (or the failure message when `failed`). Reused verbatim,
  same class names, on the agent-detail page's host-diagnostics runner
  (same "admin sends a command, result only arrives on a later heartbeat"
  shape) — duplicated per-file, same reasoning as `.ghost-link`/
  `.badge-pulsing` above. Reach for this exact shape for any future
  "queue a command, show its result once Pulse reports it" UI rather than
  a bespoke transcript.
- **Compact inline admin form + table** (`.release-form` + `.releases`
  table, `/settings`): a horizontal, wrapping form
  (`display: flex; align-items: flex-end; gap: 0.75rem; flex-wrap: wrap`)
  with each `<label>` sized to its content, plus a `.wide` modifier
  (`flex: 1 1 16rem`) for a field that should absorb remaining space,
  followed by a plain bordered `<table class="releases">` listing
  existing rows (thin `border-bottom` per row, header cells at
  `opacity: 0.7`). Used for "Publish Pulse release" and "Server
  Definitions". This is the layout for "admin adds one row to a global
  config list" — distinct from the vertical `.create-form` stack (single
  primary action, e.g. creating one server), which stays vertical.
- **Cascading/dependent selects** (the create-server and
  server-definition forms' Software → Version dropdowns): a
  `$derived.by(...)` recomputes the dependent option list from the
  parent selection (e.g. `selectedSoftwareType` picks which of
  `data.javaVersions`/`paperVersions`/`fabricVersions`/`forgeVersions` is
  currently in scope), paired with a `$effect` that resets the dependent
  bound value to the new list's first entry (or `''` if empty) whenever
  the parent selection changes — so a stale selection from the previous
  list is never silently submitted. Reuse this `$derived.by` + resetting
  `$effect` shape for any future "picking A filters the options for B"
  dropdown pair rather than inventing a new reactivity pattern.
- **List row with right-aligned metadata** (`.instance-row` /
  `.instance-info` / `.instance-side` / `.instance-stats`, dashboard agent
  cards and the agent-detail page's instance list): each row is
  `display: flex; justify-content: space-between; align-items: center`.
  `.instance-info` (left) is `display: flex; flex-direction: column` —
  name/status on top, secondary detail below. `.instance-side` (right) is
  `display: flex; align-items: center; gap` — small `.instance-stats` text
  (`opacity: 0.7`, `font-size: 0.8rem`, e.g. player count/uptime) directly
  followed by the row's action buttons/links. Built to replace an earlier
  layout that wasted horizontal space on wide viewports; reuse this
  three-part shape (info left, stats+actions right) for any future list of
  like-things-with-actions rather than stacking everything vertically.
- **Inline marker icon** (`.server-icon`, prefixed before an instance's
  name in `.instance-info`): a small hand-written inline SVG (a simple
  cube glyph), `opacity: 0.6`, `flex-shrink: 0`, sized to the text line
  height, `stroke="currentColor"` per the existing inline-SVG convention
  (see Icon button above). Exists so a glance at the dashboard/agent-detail
  page can tell "this row is an actual Minecraft server instance" apart
  from a node/agent card — those look otherwise identical at a glance.
  Reuse for any future "this row is fundamentally a different kind of
  thing than its siblings" distinction; don't reach for color or badge
  text for that job, a marker icon reads faster.
- **Section label** (`.section-label`, e.g. dividing "Server settings"
  within the create-server form): a small sub-heading —
  `font-size: 0.8rem`, `opacity: 0.7`, `text-transform: uppercase`,
  `letter-spacing: 0.03em`, `border-top: 1px solid var(--axon-accent);
  padding-top: 1rem; margin-top: 0.5rem` — used to break a single long
  vertical `.create-form` into logical groups without turning it into
  multiple cards. Reuse this for any future form that grows past one
  visually flat group of fields, in place of splitting into separate
  `.card`s (which implies separable, independently-actionable sections —
  a single-submit form isn't that).
- **Capability-gated section shows an explanation, not silence**: when a
  feature genuinely cannot work for some instances (e.g. the RCON
  console/Player Management cards for a Bedrock instance — Bedrock
  Dedicated Server has no RCON support at all, see CLAUDE.md's "Raw RCON
  console" section), don't just omit the section with no trace, and don't
  show the normal controls knowing they'll only ever fail. Instead render
  a brief `<p class="meta">` explanation in the section's place (e.g. "RCON
  isn't supported on Bedrock servers — console/moderation commands aren't
  available for this instance."). The admin should be able to tell "this
  isn't available" from "this is available and something's wrong" without
  guessing. Apply this same shape to any future feature that's genuinely
  edition/platform-gated rather than universally available.

## Typography & assets

- Font stack: `system-ui, -apple-system, 'Segoe UI', sans-serif` — no
  custom webfont, no separate monospace stack yet (inline `<code>` uses the
  browser default monospace).
- No utility-CSS framework (no Tailwind) — each route's own `.svelte` file
  still owns its own scoped `<style>` block using the palette variables
  directly, and that's still the default. `lib/components/` now exists for
  genuinely cross-page shared UI (currently just `ConfirmModal.svelte`) —
  reach for it when a second unrelated page would otherwise duplicate real
  interactive behavior (not just CSS, which is still fine to duplicate per
  the `.ghost-link`/`.badge-pulsing` note above), not for every small
  visual pattern.
