# 150 — Phase 7 Core Money Cluster: Go Package Design

> **Audience:** Backend-Go, before writing any code under `backend-go/internal/position/`,
> `internal/rebalancing/`, `internal/purchasesession/`, `internal/deposit/`, or the new
> `internal/rebalancingcalc/`. Read this file's diagrams and legends first, plus
> `.claude/tasks/150-architect-output.md` (the file-by-file design-review handoff this diagram doc
> supports). Cross-reference: `docs/architecture/13-go-backend-target-architecture.md` §5/§6.3/§7,
> `docs/architecture/05-rebalancing-algorithm.md` §7.5/§8, `.claude/tasks/go-rewrite-phase-7-core-money-cluster.task.md`.

## 0. Pattern evaluation

| Candidate / principle | Considered | Verdict |
|---|---|---|
| **State** (session `PLANNED`→`DONE`) | Yes | **Adopted, simplified.** Java's `SessionState` interface + `PlannedState`/`DoneState` genuinely varies behavior by current state (4 predicates + 1 transition) — a real State pattern, correctly used, not a smell. Go has no need for the interface-plus-two-structs shape for a 2-state/4-predicate machine: collapses to a `Status` string type + 4 pure functions (`CanUpdate`, `CanComplete`, `CanDelete`, `OnComplete`) doing a `switch`. Same intent (behavior keyed by state), no class ceremony. |
| **Template Method / Factory Method** (`AbstractProportionalStrategy` + `MainRebalancingStrategy`/`RetirementRebalancingStrategy` + `RebalancingStrategyFactory`) | Yes | **Rejected as class hierarchy — collapses to one function (doc 13 §5 point 2, already named there by ticker).** Five of six template steps are identical between MAIN and RETIREMENT; only `allocateToAccounts`-equivalent genuinely differs, and even that is now just "which budget/target source." Go port: one `Calculate` function taking a small `portfolioConfig` value (budget, whether the macro de-risk override applies), not two structs implementing a `RebalancingStrategy` interface selected by a factory. |
| **Visitor** (`XirrVisitor implements ValuationVisitor`, called from `XirrService`) | Yes | **Rejected (doc 13 §5 point 3 / §1).** Confirmed by direct read of `service/history/XirrVisitor.java`: `visitHistoria` is an empty no-op override — pure ceremony. Go port: a plain loop building `[]CashFlow` structs, no `Visitor` interface. |
| **Command** (`PurchaseCommand`/`CreateSessionCommand`/`CompleteSessionCommand`) | Yes | **Rejected — confirmed dead code, do not port.** `grep -rln "CreateSessionCommand\|CompleteSessionCommand\|PurchaseCommand" backend/src` outside `service/purchase/` returns nothing: `PurchaseSessionController` calls `purchaseSessionService.createSession(req)` directly. This is the same "proposed, never wired up" smell doc 13 §5 point 1 names for the repository-factory — except here the dead classes were actually written. Do not port. |
| **Abstract-Factory / repository-factory** (per doc 13 §5 point 1) | N/A here | **Not applicable to Phase 7.** That smell is about cross-app (portfolio-b/portfolio-c) entity production; Phase 7 is investing-app-only. No action needed. |
| **Bridge-via-composition** (`PositionDeltaApplicator`) | Yes | **Adopted, simplified further than doc 13 §5 anticipated.** `PositionDeltaApplicator` already has zero DB dependency in Java — it only mutates an in-memory `Position` entity's `BigDecimal` fields (see architect-output §2). Go port needs no interface at all: two plain functions, `position.ApplyDelta` and `position.UpdateTotalSpentPln`, over a domain struct. Simpler than even a Strategy/Bridge interface. |
| **Unit-of-Work via `TransactFunc` closure** (`func(ctx, func(Queries) error) error`, `pool.Begin`/`WithTx`/commit-rollback) | Yes | **Adopted — direct reuse of the established Phase 1B/2/3/5 idiom**, confirmed present in `backend-go/cmd/server/main.go` (`newMacroService`, `newInflationService`, `newCashPositionsService`, `newHistoriaService`, `newPlanService`, `newTradeService` all build this exact closure). Not a new pattern — Phase 7 needs no invention here, just more instances of the same shape, one per package that needs multi-statement atomicity. |
| **SQL-literal + static-grep guard test** (`TestNoCompletionSourcedWriteInPackage` in `internal/binancetransfers`) | Yes | **Adopted — direct reuse of the established Phase 4 idiom** for enforcing single-writer-per-source-value on a shared table. Phase 7 should add the mirror guard in `internal/purchasesession` (never write `source='MANUAL'`). |

YAGNI/KISS tie-breakers: no per-strategy class hierarchy, no Command objects, no Visitor, no new repository-factory abstraction, no sub-packages beyond what the phase task itself asked for (three files, one package).

---

## 1. Package map and write-ownership (confirms/refines doc 13 §6.3)

```mermaid
flowchart TB
    subgraph http["HTTP-facing packages (Phase 7 scope, one per controller)"]
        POS["internal/position/\nPositionsController port"]
        REB["internal/rebalancing/\nRebalancingTransferController port\n(the manual 'Sprzedaj' endpoint)"]
        PS["internal/purchasesession/\nPurchaseSessionController port\n(3-file split, section 2 below)"]
        DEP["internal/deposit/\nDepositsController port\n(+ XIRR + account-cap-usage)"]
    end

    subgraph calc["internal/rebalancingcalc/ — NEW, not named in doc 13's phase table (see architect-output Finding 1)"]
        RC["Calculate\nport of RebalancingService +\nAbstractProportionalStrategy +\nMainRebalancingStrategy +\nRetirementRebalancingStrategy +\nMacroDeriskGate/MacroDeriskService +\nValueComputationService\n(pure computation, read-only)"]
    end

    subgraph existing["Already shipped (Phase 1/1B/4) — read-only inputs"]
        MKT["internal/marketdata/\nprices, fx rates"]
        MACRO["internal/marketdata/macro/\nblend_factor"]
        BT["internal/binancetransfers/\nread/list/MANUAL insert only"]
        SET["internal/settings/"]
    end

    POS -->|"ApplyDelta / UpdateTotalSpentPln\n(pure functions, no DB)"| POSDELTA["position.ApplyDelta\nposition.UpdateTotalSpentPln"]
    REB -->|"imports"| POSDELTA
    PS -->|"imports (execution.go)"| POSDELTA
    POSDELTA -.->|"one canonical UPDATE query"| positions[("positions")]

    POS -->|"read: effectiveTargetAllocationPct"| RC
    PS -->|"read: buy-list generation (planning.go)"| RC
    RC -->|"read"| positions
    RC -->|"read"| MACRO
    RC -->|"read"| SET
    RC -->|"read: cash snapshot"| CASHPOS["internal/cashpositions/"]

    REB -->|"write: sold/bought legs"| assetpurchases[("asset_purchases")]
    PS -->|"write: completeSession"| assetpurchases
    REB -->|"write: REGULAR->IKE/IKZE auto-deposit"| depositsT[("deposits")]
    PS -->|"write: session-completion auto-deposit"| depositsT
    DEP -->|"write: manual create"| depositsT
    PS -->|"write: SESSION-sourced insert"| binancetransfers[("binance_transfers")]
    BT -->|"write: MANUAL-sourced insert only"| binancetransfers
    PS -->|"write"| purchasesessions[("purchase_sessions, purchase_session_items")]

    DEP -->|"read: XIRR terminal value"| RC
```

*Legend.* Solid arrows are compile-time Go imports or DB writes; dashed arrows call out the single
canonical SQL entry point for a table two-or-more packages touch. Two tables have more than one
writer, exactly as doc 13 §6.3 found in Java — `positions` (via the shared `position.ApplyDelta`
pure function, called from three packages but backed by **one** sqlc UPDATE query) and
`binance_transfers` (via **two separate** sqlc INSERT queries, one per package, each hardcoding its
own `source` literal — the same split Phase 4 already built and statically enforced, see
architect-output §2). `deposits` has **three** writers (`internal/deposit` manual create,
`internal/rebalancing` auto-deposit, `internal/purchasesession` auto-deposit) but only **one**
`InsertDeposit` sqlc query — no shared abstraction needed beyond the one generated method, since
there is no arithmetic to share (each caller just sets its own `capital_source`).

---

## 2. `internal/purchasesession/` — the 3-file split

```mermaid
classDiagram
    class Service {
        <<top-level orchestrator, HTTP-facing>>
        -sessionCache : create-to-complete bridge cache, keyed by session id
        -cacheMu sync.Mutex
        +CreateSession(ctx, req) SessionDetail
        +GetActiveSession(ctx) SessionDetail
        +UpdateSessionItems(ctx, id, req) SessionDetail
        +CompleteSession(ctx, id) CompleteResult
        +DeleteSession(ctx, id) error
        +ListSessions(ctx, page) Page~SessionSummary~
        <<owns the create-to-complete bridge cache,\nport of newDepositPlnByBucketCache — same\ncache-miss fallback semantics on process restart>>
    }

    class lifecycle_go {
        <<file: session status guards + purchase_sessions/\npurchase_session_items CRUD>>
        +Status : PLANNED | DONE
        +CanUpdate(Status) bool
        +CanComplete(Status) bool
        +CanDelete(Status) bool
        +OnComplete(Status) Status
        <<port of SessionStateFactory/PlannedState/DoneState\n— collapsed from interface+2 structs to\nvalue type + switch, per pattern eval above>>
    }

    class planning_go {
        <<file: PurchasePlanningService port — ZERO DB dependency>>
        +ComputePlan(retirementList, mainList, ikeRemaining, ikzeRemaining, pricePlnMap, allPositions) ComputePlanResult
        -processPortfolio(...)
        -applyCryptoSellStep(...) CryptoSellResult
        -findBestCryptoDonor(...) Position
        -findCryptoDonorCandidate(...) Position
        -upsertCompanionRow(...) SessionItem
        -reconcileOwedCompensatingBuys(...)
        <<pure functions over plain Go values —\nno Queries interface, no DB, testable\nwithout testcontainers>>
    }

    class execution_go {
        <<file: completeSession's write half ONLY —\nnot createSession's own session/item save,\nwhich stays in lifecycle.go>>
        +CompleteSession(ctx, q Queries, transact TransactFunc, sessionID) CompleteResult
        -classifyTradeType(item) TradeType
    }

    class Queries {
        <<interface, execution.go>>
        GetSessionForUpdate(ctx, id) Session
        ListSessionItemsOrdered(ctx, sessionID) []SessionItem
        UpdateSessionStatus(ctx, ...) error
        InsertAssetPurchase(ctx, ...) error
        GetPositionByTickerAndPortfolio(ctx, ...) Position
        UpdatePositionDelta(ctx, ...) Position
        InsertDeposit(ctx, ...) Deposit
        InsertBinanceTransferSession(ctx, ...) BinanceTransfer
    }

    Service --> lifecycle_go : composes
    Service --> planning_go : composes (called from lifecycle's CreateSession)
    Service --> execution_go : composes
    execution_go --> Queries : depends on
    execution_go ..> position_ApplyDelta : imports pure function
    lifecycle_go ..> rebalancingcalc_Calculate : imports (read-only, tx-scoped)
```

*Legend.* This confirms the phase task's proposed 3-way split **is** the right decomposition, with
one refinement: `createSession`'s own persistence of the new `purchase_sessions`/
`purchase_session_items` rows belongs to **lifecycle.go**, not execution.go — it never touches
`positions`/`deposits`/`asset_purchases`/`binance_transfers`. Only `completeSession`'s body does,
so **execution.go's scope is exactly and only `completeSession`** — this is a narrower, more
precise boundary than "trade-execution persistence" might suggest at first read, and it matters
because it means lifecycle.go's own transaction is cheap (session-table-only) while execution.go's
transaction is the one that needs the full write-ownership cluster and the pessimistic lock. See
architect-output §1 for the line-by-line justification.

---

## 3. Sequence — `CreateSession` (planning phase, read-only + session-table writes only)

```mermaid
sequenceDiagram
    participant U as User (POST /purchase-sessions)
    participant Svc as purchasesession.Service
    participant RC as rebalancingcalc.Calculate
    participant Plan as planning.ComputePlan
    participant Q as Queries (tx-scoped)
    participant DB as Postgres

    U->>Svc: CreateSession(req)
    Svc->>Svc: transact.Begin (session-table tx)
    Svc->>RC: Calculate(ctx, q, RETIREMENT) (only if included)
    RC->>DB: read positions, macro_state, settings, cash_positions
    RC-->>Svc: retirementResult (buyList, budget)
    Svc->>RC: Calculate(ctx, q, MAIN) (only if included)
    RC-->>Svc: mainResult (buyList, budget)
    Note over Svc,RC: Only the portfoliosIncluded portfolios are computed (task 192:\nthe donor order is fixed, so no deviation maps are needed)
    Svc->>Q: GetAccountLimits (IKE/IKZE remaining, reads deposit.AccountCapUsage — read-only cross-package call)
    Svc->>Plan: ComputePlan(retirementList, mainList, limits, prices, positions)
    Plan-->>Svc: ComputePlanResult{items, newDepositPlnByBucket}
    Svc->>Q: InsertSession + InsertSessionItems
    Svc->>Svc: sessionCache[id] = newDepositPlnByBucket
    Svc->>Svc: transact.Commit
    Svc-->>U: SessionDetail
```

*Legend.* `rebalancingcalc.Calculate` and `planning.ComputePlan` never write; the only writes in this
flow are `InsertSession`/`InsertSessionItems`, both scoped to `purchase_sessions`/
`purchase_session_items`. The account-cap-usage read is a cross-package call into `internal/deposit`
— read-only, safe regardless of which package "owns" `deposits` writes (same shape as historia's
existing read-only dependency on `positions`, doc 13 §6.3).

---

## 4. Sequence — `CompleteSession` (the money-critical transaction — task item 4)

```mermaid
sequenceDiagram
    participant U as User (POST /purchase-sessions/{id}/complete)
    participant Svc as purchasesession.Service
    participant Exec as execution.CompleteSession
    participant Q as Queries (tx-scoped)
    participant Pos as position.ApplyDelta (pure fn)
    participant DB as Postgres

    U->>Svc: CompleteSession(id)
    Svc->>Exec: CompleteSession(ctx, transact, id)
    Exec->>Exec: transact.Begin
    Exec->>Q: GetSessionForUpdate(id)  Note: SQL FOR UPDATE — MUST be first statement
    Q->>DB: SELECT ... FOR UPDATE
    Note over Exec,DB: Pessimistic lock — a concurrent double-submit\nblocks here until the first tx commits, then hits\nthe DONE guard below (port of findWithLockById)
    Exec->>Exec: lifecycle.CanComplete(session.Status)?
    alt already DONE
        Exec-->>Svc: error SessionAlreadyDone
    else PLANNED
        loop each session item, ordered isMigrationSellDesc,portfolioAsc,plannedValuePlnDesc
            Exec->>Q: InsertAssetPurchase(item)
            Exec->>Q: GetPositionByTickerAndPortfolio(item.ticker, item.portfolio)
            Exec->>Pos: ApplyDelta(pos, item.accountType, effectiveUnits)
            Pos-->>Exec: mutated position struct
            Exec->>Pos: UpdateTotalSpentPln(pos, units, valuePln, oldTotalUnits)
            Exec->>Q: UpdatePositionDelta(pos)
        end
        Exec->>Exec: fold newDepositPlnByBucket (from sessionCache, ADR-065 §5)\nOR fall back to group-sum-then-subtract reconstruction\nif cache-miss (process restart between create and complete)
        Exec->>Q: InsertDeposit (one row per qualifying group)
        Exec->>Q: InsertBinanceTransferSession (if any BINANCE-routed group > 0)
        Exec->>Q: UpdateSessionStatus(DONE)
        Exec->>Exec: transact.Commit
        Exec-->>Svc: CompleteResult
    end
    Svc-->>U: CompleteResult
```

*Legend.* Everything inside the `alt PLANNED` branch is ONE transaction — a failure at any point
(e.g. a bad price, a constraint violation on the `positions` UPDATE) rolls back the `asset_purchases`
inserts, every `positions` delta already applied earlier in the loop, and the deposit/binance-transfer
inserts, matching Java's single `@Transactional completeSession` exactly. The `FOR UPDATE` lock is the
single most important line to get right in this whole phase — see architect-output §4 for why.

---

## 5. Sequence — `ExecuteTransfer` (manual "Sprzedaj" — the ONLY sanctioned sell path)

```mermaid
sequenceDiagram
    participant U as User (clicks "Sprzedaj", fills sold+buy legs)
    participant Ctl as POST /rebalancing-transfers
    participant Svc as rebalancing.ExecuteTransfer
    participant Q as Queries (tx-scoped)
    participant Pos as position.ApplyDelta (pure fn)

    U->>Ctl: RebalancingTransferRequest{portfolio, sold: {ticker,accountType,units,pricePln}, buys: [...]}
    Ctl->>Svc: ExecuteTransfer(req)
    Svc->>Svc: transact.Begin
    Svc->>Q: load positions for (ticker,portfolio) resolution
    Svc->>Svc: validate sold ticker exists, sufficient units
    Svc->>Svc: validate wrapper lock (IKE/IKZE sold -> same-wrapper buys only)
    Note over Svc: sold.ticker/units/accountType come ENTIRELY from the\nrequest body — never computed from a deviationPct\nor any rebalancing target. This is the manual-sell-only\nboundary; nothing upstream of this handler is reachable\nfrom rebalancingcalc.Calculate or planning.ComputePlan.
    Svc->>Pos: ApplyDelta(soldPosition, sold.accountType, sold.units.Neg())
    Svc->>Q: UpdatePositionDelta(soldPosition) + InsertAssetPurchase(REBALANCING_TRIM_SELL)
    loop each buy leg
        Svc->>Pos: ApplyDelta(buyPosition, buy.accountType, buy.units)
        Svc->>Q: UpdatePositionDelta(buyPosition) + InsertAssetPurchase(REBALANCING_REINVEST_BUY)
        alt sold.accountType == REGULAR AND buy.accountType in {IKE, IKZE}
            Svc->>Q: InsertDeposit(capitalSource=INTERNAL_TRANSFER)
        end
    end
    Svc->>Svc: transact.Commit
    Svc-->>U: RebalancingTransferResult
```

*Legend.* This whole endpoint IS the sanctioned manual sell path (§7.5's "Sprzedaj" button). There is
no computation anywhere in this sequence that decides *what* to sell — only validation of *whether*
what the user asked for is legal (sufficient units, wrapper lock). See architect-output §3 for the
exhaustive list of every other write path and why none of them can reach a sell without this exact
kind of explicit, request-body-supplied instruction.

---

## 6. State — `PurchaseSession` lifecycle (correction: 2 states, not 3)

```mermaid
stateDiagram-v2
    [*] --> PLANNED : CreateSession
    PLANNED --> PLANNED : UpdateSessionItems (CanUpdate=true)
    PLANNED --> DONE : CompleteSession (CanComplete=true, FOR UPDATE lock held)
    PLANNED --> [*] : DeleteSession (CanDelete=true)
    DONE --> DONE : any further UpdateSessionItems/CompleteSession/DeleteSession attempt\n(CanUpdate/CanComplete/CanDelete all false -> SessionAlreadyDoneException)
```

*Legend.* The phase task file's own prose describes this as "draft → confirmed → done" (three
states). The actual `SessionStatus` enum (`backend/src/main/java/pl/investing/entity/enums/SessionStatus.java`)
has exactly two values, `PLANNED` and `DONE` — there is no separate "confirmed" state anywhere in
the code. The Go port must implement this 2-state machine, not a 3-state one; see
architect-output §1 for why this matters for scoping the `lifecycle.go` file's `Status` type.
