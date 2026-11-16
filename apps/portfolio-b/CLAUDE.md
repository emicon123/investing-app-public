# CLAUDE.md — portfolio-b

This file provides guidance to Claude Code when working with code in this repository.

**This app has no specialist agents of its own.** The three apps (investing-app at the repo
root, this one, and portfolio-c) were unified onto common code (`packages/common/{ts,ui,go}/`) —
work in this tree is handled by the **root investing-app's** architect/backend/frontend-web/
devops agents, per the root `CLAUDE.md`'s orchestration table. Do not spawn per-app agents here;
follow the root `CLAUDE.md`'s "Agent orchestration" and "Task workflow" sections instead. This
file remains authoritative only for the domain facts below (account types, rebalancing formula,
infra) that differ from investing-app.

## What this is

Retirement portfolio tracker for Portfolio B (tata/tata's wife mama). Mirrors the `/investing-app`
architecture but scoped to a single retirement portfolio with simpler rebalancing.

Migrates a Google Spreadsheet (ID: `176fnZg2JeCEHqz_lxWasErUmzwWPtxnqFfLE_mvcH8o`) into
a web application.

**Key differences vs. investing-app:**
- Single portfolio (no INWESTYCJE / EMERYTURA split)
- 4 retirement accounts: IKE_TATA, IKE_MAMA, IKZE_TATA, IKZE_MAMA + INNE (historical)
- Everything bought in PLN via IKE/IKZE ETFs on XTB — no multi-currency purchase flow
- Simple proportional rebalancing (no regression weights, no ISM PMI)
- No purchase sessions, no monthly income/expense, no crypto trade tax log
- Fixed monthly deposit (currently 4,000 PLN, configurable)
- Manual sell-assist ("Sprzedaj" button / SELL status flag at ≥150% of target) exists: the common portfolio table (`packages/common/ui/src/portfolio/`, status rule in `model/rebalanceStatus.ts`) flags an over-target asset, a dialog lets the user record an actual sell + one-or-more buys as one `POST /position-transfers` transaction. Pure UI aid — the backend never computes or auto-executes a sell, only records what the user submits. See `docs/architecture.md` "Manual sell-assist (1.5x trim)".

## Domain model

See task.md for gsheet analysis and full design decisions.

**Holdings (Asset / Subasset, task 156):** the book is a list of **assets** (logical: Bitcoin, Gold, Nasdaq 100, …) that carry the target allocation; each asset groups one or more **subassets** (`positions` rows — the concrete tickers) with exactly one **primary** (the default buy destination). A subasset never has a target. `i` in the algorithm below is an asset and `current_value_i` is the sum of its subassets. Details: `docs/architecture.md` "Asset / Subasset model".

**Account types (account_type enum):**
- `IKE_TATA` — Dad's IKE, Belka-exempt at 60+
- `IKE_MAMA` — Mom's IKE, Belka-exempt at 60+
- `IKZE_TATA` — Dad's IKZE, tax-deductible contributions
- `IKZE_MAMA` — Mom's IKZE, tax-deductible contributions
- `INNE` — Other / historical holdings (physical gold, physical silver, direct BTC)

**Rebalancing algorithm (simple proportional):**
```
deficit_i    = target_value_i - current_value_i
deposit_i    = IF(deficit_i > 0, total_deposit × deficit_i / sum_positive_deficits, 0)
units_to_buy = deposit_i / price_pln_i
```
All purchases go into IKE/IKZE accounts. INNE is read-only.

## Package naming

- Go module: `portfolio-b/backend-go` (backend at `apps/portfolio-b/backend-go/`; the Java `pl.portfolio-b` package was deleted with the Java backend in Phase 9, ADR-098)
- npm packages: `@common/*` (common plumbing), `@portfolio-b/frontend-web` (per-app API code lives in `frontend-web/src/api/`; the old `@portfolio-b/shared` workspace was deleted by task 158)

## Agent orchestration

None here — see the note at the top of this file. All specialist agents (Architect, Backend,
Frontend-web, DevOps) are spawned from the root investing-app session against its orchestration
table, and operate across all three apps' trees, not just the root.

## Quality gates

- All schema changes via Flyway versioned SQL in `db/migration/` (this app's own directory), applied by the one-shot `flyway-portfolio-b` compose service
- Backend integration tests use Testcontainers / testcontainers-go (real PostgreSQL — no DB mocks)
- sqlc/pgx rows → domain types → OpenAPI-generated DTOs (no ORM, no MapStruct)
- Secrets in `.env` (`SPRING_PROFILES_ACTIVE` is still the dev/prod switch the Go backend reads)

## Market data

Same sources as investing-app:
- Yahoo Finance (Go `packages/common/go/common-go/priceprovider`) — stocks, ETFs, FX
- Binance REST API — BTC/ETH/SOL spot prices (free, no API key)

## Infra / deployment

- Separate Docker Compose (`infra/compose.yml`) with its own `flyway-portfolio-b` (one-shot migration runner), `backend-go-portfolio-b` (Go, :8092, internal only) and `frontend-portfolio-b`
  services. The database is **not** here anymore (ADR-083, 2026-09-02) — Postgres for this app
  lives in the shared `infra/shared/compose.yml` stack at the repo root (service `db-shared`,
  database `portfolio-b`), alongside investing-app's and portfolio-c's. `docker compose up -d
  backend-go-portfolio-b` in this app's own `infra/` does not start/stop any database. The old
  `db-portfolio-b` container was stopped (not removed) during migration; its volume
  (`infra_db-portfolio-b-data`) still holds the pre-migration data as a rollback path — see
  `.claude/tasks/done/132-*.task.md` at the repo root before touching it.
- Both services on a shared Docker network with the main investing-app Nginx
- Main Nginx gets `/portfolio-b/` location block pointing to this frontend + `/portfolio-b/api/` to `backend-go-portfolio-b:8092`
- No TLS termination here — main Nginx handles it
- Same Tailscale VPN access

## Rebuilding

```bash
cd apps/portfolio-b/infra
docker compose build backend-go-portfolio-b && docker compose up -d backend-go-portfolio-b   # runs flyway-portfolio-b first
docker compose build frontend-portfolio-b && docker compose up -d frontend-portfolio-b
docker compose up -d --build

until docker logs portfolio-b-backend-go-portfolio-b-1 2>&1 | grep -q "backend-go listening"; do sleep 2; done && echo "Up"

# read-only migration state:
docker compose run --rm --no-deps flyway-portfolio-b info
```
