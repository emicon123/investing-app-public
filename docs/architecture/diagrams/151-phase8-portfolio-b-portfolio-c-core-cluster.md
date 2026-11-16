# 151 — Phase 8 diagrams: portfolio-b + portfolio-c core cluster (Positions, Rebalancing, Deposits)

Companion to `.claude/tasks/151-architect-output.md` — read that doc for the full
finding-by-finding justification. This doc holds the UML required by the UML-first workflow
before Backend-Go starts Phase 8. All diagrams below were built from direct inspection of the
actual Java source (file paths cited in the architect-output doc's "Files referenced" section),
not from the phase task file's prose.

---

## 1. Write-ownership — portfolio-b (corrected: 4 controllers, 5 tables, one Phase-6 cross-link)

```mermaid
flowchart LR
    subgraph tables["portfolio-b Postgres tables"]
        L_positions[("positions")]
        L_deposits[("deposits")]
        L_lots[("purchase_lots")]
        L_transfers[("position_transfers")]
        L_cash[("account_cash_snapshot\n— Phase 6 GoOwned already")]
    end

    PositionsController -->|"save / delete"| L_positions
    RebalancingController -->|"read (RebalancingService.calculate)"| L_positions
    RebalancingController -->|"save (via PurchaseConfirmService.confirm)"| L_positions
    RebalancingController -->|"saveAll (via PurchaseConfirmService.confirm)"| L_lots
    RebalancingController -->|"createDeposit (via PurchaseConfirmService.confirm)"| L_deposits
    RebalancingController -->|"getCurrentBalances (read, RebalancingService)\n+ deductPurchases (write, PurchaseConfirmService)"| L_cash
    DepositsController -->|"save"| L_deposits
    PositionTransferController -->|"saveAll (sold + buy legs)"| L_positions
    PositionTransferController -->|"saveAll"| L_transfers
```

**Legend.** `PositionTransferController` is a **fourth** controller in this cluster, not named in
the phase task file's scope list — it lives in `packages/shared/java/.../controller/
PositionTransferController.java`, a generic `@RestController` shared verbatim by both apps,
delegating to each app's own `PositionTransferService` (`PositionTransferExecutor`). It backs the
"Sprzedaj" manual-sell-assist button (the Manual-Sell-Only Rule's actual write path) and writes
`positions` + `position_transfers` in the same transaction. See architect-output §1 Finding 1 for
why this must be in Phase 8's atomic cutover, exactly the way root's Phase 7 pulled in
`RebalancingTransferController`/`PurchaseSessionController` for the same reason (doc 13 §6.3).

`account_cash_snapshot` is the one genuinely new wrinkle vs. root's Phase 7 shape: it is **already
GoOwned** (Phase 6, `internal/accountcash` + `shared-go/accountcash`), and this cluster both reads
it (`RebalancingService.calculate()`'s cash cap) and writes it (`PurchaseConfirmService.confirm()`'s
post-purchase deduction). `shared-go/accountcash/accountcash.go`'s own doc comment already flags
this exact gap: *"getCurrentBalances (portfolio-b's rebalancing-side read helper) STAYS Java-owned — it
is not part of this kernel and must not be re-introduced here."* Phase 8 is where that deferred
read (and the corresponding write) finally gets a Go home — see architect-output §1 Finding 2 and
diagram §3 below.

---

## 2. Write-ownership — portfolio-c (4 controllers, 4 tables, no cross-phase link)

```mermaid
flowchart LR
    subgraph tables["portfolio-c Postgres tables (portfolio-scoped: VARIABLE / FIXED)"]
        M_positions[("positions")]
        M_deposits[("deposits")]
        M_lots[("purchase_lots")]
        M_transfers[("position_transfers")]
    end

    PositionsController -->|"save / delete"| M_positions
    RebalancingController -->|"read (RebalancingService.calculate)"| M_positions
    RebalancingController -->|"save (via PurchaseConfirmService.confirm)"| M_positions
    RebalancingController -->|"saveAll (via PurchaseConfirmService.confirm)"| M_lots
    RebalancingController -->|"createDeposit (via PurchaseConfirmService.confirm)"| M_deposits
    DepositsController -->|"save"| M_deposits
    PositionTransferController -->|"saveAll (sold + buy legs)"| M_positions
    PositionTransferController -->|"saveAll"| M_transfers
```

**Legend.** Structurally identical to portfolio-b's cluster minus the `account_cash_snapshot` edge —
portfolio-c's `PurchaseConfirmService` has no `AccountCashService` dependency at all (confirmed: its
constructor takes only `PositionRepository`, `PriceCacheRepository`, `PurchaseLotRepository`,
`DepositService`, `PositionDeltaApplicator`). Portfolio C's `account_cash_snapshot` table exists (Phase 6
ported it) but is a fully independent, manually-updated figure — nothing in this cluster reads or
writes it. This is the one place the task file's "structurally simpler" claim is *more* true for
portfolio-c than for portfolio-b: portfolio-c's Phase 8 cluster is genuinely table-disjoint from every Phase 6
domain, matching root's "one side owns writes per table at a time" invariant cleanly; portfolio-b's does
not.

---

## 3. Sequence — portfolio-b's cash-aware rebalance-and-confirm flow (the cross-phase coupling)

```mermaid
sequenceDiagram
    participant FE as Frontend
    participant Reb as internal/rebalancing (Phase 8, NEW)
    participant Calc as AllocateProportional (shared-go/rebalancecalc, NEW)
    participant Cash as shared-go/accountcash.Service (Phase 6, EXISTING)
    participant DB as Postgres

    FE->>Reb: POST /purchases/calculate
    Reb->>Cash: GetCurrent(ctx, "")
    Cash->>DB: load account_cash_snapshot (id=1)
    DB-->>Cash: Snapshot{ikeTata, ikeMama, ikzeTata, ikzeMama}
    Cash-->>Reb: Snapshot
    Reb->>Reb: deployable = min(monthlyContribution, balances.total())
    Reb->>Calc: AllocateProportional(rows, deployable, allocateToAccounts=waterfall)
    Calc-->>Reb: []BuyListItem (with per-account allocations)
    Reb-->>FE: RebalancingResultDto

    FE->>Reb: POST /purchases/confirm
    Reb->>DB: BEGIN
    Reb->>DB: update positions.units_ike_tata/... (per allocation)
    Reb->>DB: insert purchase_lots
    Reb->>DB: insert deposits
    Reb->>Cash: Deduct(ctx, "", vals)
    Cash->>DB: update account_cash_snapshot (floored at zero, reason=POST_PURCHASE)
    Reb->>DB: COMMIT
    Reb-->>FE: 204 No Content
```

**Legend.** `Cash` is not a new package — it is the exact `*accountcash.Service` Phase 6 already
built and already exports `GetCurrent`/`Deduct` as plain Go methods (no HTTP hop; both packages
compile into the same `backend-go` binary). Phase 8's `internal/rebalancing` package should take a
`*accountcash.Service` in its constructor (composition-root wiring in `main.go`, same instance
`internal/accountcash`'s own handler already holds) and call it directly — the identical shape
Java uses today (`PurchaseConfirmService`/`RebalancingService` both hold an injected
`AccountCashService` field). This is a same-process in-Go-binary dependency, not a new Nginx
routing concern: `internal/accountcash`'s own HTTP endpoints stay exactly where they are.

Portfolio C has no equivalent diagram — its `confirm()`/`calculate()` never touch `account_cash_snapshot`
(see diagram §2).

---

## 4. Deposit-amount resolution — portfolio-c's FIXED/VARIABLE branch (decision logic)

```mermaid
flowchart TD
    Start(["calculate(portfolio, depositAmountOverride)"]) --> HasOverride{"override\nprovided?"}
    HasOverride -->|yes| UseOverride["depositAmount = override"]
    HasOverride -->|no| IsFixed{"portfolio == FIXED?"}
    IsFixed -->|yes| UseDefault["depositAmount =\nsettings.fixedMonthlyDepositDefaultPln"]
    IsFixed -->|no, VARIABLE| Reject["400 VALIDATION_ERROR —\nVARIABLE has no stored default"]
    UseOverride --> Proceed["AllocateProportional(...)"]
    UseDefault --> Proceed
```

**Legend.** This branch lives one level above `AllocateProportional` in portfolio-c's
`RebalancingService.calculate()` (`resolveDepositAmount`), not inside the six-step algorithm
itself — the Go port keeps it there too: a small per-app function computing the deployable amount,
called before the shared `AllocateProportional`. Portfolio B has an analogous but simpler pre-step (no
branch, just `deployable = min(monthlyContribution, accountCashBalances.total())` — see diagram §3)
— confirming neither app's "compute the deployable amount" step belongs inside the shared function;
it is caller-side per-app logic in both cases, just of different shapes.

---

## 5. Template Method collapse — Java hierarchy vs. the single Go function

```mermaid
classDiagram
    class AbstractProportionalStrategy~T~ {
        <<Java, abstract, shared>>
        #buildRows(List~T~, Map) List~PositionRow~
        #computeTargetValues(List~PositionRow~, BigDecimal)
        #positiveDeficitSum(List~PositionRow~) BigDecimal
        #allocatePln(List~PositionRow~, BigDecimal, BigDecimal)
        #computeUnits(List~PositionRow~, Map)
        #makeItem(PositionRow, BigDecimal) BuyListItemDto
        #allocateToAccounts(List~BuyListItemDto~, Map)* List~BuyListItemDto~
        +calculate(...) List~BuyListItemDto~
    }
    class Portfolio BRebalancingStrategy {
        <<Java, extends, bypasses base calculate()>>
        +calculate(contribution, AccountCashBalances, ...) List~BuyListItemDto~
        #allocateToAccounts() "real IKE/IKZE waterfall"
    }
    class Portfolio CRebalancingStrategy {
        <<Java, extends, delegates to base calculate()>>
        +calculate(depositAmount, ...) List~BuyListItemDto~
        #allocateToAccounts() "inherited no-op passthrough"
    }
    AbstractProportionalStrategy <|-- Portfolio BRebalancingStrategy
    AbstractProportionalStrategy <|-- Portfolio CRebalancingStrategy

    class AllocateProportional {
        <<Go, ONE pure function, shared-go>>
        +AllocateProportional(rows []Row, deployable Decimal, fxRates, totalValuePln, allocateToAccounts func(...) []BuyListItem) []BuyListItem
    }
    class Portfolio BWaterfallCloser {
        <<Go, closure, apps/portfolio-b internal/rebalancing>>
        func(buyList, balances map[AccountType]Decimal) []BuyListItem
    }
    class Portfolio CIdentityCloser {
        <<Go, closure, apps/portfolio-c internal/rebalancing>>
        func(buyList, _ map[AccountType]Decimal) []BuyListItem "return buyList unchanged"
    }
    AllocateProportional ..> Portfolio BWaterfallCloser : passed as func value
    AllocateProportional ..> Portfolio CIdentityCloser : passed as func value
```

**Legend.** Confirms the phase task file's claim with one precision correction: it is not that
"five of six steps are identical" in the sense of both apps calling the same orchestrating
`calculate()` — **portfolio-b bypasses the shared base's own `calculate()` entirely** (documented in its
own class Javadoc: the base's `accountBalances()` hook takes no parameters, which would force a
genuinely per-request value onto singleton-bean instance state — a thread-safety risk portfolio-b's own
code explicitly rejects) and instead calls the five shared steps directly, in the same order, then
passes the real per-call `AccountCashBalances` straight into `allocateToAccounts` as an explicit
argument. Portfolio C calls the base's own `calculate()` unchanged. **The Go port makes this distinction
disappear**: a pure function has no instance-state thread-safety problem in the first place, so the
exact reason portfolio-b had to special-case the orchestration in Java never arises in Go. One
`AllocateProportional` function, called the same way by both apps, is not just possible — it is
strictly simpler than either app's current Java call site.

---

## 6. Notes for Backend-Go

- `AllocateProportional`'s five shared steps port from `AbstractProportionalStrategy.java` verbatim
  (byte-identical `BigDecimal` math → `decimal.Decimal`, `RoundingMode.HALF_UP` → `decimal.Round`
  with the same scales). No behavior change, only the mechanical collapse described above.
- The one Go function should live in a **new** `shared-go/rebalancecalc` package (not
  `shared-go/accountcash`, not app-local) — it has no DB dependency of its own (pure transform,
  matching `AbstractProportionalStrategy`'s own zero-repository shape) and is shared verbatim by
  both apps, going further than root's Phase 7 could (root's `rebalancingcalc` stayed
  investing-app-only because `MainRebalancingStrategy`/`RetirementRebalancingStrategy`/
  `MacroDeriskGate` are genuinely investing-app-specific — see architect-output §4 for the full
  comparison).
- Each app's own `internal/rebalancing` package holds: the deployable-amount pre-step (§3/§4
  above), the `allocateToAccounts` closure, the `PurchaseConfirmService`/`PositionTransferService`
  equivalents (writes), and — portfolio-b only — the `*accountcash.Service` dependency for the cash cap
  and post-purchase deduction.
- `RebalancingStrategyFactory`/`RebalancingMediator` (portfolio-b-only) and `PositionTransferCommand`
  (shared-java, `Execute`+`Undo` with `Undo` unimplemented) are all confirmed dead code — zero call
  sites outside their own definitions (see architect-output §1 Finding 3). **Do not port any of the
  three.** This is a stronger case than doc 13 §5's general Command/Mediator guidance: those were
  reasoned rejections of patterns that had *some* live purpose; these three have none at all in the
  current codebase.
