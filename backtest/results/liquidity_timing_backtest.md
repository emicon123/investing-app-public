# Liquidity-timing backtest — is the net-liq lead tradeable?

Long asset when net-liq [4, 8, 13, 26, 52]-week %change > 0 else T-bill cash. Weekly, signal lagged 1wk, cost 10bps/switch. ✦=beats buy&hold Sharpe.

## Part 1 — full-sample window scan (descriptive; choosing best L here = in-sample)


**BTC** — buy&hold: CAGR 134% · Sharpe 1.23 · maxDD -92% · n=832w

| net-liq L | CAGR% | Sharpe | maxDD% | time-in% | trades |
|--:|--:|--:|--:|--:|--:|
| 4w | 126 | 1.23 | -72 | 57 | 203 |
| 8w | 119 | 1.20 | -78 | 56 | 159 |
| 13w | 93 | 1.07 | -92 | 56 | 97 |
| 26w | 95 | 1.07 | -92 | 56 | 61 |
| 52w | 113 | 1.15 | -92 | 58 | 53 |

**ETH** — buy&hold: CAGR 93% · Sharpe 1.05 · maxDD -93% · n=568w

| net-liq L | CAGR% | Sharpe | maxDD% | time-in% | trades |
|--:|--:|--:|--:|--:|--:|
| 4w | 115 | 1.20 ✦ | -81 | 52 | 137 |
| 8w | 64 | 0.92 | -78 | 48 | 108 |
| 13w | 90 | 1.05 ✦ | -62 | 47 | 71 |
| 26w | 90 | 1.05 ✦ | -56 | 46 | 35 |
| 52w | 62 | 0.95 | -62 | 42 | 46 |

**SOL** — buy&hold: CAGR 114% · Sharpe 1.14 · maxDD -96% · n=323w

| net-liq L | CAGR% | Sharpe | maxDD% | time-in% | trades |
|--:|--:|--:|--:|--:|--:|
| 4w | 112 | 1.15 ✦ | -69 | 53 | 78 |
| 8w | 105 | 1.10 | -74 | 53 | 60 |
| 13w | 156 | 1.30 ✦ | -66 | 51 | 42 |
| 26w | 174 | 1.31 ✦ | -67 | 53 | 18 |
| 52w | 207 | 1.40 ✦ | -72 | 51 | 29 |

## Part 2 — walk-forward OOS (pick best-Sharpe L on the past, trade next 13w; never peeks ahead)

| asset | OOS CAGR% | OOS Sharpe | OOS maxDD% | B&H CAGR% | B&H Sharpe | B&H maxDD% | OOS from |
|---|--:|--:|--:|--:|--:|--:|--|
| BTC | 54 | 1.00 | -67 | 65 | 1.03 | -85 | 2013-07-24 |
| ETH | 36 | 0.81 | -76 | 22 | 0.65 | -77 | 2018-08-15 |
| SOL | -11 | 0.12 | -72 | 42 | 0.82 | -75 | 2023-04-19 |

