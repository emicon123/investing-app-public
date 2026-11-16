# 11 — On-chain correlation / BI analysis: redundancy, stability, and the LTH/STH signal vs the macro gate

Follow-on to file 10 (which established pooled-window IC for SOPR family, MVRV-Z, NUPL, LTH/STH supply ratio). This is a correlation/BI pass, not a new pooled-IC rerun: (1) how redundant are the on-chain metrics with each other, (2) does the pooled IC hold steady over 2022-2026 or is it driven by one sub-period, (3) does LTH/STH supply ratio add anything beyond the shipped macro gate, (4) any signal the pooled approach missed (divergence, regime effects). Same no-lookahead Spearman-IC methodology as files 03/08/10.

## 1. Cross-correlation matrix among the on-chain metrics (native, 2022-07+)

Pairwise Spearman correlation on the common monthly window (contemporaneous levels, not vs forward return — this is about redundancy between signals, not predictive power).

Sample: 2022-07 .. 2026-07 (49 months)

| signal | SOPR | LTH-SOPR | STH-SOPR | MVRV-Z (native) | NUPL (native) | LTH/STH supply ratio | LTH supply 3mo %chg | STH supply 3mo %chg |
|---|--:|--:|--:|--:|--:|--:|--:|--:|
| SOPR | +1.00 | +0.69 | +0.70 | +0.71 | +0.75 | -0.07 | -0.23 | +0.27 |
| LTH-SOPR | +0.69 | +1.00 | +0.21 | +0.90 | +0.90 | -0.27 | -0.26 | +0.29 |
| STH-SOPR | +0.70 | +0.21 | +1.00 | +0.28 | +0.34 | +0.26 | -0.03 | +0.05 |
| MVRV-Z (native) | +0.71 | +0.90 | +0.28 | +1.00 | +0.99 | -0.34 | -0.31 | +0.34 |
| NUPL (native) | +0.75 | +0.90 | +0.34 | +0.99 | +1.00 | -0.30 | -0.32 | +0.34 |
| LTH/STH supply ratio | -0.07 | -0.27 | +0.26 | -0.34 | -0.30 | +1.00 | +0.67 | -0.70 |
| LTH supply 3mo %chg | -0.23 | -0.26 | -0.03 | -0.31 | -0.32 | +0.67 | +1.00 | -0.99 |
| STH supply 3mo %chg | +0.27 | +0.29 | +0.05 | +0.34 | +0.34 | -0.70 | -0.99 | +1.00 |

**Near-collinear pairs (|rho| >= 0.85, off-diagonal):**
- LTH supply 3mo %chg vs STH supply 3mo %chg: rho=-0.99
- MVRV-Z (native) vs NUPL (native): rho=+0.99
- LTH-SOPR vs MVRV-Z (native): rho=+0.90
- LTH-SOPR vs NUPL (native): rho=+0.90

**High but not collinear (0.70 <= |rho| < 0.85):**
- SOPR vs NUPL (native): rho=+0.75
- SOPR vs MVRV-Z (native): rho=+0.71

## 2. Rolling-window IC vs BTC forward return — is the pooled 2022+ IC stable?

Rolling 15-month window, stepped every 3 months, Spearman IC vs BTC forward 3-month return (each window's own internal IC, not cumulative). File 10 only reported one pooled number per signal per horizon over the whole window — this checks whether that pooled reading reflects a stable relationship or a single episode.

(12 rolling windows, 15mo each, stepped 3mo; n per window ~= 15 minus a few NaNs at the tail from the fwd-3m shift)

| window | MVRV-Z (native) | NUPL (native) | LTH-SOPR | SOPR | LTH/STH supply ratio |
|---|--:|--:|--:|--:|--:|
| 2022-07..2023-09 | -0.11 | -0.12 | -0.02 | -0.19 | +0.23 |
| 2022-10..2023-12 | -0.10 | -0.11 | -0.17 | -0.22 | +0.36 |
| 2023-01..2024-03 | -0.09 | -0.10 | -0.16 | -0.22 | +0.70 |
| 2023-04..2024-06 | -0.34 | -0.34 | -0.42 | -0.16 | +0.75 |
| 2023-07..2024-09 | -0.64 | -0.63 | -0.41 | -0.42 | +0.86 |
| 2023-10..2024-12 | -0.75 | -0.74 | -0.57 | -0.48 | +0.80 |
| 2024-01..2025-03 | -0.80 | -0.76 | -0.44 | -0.53 | +0.50 |
| 2024-04..2025-06 | -0.81 | -0.82 | -0.43 | -0.63 | +0.38 |
| 2024-07..2025-09 | -0.76 | -0.60 | -0.45 | -0.60 | +0.32 |
| 2024-10..2025-12 | +0.06 | +0.18 | +0.19 | +0.02 | +0.30 |
| 2025-01..2026-03 | +0.00 | +0.04 | +0.19 | -0.06 | +0.08 |
| 2025-04..2026-06 | +0.09 | +0.15 | +0.07 | +0.04 | +0.27 |

**Sign-stability summary** (share of windows with same sign as the pooled file-10 fwd-3m IC, and min/max across windows):
| signal | pooled-sign match | min IC | max IC |
|---|--:|--:|--:|
| MVRV-Z (native) | 75% | -0.81 | +0.09 |
| NUPL (native) | 75% | -0.82 | +0.18 |
| LTH-SOPR | 75% | -0.57 | +0.19 |
| SOPR | 83% | -0.63 | +0.04 |
| LTH/STH supply ratio | 100% | +0.08 | +0.86 |

## 3. LTH/STH supply ratio vs the shipped macro gate — orthogonal or redundant?

Sample: 2022-07 .. 2026-05 (47 months)

### 3a. Contemporaneous correlation: LTH/STH ratio (level & 3mo change) vs macro inputs

| pair | rho | n | t |
|---|--:|--:|--:|
| LTH/STH level vs net-liq accel | +0.41 | 47 | 3.0 |
| LTH/STH level vs Global-M2 accel | +0.21 | 47 | 1.4 |
| LTH/STH level vs macro risk-off (0/1) | -0.38 | 47 | -2.7 |
| LTH/STH 3mo-chg vs net-liq accel | +0.22 | 44 | 1.4 |
| LTH/STH 3mo-chg vs Global-M2 accel | +0.29 | 44 | 2.0 |
| LTH/STH 3mo-chg vs macro risk-off (0/1) | -0.41 | 44 | -2.9 |

Near-zero contemporaneous correlation here would mean the two systems are reading different underlying information (on-chain holder behavior vs macro-liquidity flows) even if both end up correlating with forward BTC returns separately.

### 3b. Conditional IC — does LTH/STH ratio predict returns inside vs outside macro risk-off months?

| regime | months | fwd 1m | fwd 2m | fwd 3m | fwd 6m | fwd 12m |
|---|--:|--:|--:|--:|--:|--:|
| macro risk-off = 0 (risk-on) | 33 | +0.20(n=33,t=1.1) | +0.44(n=32,t=2.7) | +0.59(n=31,t=4.0) | +0.61(n=28,t=3.9) | +0.51(n=25,t=2.8) |
| macro risk-off = 1 | 14 | +0.12(n=14,t=0.4) | +0.23(n=14,t=0.8) | +0.33(n=14,t=1.2) | +0.59(n=14,t=2.5) | +0.55(n=11,t=2.0) |

### 3c. Partial correlation — LTH/STH ratio vs forward return, controlling for the macro composite score

Partial Spearman correlation removes the part of LTH/STH's apparent predictive power that's shared with the macro signal. If partial IC stays close to raw IC, the two are complementary; if it collapses toward 0, LTH/STH is just re-expressing the same macro information.

| horizon | raw IC (LTH/STH vs fwd) | partial IC (control macro score) | n |
|---|--:|--:|--:|
| 1m | +0.29 | +0.24 | 47 |
| 2m | +0.45 | +0.42 | 46 |
| 3m | +0.54 | +0.55 | 45 |
| 6m | +0.54 | +0.61 | 42 |
| 12m | +0.58 | +0.52 | 36 |

### 3d. Two-way split: macro regime x LTH/STH-ratio regime (median split) -> mean fwd 3m return

| macro state | LTH/STH state | months | mean fwd 3m |
|---|---|--:|--:|
| risk-off=0 | below median | 14 | +0.3% |
| risk-off=0 | at/above median | 17 | +29.6% |
| risk-off=1 | below median | 9 | -8.6% |
| risk-off=1 | at/above median | 5 | +18.2% |

## 4. Signals the pooled-IC approach in file 10 might have missed

### 4a. SOPR-family divergence (blended vs cohort-specific) as its own signal

Rationale: a widening gap between the blended SOPR and the LTH-only cut could flag late-cycle behavior (fresh/short-term coins realizing gains while long-term holders sit flat or underwater, or vice versa) that neither series shows alone.

| spread | fwd 1m | fwd 2m | fwd 3m | fwd 6m | fwd 12m |
|---|--:|--:|--:|--:|--:|
| SOPR - LTH-SOPR | -0.09(n=47,t=-0.6) | +0.06(n=46,t=0.4) | +0.15(n=45,t=1.0) | +0.36(n=42,t=2.5) o | +0.57(n=36,t=4.0) o |
| STH-SOPR - LTH-SOPR | -0.09(n=47,t=-0.6) | +0.05(n=46,t=0.3) | +0.15(n=45,t=1.0) | +0.36(n=42,t=2.5) o | +0.58(n=36,t=4.2) o |

### 4b. Regime-dependent IC — split the 2022-07..2026-06 window at its midpoint bull/bear character, not just chronological halves

File 10's split-sample check already used chronological halves for LTH/STH ratio. Here: split months by trailing-6-month BTC return sign (bear = trailing 6m return < 0, bull = >= 0) to see if MVRV-Z/NUPL/SOPR-family IC differs by market regime rather than by calendar period (a pooled stat blends both).

| signal | regime | months | fwd 3m IC | fwd 6m IC |
|---|---|--:|--:|--:|
| MVRV-Z (native) | bear (trail 6m<0) | 18 | -0.05(n=15) | -0.46(n=12) |
| MVRV-Z (native) | bull (trail 6m>=0) | 30 | -0.48(n=30) | -0.55(n=30) |
| NUPL (native) | bear (trail 6m<0) | 18 | -0.05(n=15) | -0.46(n=12) |
| NUPL (native) | bull (trail 6m>=0) | 30 | -0.43(n=30) | -0.49(n=30) |
| LTH-SOPR | bear (trail 6m<0) | 18 | +0.09(n=15) | -0.31(n=12) |
| LTH-SOPR | bull (trail 6m>=0) | 30 | -0.45(n=30) | -0.47(n=30) |
| SOPR | bear (trail 6m<0) | 18 | +0.04(n=15) | -0.10(n=12) |
| SOPR | bull (trail 6m>=0) | 30 | -0.26(n=30) | -0.27(n=30) |
| LTH/STH supply ratio | bear (trail 6m<0) | 18 | +0.70(n=15) | +0.75(n=12) |
| LTH/STH supply ratio | bull (trail 6m>=0) | 30 | +0.41(n=30) | +0.34(n=30) |

### 4c. ETH cross-check: is BTC's on-chain-metric redundancy structure the same for ETH (derived NUPL/MVRV-Z, 2015+ and 2022+ subset)?

BTC vs ETH MVRV-Z (derived, full overlap 2015-08..2026-07, n=120): rho=+0.82
BTC vs ETH NUPL (derived, n=132): rho=+0.57
Both assets' on-chain valuation metrics move almost in lockstep (shared market beta dominates the derived MVRV/NUPL construction) — consistent with file 10's finding that the macro signal (not on-chain) is what differentiates ETH's risk profile from BTC's.

