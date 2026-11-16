# Comprehensive macro→crypto time-shift search

Question: is there ANY lag at which liquidity LEADS crypto? (12w was just an example.)

## 1. WEEKLY net-liquidity lead/lag — net-liq YoY(t) vs asset YoY(t+k), k in weeks

k>0 ⇒ net-liq leads. Full profile shown every 4w; `best k≥0` = argmax over 0..52w.

| asset | -8w | 0w | 4w | 8w | 12w | 16w | 20w | 26w | 39w | 52w | best k≥0 | ρ@best [CI] |
|---|--:|--:|--:|--:|--:|--:|--:|--:|--:|--:|--:|--|
| BTC | +0.55 | +0.55 | +0.53 | +0.51 | +0.47 | +0.42 | +0.37 | +0.29 | +0.12 | -0.06 | 0w | +0.55 [+0.26,+0.78] ✓ |
| ETH | +0.53 | +0.61 | +0.65 | +0.66 | +0.64 | +0.61 | +0.55 | +0.45 | +0.18 | -0.02 | 8w | +0.66 [+0.31,+0.85] ✓ |
| SOL | +0.59 | +0.77 | +0.84 | +0.88 | +0.89 | +0.87 | +0.84 | +0.77 | +0.53 | +0.35 | 11w | +0.89 [+0.69,+0.94] ✓ |

(Peak at/near 0w ⇒ contemporaneous, no lead. Peak at k>0 with CI✓ ⇒ genuine lead.)

## 2. WEEKLY predictive — net-liq momentum(t) → asset forward return, Spearman

Momentum = N-week % change of net liquidity. Forward = return over next h weeks.


**Net-liq 13w momentum →**

| asset | fwd 4w | fwd 13w [CI] | fwd 26w | fwd 52w |
|---|--:|--|--:|--:|
| BTC | +0.09 | +0.16 [-0.04,+0.35] | +0.10 | +0.05 |
| ETH | +0.18 | +0.32 [+0.10,+0.48] ✓ | +0.29 | +0.19 |
| SOL | +0.24 | +0.49 [+0.20,+0.64] ✓ | +0.48 | +0.35 |

**Net-liq 26w momentum →**

| asset | fwd 4w | fwd 13w [CI] | fwd 26w | fwd 52w |
|---|--:|--|--:|--:|
| BTC | +0.03 | +0.12 [-0.09,+0.31] | +0.06 | -0.02 |
| ETH | +0.20 | +0.34 [+0.15,+0.50] ✓ | +0.29 | +0.11 |
| SOL | +0.27 | +0.46 [+0.14,+0.63] ✓ | +0.52 | +0.39 |

## 3. EXTENDED MONTHLY lead/lag — driver YoY(t) vs asset YoY(t+k), k = -6..+18 MONTHS

Confirms no LONGER-horizon lead hides. `best k≥0` = argmax over 0..18m.


**Global M2 5-ctry YoY%**

| asset | 0m | 3m | 6m | 9m | 12m | 15m | 18m | best k≥0 | ρ@best [CI] |
|---|--:|--:|--:|--:|--:|--:|--:|--:|--|
| BTC | +0.55 | +0.46 | +0.22 | -0.00 | -0.11 | -0.15 | -0.09 | 0m | +0.55 [+0.21,+0.78] ✓ |
| ETH | +0.58 | +0.54 | +0.31 | +0.10 | -0.04 | -0.20 | -0.31 | 1m | +0.58 [+0.17,+0.80] ✓ |
| SOL | +0.48 | +0.55 | +0.46 | +0.35 | +0.18 | -0.12 | -0.33 | 3m | +0.55 [-0.18,+0.87] |

**Global M2 G3 YoY%**

| asset | 0m | 3m | 6m | 9m | 12m | 15m | 18m | best k≥0 | ρ@best [CI] |
|---|--:|--:|--:|--:|--:|--:|--:|--:|--|
| BTC | +0.43 | +0.38 | +0.19 | +0.00 | -0.09 | -0.10 | -0.03 | 0m | +0.43 [+0.03,+0.73] ✓ |
| ETH | +0.54 | +0.59 | +0.46 | +0.28 | +0.13 | -0.03 | -0.19 | 2m | +0.59 [+0.25,+0.80] ✓ |
| SOL | +0.45 | +0.54 | +0.49 | +0.38 | +0.23 | -0.02 | -0.22 | 4m | +0.55 [-0.21,+0.85] |

**US M2 YoY%**

| asset | 0m | 3m | 6m | 9m | 12m | 15m | 18m | best k≥0 | ρ@best [CI] |
|---|--:|--:|--:|--:|--:|--:|--:|--:|--|
| BTC | +0.35 | +0.30 | +0.21 | +0.15 | +0.12 | +0.08 | +0.01 | 0m | +0.35 [+0.04,+0.57] ✓ |
| ETH | +0.47 | +0.47 | +0.41 | +0.36 | +0.29 | +0.20 | +0.10 | 1m | +0.48 [+0.07,+0.69] ✓ |
| SOL | +0.51 | +0.32 | +0.15 | +0.07 | +0.03 | -0.04 | -0.13 | 0m | +0.51 [-0.27,+0.88] |

**Fed net liq YoY%**

| asset | 0m | 3m | 6m | 9m | 12m | 15m | 18m | best k≥0 | ρ@best [CI] |
|---|--:|--:|--:|--:|--:|--:|--:|--:|--|
| BTC | +0.56 | +0.46 | +0.28 | +0.10 | -0.08 | -0.21 | -0.25 | 0m | +0.56 [+0.26,+0.78] ✓ |
| ETH | +0.62 | +0.63 | +0.41 | +0.15 | -0.07 | -0.28 | -0.34 | 1m | +0.66 [+0.27,+0.87] ✓ |
| SOL | +0.78 | +0.89 | +0.75 | +0.51 | +0.33 | +0.04 | -0.23 | 3m | +0.89 [+0.68,+0.94] ✓ |

