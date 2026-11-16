# Profit-taking trim rule backtest

Rule: if an asset ≥ 2× its target weight, trim to halfway (target+actual)/2, freed → GOLD. Tested on gold + crypto sleeve (where overshoots happen), monthly, 15bps turnover.

Window 2018-01 → 2026-06 (102 months).

| strategy | CAGR% | Sharpe | maxDD% | final gold wt% |
|---|--:|--:|--:|--:|
| Drift (never rebalance) | 19 | 0.62 | -64 | 31 |
| Annual rebalance→target | 47 | 0.93 | -68 | 37 |
| TRIM rule (2×→halfway→gold) | 31 | 0.89 | -47 | 65 |

**Trim vs Annual-rebalance, by year (portfolio return %):**
| year | TRIM% | Annual% | Δ |
|--:|--:|--:|--:|
| 2018 | -43 | -41 | -2 |
| 2019 | +23 | +36 | -13 |
| 2020 | +137 | +221 | -84 |
| 2021 | +236 | +537 | -302 |
| 2022 | -40 | -61 | +21 |
| 2023 | +50 | +114 | -65 |
| 2024 | +59 | +63 | -4 |
| 2025 | +27 | +28 | -1 |
| 2026 | -5 | -8 | +3 |

