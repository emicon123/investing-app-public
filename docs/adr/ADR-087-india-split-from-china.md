# ADR-087 — Carve India out of China, top up NASDAQ (EQUITY bucket)

**Date:** 2026-09-09
**Status:** Accepted
**Related:** ADR-050 (introduced China's risk-off premium), ADR-067 (last EQUITY re-split), §7.5
of `05-rebalancing-algorithm.md` (manual trim-and-swap strategy this decision sits alongside)

## Context

The user decided to add a third EQUITY position — an India ETF — funded by halving China's
weight, with the remainder of China's cut and a further trim routed into NASDAQ. This is a
strategy decision (diversifying single-country China exposure into China+India, and leaning
slightly more into NASDAQ), not a rebalancing-algorithm change.

**Pre-existing ticker drift found while doing this:** `05-rebalancing-algorithm.md` and every
prior ADR (ADR-050/051/066/067) call the Nasdaq-100 EQUITY position `NQSE.DE`, and V51 is
written against that ticker. The live `positions` row is actually `ticker='SXRV.DE'`
(`name='NASDAQ'`) with exactly the values V51 left `NQSE.DE` at (20.0/27.5). The instrument was
swapped directly against the live DB at some point with no Flyway migration and no doc update
to record it. This ADR uses the ticker that is actually live (`SXRV.DE`) and flags the drift;
it does not attempt to reconstruct or fix the missing history.

## Decision

| | risk-on | risk-off |
|---|--:|--:|
| SXRV.DE (NASDAQ) | 20.0 → **22.5** | 27.5 *(unchanged)* |
| ICGA.DE (China) | 7.5 → **2.5** | 15.0 → **7.5** |
| QDV5.DE (India, new) | — → **2.5** | — → **7.5** |
| **Σ EQUITY** | **27.5** *(unchanged; corrected 2026-09-26 — originally misstated as 30.0)* | **42.5** *(unchanged)* |

`QDV5.DE` = iShares MSCI India UCITS ETF (USD Acc), XETRA, EUR-priced — same shape as the
existing `ICGA.DE`/`SXRV.DE` rows (exchange `XTB`, `asset_class` `ETF`, `exposure` `EQUITY`,
`company_backed=false`, `purchase_currency` `PLN`). It starts at 0 units in every account; the
user buys it manually going forward like any other position.

Risk-on: China's 5.0pp cut splits into NASDAQ +2.5 / India +2.5. Risk-off: NASDAQ is left
untouched, and China's existing 15.0% risk-off premium (from ADR-050, carrying it as a
gold-cut landing zone) is simply halved between China and India rather than reduced — the
user's explicit choice, confirmed over the numeric alternatives (flattening India/China to
2.5% flat, or scaling risk-off proportionally to the new risk-on ratio) precisely to keep this
a pure split with no other position needing to move.

## Consequences

Neither EQUITY bucket total moves: risk-on stays at 27.5 (−5.0 China + 2.5 NASDAQ + 2.5 India
= 0; 20.0 + 7.5 before, 22.5 + 2.5 + 2.5 after) and risk-off at 42.5 (−7.5 China + 7.5 India).
**Verified**: MAIN's `target_allocation_pct` and `target_allocation_pct_risk_off` sums are
still exactly 100.0000 apiece after this change, per the same guard V51 used. *(Correction
2026-09-26: an earlier version of this section claimed the risk-on EQUITY total rose from 27.5
to 30.0 — an arithmetic slip; the table and V66 never did that.)*

India inherits China's risk-off "gold-cut destination" role at half weight (§7.5), despite
ADR-050's own finding (study #22, `backtest/results/nqse_icga_defensiveness_check.md`) that
NQSE.DE/ICGA.DE don't actually hedge risk-off — that finding didn't reverse the swap-destination
mechanism itself in ADR-050, so this ADR doesn't reopen that question either; it only says China
and its risk-off role are now split two ways instead of one.

Data-only change to `positions`: 2 `UPDATE`s (`SXRV.DE`, `ICGA.DE`) + 1 `INSERT` (`QDV5.DE`),
via `V66__india_split_from_china.sql`. No schema change, no application code change. Applied
directly against the live DB by the assistant in-session, then formalized as V66 and verified
via `flyway_schema_history` and a successful backend restart (price cache picked up `QDV5.DE`
at €7.551 with no errors).
