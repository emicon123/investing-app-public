# 04 — Timing backtest: is the lead tradeable?

Scripts: `liquidity_timing_backtest.py` (also regimes/timing in `macro_study.py`)
Raw: `results/liquidity_timing_backtest.md`, `macro_study.md`

## What was tested
Weekly, no-lookahead. Rule: **long the asset when Fed net-liquidity's L-week % change > 0,
else T-bill cash.** Signal lagged 1 week, 10 bps cost per switch, T-bill cash leg. Window
L ∈ {4,8,13,26,52}w scanned ("which time-shift").
- **Part 1** full-sample scan (in-sample → descriptive).
- **Part 2** expanding **walk-forward**: pick best-Sharpe L on the past, trade the next 13
  weeks OOS, chain the OOS blocks. This is the honest test.

## Part 1 — full-sample (in-sample, with costs)
| asset | buy&hold Sharpe | best timed Sharpe (window) |
|---|--:|--|
| BTC | 1.23 | nothing beats it |
| ETH | 1.05 | **1.20** (4w); 13/26w cut maxDD to −56% |
| SOL | 1.14 | **1.40** (52w) |

## Part 2 — walk-forward OOS (the verdict)
| asset | OOS Sharpe | B&H Sharpe | OOS CAGR | B&H CAGR | OOS maxDD vs B&H |
|---|--:|--:|--:|--:|--|
| **ETH** | **0.81** | 0.65 | **36%** | 22% | similar |
| SOL | 0.12 | 0.82 | −11% | 42% | — |
| BTC | 1.00 | 1.03 | 54% | 65% | **−67% vs −85%** |

## Outcomes
- **ETH = the one robust tradeable edge.** Net-liquidity timing beats buy&hold *out-of-sample*
  on Sharpe (0.65→0.81) and return (22→36%). Survives no-lookahead walk-forward.
- **SOL = overfitting trap.** In-sample best (1.40) → OOS collapse (0.12, −11% CAGR). Its
  in-sample brilliance was a short, single-cycle artifact. **Do not trade.**
- **BTC = drawdown control only.** No return edge (matches "no lead"), but timing cuts maxDD
  −85%→−67%.
- **Optimal window is asset-specific** (ETH short ~4w, SOL long) — itself an overfit warning;
  trust the walk-forward, not the scan.
- Supporting regime evidence (`macro_study.md`): "net-liq YoY > 0" and "Global-M2 YoY rising"
  separate forward returns strongly (BTC next-month +20.8% vs +3.3% when net-liq expanding);
  the *level* of M2 growth gives no edge — **direction beats level**.

## Caveats
Cash leg uses DGS3MO when reachable else 0% (conservative). SOL OOS short (from 2023). Binary
long/cash, no vol-targeting.

## Verdict
After costs, the durable tradeable edge is **ETH on net-liquidity momentum**. BTC's usable
angle is **de-risking** (and the Global-M2-acceleration lead from file 03, not yet backtested).
Not-yet-built: walk-forward of a "Global-M2-YoY-accelerating → risk-on" rule.
