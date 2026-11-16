# ADR-076: Shared-Package Extraction Strategy for the Portfolio Bpps

**Date:** 2026-08-20
**Status:** Accepted

## Context

Three sibling repos exist on the same Raspberry Pi host, all Java 25/Spring Boot 3.5 (Maven,
not Gradle — see ADR-014 in `08-monorepo-layout.md`) + React 19/Vite backends, deployed
together via separate `docker compose` stacks fronted by one shared Nginx:

- **investing-app** (this repo) — the user's own multi-currency/multi-exchange portfolio
  tracker. Most complex: crypto rebalancing algorithm, IKE/IKZE retirement accounts, a macro
  backtesting suite. 177 backend files / ~9,809 LOC have zero counterpart in the other two apps.
- **portfolio-b** — portfolio tracker for the user's dad. Single-currency (PLN),
  bucketed by family member × account type.
- **portfolio-c** — portfolio tracker for another family member, forked from
  portfolio-b on 2026-07-30 (`pl.portfolio-b.*` → `pl.portfolio-c.*`), domain simplified to a
  `Portfolio.VARIABLE/FIXED` 2-bucket model.

All three were bootstrapped from the same starting point and share a large amount of
plumbing code (auth stack, HTTP clients, exception handling, response-envelope DTOs, shadcn/ui
primitives, Axios/Zod client scaffolding) that has partially drifted as each app's domain grew
independently. A prior research pass measured file-by-file similarity across all three repos;
this ADR is the follow-up architecture pass verifying the specific extraction candidates named
in that research (not re-deriving the aggregate percentages) before committing to a structure.

**User's decision (made before this ADR, not re-litigated here):** offered a choice between
(a) a full monorepo merge of all three apps, (b) extracting only what's already ~100%
identical with no domain coupling into shared packages while all three apps stay separate
deployables, or (c) fixing only the unrelated Compose project-name collision — the user chose
**(b)**. This ADR concretizes what that means in practice: the physical repo structure, the
exact extraction scope, and the sequencing that keeps all three live, daily-used deployments
(the user, his dad, and another family member, reachable over the public internet via
Tailscale Funnel) unbroken throughout.

### Verified findings (checked directly against the three working trees, this session)

**Confirmed safe — byte-identical or near-identical with no behavioral divergence:**

| File | investing vs portfolio-b | portfolio-b vs portfolio-c |
|---|---|---|
| `shared-ts/src/client/errors.ts` | 0-line diff | 0-line diff |
| `shared-ts/src/utils/currency.ts` | 0-line diff | 0-line diff |
| `shared-ts/src/auth/token-store.ts` | 0-line diff | — |
| `shared-ts/src/schemas/envelope.ts` | 4-line diff (comment only) | 0-line diff |
| `shared-ts/src/auth/token-exchange.ts` | non-zero diff, but only `.describe()` JSDoc text differs — Zod shape identical | — |
| `frontend-web/src/components/ui/button.tsx` (shadcn primitive, sampled) | 0-line diff | — |
| `backend/.../dto/common/{ErrorDetail,ErrorResponse,Meta,PageInfo}.java` | 0-line diff | — |
| `backend/.../dto/common/{ListResponse,PagedResponse}.java` | 2-line diff (package statement only) | — |

**Confirmed real footguns — do NOT extract as-is:**

- **`SecurityConfig.java`**: the backend-issued JWT issuer is a hardcoded string literal
  baked directly into the Java source — `"investing-app"` / `"portfolio-b"` / `"portfolio-c"` —
  not externalized to a property. investing-app additionally has a second, `@Order(1)`
  `SecurityFilterChain` (`newsServiceTokenFilterChain`, guarding `/news/ingest` and
  `/news/assets` for the host-cron news job) that has no counterpart in portfolio-b or portfolio-c at
  all. A verbatim shared copy would either silently share one issuer across all three apps'
  JWTs (a real security boundary weakening — a token minted for one app would validate against
  another) or require every consumer to override it, at which point the "shared" file buys
  nothing. **`SecurityProperties.java`/`GoogleJwtAuthenticationConverter.java`** (the
  allowlist-enforcement mechanism itself) are structurally safe — the allowlist *values* are
  already externalized to `app.security.allowed-emails` / `.env`, only Lombok-vs-hand-rolled
  getters differ — but were left out of scope in this pass anyway, see Decision §2.
- **`GlobalExceptionHandler.java`**: not just longer in investing-app — the *same* exception
  type is mapped to a **different HTTP status per app**. `RebalancingInvariantViolatedException`
  is `400 Bad Request` in investing-app (explicitly documented as intentional — "the invariant
  breaks on an actionable input, not a server fault") and `500 Internal Server Error` in
  portfolio-b. investing-app also has two `@ExceptionHandler` methods
  (`PlanSessionAlreadyExistsException`, `SessionAlreadyDoneException`) with no portfolio-b/portfolio-c
  counterpart. This is genuine behavioral divergence, not incidental duplication — extracting
  this file would either change portfolio-b's response codes out from under it or force a
  base-class/override design not worth building for 3 consumers.
- **`YahooFinanceClient.java`, `BinanceClient.java`, `PriceCacheService.java`,
  `FxRateCacheService.java`, `AuthService.java`**: real, substantive per-app diffs (37, 34–53
  lines each, out of 100–250-line files) — these have grown independently, not just been
  copy-pasted. `YahooFinanceClient.java` in particular is 429 lines in investing-app vs 337 in
  portfolio-b (investing-app's crypto/macro price-fetching needs grew it). Not extraction
  candidates in this pass.
- **`shared-ts/src/client/axios-instance.ts`**: `baseURL` is a hardcoded per-app path prefix
  (`"/api"` vs `"/portfolio-b/api"`, driven by the Nginx path-prefix routing in
  `infra/nginx/nginx.conf`) — genuinely load-bearing, cannot be shared as a static file.
  portfolio-b's copy also wires the 401 → `triggerUnauthenticated()` interceptor and Bearer-token
  attachment inline; investing-app's does not (wired elsewhere in that app). This file stays
  per-app.
- **shadcn `ui/` design tokens / `index.css`**: investing-app's `index.css` is 888 lines vs
  753 in portfolio-b and portfolio-c (the Prism Ledger redesign, memory:
  `frontend-prism-ledger-redesign.md`) — and portfolio-b vs portfolio-c themselves already differ by 224
  lines despite forking three weeks apart. Only the *component primitives*
  (`components/ui/*.tsx`, which read CSS custom properties rather than hardcoding colors) are
  shareable; the token values themselves are not, and must not be forced to converge.

**Independent, unrelated footgun also confirmed this session:** `infra/compose.yml` in both
investing-app and portfolio-b has no `name:` field, so both default to the Compose
project name `infra` (the containing directory's basename) — a live collision risk for any
`docker compose` command (`down`, `ps`, `ls`) whose project-name resolution isn't otherwise
disambiguated. portfolio-c already fixed this in July (`name: portfolio-c` at line 12 of its
`infra/compose.yml`). This is unrelated to code deduplication — it touches the same `infra/`
files as the DevOps work below, but has nothing to do with shared packages.

## Decision

### 1. Physical structure: a new small vendor repo, git-subtree-mounted — not a monorepo merge

Create a new, dedicated repository, **`shared-app-packages`**, containing only the extraction
scope in §2 below. It is *not* merged into any of the three app repos' git history. Each of
the three app repos vendors it as a single subtree mount at `packages/shared/`:

```bash
# one-time, per app repo:
git subtree add --prefix=packages/shared <shared-app-packages-url> main --squash

# after any later change lands in shared-app-packages:
git subtree pull --prefix=packages/shared <shared-app-packages-url> main --squash
```

Rejected alternatives, and why:

- **Full monorepo merge** (`git subtree add` merging all three apps' histories into one repo
  under `apps/*`) — already rejected by the user's own choice before this ADR. Restated
  briefly for the record: it would permanently bloat investing-app's repo with two more full
  histories, require re-pointing every host-side deploy path (`boot.sh`, the per-repo
  `docker compose -f infra/compose.yml` invocations run from each repo's own directory on the
  Pi), and buys nothing the user actually asked for — they explicitly want the three apps to
  stay separate deployables. No CI exists on any of the three repos today (no `.github/`
  workflow files, no configured git remotes yet) that a merge would need to reconcile, so
  there's no CI-consolidation upside either.
- **Plain external `file:` path reference with no vendoring** (each app's `package.json` /
  `pom.xml` points at a sibling directory like `../../shared-libs/ts-common` on the same host
  filesystem, no copy into the repo) — rejected because a plain `git clone` of any one app
  repo would no longer be buildable on its own; it would silently depend on an undocumented
  sibling checkout at a fixed relative path, which breaks the moment repos aren't laid out as
  siblings (a fresh clone to a new machine, a renamed directory). It also does not solve the
  Docker-build-context problem any better than vendoring does — the shared source still has to
  physically exist inside whatever directory is passed as the Docker build context.
- **Git submodules** — rejected for solo-maintainer friction: submodules require an explicit
  `git submodule update --init` step on every fresh clone (easy to forget, breaks a plain
  `git clone && docker compose up` flow on the Pi), and put the nested repo in detached-HEAD,
  which is awkward when the same person occasionally needs to edit shared code from inside a
  consuming app's checkout. `git subtree` gives ordinary, directly-editable files with an
  explicit, auditable sync command (`subtree pull`/`push`) instead.
- **"Periodically synced copies" with a documented source-of-truth convention** (manual
  copy-paste, no tooling) — this is exactly the status quo that produced the drift measured in
  Context above (compare `PriceCacheService.java`'s 34-line drift to `dto/common`'s 0–2-line
  drift: unmanaged copies rot). `git subtree pull` costs one command more than `cp -r` and
  leaves a real commit recording when and what was synced — strictly better at equal effort.

### 2. Extraction scope — concrete, file-level

**In scope, Phase 1 (TypeScript, all three apps):** two packages inside
`shared-app-packages`, kept separate so UI primitives (which depend on Tailwind/Radix/CVA)
don't drag in business-logic dependencies (axios/zod) and vice versa:

- **`ts/` → npm package `@shared/app-core`**: `client/errors.ts`, `schemas/envelope.ts`,
  `auth/token-store.ts`, `auth/token-exchange.ts` (adopt investing-app's version — it's a
  strict superset with `.describe()` documentation, functionally identical Zod shape),
  `utils/currency.ts`. **Explicitly excluded:** `client/axios-instance.ts` (per-app `baseURL`
  + interceptor wiring, confirmed load-bearing above) stays a small per-app file in each
  app's own `shared-ts/`; it may import `ApiError` from `@shared/app-core` but keeps its own
  base URL and interceptor logic.
- **`ui/` → npm package `@shared/ui-primitives`**: 17 of investing-app's 22 `components/ui/*.tsx`
  files, verified during execution by diffing every candidate individually against portfolio-b
  (not just sampling `button.tsx`, as originally planned here). 16 are byte-identical;
  `popover.tsx` differs only in blank lines. **`badge.tsx` and `card.tsx` are excluded** — both
  carry genuine Prism Ledger Tailwind class additions (`tabular-nums` on badge, `font-display`
  on card), not just whitespace, so they stay local to each app. investing-app's three
  additional components with no portfolio-b/portfolio-c counterpart at all (`radio-group.tsx`,
  `slider.tsx`, `switch.tsx`) also stay local. **Design tokens (`index.css`, Tailwind theme
  config) are explicitly excluded** — each app keeps its own, per the confirmed divergence
  above.

  **Execution note (added post-implementation):** rather than rewriting every call site that
  imports `@investing-app/shared` (~43 files) or `@/components/ui/*` (~36 files) to point at
  the new packages, the old file paths were kept as one-line re-export shims (e.g.
  `frontend-web/src/components/ui/button.tsx` now reads
  `export * from "@shared/ui-primitives/button";`). This removes the duplicated *content* —
  each of these files has exactly one real implementation, in `@shared/app-core` or
  `@shared/ui-primitives` — without touching any consumer import, which eliminates the
  primary source of risk in this phase. Verified end-to-end: `npm run build --workspace=frontend-web`
  (zero TS errors), Docker rebuild + redeploy of `investing-nginx-1`, root serving 200,
  `index.css` unchanged at 888 lines, and `/api/positions` / `/api/actuator/health` reachable
  through the rebuilt container.

**Deferred / optional, not part of this phase — Java `dto/common`:** the response-envelope
DTOs (`ErrorDetail`, `ErrorResponse`, `ListResponse`, `Meta`, `PagedResponse`, `PageInfo`,
~150–200 LOC total, 0–2-line diff today) are the *only* Java-side files that measured as
genuinely safe. They are deliberately **not** scheduled in this phase: extracting them
requires changing all three backends' Docker build context from `backend/`-scoped to
repo-root (see §3's risk note) — real Dockerfile surgery across three services for ~150 lines
of payoff. Revisit only if the backend Dockerfiles are already being touched for an unrelated
reason, or if the shared scope grows enough later to justify the build-context change on its
own. **Every other Java class checked this session — `SecurityConfig`, `SecurityProperties`,
`GoogleJwtAuthenticationConverter`, `GlobalExceptionHandler`, `YahooFinanceClient`,
`BinanceClient`, `PriceCacheService`, `FxRateCacheService`, `AuthService` — stays app-local
indefinitely**, per the confirmed footguns in Context above. This is not a "not yet" — it is a
considered "no" for the security-boundary and behavioral-divergence classes (`SecurityConfig`,
`GlobalExceptionHandler`), and a "not worth the design cost for 3 consumers" for the
HTTP-client/cache-service classes.

### 3. Docker build-context impact (the single biggest practical risk)

- **`infra/nginx/Dockerfile`** (all three apps): build context is already the repo root, and
  it already `COPY`s `shared-ts/` alongside `frontend-web/` before `npm install` — this is the
  existing pattern for local-workspace TS packages. Extending it to also `COPY packages/shared/ts`
  and `packages/shared/ui`, and adding them to the root `package.json`'s `workspaces` array, is
  a small, low-risk, already-proven change.
- **`infra/backend/Dockerfile`** (all three apps): build context is currently **`backend/`
  only** (`COPY pom.xml .` copies from the context root, which is the `backend/` directory
  itself) — there is no existing pattern for pulling in anything outside `backend/`. This is
  exactly why Java-side extraction is deferred in §2: doing it would require changing the
  Compose `context:` for the `backend` service from `../backend` to the repo root, rewriting
  the Dockerfile to `COPY packages/shared/java/` and `mvn install` it before `backend`'s own
  `package` step, in all three apps' `infra/backend/Dockerfile`. Not done in this phase.

### 4. Sequencing — never break a live deployment

1. **Compose project-name collision fix** (independent of everything else, do first or in
   parallel — it touches `infra/compose.yml` in investing-app and portfolio-b only,
   is a one-line addition (`name: investing` / `name: portfolio-b`), and has zero code-dedup
   dependency). See `.claude/tasks/76-compose-project-name-collision-fix.task.md`.
2. **Prove Phase 1 in investing-app alone before touching portfolio-b or portfolio-c.** Scaffold
   `shared-app-packages`, subtree-mount it into investing-app only, rewire investing-app's
   imports to consume `@shared/app-core` / `@shared/ui-primitives`, delete the now-duplicated
   originals *from investing-app only*, rebuild, and verify (see §5) before portfolio-b or portfolio-c's
   repos are touched at all. investing-app is the most complex consumer (Prism Ledger design
   system, the largest `index.css`), so if extraction works cleanly there without disturbing
   its styling, it will work for the simpler siblings.
3. **Propagate to portfolio-b, then portfolio-c, one at a time**, each independently verified before
   moving to the next. Never touch both in the same working session without confirming the
   first one redeployed clean.
4. **Java-side (`dto/common`) stays deferred** — no sequencing needed until the user decides
   it's worth the Dockerfile rework in §3.

### 5. Verification approach (headless Chromium is broken on this Pi — do not attempt
screenshot verification, per `headless-chromium-broken-on-this-pi` memory)

For each app, after rewiring imports and before deleting the old in-repo copies:

- `npm run build --workspace=frontend-web` (or the app's equivalent) must succeed with zero
  TypeScript errors — this alone catches a broken import path or a type shape mismatch from
  the extraction.
- Rebuild the actual Docker images (`docker compose build backend`, `docker compose build
  nginx` per that app's own rebuild instructions) and tail startup logs
  (`docker logs -f infra-backend-1` equivalent) for a clean `Started ...Application` line —
  not just a local build.
- Smoke-test via `curl` against the deployed container: at minimum, `GET /actuator/health`
  (unauthenticated) and one authenticated `GET` (e.g. `/api/positions`) with a valid bearer
  token, per app, to confirm the auth stack and response envelope still round-trip correctly
  through whatever did or didn't change.
- Only delete the old, now-duplicated in-repo copies once the rebuilt, redeployed container has
  been smoke-tested clean. Never delete-then-verify.

## Consequences

- Each of the three app repos stays independently `git clone`-able and buildable with only an
  extra one-time `git subtree add` — no new registry, no paid service, no submodule
  init step, consistent with the cost-free constraint.
- `shared-app-packages` becomes a fourth repo the user maintains, with its own (small) release
  discipline: a change there only reaches a consuming app after an explicit
  `git subtree pull`, so drift is possible again if that step is skipped — but it is now an
  auditable, deliberate git operation instead of silent copy-paste rot.
- The scope deliberately stays small (2 TS packages; Java dto/common deferred). This is a
  conscious trade against a larger extraction: every Java class that looked identical at a
  glance turned out to have a real per-app divergence once diffed line-by-line, and forcing
  those into a shared abstraction now would cost more design effort than the ~9,800 LOC of
  investing-app-only surface justifies for 3 total consumers.
- `infra/backend/Dockerfile`'s per-app `context: ../backend` scoping is untouched by this
  phase — it remains a known, deliberately deferred piece of follow-up work if Java-side
  sharing is ever revisited.
- No structural git operation runs from this ADR alone — per the task that produced it, a
  human confirmation gate happens after this plan (and the task files below) before
  `shared-app-packages` is created or any `git subtree add` is run.

See `.claude/tasks/76-*.task.md` through `79-*.task.md` for the executable breakdown, and
`docs/architecture/12-portfolio-bpps-and-shared-packages.md` for the living architecture record
of the target topology.
