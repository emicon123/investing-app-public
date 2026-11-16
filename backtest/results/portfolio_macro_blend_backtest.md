# Study 14 — macro-blend portfolio backtest: BASELINE vs CURRENT (ADR-044) vs REVERSED

Buy-only DCA 1000+1000/mo PLN, 15bps, 2x-trim-to-1.5x-target->gold kept in all three variants (per user: still applies -- see header note in the script). Latest month: 2026-06. Each cell = XIRR%/yr (maxDD%).

## Risk-on / risk-off target tables

| asset | risk-on % | risk-off % | retention |
|---|--:|--:|--:|
| GLD | 30.000 | 30.000 | 100% |
| CASH | 10.000 | 30.625 | 306% |
| QQQ | 5.000 | 15.000 | 300% |
| FXI | 2.500 | 7.500 | 300% |
| MSTR | 2.500 | 2.500 | 100% |
| BTC_spot | 17.500 | 9.625 | 55% |
| miners | 7.500 | 2.250 | 30% |
| ETH | 13.000 | 1.300 | 10% |
| SOL | 5.000 | 0.500 | 10% |
| COIN | 4.000 | 0.400 | 10% |
| SUI | 2.000 | 0.200 | 10% |
| DOGE | 1.000 | 0.100 | 10% |

## Headline results

| variant | 13y | 8y | 5y | 3y | 1y |
|---|--:|--:|--:|--:|--:|
| BASELINE (fixed target, no macro) | 43 (-39) | 43 (-40) | 26 (-52) | 25 (-22) | -16 (-22) |
| CURRENT (ADR-044 blend, momentum framing) | 38 (-22) | 37 (-22) | 24 (-30) | 22 (-15) | -7 (-14) |
| REVERSED (1-blend, contrarian framing) | 29 (-33) | 26 (-36) | 20 (-46) | 19 (-12) | -10 (-14) |

Start months: 13y=2013-06, 8y=2018-06, 5y=2021-06, 3y=2023-06, 1y=2025-06. Cell = XIRR%/yr (maxDD%).

## Blend regime occupancy (CURRENT variant's blend; REVERSED = mirror-image zones)

Share of months in each zone, full available history (2011-07..2026-05, n=179):

| RISK_ON | MID_ON | NEUTRAL | MID_OFF | RISK_OFF |
|--:|--:|--:|--:|--:|
| 7% | 38% | 32% | 16% | 7% |

Per start-date window (CURRENT variant zone occupancy, % of months):

| window | RISK_ON | MID_ON | NEUTRAL | MID_OFF | RISK_OFF |
|---|--:|--:|--:|--:|--:|
| 13y | 7% | 38% | 31% | 15% | 8% |
| 8y | 10% | 34% | 23% | 20% | 12% |
| 5y | 5% | 33% | 22% | 23% | 17% |
| 3y | 3% | 53% | 28% | 17% | 0% |
| 1y | 8% | 58% | 17% | 17% | 0% |

## Notable sustained RISK_OFF/MID_OFF episodes (blend>=0.6, 3+ consecutive months)

| start | end | months | avg blend |
|---|---|--:|--:|
| 2012-06 | 2012-08 | 3 | 0.63 |
| 2014-11 | 2015-03 | 5 | 0.70 |
| 2018-08 | 2019-01 | 6 | 0.76 |
| 2021-05 | 2021-09 | 5 | 0.87 |
| 2021-12 | 2023-01 | 14 | 0.79 |

Caveats: miners basket return overstated (survivorship — MARA/CIFR/IREN/WULF/HUT each began trading at different points, average silently reweights among the living subset); gold rallied through most of this sample (flatters any de-risk-into-gold variant regardless of trigger quality); overlapping windows and one shared underlying price/macro history across all three variants (not independent draws); 1y/3y windows are noisy single-episode reads, not statistically robust; the risk-off table is a judgment-call adaptation of the real production V37 ratios to a simpler asset list, not itself independently re-optimized/back-tested.
