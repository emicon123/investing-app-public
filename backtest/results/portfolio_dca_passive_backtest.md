# Passive portfolio DCA backtest (1000 PLN start + 1000 PLN/month)

Window 2018-01 → 2026-07 (103 months), PLN (USD via USDPLN). Total contributed 103,000 PLN. No macro timing.

| metric | PASSIVE (no selling, drift) | REBALANCED (monthly→target) |
|---|--:|--:|
| Final value | 571,745 PLN | 664,951 PLN |
| Profit | 468,745 (+455%) | 561,951 (+546%) |
| **XIRR (money-weighted)** | **38.4%/yr** | **41.8%/yr** |
| Time-weighted CAGR | 34.6%/yr | 36.7%/yr |
| Max drawdown | -82% | -53% |

**Final allocation vs target (shows the drift):**
| asset | target% | PASSIVE% | REBAL% |
|---|--:|--:|--:|
| GLD | 20.0 | 10.9 | 43.7 |
| QQQ | 22.5 | 13.4 | 46.5 |
| FXI | 2.5 | 0.8 | 0.0 |
| BOND_PL | 0.0 | 0.0 | 0.0 |
| BOND_DE | 0.0 | 0.0 | 0.0 |
| BTC_spot | 20.0 | 14.2 | 0.0 |
| miners | 5.0 | 22.4 | 9.9 |
| ETH | 10.0 | 7.1 | 0.0 |
| SOL | 5.0 | 25.8 | 0.0 |
| COIN | 3.0 | 0.9 | 0.0 |
| SUI | 3.0 | 0.3 | 0.0 |
| DOGE | 1.0 | 3.2 | 0.0 |
| LINK | 1.5 | 0.9 | 0.0 |

Note: miners = equal-weight monthly basket of high-vol names → return likely overstated (rebalancing/volatility-pumping artifact); treat the miners line as an upper bound.
