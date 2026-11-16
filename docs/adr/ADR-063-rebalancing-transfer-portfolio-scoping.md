# ADR-063: Rebalancing-Transfer Portfolio Scoping + Companion Rows in the Buy List

**Date:** 2026-07-30
**Status:** Accepted

## Context

Two independent bugs in the "Przenieś wartość" (rebalancing transfer / trim-and-reinvest) flow
were found together while investigating a live data-integrity incident.

### Bug 1 — companion tickers invisible in the buy-ticker dropdown

`GET /positions` filters `is_companion_row = false` (`PositionRepository.findAllFiltered`), so
companion rows — e.g. `PL.ETNVIRSOL`, `PL.ETNVIRETH`, the XTB-listed ETN alternatives to raw
crypto added by the V27 migration — never appear in the top-level `positions` list.
`RebalancingTransferDialog.tsx` builds its buy-ticker dropdown (`buyableTickers`) only from that
top-level list, so a user trimming a crypto position held on IKE/IKZE can never select the XTB
companion as the rebuy target — defeating the entire reason the companion row exists (IKE/IKZE
can't hold Binance-listed crypto directly; the companion ETF is the IKE/IKZE-eligible substitute).

Each `PositionDto` already carries its companions nested in `subPositions` (main instrument
first, then companions) — no backend change is needed for this half of the fix, only flattening
the frontend's ticker list and fixing the `marketPricePln` lookup used to prefill a buy leg's
amount, which today only searches the top-level `allPositions` array and so silently fails to
prefill for any companion ticker.

### Bug 2 — cross-portfolio ticker collision (the more serious one; already caused real drift)

`RebalancingTransferService.executeTransfer` resolves both the sold leg and every buy leg via:

```java
Map<String, Position> positionByTicker = positionRepository.findAll().stream()
    .collect(Collectors.toMap(Position::getTicker, Function.identity(), (a, b) -> a));
```

keyed by **ticker alone**. But `positions` has a `(ticker, exchange, portfolio)` unique
constraint, not a ticker-alone one — many tickers (`HUT`, `SOL-USD`, `ETH-USD`,
`PL.ETNVIRSOL`, `PL.ETNVIRETH`, and others) exist as separate rows in both `MAIN` and
`RETIREMENT`. `RebalancingTransferRequest` had no `portfolio` field at all, and the OpenAPI spec
explicitly (and incorrectly) documented "portfolio/exchange are not part of the request because
they are already fixed properties of the resolved position" — the resolved position is exactly
what's ambiguous when a ticker exists twice.

**`PurchaseSessionService.completeSession` already hit and fixed this identical problem** with a
`record PositionKey(String ticker, Portfolio portfolio) {}` composite key
(`backend/src/main/java/pl/investing/service/PurchaseSessionService.java:302–305`), with an
explicit comment explaining why. The fix was never carried over to
`RebalancingTransferService`, which still resolves the old, unsafe way.

**Confirmed live impact (2026-07-30 14:14):** a HUT→XAD6 transfer, executed on the RETIREMENT
portfolio, recorded the HUT sell leg tagged `portfolio=MAIN` and the paired XAD6 buy leg tagged
`portfolio=RETIREMENT` in `asset_purchases` — one transfer split across two portfolio "books",
because the ticker-alone map's "first match wins" tiebreak landed on different portfolio rows
for the two legs. Manually corrected in the live DB (moved the affected units + proportional
`total_spent_pln` from MAIN back to RETIREMENT, retagged the audit row) — this ADR is the
code-level fix so it cannot recur, not a record of the data correction itself.

## Decision

### 1. `RebalancingTransferRequest` gains a required, top-level `portfolio` field

Not per-leg. A single transfer is always within one portfolio's book — there is no legitimate
"sell on MAIN, buy on RETIREMENT" operation anywhere in this app — so putting `portfolio` once,
at the request root, makes that invariant structurally impossible to violate from the request
shape itself, rather than relying on a service-layer check that every leg agrees.
`docs/openapi/components/schemas/rebalancing-transfer.yaml` is updated accordingly (required
field, updated schema descriptions removing the old incorrect "not part of the request" claim);
`docs/openapi.yaml` regenerated via `npm run openapi:bundle` (this repo bundles that file from
`docs/openapi/**` — it is never hand-edited).

### 2. `RebalancingTransferService.executeTransfer` resolves via `(ticker, portfolio)`, scoped by the request

Mirrors `PurchaseSessionService.completeSession`'s existing pattern exactly:

```java
record PositionKey(String ticker, Portfolio portfolio) {}
Map<PositionKey, Position> positionByKey = positionRepository.findAll().stream()
    .collect(Collectors.toMap(
        p -> new PositionKey(p.getTicker(), p.getPortfolio()), Function.identity(), (a, b) -> a));
```

Every lookup becomes `positionByKey.get(new PositionKey(leg.ticker(), req.portfolio()))` — for
the sold leg and every buy leg alike. A ticker that doesn't exist under the request's
`portfolio` is a 404 (`NoSuchElementException`, same convention as today), **even if it exists
under the other portfolio** — silently falling through to the wrong portfolio's row is exactly
the bug being closed, so there is no fallback search across portfolios.

`exchange` deliberately stays out of the key, same as `PurchaseSessionService`: a ticker never
collides across exchanges within one portfolio (the `(ticker, exchange, portfolio)` unique
constraint's third component is portfolio, and no ticker is ever listed on two exchanges within
the same portfolio in this dataset), so `(ticker, portfolio)` is the minimal safe key — adding
`exchange` would be over-specifying the request for no real ambiguity it resolves.

### 3. Companion rows included in the buy-ticker list via `subPositions` flattening

`RebalancingTransferDialog.tsx`'s `buyableTickers` is built by flattening each position's
`subPositions` (main ticker + its companions) instead of reading only the top-level positions
array. `BuyLegRow`'s `marketPricePln` lookup is fixed to resolve against the same flattened
list, so prefill works for companion tickers too. No new backend endpoint — `subPositions` was
already returned by `GET /positions`, just not consumed for this purpose.

## Consequences

- **Breaking change to `RebalancingTransferRequest`** — `portfolio` is required, so any existing
  caller (only `frontend-web` today, no external consumers) must send it. Acceptable: this is a
  single-user, no-external-API app (ADR-004), and the frontend is updated in the same rollout.
- **A ticker that exists in only one portfolio still works unchanged** — the composite key
  degenerates to the same lookup as before for the (overwhelmingly common) non-duplicated case;
  only the duplicated-ticker case changes behavior, from "silently pick one" to "correctly pick
  the request's own portfolio."
- **The companion-row fix and the portfolio-scoping fix are independent** and could in principle
  ship separately, but are bundled here since both were found investigating the same dialog and
  both are needed for the retirement trim-and-rebuy flow to work correctly end-to-end (sell HUT
  on RETIREMENT, see `PL.ETNVIRSOL` as a selectable, correctly-priced buy target, and have the
  transfer resolve against RETIREMENT's own HUT row, not MAIN's).
- **No schema/Flyway change.** Pure API-contract + application-logic fix, same category as
  ADR-060/061/062 — the existing `(ticker, exchange, portfolio)` unique constraint already
  supports the composite lookup; nothing new needs to be stored.
- **Status: spec decided here (architect scope only) — not yet implemented.** Backend and
  Frontend-web agents implement against this contract next, per
  `.claude/tasks/rebalancing-transfer-portfolio-scoping.task.md`. See
  `docs/architecture/05-rebalancing-algorithm.md` §7.8 for where this is folded into the living
  architecture doc, and `docs/architecture/03-api-contract.md`'s
  `POST /rebalancing-transfers` entry for the corrected endpoint description.
