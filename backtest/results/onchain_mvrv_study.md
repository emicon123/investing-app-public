# On-chain MVRV study — top-timing signal vs the shipped macro de-risk gate

MVRV percentile = no-lookahead expanding rank of monthly MVRV vs its own history to date (min 12mo burn-in). Cells = Spearman IC (n obs), t-stat in parens.

## BTC — sample 2011-07 .. 2026-05 (179 months)

| signal | fwd 1m | fwd 2m | fwd 3m | fwd 6m | fwd 12m |
|---|--:|--:|--:|--:|--:|
| MVRV percentile | +0.13 (n=178,t=1.7) | +0.12 (n=177,t=1.5) | +0.10 (n=176,t=1.4) | +0.04 (n=173,t=0.6) | -0.10 (n=167,t=-1.2) |
| net-liq accel | +0.16 (n=178,t=2.1) o | +0.17 (n=177,t=2.3) o | +0.16 (n=176,t=2.1) o | +0.17 (n=173,t=2.3) o | +0.18 (n=167,t=2.4) o |
| Global-M2 accel | +0.08 (n=178,t=1.1) | +0.09 (n=177,t=1.2) | +0.09 (n=176,t=1.1) | -0.02 (n=173,t=-0.2) | +0.03 (n=167,t=0.4) |
| macro risk-off (0/1) | -0.09 (n=178,t=-1.2) | -0.18 (n=177,t=-2.4) o | -0.17 (n=176,t=-2.2) o | -0.08 (n=173,t=-1.1) | -0.08 (n=167,t=-1.0) |

## ETH — sample 2016-08 .. 2026-05 (118 months)

| signal | fwd 1m | fwd 2m | fwd 3m | fwd 6m | fwd 12m |
|---|--:|--:|--:|--:|--:|
| MVRV percentile | +0.09 (n=117,t=1.0) | +0.09 (n=116,t=1.0) | +0.09 (n=115,t=1.0) | -0.05 (n=112,t=-0.5) | -0.20 (n=106,t=-2.1) o |
| net-liq accel | +0.32 (n=117,t=3.6) o | +0.37 (n=116,t=4.3) o | +0.31 (n=115,t=3.5) o | +0.25 (n=112,t=2.7) o | +0.33 (n=106,t=3.5) o |
| Global-M2 accel | +0.14 (n=117,t=1.5) | +0.15 (n=116,t=1.7) | +0.15 (n=115,t=1.7) | +0.09 (n=112,t=0.9) | +0.25 (n=106,t=2.6) o |
| macro risk-off (0/1) | -0.20 (n=117,t=-2.2) o | -0.33 (n=116,t=-3.7) o | -0.33 (n=115,t=-3.7) o | -0.25 (n=112,t=-2.7) o | -0.32 (n=106,t=-3.4) o |

`o` = |t| >= 1.96 (~95% two-sided). Negative IC = signal high -> forward return low (the desired direction for a top/de-risk trigger).

## Bucketed forward returns — triggered vs not (3-month forward)

### BTC
| trigger | months on | mean fwd 3m | median fwd 3m | months off | mean fwd 3m (off) |
|---|--:|--:|--:|--:|--:|
| MVRV pctl>=0.85 | 8 | +33.8% | +29.7% | 168 | +36.2% |
| macro risk-off | 59 | +12.5% | +0.8% | 117 | +48.0% |
| composite (OR) | 67 | +15.0% | +3.1% | 109 | +49.0% |
| both (AND) | 0 | +nan% | +nan% | 176 | +36.1% |

### ETH
| trigger | months on | mean fwd 3m | median fwd 3m | months off | mean fwd 3m (off) |
|---|--:|--:|--:|--:|--:|
| MVRV pctl>=0.85 | 2 | +109.5% | +109.5% | 113 | +43.2% |
| macro risk-off | 40 | -4.3% | -13.7% | 75 | +70.3% |
| composite (OR) | 42 | +1.2% | -10.5% | 73 | +69.2% |
| both (AND) | 0 | +nan% | +nan% | 115 | +44.4% |

## Read
MVRV-percentile IC vs macro-signal IC head-to-head over the identical window per asset; composite bucket table shows whether OR-ing on-chain into the macro trigger changes the forward-return profile of triggered months (more/less negative than macro alone), i.e. whether on-chain adds anything beyond what net-liq/Global-M2 already catch.
