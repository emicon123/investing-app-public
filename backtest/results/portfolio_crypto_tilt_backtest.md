# Dynamic crypto-tilt backtest — net-liq-acceleration 3-state vs static

Crypto sleeve only (the 50%-of-portfolio bucket). DYNAMIC tilts BTC-sleeve 50/62/75% of crypto by net-liq YoY acceleration (no-lookahead); STATIC always holds the baseline (BTC 50%) split. BTC-only / ETH-only shown as references. Monthly, 15bps turnover cost.

Sleeve backtest window: 2010-08 → 2026-06 (191 months)

| strategy | CAGR% | Sharpe | maxDD% | n |
|---|--:|--:|--:|--:|
| DYNAMIC (3-state tilt) | 154 | 1.10 | -78 | 191 |
| STATIC (baseline BTC50%) | 151 | 1.10 | -81 | 191 |
| BTC spot only | 137 | 0.91 | -82 | 191 |
| ETH only | 94 | 0.96 | -90 | 130 |

**Time in each state** (over sleeve window):
- risk_on: 34%
- neutral: 40%
- risk_off: 26%

**Dynamic − Static by calendar year (sleeve return %):**
| year | DYNAMIC% | STATIC% | Δ | BTC% |
|--:|--:|--:|--:|--:|
| 2010 | +263 | +222 | +41 | +344 |
| 2011 | +1058 | +1013 | +45 | +1471 |
| 2012 | +154 | +125 | +29 | +187 |
| 2013 | +2617 | +2617 | +0 | +5286 |
| 2014 | -39 | -28 | -11 | -56 |
| 2015 | -10 | -19 | +9 | +34 |
| 2016 | +220 | +326 | -105 | +126 |
| 2017 | +2128 | +2487 | -359 | +1337 |
| 2018 | -69 | -71 | +3 | -74 |
| 2019 | +53 | +34 | +19 | +94 |
| 2020 | +417 | +420 | -3 | +305 |
| 2021 | +405 | +472 | -67 | +60 |
| 2022 | -72 | -76 | +4 | -64 |
| 2023 | +247 | +250 | -3 | +155 |
| 2024 | +126 | +107 | +19 | +121 |
| 2025 | +13 | +4 | +9 | -6 |
| 2026 | -23 | -20 | -2 | -28 |

