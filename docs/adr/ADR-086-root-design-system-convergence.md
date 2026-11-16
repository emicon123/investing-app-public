# ADR-086: Root (investing-app) Converges onto the Shared Psychedelic Design System

**Date:** 2026-09-03
**Status:** Accepted

## Context

Task 125 (`.claude/tasks/125-shared-design-system-proposal.task.md`) proposed collapsing all
three apps' design systems (color tokens, fonts, backgrounds, layout) onto one shared
implementation, with only a per-app "theme" (accent hue) differing — the user's own framing,
verbatim: *"Actually all the palette, backgrounds, css, fonts, layout etc. can be shared. I.e.
only difference could be main theme. Green for Portfolio C, purple for Portfolio B, Psychedelic for main
app. Beside that everything could look exactly the same (unless it changes behaviour)."*

Task 125 executed and shipped the **portfolio-b/portfolio-c half** on 2026-08-26 (`packages/shared/ui/src/
design-tokens.css` + `theme-portfolio-b.css`/`theme-portfolio-c.css`, `data-app-theme="portfolio-b"|"portfolio-c"` set
statically in each app's `index.html`) — proven byte-identical to the pre-refactor hardcoded
values and already deployed. **Root's own convergence was deliberately left unauthorized**,
behind an explicit "HUMAN CONFIRMATION GATE" in the task file, because it reverses several
standing, individually-reviewed decisions (ADR-076's design-token exclusion; tasks 89, 103, 106,
110, 114's per-component exclusions) and changes investing-app's shipped 2026-07-23 "Prism
Ledger" visual identity — not a routine dedup.

**This session, the user was asked directly** which direction root's convergence should go,
framed around task 125 §5's own open question (root's restrained single-accent philosophy
spreading outward vs. portfolio-b/portfolio-c's maximalist multi-neon philosophy applied to root vs. a
fourth compromise vs. deferring root entirely). **The user chose: root becomes Psychedelic** —
investing-app adopts portfolio-b/portfolio-c's maximalist multi-neon aesthetic, with its own distinct hue,
not the reverse. This directly matches the user's original framing quoted above ("Psychedelic
for main app"). The gate is lifted; this ADR documents the resulting design and specifies
implementation shape. It does not itself request further authorization on direction — that
authorization is this session's user confirmation, cited here for traceability.

**Interpretation call, flagged explicitly per the orchestrator's instruction:** the user's test
— *"beside [the theme], everything could look exactly the same"* — read literally, includes
**fonts**. Task 125 §3 step 4 had speculated root would keep its own `--font-display`/
`--font-sans` layer (`Space Grotesk Variable`/`Sora Variable`, `@fontsource-variable`) as a
theme-layer addition on top of the hue swap. This ADR treats the user's verbatim framing as
overriding that speculation: **root's display/body fonts also converge** onto portfolio-b/portfolio-c's
system-ui stack (no custom `--font-display`/`--font-sans` token at all). This is an
interpretation of "everything," not a separately-confirmed instruction — reversible by dropping
this one clause if wrong, without touching anything else in this ADR.

### Pattern evaluation

| Candidate | Verdict | Why |
|---|---|---|
| Strategy (per-app hue set selected via `data-app-theme` attribute) | **adopted** | Already the live mechanism for portfolio-b/portfolio-c (task 125); root joins it unchanged — no new selection mechanism invented. |
| Template Method (`design-tokens.css` = fixed structural CSS; `theme-<app>.css` = hook-point fill-in) | **adopted** | Same reasoning — root's `theme-main.css` fills the same 7 `--theme-hue-*` + `--theme-chart-1` hooks, no parallel structural copy. |
| Adapter (per-app thin wrapper maps real component data onto a shared component's structural prop type) | **adopted** | Extends the pattern already used for `PositionLike`/`HistoriaEntryLike`/etc. to `AppLayout`'s nav shape and confirms it's the right shape for root's `NavSection[]`/`PageHeader`'s `eyebrow`/`description`. |
| Decorator (root-only CSS classes layered on the shared base) | **adopted, lightweight** | `.eyebrow`/`.brand-mark`/`.prism-number`/`table{tabular-nums}` have no portfolio-b/portfolio-c counterpart to converge with — additive in `theme-main.css`, not a fork of the shared file. Same shape ADR-076 already blessed for `badge.tsx`'s one extra class. |
| Open/Closed on `SharedAppLayout`/`SharedPageHeader` (extend via optional props, don't modify existing call sites) | **adopted** | `sections: NavSection[]` and `eyebrow?`/`description?` are additive; portfolio-b/portfolio-c's existing call sites compile and render unchanged. |
| New "blob-count" parameter mechanism in `design-tokens.css` | **rejected (YAGNI)** | Root's 6th blob was a redundant near-duplicate of its 5th (same gold treatment, mirrored position) — not information worth preserving via new machinery. Root converges to the shared 5-blob shape structurally. |
| Factory/Builder/Prototype/Singleton, Composite/Bridge/Flyweight/Proxy, Chain/Command/Iterator/Mediator/Memento/Observer/State/Visitor | **rejected** | No runtime object-creation or behavioral-dispatch variability exists in this change — it is declarative CSS token theming plus two prop-shape generalizations. Introducing any of these would be speculative. |

See `docs/architecture/diagrams/139-root-design-system-convergence.md` for the full UML (theme
cascade flowchart + `AppLayout`/`PageHeader` class diagram) with per-diagram legends.

## Decision

### 1. Root's hue set — distinct from portfolio-b (295, violet) and portfolio-c (145, green)

Root gets **electric blue** as `--theme-hue-primary`, following the same 7-slot pattern already
built for portfolio-b/portfolio-c in `packages/shared/ui/src/design-tokens.css`:

| Slot | Value | Role (per design-tokens.css's own slot naming) |
|---|---|---|
| `--theme-hue-primary` | `230` | `--primary`/`--ring`/`--sidebar-primary`/`--neon-violet` |
| `--theme-hue-secondary` | `218` | `--card`/`--secondary`/`--muted` |
| `--theme-hue-surface` | `222` | `--background` + several `-foreground` tones |
| `--theme-hue-foreground` | `226` | standalone `--foreground` |
| `--theme-hue-accent` | `245` | `--accent`/`--sidebar-accent` |
| `--theme-hue-popover` | `215` | `--popover` + sidebar background |
| `--theme-hue-glow` | `235` | decorative blob/glow gradients |
| `--theme-chart-1` | `oklch(0.68 0.32 230)` | full-color override, same L/C strength as `--primary`, hue reused directly |

**Why blue, not gold/amber/magenta/cyan** (the range the orchestrating task suggested as a
starting point): `design-tokens.css`'s dark-theme spectrum already has **four fixed,
non-theme-varying hues** shared by every app — `--chart-2` acid green (140), `--chart-3` hot
magenta (320), `--chart-4` electric cyan (200), `--chart-5` golden amber (65) — used in
`gradient-text-psychedelic`, `card-psychedelic`'s border, `nav-active-glow`, `topbar-iridescent`,
etc. Picking a primary hue inside that same set (gold/amber/magenta/cyan) would make root's
"distinct" accent visually blend into colors every app's spectrum already carries as supporting
hues. Blue (`230`) sits ~30° from the nearest fixed hue (cyan, 200) and ~50-65° from portfolio-b's
violet (295) — a separation comparable to or better than portfolio-b's own live `--chart-1` choice
(reuses primary 295 directly, only 25° from the fixed magenta at 320, and that was judged
sufficient). Blue also reads as a genuinely new identity rather than a re-hash of an existing
supporting color, and — administratively — suits root's role as the hub of the three apps.

**Why no separate `--theme-chart-1` full-color tuning (unlike portfolio-c):** portfolio-c needed a bespoke
`--theme-chart-1` because its primary hue (145) sits only 5° from the *fixed* `--chart-2` acid
green (140) — reusing it directly would have made `--chart-1` and `--chart-2` nearly
indistinguishable. Root's primary (230) is 30°+ from every fixed hue, so — like portfolio-b — direct
reuse (`oklch(0.68 0.32 230)`, same formula as `--primary`) stays visually distinct from
`--chart-2` through `--chart-5`. No override needed beyond restating the hue.

### 2. Scope of what un-reverses — full list, with reasoning per item

All five standing exclusions the orchestrating task named are addressed:

- **ADR-076** ("Design tokens / `index.css` / Tailwind theme config" explicitly excluded from
  the original `@shared/ui-primitives` extraction) — **un-reversed for structure and palette
  strength**, per task 125's already-authorized portfolio-b/portfolio-c mechanism, now extended to root.
  ADR-076's own narrower point — that `badge.tsx`/`card.tsx`'s one-extra-Tailwind-class pattern
  is fine to keep local rather than force through a shared file — **stands unchanged** for
  `badge.tsx` (`tabular-nums`, orthogonal, untouched by this ADR) and is resolved for `card.tsx`
  by full removal once its only differentiator (`font-display`) disappears with the font
  convergence (§3 below) — root drops its local `card.tsx` entirely and consumes
  `@shared/ui-primitives/card` directly, matching portfolio-b/portfolio-c's re-export-shim pattern.
- **Task 89 (`LoginPage`)** — **NOT un-reversed.** `LoginPage.tsx`'s exclusion was framed as "a
  real Prism Ledger visual redesign (60/66 lines differ), not a cosmetic rename" — but re-checked
  against this ADR's actual scope (color tokens, fonts, backgrounds, layout primitives), nothing
  about `LoginPage`'s own markup structure is touched here; only the CSS custom properties and
  utility classes it already consumes change value/strength underneath it, exactly as they do for
  every other unconverged root component. `LoginPage.tsx` stays a local file (no call-site or
  markup change needed) — it automatically inherits the new palette through the same CSS cascade
  every component does. Listed here for completeness, not because anything moves.
- **Task 103 (`PortfolioValueLine`)** — **NOT un-reversed, and correctly out of scope.** Its
  exclusion was a genuine feature/chart-richness divergence (503 vs. 156 lines — crypto-trading/
  regression-band/macro overlays), not a styling difference. This ADR's convergence is
  palette/font/background/layout-primitive only; `PortfolioValueLine` is untouched, same as
  `LoginPage`.
- **Task 106 (`AppLayout`)** — **un-reversed**, with one real generalization beyond a hue swap:
  root's nav is grouped into titled sections (`NAV_SECTIONS`, 6 groups, each rendering an
  `.eyebrow` header only when it has >1 item), not portfolio-b/portfolio-c's flat `NavItem[]`. This is a
  genuine shape difference the orchestrating task's "just a data-length difference" framing
  under-described — resolved by generalizing the shared `AppLayout`'s prop to `sections:
  NavSection[]` (`NavSection = { title?: string; items: NavItem[] }`), backward-compatible:
  portfolio-b/portfolio-c wrap their existing flat list as a single `{ title: undefined, items }` entry,
  rendering byte-identically (no eyebrow, since the component only renders one when `title` is
  truthy — root's own existing conditional, now promoted into the shared component). Root's
  `brand.mark` slot was already designed forward-compatible for this
  (`packages/shared/ui/src/AppLayout.tsx`'s `brand: { label, mark? }`, task 106's own note) —
  root passes `mark={<span className="brand-mark" />}`, no shape change needed there. See diagram
  2 in `docs/architecture/diagrams/139-root-design-system-convergence.md`.
- **Task 110 (`PageHeader`)** — **un-reversed**, with a similar generalization: root's
  `eyebrow`/`description` props carry real informational content (section context, page
  sub-copy), not decoration — dropping them to force root onto the shared component's plain
  `{ title, actions }` shape would be a content/information-architecture regression, not a visual
  simplification. Resolved the same way as `AppLayout`: `eyebrow?: string` and `description?:
  string` become additive optional props on the shared `PageHeader`, unused (and so
  behaviorally invisible) at portfolio-b/portfolio-c's existing call sites. The title's own styling
  (`font-display text-3xl font-semibold tracking-tight`) converges structurally — with
  `--font-display` gone (§3), it renders in the shared default sans stack, `text-3xl
  font-semibold tracking-tight` unchanged.
- **Task 114 (`mono-font.css`)** — **partially un-reversed, exactly as scoped by the user's
  fuller-convergence framing.** The monospace piece (`--font-mono`, IBM Plex Mono) was already
  3-way shared and is untouched. The piece task 114 explicitly left root-only —
  `--font-display`/`--font-sans` and the `@fontsource-variable/{space-grotesk,sora}` imports —
  now converges too: **deleted outright**, not turned into a per-app token. `design-tokens.css`
  has no `--font-display`/`--font-sans` key in its `@theme inline` block; portfolio-b/portfolio-c's shared
  default is Tailwind's stock sans stack. Root adopts that same absence. `frontend-web/
  package.json`'s `@fontsource-variable/sora`/`@fontsource-variable/space-grotesk` dependencies
  are removed; `frontend-web/src/index.css`'s two `@import` lines for them go too.

**Explicitly NOT reversed, and why (scope boundary, not an oversight):**

- **Chart/exposure color *values*** (`ASSET_PALETTE`, `EXPOSURE_BASE`, `BENCHMARK_COLORS`, etc.
  in root's `lib/chartColors.ts`) — this was already put to the user as an explicit decision on
  2026-08-26 (`docs/architecture/12-portfolio-bpps-and-shared-packages.md` §5, "one file, two
  non-overlapping per-app sections, blessed as intentional") and ruled "not a decision to unify
  the actual palette values." Root's `Exposure` type also has a genuinely different key *shape*
  (7 keys: `QUANTUM`/`BONDS`/`EXPERIMENTS`/`EQUITY`/etc., not just different colors for the same
  5 keys portfolio-b/portfolio-c use) — a data-shape divergence, not a "look" one, same category as
  `PortfolioValueLine`. Out of scope here; stays as-is.
- **`PortfolioSummaryBar`, `PositionsTable`'s `AccountBreakdown`, `EditPositionDialog`/
  `PositionTransferDialog`'s account-model differences** — all already-reviewed structural/domain
  divergences (IKE/IKZE sub-account breakdown vs. flat units, MAX/RETIREMENT portfolio split),
  unrelated to color/font/background/layout-primitive convergence. Untouched.
- **`badge.tsx`'s `tabular-nums`** — orthogonal one-class local override, already a settled,
  deliberate ADR-076 §2.1 exception. Not part of this ADR's scope; left exactly as-is.
- **6th nebula blob** — see §4.

### 3. Font convergence (the interpretation call, restated precisely)

Root's `--font-display: "Space Grotesk Variable", ...` / `--font-sans: "Sora Variable", ...` and
their two `@fontsource-variable` imports are removed entirely. `body`'s `font-family: var(--font-
sans)` and `h1, h2, h3 { font-family: var(--font-display); }` rules in root's `@layer base` are
removed with them (`design-tokens.css`'s own `@layer base` sets neither — the browser/Tailwind
default sans stack applies, identically to portfolio-b/portfolio-c). `--font-mono` (task 114, IBM Plex Mono)
is unaffected — `.prism-number` (root-only, §5) keeps using it.

### 4. Background/blob treatment — converge structurally to 5 blobs

Root's 6th nebula blob (`nebula-blob-6`) duplicates the 5th's treatment (both "restrained gold
accent," mirrored position/timing) — not a distinct piece of visual information, just a second
instance of the same idea. Rather than building a blob-count parameter into `design-tokens.css`
for one redundant decorative element (YAGNI — see pattern table), root converges structurally to
the shared file's 5-blob shape, which already differs from root's old blobs 1-5 in exactly the
way the rest of this convergence intends: a fuller, more saturated multi-hue spread
(`--theme-hue-glow`-tinted blob 1, then fixed magenta/cyan/green/amber blobs 2-5) replacing
root's previous "one dominant violet blob, four muted/near-invisible supporting blobs" treatment.

### 5. Root-only additive layer — `theme-main.css`, beyond the 7 hue variables

Three root-only decorative mechanisms have no portfolio-b/portfolio-c counterpart to converge with — they
are root's own product surface (a specific chart component and page-header content pattern), not
a "look" difference the palette/font/background convergence is about:

- **`PrismValue`** (component, unaffected by this ADR) — its `.prism-number`/`::before`/`::after`
  chromatic-aberration effect and the `prism-converge-cyan`/`prism-converge-magenta` keyframes.
- **`.eyebrow`** — used both by `PageHeader`'s `eyebrow` prop (now shared, §2) and by
  `AppLayout`'s `SidebarNav` section-title rows (§2's `NavSection.title`). Its `color:
  var(--neon-amber)` reference stays valid — `--neon-amber` is defined in both `:root` and
  `.dark` in the shared `design-tokens.css` already.
- **`.brand-mark`** — the small conic-gradient orb, referenced via `AppLayout`'s `brand.mark`
  slot (§2) and root's own (unconverged, out of scope) `LoginPage.tsx`.
- **`table { font-variant-numeric: tabular-nums; }`** — a genuine, root-only base-layer rule
  (portfolio-b/portfolio-c's shared `@layer base` has no equivalent) that keeps numeric table columns
  aligned in a finance app. Worth carrying forward as an additive rule, not dropping it as
  incidental — this affects numeric legibility, not decoration.

These land in `theme-main.css` (scoped under `[data-app-theme="main"]` where they reference
hue-varying tokens; global/unscoped where they don't, e.g. the `table` rule and the keyframes),
alongside the 7 `--theme-hue-*` values and `--theme-chart-1`. This mirrors the precedent ADR-076
already set for `badge.tsx`'s one extra class: a small, local, additive difference layered on a
shared base is not scope creep and does not need new shared-package machinery.

`text-neon-amber` (a CSS rule present in root's current `index.css` but with zero React call
sites, confirmed by repo-wide grep) is dropped with no functional effect — it was already dead
code.

### 6. Verification plan

Headless Chromium is broken on this Pi (repo-wide known constraint) — every verification in this
repo's history has been build-output/bundle-content grep, never a live screenshot. That pattern
applies here exactly as it did for portfolio-b/portfolio-c's half:

- `npm run build --workspace=frontend-web` in investing-app's own workspace: zero TypeScript/CSS
  errors.
- Grep the compiled CSS output (`frontend-web/dist/assets/*.css` inside the rebuilt `nginx`
  image, or the local build output before containerizing) for the `[data-app-theme="main"]` rule
  and the `--chart-1` override, and confirm they match this ADR's §1 values **byte-for-byte** —
  the same check task 125 ran for portfolio-b (`295/278/280/285/300/272/290`, `oklch(68% .32 295)`)
  and portfolio-c (`145/144/142/143/150/148/146`, `oklch(72% .24 165)`). For root, confirm
  `230/218/222/226/245/215/235` and `oklch(68% .32 230)` (or the CSS engine's normalized
  equivalent — compare computed values, not necessarily literal source-text formatting).
  Also grep for the *absence* of `--font-display`/`--font-sans`/`Space Grotesk`/`Sora` in the
  compiled output, and the *absence* of a 6th `.nebula-blob-6` rule.
  Also grep for `--font-display`/`Space Grotesk`/`Sora` in `frontend-web/dist/assets/*.js` (the
  fontsource imports) to confirm they no longer appear in the built bundle at all — a stronger
  signal than a CSS-only check that the dependency was actually removed, not just unused.
- Docker rebuild + redeploy (`docker compose build nginx && docker compose up -d nginx` per
  `infra/`), then `docker exec`-grep the running container's served CSS/JS for the same values,
  matching portfolio-b/portfolio-c's verification precedent exactly.
- **Explicitly not substitutable by any grep, and required before calling this done:** a human
  visual check, light and dark, of every route (12 routes vs. portfolio-b/portfolio-c's 5 — cheaper to skip
  none than to guess which ones the new blob/palette treatment might render oddly on). Grep can
  confirm the *tokens* compiled correctly; it cannot confirm the *result looks intentional* —
  new hue, new saturation, one fewer blob, dropped display font are all changes with real visual
  weight. Flag this to the user explicitly once redeployed; do not mark the task fully done until
  they've looked.

## Consequences

- Root's shipped visual identity changes: from the 2026-07-23 "Prism Ledger" restrained
  single-accent-violet, desaturated, 6-blob treatment to a saturated, multi-neon, electric-blue,
  5-blob "psychedelic" treatment matching portfolio-b/portfolio-c's structural design, with root's own hue.
  This is a deliberate, user-confirmed redesign, not an accident of refactoring.
- `frontend-web/src/index.css` shrinks from 897 lines to the same shape portfolio-b/portfolio-c already
  have: `@import`s (`tailwindcss`, `mono-font.css`, `design-tokens.css`, `theme-main.css`) plus
  `@source` registrations — no color tokens or utility classes remain locally defined in root,
  except what's documented in §5 as root-only additive CSS.
  `packages/shared/ui/src/AppLayout.tsx` and `PageHeader.tsx` gain the generalizations in §2;
  both remain backward-compatible for portfolio-b/portfolio-c (verified by their unchanged build/render, not
  merely asserted).
- `card.tsx` stops being a locally-forked shadcn primitive in root; `badge.tsx` continues to be
  one, deliberately, unrelated to this ADR.
- No backend, API, or DB change of any kind — this ADR is entirely frontend-web/shared-ui scoped.
- Implementation is delegated via `.claude/tasks/139-root-design-tokens-and-font-convergence.task.md`
  (CSS/token/font layer — foundational, must land and be verified first) and
  `.claude/tasks/140-root-shared-component-consumption.task.md` (AppLayout/PageHeader
  generalization + consumption, PageShell portfolio-route migration, card.tsx removal — depends
  on 139). Both name the exact diagram sections in
  `docs/architecture/diagrams/139-root-design-system-convergence.md` to read first.
