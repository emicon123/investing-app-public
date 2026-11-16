# AGENTS.md

Guidance for coding agents (Claude Code and others) working in this repository. The project is
described in `README.md`; the canonical specification lives in `docs/` — if a prompt or the code
disagrees with `docs/`, say so instead of silently picking one.

## Workflow

Changes follow an architect-first flow:

1. **Architect** — updates the OpenAPI contract (`docs/openapi/`), the DB schema plan, diagrams
   (`docs/architecture/diagrams/`) and, for a real decision, an ADR (`docs/adr/`). No code is
   written before the contract exists.
2. **Backend (Go) / Frontend-web** — implement against that contract, in parallel when they do not
   depend on each other, with tests.
3. **Review** — check that diagrams, contract and code agree before merging.

If you catch yourself editing the same code in several places, ask whether it belongs in
`packages/common`.

## Hard constraints

- **Cost-free only** — every tool, library, service and host must be free / open source.
- **Runs on a Raspberry Pi** (ARM64) via Docker Compose; reached over Tailscale.
- **Selling is manual.** The rebalancing planner is buy-only; no algorithm ever sells a position
  (ADR-045, ADR-048). The UI may only flag and suggest.

## Quality gates

- Every schema change is a Flyway versioned SQL file in `apps/<app>/db/migration/` (`V1` is the
  squashed baseline; continue from `V3`). No manual DDL.
- Integration tests use **Testcontainers** (testcontainers-go) against real PostgreSQL migrated by
  the production Flyway image — **do not mock the database**.
- Keep DB rows out of the API layer: sqlc/pgx rows → domain types → OpenAPI DTOs.
- Money is `shopspring/decimal` in Go and explicit rounding in TypeScript — never floats for stored
  amounts.
- Secrets live in `infra/.env` (gitignored), never in code.
- Regenerate sqlc (`sqlc generate`, v1.31.1) after any query or migration change; CI fails on drift.

## Project structure

```
apps/main/backend-go   Go API: cmd/server, internal/<domain>, db/queries + db/sqlcgen
apps/main/frontend-web React SPA (Vite)
apps/main/db/migration Flyway migrations
apps/portfolio-b/      sibling app (same shape: backend-go, frontend-web, db/migration, infra, docs)
apps/portfolio-c/      sibling app; each sibling's CLAUDE.md holds its domain facts
packages/common/go     shared Go kernels (costbasis, xirr, rebalancecalc, priceprovider, testsupport, …)
packages/common/ts     shared TS: API client, auth, hooks, schemas
packages/common/ui     shared React components (portfolio view, dialogs, tables)
docs/                  OpenAPI contract, ADRs, architecture docs
infra/                 compose stack, nginx, shared Postgres, host cron scripts (news, bot)
backtest/              Python strategy studies (see backtest/AGENTS.md)
```

## Common commands

```bash
# Go
(cd apps/main/backend-go && go test ./...)            # full suite, needs Docker
(cd apps/main/backend-go && go test -short ./...)     # unit tests only
(cd apps/portfolio-b/backend-go && go test ./...)     # likewise for portfolio-c
(cd packages/common/go/common-go && go test ./...)

# Frontend
npm install
npx vitest run
npm run lint
npm run openapi:lint

# Stack (infra/.env must exist — copy infra/.env.example)
cd infra
docker compose build backend-go && docker compose up -d backend-go   # `up` runs Flyway first
docker compose run --rm --no-deps flyway info                       # migration state
docker compose build nginx && docker compose up -d nginx             # SPA is built in the nginx image
docker logs -f investing-backend-go-1
```

## Testing the authenticated API

To verify the authenticated API on the host that runs the stack, mint a short-lived backend JWT
instead of going through Google sign-in. This only works where the stack runs: the signing secret
exists only in the backend container's environment and never in the repository.

- HS256; claims `email` + `sub` (= `ALLOWED_EMAIL_1`), `iss` = `investing-app`, `iat`,
  `exp = iat + 600` (10 minutes, never longer).
- Read `JWT_SECRET`, `ALLOWED_EMAIL_1` and `PORT` with
  `docker inspect <backend container> --format '{{range .Config.Env}}{{println .}}{{end}}'` (the
  distroless image has no shell, so `docker exec … env` fails).
- Call the container directly (its `docker inspect` IP + `PORT`), not through nginx.
- Never print, log or commit the secret or the token.

## Conventions

- Conventional Commits (`feat:`, `fix:`, `chore:`, …).
- Market data: Yahoo Finance (no key) first, Binance public API for crypto, both behind
  `packages/common/go/common-go/priceprovider`.
- The UI copy is Polish; code, comments and docs are English.
