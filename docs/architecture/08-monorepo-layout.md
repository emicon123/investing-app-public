# 08 — Monorepo Layout

> **Audience:** All agents. Canonical directory structure as it exists on disk today (as-built,
> 2026-09-19). The Java/Spring Boot backends were **deleted** in Phase 9 of the Go rewrite
> (ADR-098, commit `ff7878f`); every app runs on Go. The last commit that still contains the Java
> trees is `2be75ba` — `git checkout 2be75ba -- backend apps/portfolio-b/backend
> apps/portfolio-c/backend packages/shared/java pom.xml` restores them if ever needed.
>
> **Rename (2026-10-24, ADR-105 / task 169):** the cross-app code folder `packages/shared/` (npm
> `@shared/*`, Go module `investing-app/shared-go`) is now `packages/common/` (`@common/*`,
> `investing-app/common-go`). Older ADRs, task files and diagram write-ups keep the old names; read
> them as the new ones. The deleted Java tree really was `packages/shared/java` (that path is correct
> only at commit `2be75ba` and earlier).

---

## Top-Level

```
investing-app/
├── go.work             Go workspace (tracked) — the 4 Go modules below; `go test` resolves common-go from source
├── apps/
│   ├── main/                 Main app (this repo's own portfolio tracker + platform owner):
│   │   ├── backend-go/       Go API (module `investing-app/backend-go`, chi + pgx + sqlc + oapi-codegen)
│   │   ├── frontend-web/     React 19 + Vite 6 SPA (`src/api/` holds this app's Axios clients +
│   │   │                     Zod schemas + enums — moved unmerged out of the deleted `shared-ts/` by task 158)
│   │   └── db/               Schema source of truth
│   │       ├── migration/    Flyway migrations V1–V71 and V74–V209 for database `investing` on 2026-11-04 (177 adds V210); nothing but V*.sql in here; V72/V73 are parked
│   │       ├── unapplied/    PARKED V72/V73 + README — scanned by nothing (Flyway, sqlc, tests)
│   │       └── README.md
│   ├── portfolio-b/   backend-go/ (+ internal/{book,store} after batch 180), db/migration/ (V1–V6, V8–V20; 177 adds V21), frontend-web/, infra/, docs/
│   └── portfolio-c/    backend-go/ (+ internal/{book,store} after batch 180), db/migration/ (V1–V14; 177 adds V15),        frontend-web/, infra/, docs/
│   (no `shared-ts/` anywhere — deleted by task 158; per-app API code lives in each `frontend-web/src/api/`)
├── packages/common/
│   ├── go/common-go/   Common Go module `investing-app/common-go` — consumed by all three backend-go modules
│   │   └── siblingcore/  portfolio-b + portfolio-c service core (ADR-113, batch 180): root types + position/, transfer/,
│   │                     rebalancing/, snapshot/, deposit/ (+ refdata/, wire/ if waves 5–6 run); main never imports it
│   ├── ts/             Common TypeScript (npm workspace @common/app-core: auth, envelope, errors,
│   │                     Axios singleton, hooks, routes — domain clients live per-app, not here)
│   └── ui/             Common UI (npm workspace @common/ui-primitives): shadcn primitives, shared
│                         feature components (portfolio/ PortfolioView, AppLayout, dialogs, treemap),
│                         design-tokens.css and the self-hosted fonts (IBM Plex Sans + Mono)
├── infra/              Root-app Docker Compose + Nginx (also fronts the two sibling apps), bot host
│                       scripts, .env templates — plus `shared/` (below), the one shared PostgreSQL stack
│   └── shared/         Separate compose stack (`name: shared-infra`): the one shared PostgreSQL 17
│                       (`db-shared`, databases investing/portfolio-b/portfolio-c) — ADR-083. Own lifecycle: no
│                       app's `docker compose down` in `infra/` or `apps/*/infra/` can touch it
├── docs/archive/       One-off historical repair scripts (e.g. fix_session29_damage.sql — not a Flyway migration)
├── backtest/           Standalone Python backtesting scripts (macro/on-chain research; not part of the deployed app)
│   └── lib/ fetch/ studies/ + data/ results/ (task 159: shared helpers, data fetchers, studies)
├── docs/               Architecture docs (this directory) + domain reference material
│   ├── architecture/   The numbered living docs (00–06, 08, 10, 12, 13) + diagrams/
│   ├── adr/            ADR files — a scratchpad for decisions; consequences are folded into the numbered docs
│   ├── finanse-domain-model.md
│   ├── openapi/        Multi-file OpenAPI 3.1 SOURCE (edit these, not the bundle below)
│   ├── openapi.yaml    Generated single-file bundle — `npm run openapi:bundle` regenerates it; never hand-edit
│   └── *.csv           Source spreadsheet exports (reference only; do not touch)
├── apps/
│   ├── main/                   backend-go/, db/migration/ (V1–V71, V74–V209; 177 adds V210), frontend-web/, src/api/ (no infra/ — runtime is root infra/)
│   ├── portfolio-b/   backend-go/ (+ internal/{book,store} after 180), db/migration/ (V1–V6, V8–V20; 177 adds V21), frontend-web/, infra/, docs/
│   └── portfolio-c/    backend-go/ (+ internal/{book,store} after 180), db/migration/ (V1–V14; 177 adds V15),        frontend-web/, infra/, docs/
├── .github/workflows/go-tests.yml   CI: `go test ./...` (no -short) for the 4 Go modules, Testcontainers + the prod Flyway image
├── .claude/            Agent task files (`tasks/`, `tasks/done/`), agent definitions (`agents/`), worktrees
├── boot.sh             Host-level startup script
├── package.json        Root npm workspace manifest — workspaces: packages/common/{ts,ui}, frontend-web, apps/*/frontend-web (the `shared-ts` / `apps/*/shared-ts` entries were deleted with those dirs by task 158)
└── AGENTS.md           Project instructions (auto-loaded by all Claude sessions)
```

There is **no** Maven/Gradle build anywhere: no `pom.xml`, `mvnw`, `.mvn/`, `packages/shared/java/`, and no
`backend/` directory. There is no standalone top-level `migration/` directory either — the one-time
Google Sheets → DB ETL job was a bespoke, throwaway process rather than a persisted package.

**Monorepo tooling (ADR-004):** two independent toolchains, no cross-language build tool tying them
together. npm workspaces cover the TypeScript packages (`shared-ts`, `frontend-web`, the shared
`packages/common/{ts,ui}` and both siblings' copies); `go.work` covers the four Go modules
(`backend-go`, `apps/portfolio-b/backend-go`, `apps/portfolio-c/backend-go`,
`packages/common/go/common-go`). Each `backend-go` module also carries a relative `replace`
directive to common-go, so the Docker build (which copies only the two modules an image ships) does
not need `go.work`.

---

## `apps/main/backend-go/` (sibling layout identical under `apps/<name>/backend-go/`)

Go 1.27, single static binary (`CGO_ENABLED=0`), `distroless/static-debian12:nonroot` runtime image
on `linux/arm64`. Stack: chi router, pgx v5, sqlc-generated queries, oapi-codegen-generated
`ServerInterface`/models from `docs/openapi.yaml`, `shopspring/decimal` for money,
testcontainers-go for tests. The main app's binary listens on `:8091` (portfolio-b `:8092`, portfolio-c `:8093`),
reachable only from Nginx over the `investing-shared` Docker network (no host port).

```
apps/main/backend-go/
├── go.mod / go.sum            module investing-app/backend-go (replace → ../../../packages/common/go/common-go)
├── sqlc.yaml                  schema = explicit ordered list of ../db/migration/V*.sql (see hack/), queries = db/queries
├── cmd/server/                main.go (wiring, schedulers, graceful shutdown), botauth.go (smartAuth: JWT or bot service token)
├── openapi/                   server.gen.go — GENERATED by `go generate` from ../../../docs/openapi.yaml; never hand-edit
├── db/
│   ├── queries/*.sql          Hand-written sqlc queries, one file per domain (position, deposit, historia, bot_*, ...)
│   ├── sqlcgen/               GENERATED sqlc output (CI fails on drift)
│   └── migration_test.go      Applies db/migration through the prod Flyway image
├── hack/gen-sqlc-schema-list.sh   Regenerates sqlc.yaml's explicit schema list (Flyway's unpadded V1..V75 sorts wrongly lexicographically)
└── internal/                  One package per domain; handler.go (HTTP ↔ domain DTO), service.go (use cases), <domain>.go (types/rules).
                               In portfolio-b and portfolio-c after batch 180, internal/{position,positiontransfer,rebalancing,historia,deposit}
                               hold the handler only (plus rebalancing/funding.go): the services live in common-go/siblingcore and
                               internal/store is the one sqlc adapter (ADR-113)
    ├── position/  purchasesession/  rebalancing/  rebalancingcalc/   Rebalancing + purchase planning (strategy funcs, macro de-risk)
    ├── deposit/  cashpositions/  cryptotrades/  binancetransfers/  taxreturns/  inflation/
    ├── historia/  linearregression/  tradechart/  news/  settings/  authexchange/  marketdata/(macro/)
    └── bot/{calendar,control,plan,signal,stats,trade}/   Trading-bot API + OKX execution + schedulers
```

**Layering (per domain package):** handler (decodes/encodes the generated OpenAPI DTOs, returns the
`{data, meta}` envelope via `common-go/httpx`) → service (transaction boundaries, orchestration) →
sqlc `Queries` over a `pgxpool` (rows are pgx/sqlc structs, mapped by hand to domain types and then
to API DTOs; no ORM, no MapStruct). Pure rebalancing/valuation math lives in `common-go/rebalancecalc`
and `backend-go/internal/rebalancingcalc` so it is testable without a database; asset-level computed
fields (`GroupProfit`, `CurrentAndDeviationPct`) live in `common-go/positioncalc`; the asset-level cost
kernel is `common-go/costbasis` (pure) with its in-memory write-back edge `common-go/costmember`. XIRR
(deposits page and historia snapshots, all three apps) is the one `common-go/xirr.Solve` (Brent, the
valuation date always passed in by the caller, ADR-112). Handler/
service micro-helpers are shared, not copied: `common-go/decx` (decimal ⇄ float64 DTO mapping, 0–100 range
check), `httpx.DecodeJSON`, `pgerr.NotFoundOr`, `pgnum.Date` (task 163, implemented by batch 179).
Market-data and settings endpoints are served by the common `marketdatacore.Handler` and
`settingscore.Handler`; each app's generated `ServerInterface` method forwards to them. Errors stay
`httpx.AppError` app-wide (a deliberate choice, task 174 option A); the one exception is the pure
calculation package `rebalancingcalc`, which returns its own `UnavailableError` (no `httpx`
import) that `httpx.WriteError` answers as `503 SERVICE_UNAVAILABLE`. Request bodies decode money
straight into `shopspring/decimal` (never `float64` → `NewFromFloat`); responses keep JSON numbers.
The full common-go package inventory is in `12-portfolio-bpps-and-shared-packages.md`.

**Runtime rules (all three backends, task 174 batches 176–177, ADR-110/111):**
- *Lifecycle:* `cmd/server/main.go` is `main() → run() error`; `os.Exit` happens once, in `main`.
  Background jobs run in a `common-go/lifecycle.Group` (derived context, awaited on shutdown, a panic
  in one tick is recovered by `lifecycle.Guard` and costs only that tick). `lifecycle.Run` binds
  the port synchronously, then on SIGTERM stops the workers, drains HTTP (15 s) and waits for the
  workers (10 s); compose gives 30 s (`stop_grace_period`). **Exception:** main's bot goroutines
  (`calendar.StartScheduler`, `trade.StartWatcher`) are plain `go` statements on the signal context
  and are not awaited (owner decision 2026-11-04).
- *Transactions:* only through `common-go/pgtx.Runner` (built on `pgx.BeginFunc`, rollback
  deferred). Every transaction that reads and then writes positions, deposits, purchase lots,
  transfer records or purchase-session completion uses `pgtx.SerializedRunner`, whose first
  statement takes the database-wide advisory lock `pgtx.MoneyWriteLockKey`; the reads that feed the
  write happen inside that transaction. Money writes of one app therefore never overlap.
- *Idempotency:* `common-go/idempotency.Middleware` in the authenticated route group deduplicates
  any write carrying `Idempotency-Key` (records in `idempotency_keys`, 24 h; `04-db-schema.md`);
  the web clients send one key per dialog submission on the money POSTs (`03-api-contract.md`).
- *Pool:* `config.ConnectDB` — `MaxConns 10`, health check 30 s, server-side `statement_timeout`
  60 s and `idle_in_transaction_session_timeout` 5 min.
- *HTTP server:* `httpx.NewServer` sets ReadHeader 5 s / Read 30 s / Write 190 s / Idle 120 s;
  chi's per-request timeout stays 30 s (180 s for the two long POSTs). `GET /api/health-go` is a
  readiness check (DB ping, 503 when down) used by the compose healthcheck
  (`/app/backend-go healthcheck`, exec form — the image has no shell); `GOMEMLIMIT=48MiB` under
  `mem_limit: 64m`.
- *Request ids:* `httpx.RequestID` sets `X-Request-Id`; the access log, the "unhandled error" log
  and every envelope's `meta.requestId` carry the same UUID.

**Schedulers (in-process `lifecycle.Group` workers started from `cmd/server/main.go`):** macro
refresh (Fri 18:00 / 1st 17:30 / 5th 18:30), inflation sync (GUS, `common-go/inflationsync` in all
three apps), price cache refresh every 15 min (`common-go/marketdatacore`, at most 4 concurrent
Yahoo/Binance fetches), historia nightly snapshot 23:00 Europe/Warsaw (`common-go/historiascheduler`,
`HISTORIA_SCHEDULER_ENABLED=true`, ADR-097), plus the bot calendar/watcher/purger
(`BOT_SCHEDULER_ENABLED=true`, plain goroutines outside the group — see Runtime rules). The bot's live OKX execution was armed
on 2026-09-19 (real account, `OKX_DEMO_MODE=false`, `BOT_LIVE_EXECUTION_ENABLED=true`, risk limits =
`DefaultExecutionConfig` defaults in `internal/bot/trade/config.go`, ported verbatim from the old Java `application.yml`).

**Auth:** Google ID token → `POST /auth/token` (`internal/authexchange` + `common-go/auth`) issues the
backend HS256 JWT; every other route requires it. The root app additionally accepts a static
service token for host-cron callers (news ingest, bot scripts) through `smartAuth`. There is no user
table and no session store (single-user, allowlist by `ALLOWED_EMAIL_*`).

**Flyway migrations:** one `V{n}__{description}.sql` per schema change, monotonically numbered,
never edited after being applied. `apps/main/db/migration/` currently runs `V1__init.sql` … `V71__…` plus `V74`–`V77`
(75 files; V72/V73 are parked in `db/unapplied/`); portfolio-b `apps/portfolio-b/db/migration/` V1–V6, V8–V19 (18 files; V7
`account_balances` exists only in the live DB — a squashed-subtree loss, task 134); portfolio-c
`apps/portfolio-c/db/migration/` V1–V10. Treat the directories themselves as the source of
truth for the exact list — do not hardcode a version count elsewhere. Migrations are applied by the
one-shot Flyway runner (see `infra/` below), **not** by the application at startup. Notable
milestones: `V26__drop_monthly_entries.sql` removed the monthly-entries feature entirely (no table,
route or endpoint — do not reintroduce it from memory of older docs); `V33__macro_derisk.sql` /
`V37__sliding_macro_regime_targets.sql` back the macro de-risk gate; `V74__asset_subasset_model.sql` /
`V75__drop_companion_columns.sql` (portfolio-b V18/V19, portfolio-c V9/V10 — task 156) introduced the `assets` table and
dropped the companion columns (`04-db-schema.md`).

**Parked migrations:** `db/unapplied/V72__bot_hard_close_timeout.sql` and
`V73__bot_trade_action_execution.sql` (ADR-095 / ADR-096) are intentionally *not* applied — no Go code
uses their columns yet. They stay parked until those designs are ported to Go; when un-parked they
must be renumbered if any V74+ was applied in the meantime (`db/unapplied/README.md`).

**Known follow-up (not built):** BLS direct CPI/PPI actuals (ADR-093, task 147) existed only in the
deleted Java backend and has no Go port yet.

---

## `apps/main/frontend-web/` (sibling layout identical under `apps/<name>/frontend-web/`)

React 19, Vite 6, TypeScript 5, Tailwind v4, shadcn/ui, TanStack Query v5. Package name: `@investing-app/frontend-web`.

```
apps/main/frontend-web/
├── package.json
├── vite.config.ts
├── tsconfig.json
├── index.html
└── src/
    ├── main.tsx
    ├── App.tsx                    Router + AppLayout wrapper
    │
    ├── pages/
    │   └── LoginPage.tsx           /login — the only page-level component left outside routes/
    │
    ├── routes/                    One directory per route; route-local components live in a `_components/` subfolder
    │   ├── portfolio/index.tsx     /portfolio — one line: <MainPortfolioScreen portfolio="MAIN"/>
    │   ├── retirement/index.tsx    /retirement — one line: <MainPortfolioScreen portfolio="RETIREMENT"/>
    │   ├── purchases/
    │   │   ├── index.tsx           /zakupy — session create/actuals/complete
    │   │   └── _components/        ActiveSessionView, HistorySection, NoSessionView, PortfolioItemsTable
    │   ├── regression/index.tsx    /regression — log-scale regression + DeMarker chart
    │   ├── deposits/index.tsx      /deposits — deposit log + IKE/IKZE caps
    │   ├── history/index.tsx       /historia — portfolio value area chart
    │   ├── crypto/
    │   │   ├── index.tsx           /crypto — trade log (no delete)
    │   │   ├── tax.tsx             /crypto/tax — annual PIT-38 summary
    │   │   └── _components/        BinanceTransfersTable, MexcBotDepositsTable, TradeEntryForm, TradesHistoryTable
    │   ├── mexc-bot/index.tsx      /mexc-bot — MEXC bot status/deposits/performance
    │   ├── news/
    │   │   ├── index.tsx           /news — daily asset news feed
    │   │   └── _components/        NewsCard
    │   └── settings/
    │       ├── index.tsx           /settings — cash snapshot, macro de-risk state, contribution
    │       └── _components/        MacroDeriskSection, MacroStatePanel
    │   (no `routes/monthly/` — the monthly-entries feature was removed in V26; do not re-add it)
    │
    ├── components/                 Shared, cross-route UI components
    │   ├── ui/                     shadcn/ui primitives
    │   ├── charts/                 AllocationTreemap.tsx (re-exports common AssetAllocationTreemap),
    │   │                            HistoriaDialog.tsx, PortfolioValueLine.tsx
    │   ├── portfolio/               MainPortfolioScreen.tsx (container for both books, renders the
    │   │                            common PortfolioView), BotStatusBadge.tsx, MacroRegimePanel.tsx
    │   ├── tables/                  DepositsTable.tsx
    │   ├── AppLayout.tsx            Wrapper over the common AppLayout (nav sections, brand mark)
    │   ├── CreatePositionDialog.tsx, EditPositionDialog.tsx, AssetActionDialogs.tsx
    │   ├── RebalancingTransferDialog.tsx   Sell dialog (wrapper over the common PositionTransferDialog)
    │   ├── NebulaCanvas.tsx         Login-page background only
    │   ├── PageHeader.tsx           Route title row (eyebrow, description, actions)
    │   ├── ProtectedRoute.tsx       Google-OAuth route guarding
    │   └── ErrorBoundary.tsx
    │
    ├── hooks/                       useAssetActions, usePortfolioKpis (binding), useRefreshPrices
    └── lib/                         queryKeys, routes, auth, assetIcons (re-export), chartColors, …
```

**Structural note:** the page-per-route model documented previously (a flat `pages/` directory with names like `PortfolioPage.tsx`) has been superseded by a `routes/` directory, one subdirectory per route, with route-local components nested under `_components/`. Only `LoginPage.tsx` still lives directly under `pages/`.

**Charts:** All charts use Recharts 2.x. No TradingView Lightweight Charts library is installed.

**State:** All server state via TanStack Query. Zustand (`usePositionsStore`, `useSettingsStore`) is installed from `shared-ts` but unused — no Zustand `useStore` calls in route components.

---

## `shared-ts/` — DELETED (task 158, 2026-09-22; this section is the removal record)

The three per-app workspaces (`shared-ts/`, `apps/*/shared-ts/`, packages
`@investing-app/shared` / `@portfolio-b/shared` / `@portfolio-c/shared`) no longer exist. End state:
- `packages/common/ts/` (`@common/app-core`) owns the only Axios singleton (`client/axios-instance.ts`,
  baseURL injected once per app via `configureApiClient` in each `main.tsx`: `/api`, `/portfolio-b/api`,
  `/portfolio-c/api`) plus the domain-free plumbing (auth incl. `AuthUser`, envelope, errors, currency,
  hooks, routes, bootstrap).
- Each frontend owns `<app>/src/api/{client,schemas}/` — its former shared-ts content moved UNMERGED
  (a union was rejected: same-named enums carry different VALUES per app, e.g. root
  `MAIN|RETIREMENT`+IKE/IKZE vs portfolio-c `VARIABLE|FIXED`; deposits/historia/market-data diverge at
  domain level — ADR-076(b)). Per-app `api/index.ts` barrels preserve the old one-import ergonomics.
- Dropped, not moved: per-app `axios-instance.ts` (replaced by injection), `errors`/`envelope`/
  `currency`/`token-store`/`token-exchange` copies (app-core canonical), verified-unused Zustand stores.
- Rationale for per-app colocation over a union package: single-consumer code cannot drift when it
  lives with its consumer. Full disposition table + rewiring rule: `.claude/tasks/done/158-*.task.md`.

---

## `infra/`

```
infra/
├── compose.yml            Docker Compose v2 (project `investing`): flyway (one-shot), backend-go, nginx, pgadmin (profiles: [dev])
├── .env                   Actual credentials (gitignored)
├── .env.example           Template (committed)
├── fix_session29_damage.sql → moved to docs/archive/ (one-off manual data-repair, not a Flyway migration)
├── backend-go/
│   └── Dockerfile         golang:1.27-alpine builder → gcr.io/distroless/static-debian12:nonroot; ARM64; build context = repo root
├── bot/                   Host-side trading-bot scripts (daily-plan, scalp-launcher, manage-position, calendar sync) + prompts (logs/locks gitignored)
├── news/                  Host-cron news-research job (ADR-041): news-research.sh, prompt.md, README.md (log gitignored)
├── ci/                    Task-164 ephemeral JIT runner: run-once.sh + poller.sh (cron */3), driven by `.github/workflows/deploy-pi.yml` (push-main-only)
├── shared/                The shared-Postgres stack (project `shared-infra`, NOT part of `compose.yml`
│                          above): `compose.yml`, `.env` (gitignored), `init/01-init-databases.sh`,
│                          `backups/` (gitignored `*.dump` rollback copies) — own lifecycle, see below
└── nginx/
    ├── nginx.conf         Serves the built SPA at /; proxies /api/* to backend-go:8091 and, for the siblings, /portfolio-b/* and /portfolio-c/*
    │                      to backend-go-portfolio-b:8092 / backend-go-portfolio-c:8093 (variable `proxy_pass` + Docker resolver — re-resolves on container recreate)
    ├── limits.conf
    └── Dockerfile         Multi-stage: node:24-alpine builds packages/common/{ts,ui} + frontend-web, then nginx:1.31-alpine serves the output
```

The compose file's `nginx` service is the one that builds and serves `frontend-web` (there is no
separate `frontend` service name) — its Dockerfile build context is the repo root so it can pull in
`packages/common/{ts,ui}` alongside `frontend-web` (the per-app `src/api/` ships inside its own
frontend, no extra COPY needed). The `backend-go` Dockerfile's build context is also the repo
root (it copies `packages/common/go/common-go/` next to `backend-go/`). Each sibling app has the
same shape under `apps/<name>/infra/` (own compose project `portfolio-b` / `portfolio-c`, own
`backend-go/Dockerfile`, own `nginx/`, and a `frontend-<name>` service). All images are pinned to
`platform: linux/arm64` for Raspberry Pi compatibility.

**Flyway runner (ADR-098):** each app's compose declares a one-shot service — `flyway` (root),
`flyway-portfolio-b`, `flyway-portfolio-c` — image `flyway/flyway:12.4.0` (the same pinned tag the Go
Testcontainers helper uses, so tests and production apply migrations identically), `command: migrate`,
`restart: "no"`, `mem_limit: 384m`, `FLYWAY_CLEAN_DISABLED=true`, bind-mounting that app's own
`db/migration/` read-only. The matching `backend-go*` service `depends_on` it with
`condition: service_completed_successfully`, so a schema change ships in the same `docker compose up -d`
as the code that needs it. The runner is a transient JVM (Flyway CLI) — the only JVM left in the
system; it exits after `migrate` and holds no steady-state RAM. Portfolio B's runner previously set
`FLYWAY_VALIDATE_ON_MIGRATE=false` because its live history had checksum drift and a V7 absent
from the repo (task 134); after the 2026-09-29 metadata-only repair (V2/V5 checksums rewritten,
V7 marked DELETED) it validates clean with default settings like the others. Inspect with `docker compose run --rm flyway info`.

**Memory limits are enforced.** Each app's compose declares real `mem_limit:` values (not the inert
Swarm `deploy: resources:` form): `backend-go*` 64m, `nginx`/`frontend-*` 96m, Flyway runner 384m,
`db-shared` 384m. Go binaries idle at ~13–20 MiB, so there is no heap-tuning knob to manage.

**`infra/shared/`** is a fourth, minimal compose stack (project `shared-infra`) holding the single
PostgreSQL 17.10 instance `db-shared` (databases `investing`, `portfolio-b`, `portfolio-c`, each with its own
owner role — `infra/shared/init/01-init-databases.sh`). It is deliberately its own stack, not part of
any app's `infra/compose.yml`, so an app's routine rebuild/restart cannot take the other apps'
database down. All stacks meet on the external Docker network `investing-shared`. See ADR-083 and
`docs/architecture/12-portfolio-bpps-and-shared-packages.md` §6.

---

## `docs/`

```
docs/
├── architecture/
│   ├── 00-stack-validation.md         Stack as-built reference
│   ├── 01-system-context.md           Running system components + user routes
│   ├── 02-domain-model.md             All tables + ENUMs + design decisions
│   ├── 03-api-contract.md             All REST endpoints (human-readable)
│   ├── 04-db-schema.md                Full schema (canonical DB reference)
│   ├── 05-rebalancing-algorithm.md    Rebalancing algorithms (MAIN pure-proportional DCA, RETIREMENT)
│   ├── 06-data-flow.md                Price refresh, purchase session lifecycle, historia
│   ├── 08-monorepo-layout.md          This file
│   ├── 10-refactoring-plan.md         Tracked refactoring/cleanup backlog
│   ├── 12-portfolio-bpps-and-shared-packages.md   Portfolio B/portfolio-c + shared Go/TS/UI packages
│   ├── 13-go-backend-target-architecture.md    HISTORICAL — the Go rewrite's design (superseded by as-built; Phase 9 done 2026-09-19)
│   └── diagrams/                       Per-task design diagrams (Mermaid)
├── adr/                                ADR files — decision scratchpad; consequences live in the numbered docs above
├── openapi/                            OpenAPI 3.1 contract — SOURCE (edit here)
│   ├── openapi.yaml                    Entrypoint: info/servers/security/tags + $ref table-of-contents
│   │                                    into every schema/path below (components.schemas.<Name>
│   │                                    and paths.<template> are one-line $refs; nothing else lives here)
│   ├── components/
│   │   ├── security-schemes.yaml       bearerAuth, serviceTokenAuth
│   │   ├── responses.yaml              Shared 4xx/5xx envelope responses (Unauthorized, NotFound, ...)
│   │   └── schemas/                    One file per domain, mirrors the paths/ split below
│   │       ├── auth.yaml               enums.yaml            positions.yaml
│   │       ├── common.yaml             deposits.yaml          binance-transfers.yaml
│   │       ├── historia.yaml           cash-positions.yaml    news.yaml
│   │       ├── market-data.yaml        crypto-trades.yaml     tax-returns.yaml
│   │       ├── inflation.yaml          settings.yaml          macro.yaml
│   │       └── purchase-sessions.yaml  rebalancing-transfer.yaml
│   └── paths/                          One file per domain/tag; each holds every path template
│       │                              for that domain (e.g. positions.yaml has both
│       │                              `/positions` and `/positions/{id}`, keyed by short
│       │                              aliases like `collection`/`item` that the entrypoint's
│       │                              paths.<template>.$ref fragment points at)
│       ├── auth.yaml            deposits.yaml         historia.yaml       news.yaml
│       ├── positions.yaml       binance-transfers.yaml cash-positions.yaml market-data.yaml
│       ├── crypto-trades.yaml   tax-returns.yaml       inflation.yaml     settings.yaml
│       └── purchase-sessions.yaml                      rebalancing-transfers.yaml
├── openapi.yaml                        GENERATED single-file bundle (`npm run openapi:bundle`).
│                                        This is what backend/frontend agents read day-to-day —
│                                        never hand-edit it, edit docs/openapi/ and regenerate.
├── finanse-domain-model.md             Verified spreadsheet analysis (do not modify)
└── *.csv                                Spreadsheet exports (reference only)
```

**OpenAPI source split (2026-07):** the spec was split from a single 3700+-line `docs/openapi.yaml` into the multi-file `docs/openapi/` source tree above once it became unwieldy to read/edit. Cross-file references use relative `$ref`s (e.g. a path file references `../components/schemas/positions.yaml#/Position`; the entrypoint references `./components/schemas/positions.yaml#/Position`). Bundling is done with `@redocly/cli` (free/MIT, a root `devDependency`):

```bash
npm run openapi:bundle   # redocly bundle docs/openapi/openapi.yaml -o docs/openapi.yaml
npm run openapi:lint     # redocly lint docs/openapi/openapi.yaml
```

The generated `docs/openapi.yaml` is the contract the Go backend is generated from: `apps/main/backend-go/openapi/server.gen.go` (chi `ServerInterface` + models) is produced from it by `go generate` (oapi-codegen), so a spec change is followed by regenerating that file and letting the compiler point at every handler that no longer matches. Per-app Zod schemas (each `frontend-web/src/api/`) are still kept in sync by hand. (The Java backend's `springdoc` Swagger UI is gone with the Java code.)

**ADRs:** files in `docs/adr/` capture a decision at the moment it is made; as soon as it is accepted its consequences are folded into the numbered docs above as current fact. Read the numbered docs for how the system *is*; read an ADR only for why one option won over the alternatives.

---

## Module Ownership

| Directory | Owning agent |
|---|---|
| `docs/architecture/`, `docs/openapi/`, `docs/adr/`, `apps/*/db/migration/` | Architect |
| `apps/*/backend-go/`, `packages/common/go/` | Backend-Go agent |
| `apps/*/frontend-web/`, `packages/common/{ts,ui}` | Frontend-web agent |
| `infra/` (incl. `shared/`), `apps/*/infra/` | DevOps agent |

This matches AGENTS.md's agent-orchestration table (Architect owns system design/contracts/schema/ADRs; Backend-Go owns the Go API; Frontend-web owns the React web UI; DevOps owns Docker/Compose/Nginx/RPi deployment). The former Java **Backend** agent's trees were deleted in Phase 9 (history in git, restore anchor `2be75ba`). Schema changes are Architect-owned: a new `V<N+1>__*.sql` in the app's `db/migration/`, then sqlc regeneration by Backend-Go. The Google Sheets ETL job was a one-time, bespoke task and is not a persisted package.
