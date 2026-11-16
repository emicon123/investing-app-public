# Investing App

A self-hosted personal investment tracker and rebalancing planner that replaced a large Google
Sheets workbook. It runs as a small Docker Compose stack on a Raspberry Pi and is reached over
Tailscale.

> **About this repository.** This is the public showcase copy of a private project. All personal
> financial data (seed and data migrations, portfolio history, reconstruction tooling, live
> snapshots) has been removed. Each app's incremental database migrations are squashed into one
> schema baseline, test fixtures that were based on real data now use invented values, and the two
> sibling apps are published under neutral names (Portfolio B, Portfolio C).

## What it does

- **Portfolio book.** Assets made of several instruments ("subassets", e.g. physical BTC and a
  BTC ETN) across brokers (XTB, Binance) and account types, including the Polish tax-advantaged
  retirement accounts IKE and IKZE with their annual limits. Two portfolios: a main one and a
  retirement one.
- **Rebalancing planner.** Buy-only, deficit-proportional DCA: each monthly contribution is
  spread over every under-target position in proportion to its deficit, then split across
  accounts in a waterfall that respects IKE/IKZE room. Targets can blend between risk-on and
  risk-off allocations based on a macro regime signal. Selling is always a manual decision; the UI
  only flags positions far above target and suggests a trim amount.
- **Purchase sessions.** Plan a session, execute it at the broker, record the actual fills; units,
  cash and cost basis update in one transaction. Idempotency keys protect money-moving writes.
- **Cost basis.** A pure, shared kernel keeps asset-level average cost across partial sales,
  rebuys, same-asset swaps and cross-asset transfers, so the entry price and profit % stay correct
  when an asset is held through several wrappers.
- **Performance.** Deposit ledger, XIRR (Brent's method), daily portfolio history with
  contributed-capital and CPI-inflation overlays, benchmark price overlays and tax-year summaries.
- **Market data.** Prices from Yahoo Finance and the Binance public API with caching and fallbacks;
  FX from NBP; macro series (FRED, ECB, BoE, BoJ, BLS and others) for the regime signal.
- **Automation.** A host cron job writes a daily, AI-researched news digest per asset, and Web Push
  notifications reach the phone. An experimental crypto futures bot (OKX) has a kill switch,
  leverage caps and protective-order checks.
- **Sibling apps.** Two smaller apps for other people's portfolios (`apps/portfolio-b`,
  `apps/portfolio-c`) reuse the shared packages: a shared Go service core
  (`common-go/siblingcore`), the shared portfolio view and the cost-basis kernel, each with its own
  API, SPA, database and compose project.
- **Backtesting.** About 30 Python studies (`backtest/`) testing DCA, trimming, macro-blend and
  volatility-targeting strategies on multiple start dates with walk-forward checks.

## Tech stack

| Layer | Technology |
| --- | --- |
| Backend | Go 1.27, chi, oapi-codegen (server generated from an OpenAPI 3.1 contract), sqlc + pgx, shopspring/decimal, Web Push (VAPID), JWT with Google sign-in |
| Database | PostgreSQL 17, Flyway migrations (one-shot runner container) |
| Frontend | React 19, TypeScript, Vite, TanStack Query and Table, React Hook Form + Zod, Zustand, Tailwind CSS 4, Radix UI, Recharts, Nivo |
| Monorepo | three apps (`apps/main`, `apps/portfolio-b`, `apps/portfolio-c`) in npm workspaces and a Go workspace; shared code in `packages/common` (`go`, `ts`, `ui`) |
| Infrastructure | Docker Compose on a Raspberry Pi (ARM64), nginx serving the SPA and proxying `/api`, Tailscale |
| Analysis | Python (pandas) backtests in `backtest/` |

## Engineering practices

- **Contract first.** The OpenAPI contract in `docs/openapi/` is the source of truth; the Go server
  interface is generated from it and the bundle is linted with Redocly in CI.
- **Real database in tests.** Integration tests start PostgreSQL and the same Flyway image as
  production with Testcontainers; the database is never mocked.
- **CI quality gates** (`.github/workflows`): `go test` without `-short`, a race-detector run,
  `govulncheck`, golangci-lint, an sqlc drift check, Vitest + Testing Library, and an ESLint
  zero-warning ratchet.
- **Money as decimals.** All amounts use fixed-point decimals end to end; rounding rules are
  explicit and tested.
- **Architecture Decision Records.** More than 50 ADRs in `docs/adr/` plus design documents and
  Mermaid diagrams in `docs/architecture/`.
- **AI-assisted workflow.** Developed with Claude Code in an orchestrated setup (architect agent →
  backend and frontend agents → review), described in `AGENTS.md`.

## Repository layout

```
apps/main/
  backend-go/      Go API (cmd/server, internal/<domain>, db/queries + generated sqlc code)
  frontend-web/    React SPA
  db/migration/    Flyway migrations (V1 baseline schema, V2 reference data)
apps/portfolio-b/, apps/portfolio-c/
                   sibling apps: backend-go, frontend-web, db/migration, infra (own compose project)
packages/common/
  go/common-go/    shared Go kernels: cost basis, XIRR, rebalancing math, price providers, sibling service core, test support
  ts/              shared TypeScript: API client, auth, hooks, schemas
  ui/              shared React components and the portfolio view
docs/              OpenAPI contract, ADRs, architecture and design docs
infra/             Docker Compose, nginx, shared PostgreSQL, host cron scripts (news, bot)
backtest/          Python strategy studies and their results
```

## Running locally

Requirements: Docker with Compose, Go 1.27 and Node.js 24 for local development.

```bash
# 1. Shared PostgreSQL (creates the app database and user on first start)
docker network create investing-shared
cp infra/shared/.env.example infra/shared/.env    # set the Postgres passwords
docker compose -f infra/shared/compose.yml --env-file infra/shared/.env up -d

# 2. App configuration
cp infra/.env.example infra/.env          # DB_URL/DB_PASSWORD, JWT_SECRET, GOOGLE_CLIENT_ID, ALLOWED_EMAIL_1, ...

# 3. App stack: Flyway runner, Go backend, nginx + SPA
./boot.sh up                              # or: docker compose -f infra/compose.yml --env-file infra/.env up -d --build

# Tests
(cd apps/main/backend-go && go test ./...)          # needs Docker for Testcontainers
(cd apps/portfolio-b/backend-go && go test ./...)
(cd apps/portfolio-c/backend-go && go test ./...)
(cd packages/common/go/common-go && go test ./...)
npm install && npx vitest run
```

The compose files target `linux/arm64` (Raspberry Pi). On an x86 machine, remove the `platform:`
lines. Sign-in uses Google OAuth: set `GOOGLE_CLIENT_ID` / `VITE_GOOGLE_CLIENT_ID` and the allowed
e-mail addresses (`ALLOWED_EMAIL_1`, `ALLOWED_EMAIL_2`) in `infra/.env`. The sibling apps start the
same way from `apps/portfolio-b/infra` and `apps/portfolio-c/infra` once the shared database is up;
the main nginx serves them under `/portfolio-b/` and `/portfolio-c/`.

## Notes

- The UI is in Polish.
