# DESIGN.md — 9router-go Dashboard · "Imperial Dusk"

Design system and UI/UX overhaul spec for the 9router-go dashboard (`web/`, Svelte 5 + Tailwind v4, embedded in the Go binary via `web/embed.go`).

> **Direction:** a cyberpunk operator console for an AI gateway, with the engraved ceremony of Warhammer 40K and the vast, quiet monumentality of Dune.
> **Rule zero:** atmosphere comes from restraint. If a screen looks like an RGB gaming keyboard, remove something.

---

## 1. Product context

9router-go is a **control room**: operators connect providers, route models through combos, watch traffic and quota, and tail logs. The UI must stay fast to scan and easy to trust. The theme is flavor; **the data is the hero**.

| Influence | Take | Leave behind |
|---|---|---|
| **Cyberpunk** | Monospace readouts, HUD framing, live signal lines, one precise neon accent | Pink/purple neon, heavy glitch, flicker, glass blur |
| **Warhammer (imperial gothic)** | Engraved serif titles, brass hairlines, Roman-numeral sections, ranks/seals, solemn copy | Skulls, blackletter, heavy ornament, grimdark clutter |
| **Dune** | Negative space, spice/sand palette, slow heavy motion, brutalist monolith panels | Sand textures on every surface, sepia overload |

**Mood:** monumental · quiet · precise · ancient-meets-machine · cold signal on warm metal.

---

## 2. Principles

1. **Silence first.** One focal point per screen, generous spacing around the page header.
2. **One light source.** Signal cyan = focus, live, active. Nothing else glows.
3. **Ceremony in hierarchy.** Only page titles, modals, and the login screen get ornament.
4. **Machine precision.** Every number, ID, model name, key, URL, and timestamp is monospace with tabular numerals.
5. **Weight over speed.** Motion settles heavily and never bounces.
6. **Clarity beats flavor.** Buttons use plain verbs. Themed words live in kickers, empty states, and toasts only.

### Anti-norak guardrails (hard limits)
- ≤ **1** glowing element at rest per viewport.
- ≤ **2** type families per screen (display + body). Mono is a utility and doesn't count.
- No gradients with more than 2 stops, and no hue-shifting gradients.
- Glitch effects only on error states: ≤ 150ms, never looped.
- Grain opacity ≤ 3%; scanlines only in Console Log and Login.
- No raw Tailwind palette colors (`bg-red-500`, `bg-indigo-600`, …) in views; semantic tokens only.
- No `backdrop-blur`, no `active:scale-*`, no `hover:scale-*`.

---

## 3. Color

The dashboard already exposes semantic tokens in `web/src/index.css` (`--app-*` → `--color-*` → utilities like `bg-surface`, `text-text-main`). **Keep the names and change the values.** That way every view picks up the theme without edits.

### 3.1 Dark — "Obsidian" (default)

| Token | Hex | Role |
|---|---|---|
| `--app-bg` | `#0A0D10` | Void background |
| `--app-bg-alt` | `#0E1316` | Page sections, table zebra |
| `--app-sidebar` | `#0C1114` | Sidebar |
| `--app-surface` | `#141A1E` | Panels / cards |
| `--app-surface-2` | `#1B2227` | Raised, hover, nested |
| `--app-surface-3` | `#252D33` | Pressed, selected, disabled fill |
| `--app-input` | `#0F1519` | Inputs, code blocks |
| `--app-border` | `#33403F` | Default border |
| `--app-border-subtle` | `#242E31` | Dividers, panel edges |
| `--app-text-main` | `#ECE5D6` | Bone — primary text |
| `--app-text-muted` | `#B0B8B3` | Secondary text |
| `--app-text-subtle` | `#8C9793` | Placeholder, meta |
| `--app-primary` | `#D98A3D` | **Spice** — primary action, active nav |
| `--app-primary-hover` | `#E9A15A` | Spice hover |
| `--app-brass` *(new)* | `#B8955E` | Ornament, hairlines, ranks, kickers |
| `--app-focus` | `#5FD4C8` | **Signal cyan** — focus, live, streaming |
| `--app-glow` | `rgba(95,212,200,0.10)` | Ambient vignette tint |

### 3.2 Light — "Parchment"
Keep the current light values, with these adjustments: `--app-primary: #8A5A24`, `--app-brass: #7A5F36`, `--app-focus: #1F6E68`. Ornament opacity is halved in light mode.

### 3.3 Status

| Token | Dark | Light | Use |
|---|---|---|---|
| `--color-success` | `#6FBF95` (verdigris) | `#2E7A55` | Healthy, connected |
| `--color-warning` | `#E2B34A` (ochre) | `#8A6414` | Near quota, degraded |
| `--color-danger` / `--color-error` | `#E5655A` | `#B03A2E` | Failed, destructive |
| `--color-info` | `#7BBEBA` | `#2B6F6B` | Neutral notices |

`--color-error` and `--color-danger` are the same value; the separate `bg-red-500` usage in `Badge.svelte` goes away.

### 3.4 Ratio
**72%** obsidian surfaces · **20%** bone text · **6%** spice + brass · **2%** signal cyan.

### 3.5 Contrast (WCAG AA)
Main, muted, and subtle text, spice, and cyan all reach ≥ 4.5:1 on `--app-bg` and `--app-surface`. Primary buttons use `text-bg` on `bg-primary` (~6.9:1). **Never use white text on spice.**

---

## 4. Typography

The binary must work offline, so **no font CDNs**. Self-host subset `woff2` files in `web/public/fonts/` (Latin only, ≤ 60 KB each) and declare them with `@font-face` and `font-display: swap`.

| Role | Font | Fallback (already in `--font-*`) |
|---|---|---|
| `--font-headline` | **Cinzel** 500/600 | `"Iowan Old Style", Baskerville, Georgia, serif` |
| `--font-body` | **Inter** 400/500/600 | system-ui |
| `--font-code` | **JetBrains Mono** 400/500 | `ui-monospace, SFMono-Regular, Menlo` |

**Rules**
- Cinzel is used only for the page title (`.ui-heading`), modal titles, the login title, and empty-state headlines. It's always ≥ 20px, in uppercase, with `letter-spacing: 0.06em`. The one exception is the sidebar `9ROUTER-GO` wordmark: 15px with `0.12em` tracking.
- `.ui-kicker` (mono, uppercase, `0.17em`, brass) sits above every page title.
- Model IDs, API keys, endpoints, combo names, token counts, latency, and cost use `font-code` plus `tabular-nums`.

| Token | Size / line | Font |
|---|---|---|
| `display` (login) | 44 / 52 | Cinzel 600 |
| `h1` (`.ui-heading`) | clamp(1.5rem, 2.4vw, 2.1rem) / 1.15 | Cinzel 500 |
| `h2` (modal) | 22 / 30 | Cinzel 500 |
| `h3` (card title) | 15 / 22 | Inter 600 |
| `body` | 14 / 22 | Inter 400 |
| `small` | 12.5 / 18 | Inter 400 |
| `kicker` | 11 / 16 | Mono 600, uppercase |
| `stat` (`.ui-stat`) | 28 / 32 | Mono 500 |

---

## 5. Layout & spacing

- Base unit is 4px. Allowed steps: `1 2 3 4 6 8 12 16 24` (Tailwind).
- Shell: sidebar `w-72` → icon rail `w-16` at `< lg`, then a drawer at `< md` (already wired via `isMobileMenuOpen`).
- Content max width is `1280px`, with padding of `px-6 lg:px-10`.
- Page header block: kicker → title → one-line description → primary action on the right. Leave `mb-8` below it.
- Two density modes: **comfortable** (default views) and **compact** (Usage tables, Quota, Console Log: 36px rows, `text-[13px]`).

---

## 6. Shape, borders, depth

The codebase currently has about 20 different radii (`rounded-lg` ×293, `rounded-[14px]` ×36, `rounded-xl`, `rounded-2xl`, …). Collapse them into **three**:

| Token | Value | Use |
|---|---|---|
| `--radius-brand` | `3px` | Buttons, inputs, badges, pills, nav items |
| `--radius-brand-lg` | `4px` | Cards, panels, modals |
| `rounded-full` | — | Only for status dots, avatars, and toggle knobs |

- **Signature shape:** an 8px chamfered corner (`.ui-chamfer`) on **solid-filled** elements only (primary button `md`/`lg`). `clip-path` would cut the corners off a bordered box, so bordered containers such as modals and the login card use `.ui-frame` brackets instead. The chamfer uses an inset focus ring so it isn't clipped.
- **Brass tick:** keep the existing `.ui-panel::before` 54px hairline. It is the house mark on every panel.
- **HUD corner brackets** (`.ui-frame`): 10px brass L-marks, used only on featured panels (Endpoint URL, Topology, Login).
- **Depth:** use a surface step plus a border instead of big drop shadows. Replace `--shadow-warm` hovers with a border shift to `--app-brass/50`. Keep `--shadow-elevated` for modals only.

---

## 7. Texture & effects

| Effect | Where | Spec |
|---|---|---|
| Vignette | `body` (already present) | Radial `--app-glow` top-right, spice 5% bottom-left |
| Landing grid | Endpoint and Login heroes (`.landing-grid`) | Keep at 0.12 opacity with a diagonal mask |
| Grain | Global overlay | Inline SVG turbulence, 3%, `pointer-events:none` |
| Scanlines | `TerminalView`, `LoginView` | `repeating-linear-gradient` at 2px, 3% opacity |
| Glow | Focus, live stream indicator, topology core | `0 0 0 1px var(--app-focus), 0 0 14px rgba(95,212,200,.22)` |
| Glitch | Input error, failed connection test | 120ms x-offset plus a crimson/cyan channel split, once |
| Blur | **None** | Remove `backdrop-blur-xl` from `Sidebar` and `TopBar` |

---

## 8. Iconography & ornament

- **Icons:** Material Symbols Outlined (the current system), `wght 300` for nav and `400` inline, at 18px or 20px. Lucide is allowed only where it's already used for spinners. Don't mix the two in one row.
- **Provider logos** (`/providers`, `getIconPath`): render them on a neutral `bg-surface-2` 32px tile with a 3px radius. Don't tint them.
- **Ornament kit:**
  - Divider: a hairline with a centered `◆` in brass (`.ui-divider`).
  - Roman numerals for nav sections and wizard steps (I, II, III…).
  - **Seal** (octagon badge) for ranks: provider priority, combo strategy, API key scope.
- Evoke the influences without copying them: no aquilas, franchise glyphs, or trademarked marks.

---

## 9. Components (maps to `web/src/lib/ui/*`)

### 9.1 `Button.svelte`
| Variant | Spec |
|---|---|
| `primary` ("decree") | `bg-primary text-bg hover:bg-primary-hover`, Inter 600, `uppercase tracking-[0.06em] text-[12.5px]`, `.ui-chamfer` at `md`/`lg` |
| `secondary` | `bg-surface-2 border border-border text-text-main hover:border-[--app-brass]/50` |
| `outline` | Transparent, `border-[--app-brass]/40`, `text-text-main` |
| `ghost` | `text-text-muted hover:text-text-main hover:bg-surface-2` |
| `danger` | `bg-danger/12 text-danger border border-danger/40 hover:bg-danger/20` (tonal everywhere, including `ConfirmModal`) |
| `success` | Same pattern as danger, using the success tone |

Sizes: `sm h-8`, `md h-9`, `lg h-11`, all `rounded-[3px]`. **Remove `active:scale-[0.97]`**; the pressed state is `bg-surface-3` or `--app-primary` darkened by 8%. The loading state keeps the label and swaps the icon for a spinner.

### 9.2 `Input.svelte` / `.ui-input`
- Label is a kicker above the field. Helper text goes below it in `text-subtle`.
- Focus: the border becomes `--app-focus`, plus a 2px left "lock-on" bar (`box-shadow: inset 2px 0 0 var(--app-focus)`).
- Error: the border becomes `--color-danger`, with an icon and message below and a one-time glitch.
- Secrets (API keys and tokens) are masked by default, with reveal and copy icon buttons. The value is mono.

### 9.3 `Card.svelte` / `.ui-panel`
- `bg-surface border-border-subtle rounded-[4px]` with the brass tick, `p-6` (`p-4` compact).
- Header: an optional kicker, then the title (Inter 600), then the subtitle. Actions go on the right.
- Clickable (`hover`) cards shift their border to brass/50 and background to `surface-2`. No lift or scale.
- **Featured** (`elev`): add `.ui-frame` corner brackets.

### 9.4 `Badge.svelte`
- `rounded-[3px]`, mono `text-[11px] tracking-wide`, 1px tone border, 10% tone fill. **No uppercase**, because badges often carry case-sensitive model IDs.
- Dot colors come from semantic tokens (`bg-success`, `bg-warning`, `bg-danger`, `bg-info`, `bg-primary`), with no raw palette colors.
- A live/streaming state gets a signal-cyan dot with a slow 2.4s pulse. This is the only badge allowed to animate.

### 9.5 `Toggle.svelte`
- Track is `surface-3`; when on, it becomes `bg-primary/80`. The knob is bone.
- Use a 160ms settle ease with no overshoot.

### 9.6 `Modal.svelte` / `ConfirmModal.svelte` ("edicts")
- Max width 560px (`lg` 760px), `.ui-frame` corner brackets, `border-[--app-brass]/40`, `--shadow-elevated`.
- Title in Cinzel h2, followed by a `.ui-divider`.
- Backdrop: `rgba(5,7,9,0.78)`, **no blur**.
- Destructive confirms show the target name in mono. Bulk or irreversible actions (deleting a provider with connections, clearing usage) require type-to-confirm.

### 9.7 `Toasts.svelte`
- Bottom-right, 360px wide, with a 3px left tone bar and a mono timestamp.
- Success and info dismiss after 4s. Errors persist and include a **Copy details** action.

### 9.8 `ThemeToggle.svelte`
- Keep the `9router-theme` storage key. Label the options "Obsidian" and "Parchment" in the tooltip only; the icon stays sun/moon.

### 9.9 Tables (Usage, Quota, Request details, Proxy pools)
- The header is a kicker row on `bg-bg-alt` with a bottom border in brass/40.
- Rows are 44px (36px compact), with zebra `bg-bg-alt` on even rows. Hover adds a 2px cyan left inset.
- Numbers are right-aligned mono. Status is a Badge, never colored text alone.
- Sticky header, and horizontal scroll inside `.custom-scrollbar` on small screens.

---

## 10. Shell & navigation

### 10.1 `Sidebar.svelte`
- **Remove the fake macOS traffic-light dots.** They're decoration that implies window controls that don't exist.
- Brand block: logo tile (3px radius, no hover scale), then `9ROUTER-GO` in Cinzel 15px `tracking-[0.12em]`, then the version in mono `text-subtle`.
- Update banner: a spice-toned inline panel instead of the green/amber split. "Update now" uses the `primary` Button size `sm`.
- Active item: a 2px left bar in `--app-primary`, `bg-surface`, and `text-text-main` with a filled icon. Inactive items are `text-text-muted`.
- Group the current 20 links into numbered sections (labels stay plain):

| Section | Items |
|---|---|
| **I · Gateway** | Endpoint & Key, Providers, Combo & Vision Adapter, Proxy Pools |
| **II · Observe** | Usage, Quota Tracker, Console Log |
| **III · Optimize** | Token Saver, Feature Flags |
| **IV · Media** *(collapsible, remembers state)* | Embedding, Text to Image, TTS, STT, Video, System One, Web Fetch & Search |
| **V · Tools** | CLI Tools, Skills, Bug Bounty Assist, Persona Loader |

- Footer: the **Settings** link (the `SYSTEM_NAV_GROUP`), then a mono connection readout (`● 7/9 ACTIVE`) that links to Providers. Sign out lives only in the TopBar account menu.
- The nav model (`NAV_GROUPS`, `SYSTEM_NAV_GROUP`, `sectionLabelFor`) is exported from `Sidebar.svelte`'s module script. The breadcrumb and the command palette read it, so a destination can't drift out of sync.

### 10.2 `TopBar.svelte`
- Height 56px, solid `bg-vibrancy` without blur, with a bottom border in `border-subtle`.
- Left: a breadcrumb whose first item is the section kicker (`I · GATEWAY › Providers › OpenRouter`), with the provider icon when `selectedProviderMeta` is set.
- Right: the `› Jump to… ⌘K` palette trigger (an icon below `md`), theme, changelog, and account.
- **⌘K command palette** (`CommandPalette.svelte`): jump to any destination or connected provider, or run an action (copy endpoint, switch theme, release notes, sign out). It's styled as a terminal prompt (`›`) with a mono input and signal-cyan selection. It won't open over another dialog.

### 10.3 Page header pattern (every view)
```
I · GATEWAY                                  [ + Add provider ]
PROVIDERS & ENDPOINTS
Manage your AI provider connections.
─────────────────────── ◆ ───────────────────────
```
Titles and descriptions come from `pageMeta` in `App.svelte`, and the kicker comes from the nav section.

---

## 11. Page-specific notes

| View | Overhaul focus |
|---|---|
| **Login** (`LoginView`) | The one "ceremonial" screen: centered chamfered card with `.ui-frame`, Cinzel display title, landing grid and scanlines behind it. Copy: *"Authenticate to the gateway."* |
| **Endpoint & Key** (`EndpointView`) | A monolith hero panel showing the base URL in large mono with a copy button. The key is masked. Quick-start snippets are in `.ui-code` tabs. |
| **Providers** (`ConnectionsView`, `ProviderCard`, `ProvidersOverviewGrid`) | Uniform card grid, with status as a Badge and dot. The priority seal sits top-right. An OAuth expiry warning uses the warning tone. Test results inline (latency in mono). |
| **Provider detail** | Two columns: connections list on the left, models/settings on the right. The breadcrumb comes from the TopBar. |
| **Combos** (`CombosView`, `ComboCard`, `ModelPickerModal`) | Show the strategy (fallback / round-robin / sticky / fusion) as a seal. Model chain as a numbered vertical list (I → II → III) with drag handles (`dnd-kit`-style). The picker modal lists models in mono, grouped by provider. |
| **Usage** (`analytics/*`) | KPI row in `.ui-stat`. Charts use only spice, cyan, brass, and dust on hairline grids with no fills heavier than 12%. Topology keeps its animations at current subdued values, and the router core is the one allowed glow. |
| **Quota** (`QuotaTable`) | Ochre bars from 75%, danger from 90%. Numbers show used / limit in mono. |
| **Token Saver** | Before/after token comparison as two stat blocks with a saved % in spice. |
| **Console Log** (`TerminalView`) | Allowed CRT flavor: scanlines 3%, mono 12.5px, level colors from status tokens, sticky filter bar, and a pause/auto-scroll toggle. |
| **Proxy Pools** | Table-first layout, compact density, health dots, bulk import in a modal with a validation preview. |
| **Feature Flags / Settings** | A list of rows: label and description on the left, `Toggle` on the right. Section groups separated with `.ui-divider`. |
| **Media views** | Share the `MediaProviderCard` grid pattern with Providers so the two screens don't drift apart visually. |

---

## 12. Motion

| Token | Duration | Use |
|---|---|---|
| `--dur-instant` | 80ms | Hover color, focus ring |
| `--dur-swift` | 160ms | Buttons, toggles, nav |
| `--dur-measured` | 260ms | Dropdowns, toasts, accordions |
| `--dur-monumental` | 440ms | Modals, page transitions |

- Easing: `--ease-imperial: cubic-bezier(0.2, 0, 0, 1)`.
- Entrances fade in with a 6px rise. Exits are fade-only and take 70% of the entrance duration.
- Page change: content fades in over 200ms, and the sidebar never animates.
- Loading: a 2px spice progress line under the TopBar ("sandworm" sweep, 1.6s). Skeletons are `surface-2` blocks with a 4% shimmer.
- The existing `prefers-reduced-motion` block stays and also disables grain, scanlines, glitch, and topology animations.

---

## 13. Voice & microcopy

Tone is solemn, concise, and confident. Flavor goes in kickers, empty states, and toasts; action buttons use plain verbs.

| Context | Plain (buttons) | Themed (titles / messages) |
|---|---|---|
| Empty providers | `Add provider` | **No providers sworn to the gateway.** |
| Empty combos | `Create combo` | **No routes have been decreed.** |
| Empty usage | — | **The ledger is silent.** Traffic will appear here. |
| Saved | — | **Sealed.** Settings saved. |
| Connection OK | — | **Signal acquired** · 312ms |
| Connection failed | `Retry` | **Transmission failed.** 401 Unauthorized. |
| Delete provider | `Delete` | **Purge this provider?** 3 connections will be removed. |
| Update available | `Update now` | **A new version awaits** · v1.9.1 |

Error messages always include the technical cause (status code, provider message) in mono.

---

## 14. UX rules

1. **One primary action per view.** It's the spice button in the page header.
2. **Feedback within 100ms** for every action (button loading, inline status, or toast).
3. **Optimistic toggles** for feature flags and connection enable/disable, rolled back with an error toast on failure.
4. **Copy everywhere:** endpoint, keys, model IDs, combo names, and CLI commands all get one-click copy with a "Copied" state.
5. **Progressive disclosure:** advanced provider fields (headers, proxy, cloaking) sit behind "Advanced" accordions.
6. **Keyboard:** full tab order, the visible cyan ring, `⌘K` palette, `Esc` closes modals, `/` focuses search in lists.
7. **Forms:** validate on blur and show errors beside the field, with icon and text (never color alone).
8. **Background refresh** (the 10s interval in `App.svelte`) must never steal focus or reorder a list under the cursor.
9. **Responsive:** icon rail below `lg`, drawer below `md`. Ornament (`.ui-frame`, dividers) is hidden below `sm`.

---

## 15. Accessibility

- WCAG 2.2 AA text and UI contrast in both themes.
- `:focus-visible` ring is 2px `--app-focus` with a 2px offset (already global in `index.css`).
- Hit targets are ≥ 36px on desktop and ≥ 44px on touch.
- Status is always shown as icon or text plus color.
- Cinzel is never below 20px and never used in running text.
- Modals trap focus and restore it on close. Toasts use `role="status"`, and errors use `role="alert"`.

---

## 16. Token additions (`web/src/index.css`)

```css
:root, :root.dark {
  /* values from §3.1 replace the existing --app-* values */
  --app-brass: #B8955E;
  --ease-imperial: cubic-bezier(0.2, 0, 0, 1);
  --dur-instant: 80ms;
  --dur-swift: 160ms;
  --dur-measured: 260ms;
  --dur-monumental: 440ms;
  --glow-signal: 0 0 0 1px var(--app-focus), 0 0 14px rgba(95, 212, 200, 0.22);
}

@theme inline {
  --color-brass: var(--app-brass);
  --radius-brand: 3px;
  --radius-brand-lg: 4px;
  --font-headline: "Cinzel", "Iowan Old Style", "Baskerville", Georgia, serif;
}

.ui-chamfer { clip-path: polygon(10px 0,100% 0,100% calc(100% - 10px),calc(100% - 10px) 100%,0 100%,0 10px); }

.ui-divider {
  display: flex; align-items: center; gap: .75rem; color: var(--app-brass);
}
.ui-divider::before, .ui-divider::after {
  content: ''; flex: 1; height: 1px; background: currentColor; opacity: .35;
}

.ui-frame { position: relative; }
.ui-frame::after {
  content: ''; position: absolute; inset: -1px; pointer-events: none;
  --c: var(--app-brass); --l: 10px;
  background:
    linear-gradient(var(--c) 0 0) top left / var(--l) 1px,
    linear-gradient(var(--c) 0 0) top left / 1px var(--l),
    linear-gradient(var(--c) 0 0) bottom right / var(--l) 1px,
    linear-gradient(var(--c) 0 0) bottom right / 1px var(--l);
  background-repeat: no-repeat; opacity: .7;
}
```

---

## 17. Migration plan

1. **Tokens:** apply §3 and §16 in `index.css`. This finishes the in-progress theme change and needs no view edits.
2. **Primitives:** update `Button`, `Card`, `Badge`, `Input`, `Modal`, `Toasts`, and `Toggle` per §9.
3. **Shell:** `Sidebar` (sections, no traffic lights, no blur) and `TopBar` (breadcrumb, status, ⌘K).
4. **Sweep raw colors:** replace `bg-red-500` (×37), `bg-amber-500` (×22), `bg-emerald-500` (×16), `bg-blue-500` (×13), `bg-green-500` (×11), and the indigo/pink/orange one-offs with semantic tokens.
5. **Sweep radii:** replace `rounded-lg`/`xl`/`2xl`/`[8–14px]` with `rounded-[3px]` or `rounded-[4px]`.
6. **Pages:** apply §11, starting with Login → Endpoint → Providers → Combos → Usage.
7. **Fonts:** self-host subset woff2 files in `web/public/fonts/`, then check the size of `web/dist` embedded in the binary.
8. **QA:** run the §18 checklist on each view in both themes, then `bun run build` and `bun run lint`.

---

## 18. Review checklist (per view)

- [ ] Kicker, Cinzel title, and description header pattern
- [ ] Exactly one spice primary action
- [ ] ≤ 1 glowing element at rest
- [ ] No raw Tailwind palette colors, no blur, no scale transforms
- [ ] Radii only 3px / 4px / full (dots)
- [ ] IDs, keys, numbers, and timestamps in mono with tabular numerals
- [ ] Empty, loading, and error states designed with themed titles and plain actions
- [ ] AA contrast in Obsidian and Parchment
- [ ] Keyboard reachable, focus ring visible, reduced-motion verified
- [ ] Looks correct at `sm` / `md` / `lg` breakpoints
