# ADR-110: Process Lifecycle and Resilience Kit in common-go (All Three Backends)

**Date:** 2026-11-04
**Status:** Accepted (task 174 review → batch 176). Design:
`docs/architecture/diagrams/174-remediation-design.md` §2.1–§2.4, §4.
**Scope:** main, portfolio-b, portfolio-c `cmd/server/main.go`; common-go `lifecycle`, `pgtx`, `config`, `httpx`;
the three compose files. **Not** the bot (owner decision 2026-11-04, REPORT §7a.2).

## Context

The task-174 review (F1.2, F1.3, F1.9, F1.10, F2.5, F3.6, F4.3) found that all three backends share
the same hand-rolled `main()` with the same gaps:

- background goroutines (schedulers, price refresh, backfill) are started with a bare `go` and never
  awaited; `main` returns right after `srv.Shutdown`, and the serve goroutine calls `os.Exit(1)`, which
  skips every deferred cleanup;
- no `recover` anywhere outside chi's HTTP `Recoverer`, so one panic in a scheduler kills the process;
- the transaction runner is copied 19 times in production code (plus ~20 test helpers), none with a
  deferred rollback, so a panic inside a transaction leaks a connection "idle in transaction" holding
  its row locks — with pgxpool's default of **4** connections on the Pi 5;
- `http.Server` sets only `ReadHeaderTimeout`; the health endpoint is a constant; the containers run
  under `mem_limit: 64m` without `GOMEMLIMIT`, healthcheck or `stop_grace_period`.

## Decision

1. **`common-go/lifecycle`** owns the process lifecycle: `Group` (workers with a derived context,
   `sync.WaitGroup.Go`, a recover backstop), `Guard` (per-iteration panic → error), `Run` (synchronous
   listen, then ordered shutdown: stop workers → drain HTTP 15 s → await workers 10 s, never
   `os.Exit`) and `Probe` (the `healthcheck` subcommand for the distroless image).
2. **`common-go/pgtx.Runner`** is the only transaction runner in server code: a generic wrapper over
   `pgx.BeginFunc`, which defers the rollback. Each package keeps its own `TransactFunc` type; the
   runner's result is assignable to it.
3. **`config.ConnectDB`** builds the pool with an explicit policy: `MaxConns 10`,
   `HealthCheckPeriod 30s`, `MaxConnIdleTime 5m`, server-side `statement_timeout=60s` and
   `idle_in_transaction_session_timeout=5min`. It returns errors; `main` exits once, at the top.
4. **`httpx.NewServer`** sets every timeout (ReadHeader 5 s, Read 30 s, Write 190 s =
   `LongRequestTimeout` + 10 s, Idle 120 s, 64 KiB headers). **`httpx.HealthHandler`** turns
   `GET /api/health-go` into a readiness check (DB ping, 1 s → 503) and drops `"phase":"1"`.
5. **Compose (DevOps):** `GOMEMLIMIT=48MiB`, `stop_grace_period: 30s` and an exec-form
   `healthcheck` running `/app/backend-go healthcheck` for `backend-go`, `backend-go-portfolio-b`,
   `backend-go-portfolio-c`. Nginx keeps `depends_on: service_started` (the edge proxy must never wait on
   one backend's health).
6. **Bot exception:** main's `go calendar.StartScheduler(ctx, …)` and `go trade.StartWatcher(ctx, …)`
   stay exactly as they are: plain goroutines on the root signal context, not in the `Group`, not
   awaited. The bot's own transaction closures in `main.go` (`newPlanService`, `newTradeService`)
   switch to `pgtx.Runner` in a separate commit (same behaviour on every non-panic path); the owner
   may veto that one commit without affecting the rest.

## Alternatives rejected

- **`errgroup.Group` as the worker owner:** cancels every sibling on the first error; the schedulers
  are fail-soft by contract, and an `x/sync` dependency buys nothing over `sync.WaitGroup.Go`.
- **Per-goroutine recover only:** the loop dies on the first panic; per-tick `Guard` keeps it alive.
- **A framework (fx, oklog/run):** a new dependency for ~80 lines of code.
- **`MaxConns` left at the default:** 4 connections shared by HTTP, five schedulers and 180 s
  requests; the shared Postgres has headroom for 3 × 10.
- **Compose healthcheck via a shell image:** gives up distroless for one probe; an exec-form
  self-probe needs no shell.
- **`nginx depends_on: service_healthy`:** a DB outage of one app would keep the edge proxy (all
  three apps) from starting.

## Consequences

- Shutdown is bounded at ~25 s and ordered; the deploy's SIGTERM no longer cuts schedulers mid-tick
  without waiting. F1.1 (bot) is **not** fixed by this ADR: the watcher is not awaited by owner choice.
- A panic in a scheduler costs one tick; a panic inside a transaction rolls back and frees the
  connection; a leaked idle transaction is killed by the server after 5 min at the latest.
- `/api/health-go` can now answer 503; nothing but the compose healthcheck consumes it (the deploy
  workflow polls `/api/positions`).
- Every later batch (177 locking, 180 siblingcore) builds on `pgtx` and `lifecycle`.

## Amendment 2026-11-09 — Decision 6 reversed (ADR-114, task 191)

The owner has asked for the bot to be fixed. `trade.StartWatcher` and `calendar.StartScheduler`
become `lifecycle.Group` workers ("bot-watcher", "bot-calendar-purge") with a per-tick `Guard`.
Main's `lifecycle.Run` uses `WorkerTimeout: 60s`, so a shutdown waits for an in-flight OKX
commit phase (≤ 55 s). Compose `stop_grace_period` for `backend-go` goes to 90 s. The siblings
are unchanged. The bot's `newPlanService`/`newTradeService` closures move to `pgtx.Runner`.
