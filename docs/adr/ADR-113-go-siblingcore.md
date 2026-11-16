# ADR-113: Go `siblingcore` — one shared service core for the portfolio-b and portfolio-c backends

**Date:** 2026-11-04
**Status:** Accepted (owner decision 4 of task 174, 2026-11-04; design: Architect, batch 180)
**Amends:** ADR-076 §2, for the **Go backends of portfolio-b and portfolio-c only**. ADR-076 stays in force for
the frontends, for main, and for everything this ADR does not name.
**Design:** `docs/architecture/diagrams/180-siblingcore-design.md` (diagrams, exact signatures,
migration waves). **Living doc:** `docs/architecture/12-sibling-apps-and-shared-packages.md` §2.2.

## Context

ADR-076 (2026-08-20, owner choice b) allowed only code that is "~100 % identical with no domain
coupling" into shared packages and kept every per-app service local "indefinitely". Since then
both siblings moved to Go (ADR-094/098), and seven common-go kernels were extracted under that
rule (`settingscore`, `accountcash`, `marketdatacore`, `historiaquery`, `inflationsync`,
`rebalancecalc`, `positioncalc`). What is left is the service layer around those kernels, and it is
still a fork. Task 174 (F2.4) measured it; this ADR re-measured it with comments and blank lines
removed and the module path normalised (`portfolio-b` / `portfolio-c` → `APP`):

| Package | portfolio-b code lines | identical with portfolio-c | What differs (classified) |
|---|---:|---:|---|
| `marketdata` | 227 | 100 % | nothing |
| `inflation` | 68 | 98 % | comment-level only |
| `cmd/server` | 356 | 96 % | port, JWT issuer, service name (per-app literals); portfolio-b passes the account-cash service to rebalancing |
| `settings` | 126 | 95 % | the registered keys (already a `settingscore.Validator` parameter) |
| `historia` | 400 | 89 % | **portfolio scoping**; **units model** (5 columns summed vs 1); scheduler loops over portfolios; inflation base date (already `historiaquery.Config`); wire `portfolio` field |
| `position` | 1,428 | 86 % | **portfolio scoping** (4 queries + 2 "same portfolio" checks); **units model** (5 buckets vs 1); one error text names the portfolio; wire units fields |
| `deposit` | 301 | 84 % | **portfolio scoping**; **units model** (total only) |
| `positiontransfer` | 336 | 81 % | **portfolio scoping**; **units model** (leg `accountType` picks a bucket in portfolio-b; audit row stores it) |
| `rebalancing` | 805 | 70 % | **portfolio scoping**; **units model** (confirm items carry per-bucket allocations); **genuinely divergent funding policy** (below) |
| `admin` (backfill) | 413 | 59 % | **genuinely divergent algorithm**: portfolio-b has a cutoff date, replays `position_transfers`, folds subassets into their primary; portfolio-c rewinds purchase lots only, per portfolio |
| `accountcash` | 182 | 49 % | **genuinely divergent schema**: one row with 4 IKE/IKZE balances vs one row per portfolio (the kernel is already shared) |

So nearly all of the difference is two orthogonal, data-shaped axes, not behaviour:

1. **Portfolio set.** portfolio-b has no `portfolio` column (one book). portfolio-c partitions every domain
   table by `portfolio_enum {VARIABLE, FIXED}`. portfolio-b behaves exactly like portfolio-c with one fixed
   portfolio: portfolio-c's extra checks ("asset belongs to a different portfolio") can never fire there.
2. **Unit buckets.** portfolio-b stores units in 5 account-type columns (`IKE_TATA`, `IKE_MAMA`,
   `IKZE_TATA`, `IKZE_MAMA`, `INNE`); portfolio-c stores one `units` column. Every units operation
   (total, units held in a bucket, apply a delta to a bucket, per-bucket patch) is the same
   arithmetic over a different bucket set. (CLAUDE.md for portfolio-c says "single units"; the code agrees.)

Two policies are genuinely different and must stay per app:

- **Rebalancing funding.** portfolio-b deploys `min(monthly_contribution_pln setting, Σ IKE/IKZE cash)`,
  splits each buy over the 4 IKE/IKZE accounts by a cash waterfall, and on confirm deducts account
  cash per account and records the monthly contribution as the deposit. portfolio-c takes the deposit
  amount from the request (FIXED falls back to its stored default; VARIABLE requires it), allocates
  without accounts, and on confirm records that amount as the deposit.
- **Admin position-values backfill** (one-shot, ADR-097): two different algorithms (table above).

Schema end state (sqlc `models.go` of both apps, plus the migrations' constraints):

| Tables | portfolio-b vs portfolio-c |
|---|---|
| `price_cache`, `fx_rate_cache`, `inflation`, `settings` | column-identical (and identical to main) |
| `assets`, `deposits`, `historia`, `purchase_lots` | identical except portfolio-c's extra `portfolio` column; uniqueness is global in portfolio-b (`assets.key`, `historia.snapshot_date`) and per portfolio in portfolio-c (`(key, portfolio)`, `(portfolio, snapshot_date)`) |
| `position_transfers` | portfolio-c adds `portfolio`; portfolio-b adds `account_type` |
| `positions` | portfolio-c adds `portfolio` and has `units`; portfolio-b has the 5 `units_*` columns; uniqueness `ticker` vs `(ticker, portfolio)` |
| `account_cash_snapshot` | different shape (see above) |

## Options considered

| # | Option | Verdict | Why |
|---|---|---|---|
| 1 | Status quo: two forks | rejected | Owner rule (2026-11-04): same or very similar code in 2–3 apps goes to `packages/common`. The fork already drifted in ways that cost real fixes twice (task 173 shipped twin commits `ea932fe`/`1874edd`; portfolio-b's backfill replays position transfers, portfolio-c's does not). |
| 2 | Keep extracting per function (the task 158/163 style) | rejected as the mechanism | A function typed on each app's `sqlcgen` rows cannot move without a seam, and the seam per function costs about what it saves (163 declined G3 `assetjoin` for exactly that reason). It would leave the 130-line `position.Service.Create` and `positiontransfer.ExecuteTransfer` duplicated. |
| 3 | **One `common-go/siblingcore` (service core) + per-app adapters** | **adopted** | One seam (domain types + consumer-side ports) pays for the whole service layer at once. It is the established common-go shape (seven kernels already do it). |
| 4 | One merged deployable serving both apps (tenant switch) | rejected (owner) | Owner decision 4: portfolio-b and portfolio-c stay separate deployables with their own `main.go`, database, sqlc adapters and Flyway migrations. A shared process would also put two family members' books behind one failure domain. |

### How shared code talks to per-app sqlc rows

| Seam | Verdict | Why |
|---|---|---|
| **(a) Domain types in siblingcore + a per-app Store adapter implementing small consumer-side ports** | **adopted** | Seven common-go kernels already use it, so there is nothing new to learn. siblingcore stays unit-testable with in-memory fakes. Each app keeps its schema freedom. Cost: one per-app adapter (~250–300 lines each) that maps rows to domain values, most of which already exists as each app's `decode.go`/`decodeUnits`. |
| (b) Generics over the per-app row types | rejected | Go type parameters cannot constrain struct fields, so the shared code would need accessor methods on generated sqlc structs (hand-written files inside generated packages) and generic versions of every `…Params` struct, which already differ by the `portfolio` field. More code than (a), and harder to read. Generics are used in one narrow place only: the constructors `NewService[S Port](…)` bind each service to its transaction runner without a type assertion. |
| (c) A shared sqlc query set generated into common-go | rejected for the core tables, deferred for the reference tables | Only the four reference tables are column-identical; every core table differs (`portfolio`, units columns), so (c) cannot cover position/transfer/rebalancing/historia/deposit. For `price_cache`/`fx_rate_cache`/`settings`/`inflation` it would work (and they are identical in main too), so it is an optional wave (design doc §6, wave 5), taken only if batch 179 has not already removed those adapters. |
| (c') Converge the schemas (give portfolio-b a `portfolio` column) | rejected | A Flyway migration on a live family database for no user-visible change; the units difference would remain anyway (YAGNI). |

### The units-model difference

| Shape | Verdict | Why |
|---|---|---|
| `UnitsExtractor` interface (doc 13 §5's original design) | rejected | It covers only reads. Writes vary too (apply a delta to a bucket, per-bucket patch, per-bucket validation and lot rules), so the interface would grow to 4–5 methods per app that are all the same arithmetic. |
| Generics over each app's own units struct (`Service[U]`, the `costmember.Member[H]` style) | rejected | The type parameter would spread into every DTO, input and port of five packages for one difference that is data, not behaviour. |
| **A bucket set in configuration + one immutable `Units` value keyed by bucket** | **adopted** | The variation becomes data (`Config.Buckets`): portfolio-b lists 5 buckets, portfolio-c lists one. Same idea as the existing `accountcash.Snapshot.Balances` (map keyed by the app's own key list). `Units` copies on write, so value semantics hold. `*Holding` implements `costmember.Holding` once for both apps. |

### Handlers (the HTTP edge)

Handlers stay per app in waves 1–4. They are the adapters to each app's own OpenAPI contract, and
that contract really differs (units fields, `portfolio` params, account allocations, account-cash
shape). The domain handlers already write hand-written float64 wire structs, so an optional wave 6
shares the wire structs of the endpoints whose only difference is the `portfolio` field
(`omitempty`). Position and rebalancing-calculate wire shapes stay per app. See design doc §6.

## Decision

1. **Scope.** A new directory `packages/common/go/common-go/siblingcore/` (module
   `investing-app/common-go`) holds the service core shared by portfolio-b and portfolio-c only. main never
   imports it. Anything that also serves main belongs to the batch 176–179 packages
   (`pgtx`, `lifecycle`, `config`, `httpx`, `idempotency`, `xirr`, `decx`, `pgerr`, `costmember`,
   `rebalancecalc`), which siblingcore consumes and never re-implements.
2. **Packages.** `siblingcore` (root: `Config`, `Portfolio`, `Bucket`, `Units`, `Holding`, `Asset`,
   `Subasset`, `TxFunc`, `PriceSource`), `siblingcore/position`, `siblingcore/transfer`,
   `siblingcore/rebalancing`, `siblingcore/snapshot`, `siblingcore/deposit`. Optional:
   `siblingcore/refdata` (wave 5), `siblingcore/wire` (wave 6). Each sub-package declares its own
   consumer-side `Store` port over siblingcore domain types only. No sub-package imports another
   sub-package except through the root types.
3. **Per-app configuration.** Each app declares one `siblingcore.Config` in `internal/book`:
   portfolio-b `Portfolios {WholeBook}` (the empty `Portfolio` = the whole book) and 5 `BucketSpec`s
   (`INNE` marked not purchasable); portfolio-c `Portfolios {VARIABLE, FIXED}` and one bucket
   (`SingleBucket`). portfolio-c's three copies of `[]string{"VARIABLE","FIXED"}` (historia, admin,
   accountcash) read this one value.
4. **Per-app adapters.** Each app gets one driven adapter, `internal/store` (`*store.Queries`
   wrapping its `*sqlcgen.Queries`), which implements every siblingcore port structurally and does
   all row ↔ domain mapping (portfolio binding, bucket ↔ column mapping). The driving adapters
   (`internal/*/handler.go`, the `apiServer` forwarding in `cmd/server`) stay per app and map
   siblingcore DTOs to that app's wire structs.
5. **Policies stay local behind one port.** `rebalancing.Funding[S]` (Strategy) has two
   implementations, one per app, living in that app's `internal/rebalancing/funding.go`. The admin
   backfill algorithms and `internal/accountcash` stay per app (declined, reasons above).
6. **Transactions and money-write safety** come from batches 176/177 unchanged: services take a
   `siblingcore.TxFunc[S]`-shaped runner (never a pool); each app wires position, transfer and
   rebalancing-confirm with `pgtx.SerializedRunner(pool, pgtx.MoneyWriteLockKey, …)`, and every
   read that feeds a money write happens inside that transaction (ADR-111).
7. **Behaviour is preserved.** Wire bytes, status codes and error texts stay what each app returns
   today, including portfolio-b's portfolio-free error texts. The only intended deltas are listed in the
   design doc §7 (each with a test); the main one: a portfolio-b transfer or confirm leg whose
   `accountType` is missing or unknown becomes `400 VALIDATION_ERROR` before any write, instead of
   today's misleading units check followed by a `500` from the enum cast.
8. **ADR-076 §2 is amended:** for the Go backends of portfolio-b and portfolio-c, "every other class stays
   app-local indefinitely" no longer holds. Code that differs only by portfolio set or bucket set
   goes to siblingcore. Code with a real domain difference stays local, and the reason is recorded
   in doc 12.

## Consequences

- One copy of the sibling service layer: about **2,050 fewer lines** across both apps for the core
  waves 1–4 and about **2,700** with the optional waves 5–6 (non-test Go, comments included,
  ±20 %; per-wave numbers in the design doc §6). The per-app adapters add roughly 600 lines back,
  which is why this is below the review's "≈3,000".
  The duplicated unit tests shrink by a similar amount; integration tests stay per app.
- A fix to positions, transfers, confirm, snapshots or deposits lands once and reaches both apps.
  The per-app integration suites (Testcontainers, each app's real migrations) now also guard the
  shared code, so both suites must run for any siblingcore change.
- portfolio-b and portfolio-c still deploy, migrate and fail independently. A siblingcore change rebuilds both
  images (they already rebuild together whenever common-go changes).
- New coupling: a portfolio-c-only feature in a shared service now needs either a config value or a new
  port method, not a local edit. That is the intended pressure. If the apps ever diverge
  for real, a package moves back into the app (the per-app ports make that a mechanical move).
- doc 13 §5's "one PositionService with a UnitsExtractor seam" is realised for the two siblings,
  with a bucket set instead of an extractor. main keeps its own position service (its divergence:
  MAIN/RETIREMENT, multi-currency regular units, purchase sessions).
- Order: batch 180 runs after 176–179 and consumes their APIs (contract:
  `docs/architecture/diagrams/174-remediation-design.md` §2). Batch 182 (money in decimals on the
  wire) and 183 (`go fix`) run after it and touch the slimmer per-app handlers.
