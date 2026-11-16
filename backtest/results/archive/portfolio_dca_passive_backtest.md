# Passive portfolio DCA backtest (1000 PLN start + 1000 PLN/month)

Window 2018-01 → 2026-06 (102 months), PLN (USD via USDPLN). Total contributed 102,000 PLN. No macro timing.

| metric | PASSIVE (no selling, drift) | REBALANCED (monthly→target) |
|---|--:|--:|
| Final value | 557,816 PLN | 596,509 PLN |
| Profit | 455,816 (+447%) | 494,509 (+485%) |
| **XIRR (money-weighted)** | **38.5%/yr** | **40.0%/yr** |
| Time-weighted CAGR | 33.8%/yr | 34.3%/yr |
| Max drawdown | -82% | -51% |

**Final allocation vs target (shows the drift):**
| asset | target% | PASSIVE% | REBAL% |
|---|--:|--:|--:|
| GLD | 30.0 | 14.6 | 30.4 |
| CASH | 10.0 | 2.8 | 10.8 |
| QQQ | 5.0 | 2.7 | 5.5 |
| FXI | 2.5 | 0.7 | 2.6 |
| MSTR | 2.5 | 2.3 | 2.1 |
| BTC_spot | 17.5 | 10.7 | 16.3 |
| miners | 7.5 | 29.5 | 8.3 |
| ETH | 13.0 | 8.3 | 12.1 |
| SOL | 5.0 | 24.5 | 4.9 |
| COIN | 4.0 | 1.0 | 3.9 |
| SUI | 2.0 | 0.2 | 1.9 |
| DOGE | 1.0 | 2.7 | 0.9 |

Note: miners = equal-weight monthly basket of high-vol names → return likely overstated (rebalancing/volatility-pumping artifact); treat the miners line as an upper bound.
