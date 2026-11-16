# ADR-111: Money-Write Serialisation and Idempotency-Key (All Three Apps)

**Date:** 2026-11-04
**Status:** Accepted (task 174 review → batch 177). Design:
`docs/architecture/diagrams/174-remediation-design.md` §2.1, §2.5, §5.
**Scope:** main, portfolio-b, portfolio-c backends + frontends; Flyway main V210, portfolio-b V21, portfolio-c V15
(next free numbers on 2026-11-04 — re-check before writing the files).

## Context

F3.1: every money write is a read-modify-write in Go under READ COMMITTED with no lock: main
`rebalancing.ExecuteTransfer` reads all positions, checks units, computes absolute new units and
cost, writes them back; portfolio-b/portfolio-c `positiontransfer.ExecuteTransfer` and `rebalancing.Confirm`
(whose position read happens **outside** its transaction) and every `PUT /positions/{id}` edit
(main's read and write are not even in one transaction) have the same shape. Two overlapping
requests (a double click, a retry, a transfer racing a purchase-session completion) both pass the
"enough units" check and the second write silently erases the first. The writes are absolute
because the costbasis kernel computes absolute asset-level costs.

F3.2: none of the money POSTs is idempotent. A retry after a lost response (Tailscale Funnel hiccup,
phone going to sleep) applies the transfer, deposit or trade twice, even once F3.1 serialises them.

## Decision

### 1. One advisory lock per database serialises money writes

Every transaction that reads and then writes `positions`, `deposits`, `asset_purchases` /
`purchase_lots`, transfer records or purchase-session completion state starts with
`SELECT pg_advisory_xact_lock(pgtx.MoneyWriteLockKey)` — wired in each `main.go` with
`pgtx.SerializedRunner`. Every read that feeds such a write moves inside the transaction. Scope per
app (exact list: design §5.1): main position writes, rebalancing transfer, purchase-session
completion; portfolio-b/portfolio-c position writes, position transfer, purchase confirm. Single-statement
inserts with no read (deposit create, crypto-trade create, binance-transfer create, cap
adjustment upsert) need no lock; idempotency covers their duplicates.

### 2. Optional `Idempotency-Key` header on money-moving POSTs

`common-go/idempotency.Middleware`, mounted once in each app's authenticated route group, acts only on
non-GET requests that carry the header (IETF draft semantics):

| Situation | Answer |
|---|---|
| new key | run the handler; store a 2xx response for 24 h; on 4xx delete the record; on 5xx keep it IN_PROGRESS |
| same key + same request hash, COMPLETED | replay status + body, header `Idempotent-Replayed: true` |
| same key, IN_PROGRESS (running, or ended 5xx) | `409 IDEMPOTENCY_IN_PROGRESS` |
| same key, different method/path/body | `422 IDEMPOTENCY_KEY_REUSED` |
| malformed key | `400 VALIDATION_ERROR` (`field: Idempotency-Key`) |

Request hash = SHA-256 of `METHOD SP path NL body`. Records live in a per-app `idempotency_keys`
table (identical DDL in the three databases); rows older than 24 h are purged lazily on each insert.
The header is declared on main `createRebalancingTransfer`, `createDeposit`, `createCryptoTrade`,
`createBinanceTransfer` and portfolio-b/portfolio-c `execute`, `confirm`, `createDeposit`. The web clients
generate one key per user intent (dialog submission), reuse it on retry and rotate it after a
success or when the dialog closes. Purchase-session completion is already idempotent (status check
under `FOR UPDATE`) and does not get the header.

## Alternatives rejected

- **`SELECT … FOR UPDATE ORDER BY id`:** a locking query per read path per app, a lock-ordering
  discipline, and insert races (two transfers creating the same `asset_purchases` row) stay open.
- **Relative SQL (`SET units = units + $x`):** the cost half of each write is an absolute
  kernel result, so the lost-update window would remain for `total_spent_pln`.
- **SERIALIZABLE isolation + retry loop:** correct, but every money service would need retry
  plumbing and idempotent bodies; the single user never needs parallel money writes.
- **Storing the idempotency record in the handler's own transaction (exactly-once):** requires
  threading the record through every money service. The middleware-level record with
  "5xx = outcome unknown → 409" is never wrong in the dangerous direction (it may refuse a retry, it
  never applies twice) and touches no service.
- **A required header:** would break cached PWA bundles and API scripts during the rollout; optional
  keeps both working.
- **Fingerprint dedup without a key (same body within N seconds):** cannot tell a retry from a
  genuine second identical transfer.

## Consequences

- Money writes in one app are strictly sequential; waits are bounded by the request context (30 s)
  and `statement_timeout` (60 s). Throughput is irrelevant for a single user.
- A lost response no longer duplicates money; the worst case after a crash between commit and
  record completion is a `409` that tells the user to look at the history.
- 180 (siblingcore) inherits both rules for the portfolio-b/portfolio-c services it moves.
- New table `idempotency_keys` in all three databases (`04-db-schema.md`).
