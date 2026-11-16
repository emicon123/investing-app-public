# Study 23 -- tax/cost-drag audit on study 15's trim-rule fire log

Single continuous pass 2007-02..2026-09 (19.6y, no overlapping windows) for fire-count/tax-total analysis; the standard 5 start-date windows (1/3/5/8/13y, matching study 15) for XIRR/maxDD comparability. Buy-only DCA 1000+1000/mo PLN, 15bps trading cost + 19% Belka tax (average-cost basis approximation, REGULAR-account assumption -- see methodology section of the numbered writeup).

## 0. Data-drift check -- tax_on=False fire counts vs. study 15's published table

This section runs the tax-free (`tax_on=False`) variant of the sim, which should be arithmetically IDENTICAL to study 15's own unmodified `trim_frequency_backtest.py` (same formulas, same tables, same data loaders -- verified line-by-line). It is NOT.

- CURRENT (1.5x trigger / 1.0x trim-to -- ADR-048 live rule) / BASELINE (fixed target, no macro): 41 fires (tax_on=False, today's data)
- CURRENT (1.5x trigger / 1.0x trim-to -- ADR-048 live rule) / CURRENT (ADR-044 blend, live mechanism): 48 fires (tax_on=False, today's data)
- 1.75 / 1.25 (sensitivity midpoint, between CURRENT and PRIOR) / BASELINE (fixed target, no macro): 39 fires (tax_on=False, today's data)
- 1.75 / 1.25 (sensitivity midpoint, between CURRENT and PRIOR) / CURRENT (ADR-044 blend, live mechanism): 45 fires (tax_on=False, today's data)
- PRIOR (2.0x / 1.5x -- pre-ADR-048, reference only, NOT current) / BASELINE (fixed target, no macro): 37 fires (tax_on=False, today's data)
- PRIOR (2.0x / 1.5x -- pre-ADR-048, reference only, NOT current) / CURRENT (ADR-044 blend, live mechanism): 41 fires (tax_on=False, today's data)

**Study 15's published table (2026-07-09): 2.0/1.5 BASELINE=22/CURRENT=25; 1.75/1.25 BASELINE=32/CURRENT=35; 1.5/1.0 BASELINE=79/CURRENT=83.** Today's numbers above do not match -- confirmed NOT a bug in this script (re-running study 15's own, byte-for-byte unmodified `trim_frequency_backtest.py` today reproduces these same higher counts, e.g. 54/57 at 2.0/1.5). **Root cause, traced**: `data/prices/QQQ_full.csv` and `FXI_full.csv` were created/refreshed 2026-07-10 21:35 (`git log`), i.e. AFTER study 15's 2026-07-09 publication date and after its committed `trim_events.csv` snapshot -- the raw fallback `QQQ.csv`/`FXI.csv` files study 15 originally read no longer even exist on disk (removed by the same refresh, commit `2b03ed0`). The loader prefers `_full.csv` when present, so today's re-run uses a materially longer/different QQQ and FXI history (`QQQ_full.csv` now runs back to 1999-03) than whatever shorter/differently-adjusted series study 15's original run saw. This is a genuine data-drift discovery, out of scope to fix here (study 15's own artifacts are not touched by this script), but material enough to flag prominently: **QQQ and FXI now fire at the LIVE 2.0/1.5 rule too** (13 and 11 times respectively over the 19.3y window), not just at the tighter 1.5/1.0 rule as study 15 reported. Every number in this study is computed against TODAY's on-disk data for both the tax and no-tax variants, so the internal WITH-tax-vs-NO-tax comparison below remains apples-to-apples valid even though the absolute fire counts and XIRR/maxDD figures no longer match study 15's committed table. See the numbered writeup for the recommendation to refresh study 15 itself as a separate follow-up.

**2026-08-06 addendum (task 28)**: study 15 WAS refreshed as recommended above, on the same date as this script -- both now import the same LIVE target table/blend formula from `portfolio_spec.py`, so their fire counts should (and, checked, do) match each other again at equivalent configs; they will NOT match either of the two prior tables cited above (2026-07-09's or the 2026-07-13 price-drift figures) for a third, unrelated reason: the target-allocation table itself changed (pre-ADR-047 demo proxy -> live ADR-067 weights, no more CASH bucket) and the blend divisor/orientation were corrected (2.0 -> 0.5, ADR-049; orientation flipped, ADR-071). This is expected, not a new bug -- see this file's top-of-module 2026-08-06 note.

**2026-09-25 addendum (task 28, second pass)**: the shared tables moved again (live ADR-074/087 weights: QQQ 22.5/27.5, FXI 2.5/7.5, ETH 10.0/1.0, LINK-USD 1.5/0.15 added; Experiments now 17 positions) -- both scripts import them from `portfolio_spec.py`, so both pick the move up with no further code change; fire counts at equivalent configs should still match each other, and will again NOT match the 2026-08-06 tables. Expected, not a new bug.

## 1. Continuous pass, WITH tax -- fire counts, total tax paid, per-asset breakdown

| config | mode | fires | total tax paid (PLN, nominal, over full pass) | avg tax/fire (PLN) | median gain-frac of trimmed $ |
|---|---|--:|--:|--:|--:|
| CURRENT (1.5x trigger / 1.0x trim-to -- ADR-048 live rule) | BASELINE | 44 | 960,210 | 21,823 | 13% |
| CURRENT (1.5x trigger / 1.0x trim-to -- ADR-048 live rule) | CURRENT | 51 | 902,503 | 17,696 | 22% |
| 1.75 / 1.25 (sensitivity midpoint, between CURRENT and PRIOR) | BASELINE | 40 | 1,428,793 | 35,720 | 11% |
| 1.75 / 1.25 (sensitivity midpoint, between CURRENT and PRIOR) | CURRENT | 46 | 1,513,742 | 32,907 | 18% |
| PRIOR (2.0x / 1.5x -- pre-ADR-048, reference only, NOT current) | BASELINE | 39 | 2,007,683 | 51,479 | 11% |
| PRIOR (2.0x / 1.5x -- pre-ADR-048, reference only, NOT current) | CURRENT | 45 | 2,221,578 | 49,368 | 20% |

### Per-asset tax breakdown, CURRENT mode, 2.0/1.5 (PRIOR, pre-ADR-048 -- NOT current)

| asset | fires | total tax (PLN) | gain-frac of trimmed $ (min / median / max) |
|---|--:|--:|--:|
| BTC_spot | 14 | 1,968,087 | 97% / 100% / 100% |
| ETH | 4 | 228,616 | 99% / 99% / 100% |
| DOGE | 1 | 11,201 | 99% / 99% / 99% |
| SOL | 1 | 9,773 | 99% / 99% / 99% |
| LINK | 1 | 3,504 | 98% / 98% / 98% |
| QQQ | 12 | 351 | -12% / -0% / 20% |
| FXI | 12 | 46 | -16% / 2% / 20% |

BTC_spot+ETH+DOGE (crypto-spot, structurally REGULAR) = 99.4% of total tax; FXI+MSTR+QQQ (equity/ETF, REGULAR only under the capacity-exhaustion assumption) = 0.02% of total tax.

### Per-asset tax breakdown, CURRENT mode, 1.5/1.0 (CURRENT -- ADR-048 live rule)

| asset | fires | total tax (PLN) | gain-frac of trimmed $ (min / median / max) |
|---|--:|--:|--:|
| BTC_spot | 14 | 719,455 | 88% / 99% / 100% |
| ETH | 6 | 140,882 | 98% / 99% / 100% |
| SOL | 2 | 17,571 | 98% / 99% / 99% |
| DOGE | 1 | 15,260 | 99% / 99% / 99% |
| LINK | 1 | 8,658 | 98% / 98% / 98% |
| QQQ | 13 | 346 | -22% / 0% / 17% |
| miners | 1 | 284 | 48% / 48% / 48% |
| FXI | 13 | 46 | -18% / 0% / 22% |

Crypto-spot (structurally REGULAR) = 99.0% of total tax; equity/ETF (REGULAR only under the capacity-exhaustion assumption) = 0.04% of total tax.

## 1b. Full continuous-pass XIRR/maxDD/total-tax, WITH tax vs. WITHOUT tax (single 19.3y number, no window-overlap)

| config | mode | tax | XIRR%/yr | maxDD% | total tax paid (PLN) |
|---|---|---|--:|--:|--:|
| CURRENT (1.5x trigger / 1.0x trim-to -- ADR-048 live rule) | BASELINE | no tax | 36.97 | -19.9 | 0 |
| CURRENT (1.5x trigger / 1.0x trim-to -- ADR-048 live rule) | BASELINE | WITH TAX | 34.85 | -25.8 | 960,210 |
| CURRENT (1.5x trigger / 1.0x trim-to -- ADR-048 live rule) | CURRENT | no tax | 36.85 | -16.9 | 0 |
| CURRENT (1.5x trigger / 1.0x trim-to -- ADR-048 live rule) | CURRENT | WITH TAX | 33.91 | -22.3 | 902,503 |
| PRIOR (2.0x / 1.5x -- pre-ADR-048, reference only, NOT current) | BASELINE | no tax | 44.95 | -32.1 | 0 |
| PRIOR (2.0x / 1.5x -- pre-ADR-048, reference only, NOT current) | BASELINE | WITH TAX | 41.98 | -32.3 | 2,007,683 |
| PRIOR (2.0x / 1.5x -- pre-ADR-048, reference only, NOT current) | CURRENT | no tax | 44.54 | -25.6 | 0 |
| PRIOR (2.0x / 1.5x -- pre-ADR-048, reference only, NOT current) | CURRENT | WITH TAX | 41.31 | -29.6 | 2,221,578 |

**Full-pass tax drag, CURRENT mode (2007-02..2026-06, 19.3y, single continuous compounding path):**

- **1.5/1.0 (CURRENT, ADR-048 live rule)**: XIRR 36.85% -> 33.91% (-2.94pp), maxDD -16.9% -> -22.3% (-5.4pp), total tax paid 902,503 PLN (final portfolio value 12,932,048 PLN with tax, 18,479,765 without -- tax is 4.88% of what the no-tax final value would have been).
- **2.0/1.5 (PRIOR, pre-ADR-048)**: XIRR 44.54% -> 41.31% (-3.24pp), maxDD -25.6% -> -29.6% (-3.9pp), total tax paid 2,221,578 PLN (final portfolio value 31,571,385 PLN with tax, 46,314,539 without -- tax is 4.80% of what the no-tax final value would have been).

## 2. Does adding tax change the fire count itself? (second-order NAV-path effect)

Paying tax removes real capital from the portfolio (less credited to the GLD sink than in the no-tax run), which marginally shrinks total NAV going forward and can very slightly change *when* later fires cross their trigger. Comparing fire counts tax_on=True vs tax_on=False on the identical continuous pass:

| config | mode | fires (no tax) | fires (with tax) | delta |
|---|---|--:|--:|--:|
| CURRENT (1.5x trigger / 1.0x trim-to -- ADR-048 live rule) | BASELINE | 41 | 44 | +3 |
| CURRENT (1.5x trigger / 1.0x trim-to -- ADR-048 live rule) | CURRENT | 48 | 51 | +3 |
| 1.75 / 1.25 (sensitivity midpoint, between CURRENT and PRIOR) | BASELINE | 39 | 40 | +1 |
| 1.75 / 1.25 (sensitivity midpoint, between CURRENT and PRIOR) | CURRENT | 45 | 46 | +1 |
| PRIOR (2.0x / 1.5x -- pre-ADR-048, reference only, NOT current) | BASELINE | 37 | 39 | +2 |
| PRIOR (2.0x / 1.5x -- pre-ADR-048, reference only, NOT current) | CURRENT | 41 | 45 | +4 |

## 3. Headline XIRR/maxDD, WITH tax vs. WITHOUT tax, 5 start-date windows

Cell = XIRR%/yr (maxDD%). Primary comparison: 1.5/1.0 (CURRENT, ADR-048 live rule) vs. 2.0/1.5 (PRIOR, pre-ADR-048), both modes, no-tax vs. with-tax.

| config | mode | tax | 13y | 8y | 5y | 3y | 1y |
|---|---|---|--:|--:|--:|--:|--:|
| CURRENT (1.5x trigger / 1.0x trim-to -- ADR-048 live rule) | BASELINE | no tax | 37 (-31) | 37 (-32) | 22 (-53) | 14 (-21) | -34 (-33) |
| CURRENT (1.5x trigger / 1.0x trim-to -- ADR-048 live rule) | BASELINE | WITH TAX | 35 (-33) | 35 (-33) | 20 (-53) | 12 (-22) | -34 (-33) |
| CURRENT (1.5x trigger / 1.0x trim-to -- ADR-048 live rule) | CURRENT | no tax | 35 (-14) | 36 (-12) | 27 (-25) | 20 (-10) | -27 (-20) |
| CURRENT (1.5x trigger / 1.0x trim-to -- ADR-048 live rule) | CURRENT | WITH TAX | 33 (-21) | 33 (-13) | 24 (-25) | 19 (-10) | -27 (-21) |
| PRIOR (2.0x / 1.5x -- pre-ADR-048, reference only, NOT current) | BASELINE | no tax | 43 (-40) | 41 (-39) | 23 (-54) | 15 (-22) | -34 (-33) |
| PRIOR (2.0x / 1.5x -- pre-ADR-048, reference only, NOT current) | BASELINE | WITH TAX | 40 (-42) | 39 (-43) | 21 (-54) | 14 (-23) | -34 (-33) |
| PRIOR (2.0x / 1.5x -- pre-ADR-048, reference only, NOT current) | CURRENT | no tax | 40 (-26) | 39 (-14) | 26 (-25) | 18 (-12) | -28 (-22) |
| PRIOR (2.0x / 1.5x -- pre-ADR-048, reference only, NOT current) | CURRENT | WITH TAX | 37 (-30) | 37 (-15) | 24 (-25) | 17 (-13) | -28 (-22) |

Start months: 13y=2013-09, 8y=2018-09, 5y=2021-09, 3y=2023-09, 1y=2025-09. Cell = XIRR%/yr (maxDD%).

### Tax-drag deltas (XIRR pp lost to tax; maxDD pp change), CURRENT mode

| config | 13y | 8y | 5y | 3y | 1y |
|---|--:|--:|--:|--:|--:|
| CURRENT (1.5x trigger / 1.0x trim-to -- ADR-048 live rule) | -2.1xirr / -6.4dd | -2.9xirr / -0.6dd | -2.1xirr / -0.3dd | -1.3xirr / -0.2dd | -0.5xirr / -0.3dd |
| PRIOR (2.0x / 1.5x -- pre-ADR-048, reference only, NOT current) | -2.8xirr / -3.8dd | -2.8xirr / -0.8dd | -1.9xirr / -0.5dd | -1.6xirr / -0.9dd | -0.3xirr / -0.4dd |

## 4. Does the tax-adjusted gap between 2.0/1.5 (PRIOR) and 1.5/1.0 (CURRENT) widen or narrow?

No-tax gap (from study 15) vs. with-tax gap, CURRENT mode, XIRR%/yr (maxDD%) at each window, config = 2.0/1.5 [PRIOR, pre-ADR-048] minus 1.5/1.0 [CURRENT, ADR-048 live rule] (positive XIRR-gap = looser/PRIOR rule wins on return; positive maxDD-gap = looser/PRIOR rule has WORSE (deeper) drawdown):

| window | no-tax XIRR gap | with-tax XIRR gap | no-tax maxDD gap | with-tax maxDD gap |
|---|--:|--:|--:|--:|
| 13y | +5.0pp | +4.3pp | -11.7pp | -9.1pp |
| 8y | +3.9pp | +4.0pp | -2.2pp | -2.4pp |
| 5y | -0.6pp | -0.4pp | -0.2pp | -0.4pp |
| 3y | -1.8pp | -2.0pp | -2.9pp | -3.6pp |
| 1y | -1.0pp | -0.9pp | -1.2pp | -1.3pp |

## 5. Sensitivity grid -- fire count AND cumulative tax across all 3 configs, CURRENT mode

| config | fires (no tax) | fires (with tax) | total tax paid (PLN) | tax paid per fire (PLN) |
|---|--:|--:|--:|--:|
| CURRENT (1.5x trigger / 1.0x trim-to -- ADR-048 live rule) | 48 | 51 | 902,503 | 17,696 |
| 1.75 / 1.25 (sensitivity midpoint, between CURRENT and PRIOR) | 45 | 46 | 1,513,742 | 32,907 |
| PRIOR (2.0x / 1.5x -- pre-ADR-048, reference only, NOT current) | 41 | 45 | 2,221,578 | 49,368 |


Caveats carried over from study 15: miners-basket survivorship, gold's tailwind flattering any gold-destination trim rule, overlapping windows in section 3's 5-window table, thin history for SUI/COIN/SOL. **New caveat for this study**: the average-cost cost-basis approximation (not true FIFO) likely UNDERSTATES true tax drag on exactly the assets this audit cares about most (BTC-dominated fires at the live rule) -- see the numbered writeup's methodology section for the reasoning. No loss offset/carryforward is modeled (every realized loss is simply untaxed, not banked against a future gain) -- a further simplification, not modeled either direction.
