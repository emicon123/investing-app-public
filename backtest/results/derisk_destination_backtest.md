# De-risk destination test — GOLD vs BOND ETFs (ETFBCASH.WA + ZPR1.DE)

Real algo (buy-only Q_pos catch-up DCA + CURRENT 1.5x-trigger/1.0x-trim-to rule, ADR-048), macro dynamic-target cut x0.33 (07's recommendation). Only the TRIMMED-PROCEEDS destination varies. Both bond ETFs are recent listings (ETFBCASH.WA from 2021-06, ZPR1.DE from 2023-11) — windows/months before a ticker existed fall back to gold for that leg (mirrors production: can't route to a ticker before it has price history).

| destination | 13y | 8y | 5y | 3y | 1y |
|---|--:|--:|--:|--:|--:|
| GOLD (shipped, ADR-042) | 33 (-19) | 38 (-13) | 29 (-21) | 27 (-11) | -27 (-21) |
| BONDS (ETFBCASH+ZPR1, gold fallback) | 32 (-19) | 36 (-14) | 23 (-24) | 20 (-9) | -27 (-21) |
| MIX (50% gold / 50% bonds) | 32 (-19) | 37 (-13) | 26 (-23) | 24 (-7) | -27 (-21) |

Start months: 13y=2013-09, 8y=2018-09, 5y=2021-09, 3y=2023-09, 1y=2025-09. Cell = XIRR%/yr (maxDD%).

## Final allocation drift into destination (all windows — every simulation runs through to the present, so even a 13y/8y window can route POST-2021/2023 trims into the bond ETFs once they existed; only trims before a ticker's inception fall back to gold)
| destination | window | GLD% | BOND_PL(ETFBCASH)% | BOND_DE(ZPR1)% |
|---|---|--:|--:|--:|
| GOLD | 13y | 78.4 | 0.0 | 0.0 |
| GOLD | 8y | 73.0 | 0.0 | 0.0 |
| GOLD | 5y | 62.8 | 0.0 | 0.0 |
| GOLD | 3y | 57.1 | 0.0 | 0.0 |
| GOLD | 1y | 25.9 | 0.0 | 0.0 |
| BONDS | 13y | 72.6 | 3.9 | 0.9 |
| BONDS | 8y | 58.7 | 7.9 | 4.7 |
| BONDS | 5y | 23.7 | 22.2 | 18.7 |
| BONDS | 3y | 24.8 | 18.7 | 16.9 |
| BONDS | 1y | 25.3 | 0.9 | 0.9 |
| MIX | 13y | 75.6 | 1.9 | 0.4 |
| MIX | 8y | 66.1 | 3.8 | 2.2 |
| MIX | 5y | 45.9 | 10.5 | 8.6 |
| MIX | 3y | 41.9 | 9.1 | 8.1 |
| MIX | 1y | 25.6 | 0.4 | 0.5 |

## Read
ETFBCASH.WA is NOT a cash-equivalent — it fell -23% through the 2022 rate-hike year (real duration risk on PLN treasuries). ZPR1.DE (1-3mo T-bills) is genuinely near-cash (~4-5%/yr, near-zero drawdown). **BONDS/MIX underperform pure GOLD on every window, including 13y/8y** where most of the diverted capital still went to gold pre-2021 — because gold happened to rally hard through exactly these risk-off windows (see file 07's caveat) while the bond ETFs' total return over the *same* recent stretch was far more modest (ETFBCASH: roughly flat 2021-23, +5%/yr 2024-26; ZPR1: steady ~4-5%/yr, no upside kicker). Diverting even a fraction of trimmed proceeds away from gold cost real return with only a modest or negative drawdown benefit (5y: DD gets WORSE for BONDS, -18%→-22%, because some proceeds still went into ETFBCASH which itself has duration-driven drawdown risk, unlike gold which was in a multi-year uptrend).

## Caveats
Short bond-ETF history (2.6-5y) means this is NOT a full-cycle test of the bond leg — no rate-hike-cycle stress test for ZPR1 (T-bill yield could go to ~0% in a future cutting cycle) and only ONE rate-hike drawdown observed for ETFBCASH (2022). Gold rose through the same risk-off windows in file 07 — that tailwind is specific to gold and won't repeat identically for bonds.
