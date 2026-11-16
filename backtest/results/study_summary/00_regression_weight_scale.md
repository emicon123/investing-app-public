# 00 — Regression-weight band scale: is ×2.0 too aggressive?

**Date:** 2026-06-16/17. **Status: SUPERSEDED — the mechanism this tested was removed
entirely from production on 2026-06-17 (ADR-040).**

Scripts: `dca_regression_backtest.py` (→ `results/dca_regression_summary.json`),
`portfolio_dca_backtest.py` (→ `results/portfolio_dca_summary.json`),
`example_portfolio_backtest.py` (→ `results/example_portfolio_summary.json`).

## What was being tested
On 2026-06-16 MAIN's DCA used a geometric regression-band weight scale: weight 1.0 at
0σ off a log-linear trend, ×2.0 per SD below trend (undervalued → buy more), ×0.5 per SD
above, out to ±4σ (`RegressionWeightService.BAND_WEIGHTS`, since deleted). Question: is
the ×2.0 scale justified, versus a milder ×1.5/×1.25, or plain equal-weight?

## Three backtests, increasing rigor
1. **`dca_regression_backtest.py`** — generic 26-asset universe, but data only spans
   2017-2026 so early buys regressed on 4-week–2yr stubs, not a full 10y window. Its
   headline result (×2.0 wins at 8yr) is a **short-window artifact — not trustworthy**.
2. **`portfolio_dca_backtest.py`** — the ACTUAL MAIN portfolio, max-available history per
   asset, PLN-native with USD/EUR FX, regression on each asset's native weekly close (no
   lookahead). Most trustworthy real-book test. **Tilt hurt returns at every horizon
   (1/3/5/8yr), monotonically worse the more aggressive the scale**: 5yr equal +1018% vs
   ×2.0 +258%; 8yr equal +958% vs ×2.0 +503%. Reason: the book is momentum/crypto-heavy,
   so "buy below trend / trim above trend" under-bought the parabolic winners. ×2.0 did
   cut max drawdown (8yr −85% vs equal −96%) but at a steep growth cost, and pushed
   concentration up to ~16-20% avg single-asset month vs ~4% equal-weight. Caveat: only
   6/30 holdings had ≥10y of history, so most of the book never got the full window
   regression was designed for — production hit the same data-depth wall.
3. **`example_portfolio_backtest.py`** — a MATURE, fully-covered proxy portfolio (Gold
   30% / Silver 10% / six 20+yr US large-caps 10% each) where the 10y regression window
   is genuinely full at every buy date, isolating the *weighting* question from data-depth
   noise. Result: the tilt is roughly a wash long-run (8yr ×1.5 122.6% vs fixed 121.5%,
   ×2.0 121.4%), mildly hurts at 3-5yr, and **×2.0 is strictly dominated** — same/worse
   return, highest drawdown (8yr −11.4% vs −7.9% fixed), most concentration (~36% vs 30%
   target).

## Verdict
Across all three tests, ×2.0 never earns its risk; at best a mild ×1.25-1.5 tilt is
marginally net-positive; the strategic/target allocation itself does essentially all the
real work. **Absolute ROIs above are survivorship fantasy** (DCA into today's winners
since inception) — only the relative equal-vs-tilt ranking is meaningful.

## Outcome
This evidence (≈zero information coefficient, no return uplift, added concentration and
drawdown) led to **removing the regression-weight tilt outright** on 2026-06-17 rather
than retuning it (ADR-040). MAIN now does pure target-allocation-proportional, buy-only
DCA (`T_i = R_i`), same shape as RETIREMENT. `RegressionWeightService`, the
`regression_weight`/`regression_ticker` columns, and the "Waga" UI column were deleted.
The standalone log-linear regression *analytics* chart (`LinearRegressionService`) was
kept — visualization only, never fed purchase math. There is no tilt knob left to tune;
if one is ever reconsidered, this study's conclusion (equal/strategic weight does the
real work, ×2.0 never earns its risk) is the prior to beat.
