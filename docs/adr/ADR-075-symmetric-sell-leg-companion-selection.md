# ADR-075: Symmetric Companion-Row Selection in the Sell Leg

**Date:** 2026-08-20
**Status:** Accepted

## Context

ADR-063 (2026-07-30) fixed the *buy* side of the "Przenieś wartość" (trim/rebuy) dialog,
`RebalancingTransferDialog.tsx`: `buyableTickers` is built by flattening every position's
`subPositions` (main instrument first, then companions), so a companion row — e.g.
`PL.ETNVIRETH`, the XTB-listed ETN alternative to real spot `ETH-USD`, added by the V27
migration — can be selected as a buy target. That fix was never applied to the *sold* side of
the same dialog: the sold leg is still hard-coded to whatever `position` prop was clicked in
`PositionsTable`, rendered as a fixed `<Badge>{position.ticker}</Badge>` with no `<Select>` at
all.

Companion rows only ever render inside the expanded `SubPositionRows` sub-table, which has no
sell trigger of its own — so a companion (e.g. the ETH ETF) can never be chosen as the sold
leg, only the parent ("real ETH") can, even though selling the companion instead of the parent
is a completely legitimate, symmetric operation: the user may hold both `ETH-USD` and
`PL.ETNVIRETH` and want to trim whichever one is actually overweight, or whichever tax
wrapper/lot makes more sense to reduce.

**This is a frontend-only gap — the backend contract never restricted the sold leg this way.**
`RebalancingTransferService.executeTransfer` resolves the sold leg exactly the same way it
resolves every buy leg: via the `(ticker, portfolio)` composite key introduced by ADR-063,
followed by a units-sufficiency check. Neither the resolution logic nor the validation contains
any `is_companion_row`-based restriction. The OpenAPI schema
(`docs/openapi/components/schemas/rebalancing-transfer.yaml`) defines a single
`RebalancingTransferLeg` shape reused identically for `sold` and every entry in `buys` — its
`ticker` field's description ("Must match an existing position's ticker within the request's
top-level `portfolio` — resolved as `(ticker, portfolio)`, not by ticker alone") never singles
out the sold leg as special or companion-restricted, and neither does
`docs/architecture/03-api-contract.md`'s `POST /rebalancing-transfers` entry (checked directly
during this ADR — no wording implying that restriction exists there; no change needed to either
file). The asymmetry existed purely in the frontend component's hardcoded `<Badge>` for the
sold leg versus its `<Select>` for buy legs.

Confirmed by the user: both the parent ticker and its companion(s) should be fully,
symmetrically sellable from the trim dialog.

## Decision

Extend `RebalancingTransferDialog.tsx`'s companion-flattening pattern (ADR-063) symmetrically
to the sold leg:

1. Add a sold-ticker `<Select>` mirroring the existing buy-ticker picker, populated from the
   same kind of flattened list used for `buyableTickers` — `position.ticker` plus every
   `subPositions` entry tied back to this position via `is_companion_row` /
   `companion_parent_ticker` (main instrument first, then companions) — scoped to the one
   position the dialog was opened for, not the whole portfolio.
2. Default the selection to `position.ticker` — the row that was actually clicked. This is a
   pure UI unlock, not a change to default behavior: a user who never opens the new `<Select>`
   sees no difference from today.
3. `sellSubAccounts`, `availableUnitsForSubAccount`, and `sellMarketPricePln` — all currently
   derived from the fixed `position` prop — are re-derived from whichever ticker is currently
   selected in the new sold-ticker `<Select>`.

No backend or database change of any kind. `RebalancingTransferService.executeTransfer`,
`positions`, and the OpenAPI contract already support selling any ticker resolvable via
`(ticker, portfolio)` — companion or not — for either leg.

## Consequences

- The dialog's sold-side and buy-side ticker pickers become fully symmetric: both flatten
  `subPositions`, both apply to any leg, both default sensibly (buy legs default to no
  selection / first tradeable ticker as today; the sold leg defaults to the clicked position).
- No schema, Flyway, or endpoint change. Same category as ADR-063's buy-side fix: existing
  contract, frontend-only consumption change.
- `PositionsTable.tsx`'s SELL badge trigger and the deviation-threshold gating that decides
  *which positions* show a "Sprzedaj" button at all (§7.5/§7.6, ADR-048/053) are unaffected —
  this ADR only changes which ticker(s) are selectable **once the dialog is already open**, not
  which rows surface a sell affordance in the first place.
- **Status: spec decided here (architect scope only) — not yet implemented.** Frontend-web
  agent implements against this contract next, per
  `.claude/tasks/54-companion-row-sell-ticker-picker.task.md`. See
  `docs/architecture/05-rebalancing-algorithm.md` §7.8 (point 3) for where this is folded into
  the living architecture doc.
