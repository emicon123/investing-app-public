# 10 — On-chain signal study #2: SOPR family, MVRV-Z, NUPL, LTH/STH supply

Script: `onchain_signals_study.py` → `results/onchain_signals_study.md`. Data:
`bitcoin-data.com` (free, unauthenticated REST API — a much richer catalog than
Coinmetrics community, ~600 endpoints incl. SOPR/LTH-STH cohorts/HODL waves), daily
since ~2022-07 → `data/onchain/BTC_{SOPR,LTH_SOPR,STH_SOPR,MVRV_ZSCORE,NUPL,
LTH_SUPPLY_BTC,STH_SUPPLY_BTC}.csv`. Cross-checked against our own longer-history
(2010+) derived NUPL/MVRV-Z from free Coinmetrics (file 08's pipeline). Same
no-lookahead Spearman-IC methodology as files 03/08. Follow-on to file 08 (which
rejected raw MVRV percentile-rank as a standalone trigger over the full 2011-2026
history).

## Headline result: several signals show STRONG IC — but only in this recent window
| signal (fwd 6m / fwd 12m) | IC | significant? |
|---|--:|---|
| MVRV Z-Score (native, 2022+) | −0.49 / −0.62 | **yes**, t=−3.6 / −4.6 |
| NUPL (native, 2022+) | −0.45 / −0.59 | **yes**, t=−3.1 / −4.3 |
| LTH-SOPR | −0.37 / −0.57 | **yes**, t=−2.5 / −4.0 |
| LTH/STH supply ratio | +0.54 / +0.58 | **yes**, t=4.0 / 4.1 (all horizons 1-12m) |
| macro risk-off (same window, for comparison) | −0.10 / −0.08 | no |

Over 2022-07→2026-06, these on-chain reversal signals **out-predict the shipped macro
gate** on its own recent turf. That's a genuinely different picture than file 08.

## But: doesn't replicate on the full 2010+ history — small-sample caveat is real
The derivation is validated (native vs derived MVRV-Z/NUPL correlate at **0.999** in
the overlap window — same underlying data, sound methodology). Yet the **same derived
series, extended back to 2010**, shows a much weaker relationship:
| signal (fwd 6m / 12m) | 2022+ (native) | 2010+ (derived) |
|---|--:|--:|
| MVRV Z-Score | −0.49 / −0.62 ✓ | −0.04 / **−0.20** (12m only, weak) |
| NUPL | −0.45 / −0.59 ✓ | +0.08 / −0.07 (both n.s., **wrong sign** at 6m) |

This is the same pattern file 08 already found (raw MVRV percentile: weak/insignificant
over the full history). The strong 2022+ reading likely reflects that this window
happens to be one clean cycle (2022 bear bottom → 2024-25 rally → 2026 cooldown) where
level-reversion worked well — not evidence the relationship is structural. **Treat the
native-2022+ numbers as suggestive, not confirmed** — n=36-45 months, and it's one
cycle, not several.

## LTH/STH supply ratio — the most robust finding, survives extra scrutiny
Unlike the reversal metrics above, this one is a **trend-continuation** signal (positive
IC: high LTH/STH ratio → *higher* forward returns, not lower) and passed three
robustness checks that argue against it being a spurious trend artifact:
1. **Not monotonic** — ratio oscillates 2.4-5.0 over the sample (2022-07: 3.27 →
   2023-11: 4.95 → 2025-11: 2.39 → 2026-03: 2.86), not a smooth secular climb.
2. **Detrended version still works** — the 3-month *change* in the ratio (not the
   level) has IC +0.50 (t=3.6) at 3m, +0.37 (t=2.5) at 6m — the signal isn't just
   riding a shared trend with price.
3. **Split-sample stable** — first half (2022-07→2024-06) IC=+0.55 (t=2.9), second
   half (2024-07→2026-06) IC=+0.40 (t=1.9). Holds in both halves, not concentrated in
   one sub-period.

Reads as: when long-term holders are accumulating faster than short-term supply grows
(illiquid-supply thesis), forward returns tend to be higher — a **conviction-to-stay-
allocated** signal, structurally different from a de-risk/sell trigger.

## SOPR itself (the user's flagged "important one") — weak in isolation
Raw SOPR level: only marginally significant at 12m (−0.32, t=−2.0), nothing shorter.
Threshold bucketing (SOPR<1 vs ≥1, <0.98, etc.) shows no clean actionable split — even
SOPR<1 months (traditionally read as "capitulation") still average +14.3% forward 3m,
not negative. **SOPR alone doesn't clear the bar** as a standalone trigger — consistent
with how it's normally used in practice (a coincident confirming indicator, like RSI,
not a lead signal). LTH-SOPR (the long-term-holder-only cut) is the more useful half of
the SOPR family here, not the blended headline number.

## Verdict
- **Don't wire the native-2022+ reversal signals (MVRV-Z, NUPL, LTH-SOPR) into the
  live gate as-is** — real risk this is one-cycle overfitting; the same series over the
  full 2010+ history is weak-to-wrong-signed, matching file 08's prior rejection.
- **LTH/STH supply ratio is the one worth carrying forward** — passed genuine
  robustness checks, but it's a buy-conviction signal, not a sell trigger, so it doesn't
  slot into the existing de-risk mechanism the same way; would need its own design.
- **Options OI by strike and URPD**: out of scope per user direction (OI has no free
  historical series; URPD explicitly deprioritized).
- **Addresses >100/>1000 BTC**: `bitcoin-data.com` has the banded cohort data needed
  (`balance-addr-{1K-100,10K-1K,10K}-BTC`) but fetches are still rate-limited as of this
  write-up; pending.

## ETH extension: same derived NUPL/MVRV-Z, no new data needed
`bitcoin-data.com` is BTC-only (SOPR/LTH-STH-SOPR/native-MVRV-Z/native-NUPL/LTH-STH
supply have no ETH equivalent there), but the Coinmetrics-derived NUPL/MVRV-Z pipeline
(file 08) already covers ETH (2015+) for free — tested it against ETH forward returns
using data already on disk, no new fetching.

| signal (fwd 6m / 12m) | ETH full history (2015+) | ETH recent-only (2022+) |
|---|--:|--:|
| MVRV Z-Score (derived) | −0.05 / **−0.26** (t=−2.8) | −0.58 / −0.41 (t=−4.5 / −2.6) |
| NUPL (derived) | +0.08 / −0.02 (n.s.) | −0.53 / −0.39 (t=−3.9 / −2.5) |
| **macro risk-off (shipped)** | **−0.28 / −0.36** (t=−3.2 / −4.2, and every horizon 1-12m significant) | — |

**Same pattern as BTC**: on-chain reversal signals look strong in the narrow 2022+
window but weak-to-insignificant over the full history — same one-cycle-overfitting
concern, same "don't wire it in" conclusion. The genuinely useful finding here is that
**the shipped macro risk-off signal is strong and significant for ETH across the
*entire* 2015-2026 history**, not just recently — a stronger, more robust result than
what file 08 found for BTC's macro IC alone. This reinforces (doesn't change) the
existing design: the macro gate is doing real work for ETH specifically, consistent
with file 03's original finding that ETH is more macro-sensitive than BTC.

## Portfolio-level test: does LTH-SOPR improve on the shipped macro trigger?
Script: `onchain_portfolio_backtest.py` → `results/onchain_portfolio_backtest.md`. Same
real algo as files 07/09 (buy-only + 2x-trim-to-1.5x-target, cut=0.33), same target
roster — only the risk-off trigger varies. On-chain data only exists from 2022-07, so
**only 3y/1y start dates are testable** (not 5/8/13y). LTH-SOPR trigger = ≥75th
percentile of the full sample (1.86), which is itself calibrated in-sample given how
little history exists — flagged, not hidden.

| config | 3y | 1y |
|---|--:|--:|
| NO de-risk (baseline) | 25 (−23) | −5 (−17) |
| **Macro only (shipped, ADR-042)** | **31 (−11)** | **3 (−8)** |
| On-chain only (LTH-SOPR≥1.86) | 28 (−9) | −2 (−9) |
| Composite OR | 27 (−11) | 2 (−9) |
| Composite AND | 30 (−14) | −5 (−17) |

**The shipped macro trigger wins outright on both testable windows** — better return
AND (tied-or-better) drawdown than on-chain-only or either composite. Composite AND
barely fires (2/48 months overlap between the two triggers) so it collapses toward the
no-de-risk baseline. This confirms, at the portfolio level, what the IC caveat already
suggested: LTH-SOPR's promising correlation numbers don't translate into a better
sell/de-risk trigger than what's already live. Same standard applied to ISM, the
miners-cap schedule, and raw-MVRV-percentile (file 08) — reject as a trigger.

## Caveats
Small sample throughout (n=36-47 months for native series — one cycle, not several);
overlapping windows inflate apparent significance somewhat; `bitcoin-data.com` is a
third-party free aggregator, not the primary source — worth a spot-check against
Coinmetrics-derived numbers periodically (already done here for MVRV-Z/NUPL, 0.999
correlated, so trustworthy); LTH/STH supply ratio's mechanism (155-day age threshold)
means it mechanically lags regime shifts by design; portfolio backtest can only use 2
start dates (2022-07+ data) vs files 07/09's 5, a materially weaker robustness check.
