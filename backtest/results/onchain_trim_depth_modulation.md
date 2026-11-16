# 12 — On-chain-modulated trim depth for the shipped +100% rule

Script: `onchain_trim_depth_modulation.py`. Follow-on to study #11 / file 10. Does NOT use on-chain data as a standalone sell trigger (already tested, lost to macro-only in file 10). Tests instead whether on-chain euphoria (derived NUPL percentile, no-lookahead) should modulate how DEEP the existing purely price-based +100% trim goes, only for BTC_spot/ETH (the only sleeve members with a free on-chain read). Macro dynamic-target cut is DISABLED (cut=1.0) throughout to isolate the trim-depth question from the already-tested macro overlay.

## 1. Inventory — every historical +100% trigger-fire event, BTC_spot & ETH (2010-07 -> present, one continuous run, baseline trim_to=1.5x throughout so the event list itself isn't shaped by the modulation being tested)

Total +100% trigger-fire events, all assets, 2010-07..2026-06: 18
Of which BTC_spot/ETH (on-chain-conditionable): 18

**By asset:**
| asset | fire events |
|---|--:|
| BTC_spot | 15 |
| ETH | 3 |

**BTC_spot/ETH fire events in detail** (month, weight/target ratio at fire, euphoric = derived-NUPL percentile >= 0.75 at that month, `n/a` = pre-burn-in, no on-chain reading yet):

| month | asset | ratio (actual/target) | NUPL percentile | euphoric? |
|---|---|--:|--:|---|
| 2010-11 | BTC_spot | 2.04x | n/a | n/a |
| 2011-02 | BTC_spot | 2.60x | n/a | n/a |
| 2011-05 | BTC_spot | 3.70x | n/a | n/a |
| 2011-06 | BTC_spot | 2.68x | n/a | n/a |
| 2011-07 | BTC_spot | 2.29x | 0.08 | no |
| 2013-03 | BTC_spot | 2.68x | 0.85 | YES |
| 2013-04 | BTC_spot | 2.85x | 0.68 | no |
| 2013-05 | BTC_spot | 2.06x | 0.63 | no |
| 2013-11 | BTC_spot | 2.06x | 0.90 | YES |
| 2013-12 | BTC_spot | 3.84x | 0.64 | no |
| 2017-08 | BTC_spot | 2.32x | 0.86 | YES |
| 2017-09 | ETH | 2.00x | 0.42 | no |
| 2017-11 | BTC_spot | 2.34x | 0.88 | YES |
| 2018-01 | BTC_spot | 2.12x | 0.56 | no |
| 2018-02 | ETH | 2.49x | 0.42 | no |
| 2021-02 | BTC_spot | 2.01x | 0.87 | YES |
| 2021-04 | BTC_spot | 2.16x | 0.85 | YES |
| 2021-09 | ETH | 2.06x | 0.61 | no |

**Split**: 6 euphoric-state fires, 8 normal-state fires, 4 pre-burn-in/n-a fires (out of 18 total BTC/ETH fires).

## 2. Secondary/corroborating check: native NUPL (bitcoin-data.com, 2022-07+) vs the derived-percentile euphoria flag, in the overlap window

Overlap 2022-07..2026-07 (49 months). Native NUPL>=0.5 vs derived-percentile>=0.75 agree in 78% of months (both are reading the same underlying quantity, different absolute-vs-relative cutoffs — rough agreement expected, not identity).

BTC_spot/ETH +100% fires inside the native-SOPR/NUPL-covered window (2022-07+): 0 — none. This is the corroborating check the user asked for; the derived series (§1) is the primary test given it has multi-cycle coverage.

## 3. Portfolio-level test — does modulating trim depth change XIRR/drawdown?

Same real-algo mechanic as files 07/09/11's portfolio tests (buy-only + +100%-rule, macro cut DISABLED here to isolate this question), 1/3/5/8/13y starts. Each cell = XIRR%/yr (maxDD%).

| mode | 13y | 8y | 5y | 3y | 1y |
|---|--:|--:|--:|--:|--:|
| baseline (fixed 1.5x, shipped) | 39 (-35) | 40 (-43) | 24 (-48) | 25 (-23) | -5 (-17) |
| deeper on euphoric fires only (euphoric->1.0x, normal->1.5x) | 38 (-33) | 41 (-39) | 24 (-48) | 25 (-23) | -5 (-17) |
| deeper still on euphoric (euphoric->0.75x, i.e. BELOW target, normal->1.5x) | 36 (-34) | 41 (-38) | 24 (-48) | 25 (-23) | -5 (-17) |
| shallower on normal fires only (euphoric->1.5x, normal->1.75x) | 40 (-33) | 39 (-45) | 24 (-48) | 25 (-23) | -5 (-17) |
| skip normal fires entirely (euphoric->1.5x, normal->no trim) | 41 (-62) | 37 (-52) | 24 (-48) | 25 (-23) | -5 (-17) |
| both (euphoric->1.0x, normal->1.75x) | 39 (-32) | 40 (-42) | 24 (-48) | 25 (-23) | -5 (-17) |

Start months: 13y=2013-06, 8y=2018-06, 5y=2021-06, 3y=2023-06, 1y=2025-06.

**Count of BTC_spot/ETH fires actually affected by the modulation, per start-date window** (euphoric fires / normal fires, out of on-chain-eligible fires in that window — this is the real n behind the table above):

| window | euphoric fires | normal fires | n/a (pre burn-in) |
|---|--:|--:|--:|
| 13y | 5 | 5 | 0 |
| 8y | 2 | 1 | 0 |
| 5y | 0 | 1 | 0 |
| 3y | 0 | 0 | 0 |
| 1y | 0 | 0 | 0 |

## 4. Proxy check — forward BTC drawdown conditional on NUPL state while "in 2x-trigger territory" generally (larger sample than discrete fire events)

Broader, less strict version of the same question: instead of only the exact months the rule fires, take every month where BTC's *simulated portfolio weight* is already at/above the 2x-target threshold (i.e. sustained overshoot, not just the instant it first crosses) and compare subsequent BTC drawdown over the following 3/6/12 months, split by NUPL-percentile state.

Months with BTC portfolio-weight-ratio >= 1.9x target: 23 (2010-11..2021-04).

| horizon | euphoric (pctl>=0.75) n | mean fwd maxDD | normal n | mean fwd maxDD |
|---|--:|--:|--:|--:|
| 3m | 11 | -28.0% | 6 | -37.6% |
| 6m | 11 | -38.6% | 6 | -43.6% |
| 12m | 11 | -54.3% | 6 | -62.6% |

Note: consecutive in-territory months are highly overlapping/autocorrelated (the same overshoot episode spans several months in a row) — this inflates the apparent n, it is NOT n independent observations. Read directionally only.

## Verdict

See discussion in the write-up (`results/study_summary/12_onchain_trim_depth_modulation.md`) for the honest read on sample size and whether any mode actually helps.
