# Do two macro indicators work better together?

Breadth across 28 assets (cash leg=0, conservative). ΔSharpe = median(strategy − B&H); %beat = assets where timed Sharpe > B&H. Compare pairs (AND/OR) to best single.

## Singles
| signal | medianΔSharpe | %beat |
|---|--:|--:|
| NLacc | +0.10 | 20/28 |
| NLyoy | +0.01 | 16/28 |
| GMacc | -0.03 | 13/28 |
| GMyoy | +0.03 | 18/28 |
| USacc | -0.02 | 11/28 |

## Pairs (combined signal). AND = both ON; OR = either ON.

| pair | AND ΔSharpe | AND %beat | OR ΔSharpe | OR %beat |
|---|--:|--:|--:|--:|
| NLacc+NLyoy | -0.04 | 11/28 | +0.13 | 22/28 |
| NLacc+GMacc | -0.08 | 7/28 | +0.16 | 21/28 |
| NLacc+GMyoy | +0.08 | 17/28 | +0.09 | 21/28 |
| NLacc+USacc | -0.12 | 8/28 | +0.15 | 23/28 |
| NLyoy+GMacc | -0.13 | 7/28 | +0.10 | 20/28 |
| NLyoy+GMyoy | +0.03 | 16/28 | +0.02 | 22/28 |
| NLyoy+USacc | -0.15 | 7/28 | +0.05 | 17/28 |
| GMacc+GMyoy | -0.03 | 12/28 | +0.03 | 18/28 |
| GMacc+USacc | -0.09 | 9/28 | +0.02 | 16/28 |
| GMyoy+USacc | -0.05 | 9/28 | +0.03 | 23/28 |

**Best pair:** OR NLacc+GMacc — medianΔSharpe +0.16, 21/28 beat.
(Single net-liq accel baseline ≈ +0.10, 20/28.)

