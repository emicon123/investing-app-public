# 142 — Promote a companion row to main (swap group leadership)

> **Audience:** Backend and Frontend-web, before touching `PositionService` (any of
> `pl.investing`/`pl.portfolio-b`/`pl.portfolio-c`), `PositionController`, or
> `packages/shared/ui/src/SubPositionRows.tsx`. Read this file's diagrams and legends first —
> this is the committed contract task 142 delegates against. Cross-reference:
> `docs/adr/ADR-088-companion-row-promote-swap.md` (full rationale, including why the
> donor/substitution mechanics in §7.8/§8 of `05-rebalancing-algorithm.md` need no code change),
> `docs/architecture/02-domain-model.md` §2.1 + Decision 13, `docs/architecture/03-api-contract.md`
> (`POST /positions/{id}/promote`), `docs/openapi/paths/positions.yaml` (`promote:` operation).

## 0. Pattern evaluation

| Pattern / principle | Considered | Verdict |
|---|---|---|
| **New dedicated action endpoint** (`POST /positions/{id}/promote`) vs. extending `PUT /positions/{id}` to accept the companion fields | Yes | **Adopted: dedicated endpoint.** A `PUT` that could silently flip `isCompanionRow` on one row alone could never satisfy the CHECK constraint or the sibling-repoint/target-sum invariants — those require touching 2+ rows atomically as one operation with its own validation, which is a fundamentally different shape from `UpdatePositionRequest`'s "patch a few scalar fields on one row" contract. Reusing `PUT` would either silently corrupt a group (partial update) or require `UpdatePositionRequest` to grow a bespoke multi-row side effect, which is worse than naming the operation for what it is. |
| **Guard clause / fail-fast validation chain** (existence → not-already-main → parent resolves → invariant holds, each throwing immediately) | Yes | **Adopted.** Same shape `deletePosition`/`RebalancingTransferService.executeTransfer` already use — sequential checks before any mutation, no partial-mutation risk, no new abstraction needed for 4 checks. |
| **Chain of Responsibility** (each validation step as a pluggable handler) | Yes | **Rejected — YAGNI**, same reasoning task 137 used for companion create/delete: a fixed 4-step sequential validation is not a pipeline that needs runtime reconfiguration. |
| **Command pattern** (`PromotePositionCommand` object capturing before/after state, undo()) | Yes | **Rejected — YAGNI.** The operation is naturally undoable already (promote the demoted row back — see ADR-088 "Reversible by construction"), so a formal undo stack adds a class hierarchy for a property the domain already gives for free via a second API call. |
| **Reuse existing exception + handler for the sum invariant** (`RebalancingInvariantException` / `400 REBALANCING_INVARIANT_ERROR`) vs. a new `PromoteInvariantException` | Yes | **Adopted: reuse.** Same failure mode (target allocations must sum to 100 for a portfolio), same remediation (fix the data), same HTTP semantics (400, actionable input, not a server fault) — a second exception class for the identical concept would be needless duplication (DRY). |
| **Template Method — de-duplicate `promotePosition` across `pl.investing`/`pl.portfolio-b`/`pl.portfolio-c`** | Yes | **Considered, not adopted in this task** — same call task 137 made for `deletePosition`'s cascade: a shared-Java abstraction is a real, larger refactor (tracked informally as a good follow-up once `packages/shared/java` exists for more than one method), not a blocker for shipping the same fix independently in three near-identical `PositionService` copies now. |
| **Involution / self-inverse operation as an explicit domain property** | — | **Noted, not a "pattern" to adopt/reject** — worth stating in the legend below because it directly informs the Frontend-web confirmation-UX decision (a lightweight confirm is enough; the action is not destructive). |

YAGNI also ruled out a request body — `{id}` (which companion becomes main) fully determines the
operation; there is nothing else for a caller to specify.

---

## 1. Type & exception contract — class diagram

```mermaid
classDiagram
    class Position {
        +Long id
        +String ticker
        +Portfolio portfolio
        +boolean isCompanionRow
        +String? companionParentTicker
        +BigDecimal? targetAllocationPct
        +BigDecimal? targetAllocationPctRiskOff
        <<unchanged entity — no new columns, no migration>>
    }

    class PositionService {
        +getPosition(Long id) PositionDto
        +updatePosition(Long id, UpdatePositionRequest) PositionDto
        +promotePosition(Long id) PositionDto
        <<NEW method — pl.investing / pl.portfolio-b / pl.portfolio-c,\n3 independent copies, same contract>>
    }

    class RebalancingInvariantException {
        <<existing — GlobalExceptionHandler already maps\nthis to 400 REBALANCING_INVARIANT_ERROR>>
    }

    PositionService --> Position : reads 2+ rows,\nupdates atomically in one\n@Transactional method
    PositionService ..> RebalancingInvariantException : throws if post-swap\ntarget sums != 100

    class PositionController {
        +GET /positions/{id}
        +PUT /positions/{id}
        +POST /positions/{id}/promote
        <<NEW route — no request body>>
    }
    PositionController ..> PositionService : promotePosition(id)

    class PositionSingleResponse {
        +Position data
        +Meta meta
        <<REUSED — identical shape to GET/PUT /positions/{id};\nno new DTO>>
    }
    PositionController ..> PositionSingleResponse : 200 response

    class SubPositionRowsProps {
        <<packages/shared/ui/src/SubPositionRows.tsx>>
        +onPromoteClick? (sub) void
        <<NEW — "Set as main" action,\nrendered only on companion sub-rows>>
    }
```

**Legend / key decisions:**
- No new exception type — `promotePosition` reuses `IllegalArgumentException`
  (→ existing `400 VALIDATION_ERROR`), `NoSuchElementException` (→ existing `404 NOT_FOUND`), and
  `RebalancingInvariantException` (→ existing `400 REBALANCING_INVARIANT_ERROR`), exactly the
  three exception types `PositionService`/`RebalancingTransferService` already throw elsewhere.
- No new DTO — the response is the same `PositionSingleResponse` `GET`/`PUT /positions/{id}`
  already return, now describing the **promoted** row (its `subPositions` include the demoted
  old main).
- `Position` entity gains no columns — this is a pure runtime relabeling of 4 existing columns
  across 2+ existing rows (see the sequence diagram below).

---

## 2. Sequence — promote a companion to main

```mermaid
sequenceDiagram
    actor User
    participant Table as PositionsTable / SubPositionRows
    participant API as POST /positions/{id}/promote
    participant Svc as PositionService.promotePosition
    participant Repo as PositionRepository

    User->>Table: click "Set as main" on a companion sub-row
    Table->>API: POST /positions/{companionId}/promote
    API->>Svc: promotePosition(companionId)
    Svc->>Repo: findById(companionId)
    Repo-->>Svc: companion
    alt companion.isCompanionRow == false
        Svc-->>API: throw IllegalArgumentException\n("already main")
        API-->>Table: 400 VALIDATION_ERROR
    else companion.companionParentTicker == null
        Svc-->>API: throw IllegalArgumentException\n("orphaned companion")
        API-->>Table: 400 VALIDATION_ERROR
    else
        Svc->>Repo: findByTickerAndPortfolioAndIsCompanionRowFalse(\n  companion.companionParentTicker, companion.portfolio)
        alt no such row
            Svc-->>API: throw IllegalArgumentException\n("parent does not resolve")
            API-->>Table: 400 VALIDATION_ERROR
        else oldMain found
            Svc->>Repo: findAllByCompanionParentTickerAndPortfolio(\n  oldMain.ticker, companion.portfolio)
            Repo-->>Svc: siblings (excluding companion itself)
            Note over Svc: in-memory swap —\ncompanion <- oldMain's targets, isCompanionRow=false,\ncompanionParentTicker=null;\noldMain -> isCompanionRow=true,\ncompanionParentTicker=companion.ticker,\ntargets=null;\nsiblings.companionParentTicker <- companion.ticker
            Svc->>Svc: verify Σ target_allocation_pct == 100\n(+ Σ COALESCE(riskOff, riskOn) == 100 if MAIN)\nfor companion.portfolio, post-swap
            alt invariant fails
                Svc-->>API: throw RebalancingInvariantException
                API-->>Table: 400 REBALANCING_INVARIANT_ERROR
            else invariant holds
                Svc->>Repo: saveAll([companion, oldMain, ...siblings])
                Repo-->>Svc: ok (one transaction)
                Svc->>Svc: reload + recompute\n(computed fields, subPositions —\nsame path getPosition() already uses)
                Svc-->>API: PositionDto (promoted row, now main)
                API-->>Table: 200 PositionSingleResponse
            end
        end
    end
    Table->>Table: invalidate positions query;\ngroup re-renders with new main
```

**Legend / key decisions:**
- **Every branch that mutates data does so in one `@Transactional` method** — the invariant check
  runs against the *in-memory* post-swap state before any `save`, so a failed check leaves the DB
  untouched (no partial swap ever persists). This mirrors `RebalancingTransferService.executeTransfer`'s
  existing "validate everything, then mutate" ordering.
- **`is_companion_row` and `companion_parent_ticker` must be set together per entity before
  flush**, not as two separate saves — `positions_companion_consistency` (V1) is a plain,
  non-deferred `CHECK`, evaluated at the end of each statement. Setting both fields on the same
  managed JPA entity before the transaction flushes satisfies this by construction; splitting
  them across two separate `save()` calls with an intervening flush would not. See ADR-088.
- **No interlock with `PurchaseSessionService`.** Deliberately absent from this diagram — ADR-088's
  Context section traces every consumer of `isCompanionRow`/`companionParentTicker` and confirms
  each one re-reads fresh from the DB per request, so nothing here needs to check for or block on
  an in-flight `PLANNED` session. A pending session's already-frozen `PurchaseSessionItem` rows
  settle by `(ticker, portfolio)` at completion time regardless of which row is currently "main."
- **Involution.** Running this same sequence again with `{id}` = the now-demoted old main's `id`
  returns the group to its exact prior state — the User's "undo" for a mis-click is simply
  clicking "Set as main" on the other row, not a separate feature.
- Applies identically in RETIREMENT — the sequence has no portfolio-specific branch (the MAIN-only
  risk-off half of the invariant check is the only portfolio-conditional step, and RETIREMENT
  simply skips it, per `05-rebalancing-algorithm.md` §7 — RETIREMENT never populates
  `target_allocation_pct_risk_off`).
