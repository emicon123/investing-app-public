# On-chain signal study #2 — SOPR / LTH-STH-SOPR / MVRV-Z / NUPL / LTH-STH supply

Data: bitcoin-data.com (free, no auth), daily since ~2022-07 for native series; derived NUPL/MVRV-Z (Coinmetrics-sourced, 2010+) for a longer-history cross-check. Spearman IC vs BTC forward return, no-lookahead (signal known at month t, target = return over t..t+h). Cells: IC (n, t-stat); `o` = |t|>=1.96.

| signal | fwd 1m | fwd 2m | fwd 3m | fwd 6m | fwd 12m |
|---|--:|--:|--:|--:|--:|
| SOPR (level) | +0.10 (n=47,t=0.6) | -0.06 (n=46,t=-0.4) | -0.12 (n=45,t=-0.8) | -0.24 (n=42,t=-1.6) | -0.32 (n=36,t=-2.0) |
| SOPR<1 (indicator) | -0.05 (n=47,t=-0.4) | +0.04 (n=46,t=0.2) | +0.09 (n=45,t=0.6) | +0.19 (n=42,t=1.2) | +0.15 (n=36,t=0.9) |
| LTH-SOPR (level) | +0.10 (n=47,t=0.7) | -0.05 (n=46,t=-0.4) | -0.15 (n=45,t=-1.0) | -0.37 (n=42,t=-2.5) o | -0.57 (n=36,t=-4.0) o |
| STH-SOPR (level) | +0.10 (n=47,t=0.7) | +0.05 (n=46,t=0.3) | +0.16 (n=45,t=1.1) | +0.00 (n=42,t=0.0) | +0.20 (n=36,t=1.2) |
| STH-SOPR<1 (indicator) | -0.08 (n=47,t=-0.5) | -0.08 (n=46,t=-0.5) | -0.13 (n=45,t=-0.9) | -0.04 (n=42,t=-0.3) | -0.08 (n=36,t=-0.5) |
| MVRV Z-Score (native) | -0.07 (n=47,t=-0.4) | -0.20 (n=46,t=-1.3) | -0.25 (n=45,t=-1.7) | -0.49 (n=42,t=-3.6) o | -0.62 (n=36,t=-4.6) o |
| NUPL (native) | -0.03 (n=47,t=-0.2) | -0.15 (n=46,t=-1.0) | -0.20 (n=45,t=-1.3) | -0.45 (n=42,t=-3.1) o | -0.59 (n=36,t=-4.3) o |
| MVRV Z-Score (derived, 2010+) | +0.06 (n=179,t=0.8) | +0.03 (n=178,t=0.4) | +0.02 (n=177,t=0.2) | -0.04 (n=174,t=-0.5) | -0.20 (n=168,t=-2.6) o |
| NUPL (derived, 2010+) | +0.13 (n=191,t=1.8) | +0.15 (n=190,t=2.1) o | +0.15 (n=189,t=2.0) o | +0.08 (n=186,t=1.0) | -0.07 (n=180,t=-0.9) |
| LTH supply 3mo %chg | +0.17 (n=44,t=1.1) | +0.34 (n=43,t=2.3) o | +0.46 (n=42,t=3.3) o | +0.35 (n=39,t=2.3) o | +0.09 (n=33,t=0.5) |
| STH supply 3mo %chg | -0.17 (n=44,t=-1.1) | -0.35 (n=43,t=-2.4) o | -0.49 (n=42,t=-3.5) o | -0.36 (n=39,t=-2.4) o | -0.11 (n=33,t=-0.6) |
| LTH/STH supply ratio | +0.29 (n=47,t=2.0) o | +0.45 (n=46,t=3.3) o | +0.54 (n=45,t=4.3) o | +0.54 (n=42,t=4.0) o | +0.58 (n=36,t=4.1) o |

Negative IC = high signal -> lower forward return (the desired direction for a top/de-risk trigger). Positive IC = signal moves WITH the trend (momentum/coincident, not a reversal signal).

## Threshold/bucket check on the two signals the user flagged as important

### SOPR (fwd 3m)
| threshold | months <thr | mean fwd (below) | months >=thr | mean fwd (above) |
|---|--:|--:|--:|--:|
| 0.95 | 1 | +72.6% | 44 | +10.2% |
| 0.98 | 1 | +72.6% | 44 | +10.2% |
| 1.0 | 19 | +14.3% | 26 | +9.6% |
| 1.02 | 43 | +12.2% | 2 | -1.0% |
| 1.05 | 45 | +11.6% | 0 | +nan% |

### STH-SOPR (fwd 3m)
| threshold | months <thr | mean fwd (below) | months >=thr | mean fwd (above) |
|---|--:|--:|--:|--:|
| 0.95 | 0 | +nan% | 45 | +11.6% |
| 0.98 | 2 | +27.1% | 43 | +10.9% |
| 1.0 | 21 | +8.6% | 24 | +14.2% |
| 1.02 | 42 | +10.5% | 3 | +26.5% |
| 1.05 | 44 | +11.6% | 1 | +9.7% |

## Cross-check: native (2022+) vs derived (2010+) MVRV-Z and NUPL

Overlap window: 2022-07 .. 2026-07 (49 months)
MVRV Z-Score native vs derived, Spearman corr = 0.999 (n=49) -- should be near 1.0 if the derivation is sound (both track the same underlying MVRV/MarketCap).
NUPL native vs derived, Spearman corr = 0.999 (n=49).

## Comparison to the shipped macro risk-off signal (same window, 2022+)

| macro risk-off (0/1) | -0.09 (n=191,t=-1.2) | -0.18 (n=190,t=-2.6) o | -0.18 (n=189,t=-2.5) o | -0.10 (n=186,t=-1.4) | -0.08 (n=180,t=-1.1) |
