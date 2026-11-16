# ADR-078: Shared Backend Abstract Core (portfolio-b ↔ portfolio-c, investing isolated)

**Status:** Accepted 2026-08-24
**Context:** Phase 1 pilot of `shared-backend-refactor` task (`shared-backend-refactor.task.md`)

## Context

- `investing-app` (main) → `portfolio-b` (extracted) → `portfolio-c` (forked from portfolio-b). `packages/shared/java` held 42 files (DTOs/enums/entities/repositories/mappers/clients/config) but every domain service remained duplicated (`pl.portfolio-b.*` vs `pl.portfolio-c.*` normalized diff 85-95% identical; diverging only on `Portfolio` scoping and units model).
- Portfolio B already carried full GoF pattern suite (`chain`, `composite`, `command`, `facade`, `iterator`, `memento`, `visitor`, `observer`, `mediator`, `prototype`, `state` in `pl.portfolio-b.*`) while portfolio-c had none — duplication without reuse.
- Main backend diverged intentionally (macro, Mexc, BinanceTransfers, purchase sessions) and must stay isolated.

## Decision

Promote reusable behavioral/structural patterns to `pl.shared.*` as generic abstract bases; make `portfolio-b` and `portfolio-c` thin concrete extensions. Keep Flyway per-app, no shared DDL.

### What moved to shared (2026-08-24, 106 files total)

**Pattern infrastructure (new `pl.shared.*`):**
- `chain/ValidationHandler<T>` (+ `TickerValidationHandler`, `TargetPctValidationHandler`) — Chain of Responsibility for `CreatePositionRequest`
- `composite/PositionComponent`, `PositionComposite`, `PositionLeaf` — Composite for companion aggregation
- `iterator/PagingIterator<T>` — generic paging iterator
- `visitor/PositionVisitor<T,R>`, `PositionValueVisitor<T>` — Visitor (portfolio total)
- `memento/Memento<T>`, `memento/Caretaker<T>` — Memento/Caretaker
- `command/Command<R>`, `CommandBus` — Command
- `observer/DomainEvent`, `EventListener`, `EventPublisher` — Observer
- `facade/MarketDataFacade` — Facade aggregating `FxRateCacheService` + price resolver
- `mediator/RebalancingMediator` — Mediator
- `prototype/Prototype`, `state/State`, `bridge/PriceSourceBridge`, `factory/PositionFactory`, `flyweight/FxRateFlyweight`, `proxy/PriceCacheProxy` (pre-existing) — Creational/Structural
- `history/HistoriaMemento<T>`, `HistoriaCaretaker<T>`, `HistoriaIterator<T>` — generic history behavioral quartet (promoted from `pl.investing.service.history`)

**Service bases (Template Method + Bridge + Strategy):**
- `service/base/AbstractHistoriaService<E,D>` / `AbstractPortfolioHistoriaService<E,D>` — Template Method for historia pagination (with `HistoriaCaretaker`/`Iterator` visitor). `portfolio-b` now `extends AbstractHistoriaService`; `portfolio-c` extends portfolio variant.
- `service/base/AbstractDepositService<E,D>` / `AbstractPortfolioDepositService<E,D>` — Template Method + Specification composition. `portfolio-b`/`portfolio-c` extend respective variants.
- `service/base/UnitsExtractor<T>` (Bridge) + `PositionMetricsCalculator<T>` — Bridge for units extraction (`computeUnitsTotal` vs `getUnits`) + Strategy for price resolution. Both `PositionService`s now delegate `computeFields` to shared calculator.
- `service/base/AbstractXirrService` — Template Method + Strategy (`findBracket` expanding bracket vs fixed) — available for Xirr consolidation.
- `service/rebalancing/AbstractProportionalStrategy<T>` — Template Method + Strategy for proportional DCA (`buildRows → computeTarget → allocate → computeUnits → makeItem → allocateToAccounts`). Portfolio C's `RebalancingStrategy` now extends shared generic; portfolio-b's existing abstract remains reference.

**Behavioral suite already in portfolio-b stays lesson:** `pl.portfolio-b.{chain,composite,facade,iterator,memento,visitor,observer,mediator,command,prototype,state}` now have shared generic counterparts; portfolio-b concrete reuses shared visitor/calculator, portfolio-c gains pattern richness without copy-paste.

### What stayed per-app

- Entities/DTOs/Repositories for `Position`/`Deposit`/`Historia`/`PurchaseLot`/`AccountCashSnapshot`/`PositionTransfer` — schema differs (5-column units in portfolio-b vs single `units` in portfolio-c vs 5 different columns in main); no `@MappedSuperclass` sharing, only interface-level Bridge.
- `ValueComputationService` — per-app companion aggregation (portfolio-b `computeUnitsTotal`, portfolio-c `getUnits`) still per-app, now bridged via `UnitsExtractor`.
- `RebalancingService` orchestration — per-app mediator (portfolio-b caps via `AccountCashBalances`, portfolio-c flat, main macro gate).
- Main-only: `client/macro`, `service/macro`, `service/mexc`, `CryptoTrade`, `BinanceTransfer`, `PurchaseSession` (729 LOC), `Inflation`, `News`, `TradeChart`, `LinearRegression` — never shareable.

## Consequences

- `packages/shared/java` grows 42 → 106 files; `portfolio-b`/`portfolio-c` backends shrink ~15% LOC via delegation (full 45% reduction requires entity/mapper base migration, deferred).
- Build: `mvn -pl packages/shared/java install` then `mvn -pl apps/.../backend test` green (verified `PositionTransferIntegrationTest`, `PurchaseHistoryIntegrationTest`, `RebalancingStrategyTest` for both).
- Controllers stay thin per-app Facade; Flyway stays per-db.
- Next phases: `AbstractAccountCashService`, `AbstractPurchaseHistoryService`, `AbstractPositionTransferService`, `@MappedSuperclass BasePosition` (if schema converges), full `ValueComputation` bridge, `State` for `PurchaseSession` lifecycle.

## Alternatives Considered

- Full `@MappedSuperclass` inheritance for entities — rejected: DB schemas diverge too much (units columns, `group_name`, `sort_order`, `portfolio` dimension, `company_backed`, `targetAllocationPctRiskOff`). Interface + Bridge achieves reuse without DDL coupling.
- Shared `SecurityConfig`/`GlobalExceptionHandler` — rejected per ADR-076 (hardcoded `iss` string, per-app HTTP status 400 vs 500, investing-app extra filter chain).

