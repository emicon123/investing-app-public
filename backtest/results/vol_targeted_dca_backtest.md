# Study 20 -- vol-targeted / risk-parity-weighted catch-up DCA

Buy-only DCA 1000+1000/mo PLN, 15bps, CURRENT 1.5x/1.0x-trim->gold kept in every variant (ADR-048 live rule; was 2.0x/1.5x PRIOR before the 2026-09-25 refresh). Latest common month: 2026-09. Cell = XIRR%/yr (maxDD%) [Sharpe]. Sharpe = annualized mean/std of the monthly nav-growth-factor return chain (x sqrt(12), no risk-free subtraction).

## Risk-on / risk-off target tables (unchanged from study 14)

| asset | risk-on % | risk-off % | retention |
|---|--:|--:|--:|
| GLD | 20.000 | 30.000 | 150% |
| QQQ | 22.500 | 27.500 | 122% |
| FXI | 2.500 | 7.500 | 300% |
| BOND_PL | 0.000 | 7.500 | nan% |
| BOND_DE | 0.000 | 2.500 | nan% |
| BTC_spot | 20.000 | 12.000 | 60% |
| miners | 5.000 | 0.500 | 10% |
| ETH | 10.000 | 1.000 | 10% |
| SOL | 5.000 | 0.500 | 10% |
| COIN | 3.000 | 0.300 | 10% |
| SUI | 3.000 | 0.300 | 10% |
| DOGE | 1.000 | 0.100 | 10% |
| LINK | 1.500 | 0.150 | 10% |

## Realized-vol sanity check (annualized trailing-12mo sigma, median over each asset's own available history)

| asset | median annualized vol | min | max | months with a valid sigma |
|---|--:|--:|--:|--:|
| GLD | 17% | 7% | 34% | 222 |
| QQQ | 15% | 7% | 29% | 259 |
| FXI | 18% | 9% | 45% | 248 |
| BOND_PL | 11% | 7% | 17% | 49 |
| BOND_DE | 8% | 7% | 9% | 20 |
| BTC_spot | 66% | 32% | 219% | 179 |
| miners | 92% | 36% | 183% | 158 |
| ETH | 92% | 21% | 173% | 118 |
| SOL | 112% | 49% | 216% | 62 |
| COIN | 88% | 58% | 111% | 50 |
| SUI | 110% | 66% | 134% | 25 |
| DOGE | 101% | 38% | 275% | 91 |
| LINK | 89% | 54% | 196% | 91 |

## Headline results -- fixed N=4 variant matrix (decided up front, not a scan)

| variant | 13y | 8y | 5y | 3y | 1y |
|---|--:|--:|--:|--:|--:|
| BASELINE (fixed target, no macro) / dollar-weighted catch-up (control, = study 14) | 37 (-31) [0.98] | 37 (-32) [1.22] | 22 (-53) [0.57] | 14 (-21) [1.13] | -34 (-33) [-1.45] |
| BASELINE (fixed target, no macro) / vol-weighted catch-up (new) | 36 (-29) [1.30] | 36 (-31) [1.31] | 22 (-47) [0.61] | 13 (-20) [1.08] | -33 (-30) [-1.19] |
| CURRENT (ADR-044 blend, momentum framing) / dollar-weighted catch-up (control, = study 14) | 35 (-14) [1.00] | 36 (-12) [1.49] | 26 (-24) [1.15] | 20 (-10) [1.67] | -27 (-20) [-0.77] |
| CURRENT (ADR-044 blend, momentum framing) / vol-weighted catch-up (new) | 34 (-13) [1.44] | 35 (-12) [1.53] | 26 (-22) [1.18] | 17 (-10) [1.53] | -25 (-20) [-0.50] |

Start months: 13y=2013-09, 8y=2018-09, 5y=2021-09, 3y=2023-09, 1y=2025-09.

## CAGR-vs-Sharpe vol-targeting diagnostic (dollar-weighted control vs. vol-weighted)

| mode | window | XIRR dollar | XIRR vol | Delta XIRR (pp) | Sharpe dollar | Sharpe vol | Delta Sharpe |
|---|---|--:|--:|--:|--:|--:|--:|
| BASELINE | 13y | 36.9% | 35.8% | -1.0 | 0.98 | 1.30 | +0.32 |
| BASELINE | 8y | 37.2% | 35.7% | -1.5 | 1.22 | 1.31 | +0.09 |
| BASELINE | 5y | 22.1% | 22.0% | -0.1 | 0.57 | 0.61 | +0.03 |
| BASELINE | 3y | 13.8% | 13.2% | -0.6 | 1.13 | 1.08 | -0.04 |
| BASELINE | 1y | -34.4% | -32.8% | +1.6 | -1.45 | -1.19 | +0.25 |
| CURRENT | 13y | 35.3% | 33.5% | -1.7 | 1.00 | 1.44 | +0.44 |
| CURRENT | 8y | 35.6% | 35.0% | -0.6 | 1.49 | 1.53 | +0.04 |
| CURRENT | 5y | 26.5% | 25.8% | -0.7 | 1.15 | 1.18 | +0.04 |
| CURRENT | 3y | 20.0% | 17.0% | -3.0 | 1.67 | 1.53 | -0.15 |
| CURRENT | 1y | -26.7% | -25.4% | +1.3 | -0.77 | -0.50 | +0.27 |

## Cross-check: 60/40 classic portfolio, dollar- vs. vol-weighted catch-up (BUYHOLD only)

Same SPY/AGG named weights and DCA convention as `classic_portfolios_backtest.py`'s BUYHOLD mode (deficit-proportional catch-up, no selling, no annual rebalance) -- this script's own self-contained `sim_classic()`, not an import of that file. Purpose: check whether any dollar-vs-vol effect seen on the user's concentrated crypto-heavy book also shows up on a plain, diversified 2-asset textbook portfolio.

| weighting | 13y | 8y | 5y | 3y | 1y |
|---|--:|--:|--:|--:|--:|
| dollar-weighted catch-up (control, = study 14) | 9 (-12) [1.02] | 8 (-12) [0.89] | 7 (-12) [0.64] | 7 (-12) [1.03] | -14 (-3) [2.04] |
| vol-weighted catch-up (new) | 9 (-12) [1.02] | 8 (-12) [0.88] | 7 (-12) [0.63] | 7 (-12) [1.03] | -14 (-3) [1.98] |

| window | XIRR dollar | XIRR vol | Delta XIRR (pp) | Sharpe dollar | Sharpe vol | Delta Sharpe |
|---|--:|--:|--:|--:|--:|--:|
| 13y | 9.5% | 9.5% | -0.0 | 1.02 | 1.02 | -0.00 |
| 8y | 8.4% | 8.4% | -0.0 | 0.89 | 0.88 | -0.00 |
| 5y | 6.9% | 6.9% | -0.0 | 0.64 | 0.63 | -0.00 |
| 3y | 6.7% | 6.6% | -0.1 | 1.03 | 1.03 | +0.00 |
| 1y | -14.1% | -14.3% | -0.1 | 2.04 | 1.98 | -0.06 |

Caveats: same caveats as study 14 apply unchanged (miners survivorship, gold's rally flatters de-risk-into-gold variants, overlapping windows share one underlying price/macro history, 1y/3y windows are noisy single-episode reads, the RISK_OFF table is a judgment-call adaptation not independently re-optimized); additionally: sigma_i is computed over each asset's FULL available price history (not reset per backtest window), so by the time any of the 5 windows starts, only genuinely recently-listed assets (SUI, COIN, near their own inception) can still be in the <12mo fallback state -- see the sanity-check table above for how many months of sigma each asset actually has; only ONE lookback window (12 months) was tried for sigma_i, not scanned -- if a different window is tried in a future study, that must be counted and labeled as its own scan, not silently folded into this fixed N=4 comparison; study 04's exact original Sharpe convention could not be re-verified (that script no longer exists, see module docstring) -- the convention used here (annualized mean/std, no risk-free subtraction) is the standard one, named explicitly as an assumption.
