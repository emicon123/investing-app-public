# CLAUDE.md — portfolio-c

This file provides guidance to Claude Code when working with code in this repository.

**This app has no specialist agents of its own.** The three apps (investing-app at the repo
root, this one, and portfolio-b) were unified onto common code (`packages/common/{ts,ui,go}/`) —
work in this tree is handled by the **root investing-app's** architect/backend/frontend-web/
devops agents, per the root `CLAUDE.md`'s orchestration table. Do not spawn per-app agents here;
follow the root `CLAUDE.md`'s "Agent orchestration" and "Task workflow" sections instead. This
file remains authoritative only for the domain facts below (portfolio dimension, rebalancing
formula, infra) that differ from investing-app.

## What this is

Portfolio tracker for Portfolio C, built as a mechanical fork of `portfolio-b`
(package `pl.portfolio-c`, class `Portfolio CApp`, npm scope `@portfolio-c/*`) that has since been
redesigned for Portfolio C's own, meaningfully different domain — this is NOT Portfolio B's
IKE/IKZE retirement tracker with names swapped. There is no source Google Spreadsheet
being migrated here; the domain below was specified directly by the user.

**Two portfolios, completely separated — their positions, deposits, cash, history,
and settings never mix:**

- **VARIABLE** — deposit amount varies every month, no stored default. Targets: 50%
  SXRV.DE / 20% ICGA.DE / 30% 4GLD.DE.
- **FIXED** — fixed 400 PLN/month deposit (changeable via Settings). Targets: 33.34%
  ETNVIRBTCP.WA / 33.33% ICGA.DE / 33.33% SXRV.DE. (Ticker suffix flagged for
  verification against Yahoo Finance — see `docs/adr/001-portfolio-dimension.md`.)

**Key differences vs. investing-app (and vs. the Portfolio B app this was forked from):**
- Two independent flat portfolios (`portfolio_enum`: `VARIABLE`, `FIXED`), not one
  portfolio split into IKE/IKZE sub-accounts. **There is no IKE/IKZE concept in this
  app at all** — no `IKE_TATA`/`IKE_MAMA`/`IKZE_TATA`/`IKZE_MAMA`/`INNE`, no
  per-account unit columns, no per-account cash balances, no annual IKE/IKZE
  contribution limits.
- No sub-accounts within a portfolio — each is one flat book: one set of positions,
  one cash balance.
- Everything bought in PLN. The underlying Xetra-listed tickers (SXRV.DE, ICGA.DE,
  4GLD.DE) are EUR-denominated on the exchange, so `fx_rate_cache` (EUR→PLN) is kept
  exactly as in Portfolio B for pricing; ETNVIRBTCP.WA is PLN-denominated directly.
- Simple proportional rebalancing (no regression weights, no ISM PMI, no per-account
  priority fill).
- The deposit amount for `POST /purchases/calculate` is **request-scoped**, not a
  single global setting: FIXED falls back to its stored 400 PLN default if omitted;
  VARIABLE has no default and requires the amount every call (omitting it is a
  validation error). See `docs/architecture.md` "Rebalancing flow" and
  `docs/adr/001-portfolio-dimension.md`.
- Manual sell-assist ("Sprzedaj" button / SELL status flag at ≥150% of target) exists,
  scoped within one portfolio: the common portfolio table (`model/rebalanceStatus.ts`) flags an over-target asset, a
  dialog lets the user record an actual sell + one-or-more buys as one
  `POST /position-transfers` transaction. Pure UI aid — the backend never computes or
  auto-executes a sell, only records what the user submits. See
  `docs/architecture.md` "Manual sell-assist (1.5x trim)".

## Domain model

Full schema, ERD, and rationale live in `docs/architecture.md` and
`docs/adr/001-portfolio-dimension.md` — this section is a summary, not the source
of truth.

**Portfolio (`portfolio_enum`): `VARIABLE`, `FIXED`.** This is the only partition
dimension in the schema — it replaces the IKE/IKZE sub-account dimension entirely.
Every domain table (`positions`, `deposits`, `historia`, `account_cash_snapshot`,
`purchase_lots`, `position_transfers`) carries a `portfolio` column (or, for
`account_cash_snapshot`, has it as the primary key). A ticker may appear in both
portfolios at different target weights (ICGA.DE and SXRV.DE both do) — uniqueness
is `(ticker, portfolio)`, not `ticker` alone.

**Holdings (Asset / Subasset, task 156):** each portfolio is a list of **assets** (logical, carry the
target allocation; `UNIQUE (key, portfolio)`) grouping one or more **subassets** (`positions` rows —
the concrete tickers) with exactly one **primary** (the default buy destination). A subasset never has
a target; an asset's value is the sum of its subassets. Today every asset has a single subasset.
Details: `docs/architecture.md` "Asset / Subasset model".

**Rebalancing algorithm (simple proportional, per portfolio):**
```
deficit_i    = target_value_i - current_value_i
deposit_i    = IF(deficit_i > 0, total_deposit × deficit_i / sum_positive_deficits, 0)
units_to_buy = deposit_i / price_pln_i
```
Run independently per portfolio against that portfolio's own positions and its own
`depositAmountPln` for the call — see "Rebalancing flow" in `docs/architecture.md`
for the FIXED-default-vs-VARIABLE-required rule.

## Package naming

- Go module: `portfolio-c/backend-go` (backend at `apps/portfolio-c/backend-go/`; the Java `pl.portfolio-c` package was deleted with the Java backend in Phase 9, ADR-098)
- npm packages: `@common/*` (common plumbing), `@portfolio-c/frontend-web` (per-app API code lives in `frontend-web/src/api/`; the old `@portfolio-c/shared` workspace was deleted by task 158)

## Agent orchestration

None here — see the note at the top of this file. All specialist agents (Architect, Backend,
Frontend-web, DevOps) are spawned from the root investing-app session against its orchestration
table, and operate across all three apps' trees, not just the root.

## Quality gates

- All schema changes via Flyway versioned SQL in `db/migration/` (this app's own directory), applied by the one-shot `flyway-portfolio-c` compose service
- Backend integration tests use Testcontainers / testcontainers-go (real PostgreSQL — no DB mocks)
- sqlc/pgx rows → domain types → OpenAPI-generated DTOs (no ORM, no MapStruct)
- Secrets in `.env` (`SPRING_PROFILES_ACTIVE` is still the dev/prod switch the Go backend reads)

## Market data

Same sources as investing-app:
- Yahoo Finance (Go `packages/common/go/common-go/priceprovider`) — stocks, ETFs, FX
- Binance REST API — BTC/ETH/SOL spot prices (free, no API key)

## Infra / deployment

- Separate Docker Compose (`infra/compose.yml`) with its own `flyway-portfolio-c` (one-shot migration runner), `backend-go-portfolio-c` (Go, :8093, internal only) and
  `frontend-portfolio-c` services — each on both an internal `portfolio-c-net` bridge network
  and the shared external `investing-shared` Docker network. The database is **not**
  here anymore (ADR-083, 2026-09-02) — Postgres for this app lives in the shared
  `infra/shared/compose.yml` stack at the repo root (service `db-shared`, database
  `portfolio-c`), alongside investing-app's and portfolio-b's. `docker compose up -d backend-go-portfolio-c`
  in this app's own `infra/` does not start/stop any database. The old `db-portfolio-c`
  container was stopped (not removed) during migration; its volume
  (`portfolio_c_db-portfolio-c-data`) still holds the pre-migration data as a rollback path — see
  `.claude/tasks/done/132-*.task.md` at the repo root before touching it.
- `frontend-portfolio-c` publishes **no host port** — it's reached only by the main
  investing-app's Nginx container resolving `frontend-portfolio-c:80` / `backend-go-portfolio-c:8093`
  over `investing-shared`, exactly like the existing `frontend-portfolio-b`/`backend-go-portfolio-b`
  containers.
- The MAIN investing-app's Nginx (not a Nginx belonging to this app) gets a `/portfolio-c/`
  location block: explicit per-endpoint Go locations plus a generic catch-all `proxy_pass` to `backend-go-portfolio-c:8093` for `/portfolio-c/api/` (variable upstream + Docker resolver, ADR-098) and
  `proxy_pass http://frontend-portfolio-c:80/;` for `/portfolio-c/` — mirroring the existing
  `/portfolio-b/` block in `investing-app/infra/nginx/nginx.conf` line-for-line.
- No TLS termination here — main Nginx handles it.
- Reachable the same way the main app and Portfolio B's app already are (whatever that
  access model actually is on this host) — this app doesn't introduce a new
  exposure surface, it rides on the main app's existing Nginx entry point.

## Rebuilding

```bash
cd apps/portfolio-c/infra
docker compose build backend-go-portfolio-c && docker compose up -d backend-go-portfolio-c   # runs flyway-portfolio-c first
docker compose build frontend-portfolio-c && docker compose up -d frontend-portfolio-c
docker compose up -d --build

until docker logs portfolio-c-backend-go-portfolio-c-1 2>&1 | grep -q "backend-go listening"; do sleep 2; done && echo "Up"

# read-only migration state:
docker compose run --rm --no-deps flyway-portfolio-c info
```
