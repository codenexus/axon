# STYLE.md

UI/UX and styling conventions for Axon Panel. Established from the first
vertical slice (`panel/src/routes/+page.svelte`, `login/+page.svelte`,
`lib/theme/`) — follow these rather than introducing new patterns per
component.

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
  `color: var(--axon-status-error)`.
- **Inputs**: padding `0.5rem 0.625rem`, `border-radius: 0.375rem`,
  `border: 1px solid var(--axon-accent)`, `background: var(--axon-background)`.

## Typography & assets

- Font stack: `system-ui, -apple-system, 'Segoe UI', sans-serif` — no
  custom webfont, no separate monospace stack yet (inline `<code>` uses the
  browser default monospace).
- No component library, no utility-CSS framework (no Tailwind) — each
  Svelte component has its own scoped `<style>` block using the palette
  variables directly. Keep it that way rather than introducing a framework
  mid-slice.
