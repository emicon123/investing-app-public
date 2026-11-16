# Study 17 -- "Global 20": 100%-individual-stock global equity portfolio

Buy-only DCA 1000+1000/mo PLN, 15bps. Equal-weight (5% each) 20-stock global portfolio: 12 USA / 4 Europe / 4 Rest-of-World. Three active variant families (BUYHOLD real catch-up DCA, FIXED annual rebalance, TRIM threshold trigger at 3 configs); BLEND is NOT implemented as an active overlay here (see script header/write-up for rationale) -- its regime occupancy is shown as context only. Latest common month: 2026-09. Cell = XIRR%/yr (maxDD%).

## Portfolio (equal weight, 5% each)

| ticker | company | region | weight % |
|---|---|---|--:|
| AAPL | Apple | USA | 5.00 |
| MSFT | Microsoft | USA | 5.00 |
| AMZN | Amazon | USA | 5.00 |
| GOOGL | Alphabet (Google) | USA | 5.00 |
| NVDA | Nvidia | USA | 5.00 |
| JPM | JPMorgan Chase | USA | 5.00 |
| JNJ | Johnson & Johnson | USA | 5.00 |
| WMT | Walmart | USA | 5.00 |
| TSLA | Tesla | USA | 5.00 |
| AVGO | Broadcom | USA | 5.00 |
| KO | Coca-Cola | USA | 5.00 |
| V | Visa | USA | 5.00 |
| ASML | ASML (Netherlands) | Europe | 5.00 |
| NVO | Novo Nordisk (Denmark) | Europe | 5.00 |
| SAP | SAP (Germany, US ADR) | Europe | 5.00 |
| UL | Unilever (UK/Netherlands, US ADR) | Europe | 5.00 |
| TSM | Taiwan Semiconductor (US ADR) | Rest of World | 5.00 |
| TM | Toyota (Japan, US ADR) | Rest of World | 5.00 |
| BABA | Alibaba (China, US ADR) | Rest of World | 5.00 |
| SAMSUNG | Samsung Electronics (Korea, 005930.KS via KRW->USD->PLN) | Rest of World | 5.00 |

## 13-year-history requirement — confirmed for every ticker

Window start dates: 13y=2013-09, 8y=2018-09, 5y=2021-09, 3y=2023-09, 1y=2025-09 (13y window starts 2013-09).

| ticker | true first-available date | clears 13y-window start (2013-06)? | margin |
|---|---|---|--:|
| AAPL | 1990-01-02 | YES | 23.7y |
| MSFT | 1990-01-02 | YES | 23.7y |
| AMZN | 1997-05-15 | YES | 16.3y |
| GOOGL | 2004-08-19 | YES | 9.1y |
| NVDA | 1999-01-22 | YES | 14.7y |
| JPM | 1990-01-02 | YES | 23.7y |
| JNJ | 1990-01-02 | YES | 23.7y |
| WMT | 1990-01-02 | YES | 23.7y |
| TSLA | 2010-06-29 | YES | 3.2y |
| AVGO | 2009-08-06 | YES | 4.1y |
| KO | 1990-01-02 | YES | 23.7y |
| V | 2008-03-19 | YES | 5.5y |
| ASML | 1995-03-15 | YES | 18.5y |
| NVO | 1990-01-02 | YES | 23.7y |
| SAP | 1995-09-18 | YES | 18.0y |
| UL | 1990-01-02 | YES | 23.7y |
| TSM | 1997-10-09 | YES | 15.9y |
| TM | 1990-01-02 | YES | 23.7y |
| BABA | 2014-09-19 | **NO** | MISSING until 2014-09-19 (1.0y after window start) |
| SAMSUNG | 2000-01-04 | YES | 13.7y |

**Only BABA (Alibaba) fails the 13y-history requirement** -- IPO'd 2014-09-19, after the 13y window's 2013-06 start. It is naturally absent (redistributed pro-rata among the other present stocks via the same `present`-list mechanism used throughout this folder for not-yet-existing assets) from 2013-06 through 2014-09 inside the 13y window only; it is present for the ENTIRE 8y/5y/3y/1y windows (all start after its IPO). **Samsung clears 13y comfortably, contrary to the a-priori concern in the task brief** -- its data (005930.KS) starts 2007-01 in this dataset, well before the 2013-06 window start.

## Headline results — cell = XIRR%/yr (maxDD%)

| variant | 13y | 8y | 5y | 3y | 1y |
|---|--:|--:|--:|--:|--:|
| BUYHOLD | 31 (-34) | 26 (-23) | 23 (-15) | 22 (-12) | -7 (-14) |
| FIXED | 27 (-15) | 26 (-15) | 23 (-15) | 23 (-13) | -6 (-15) |
| TRIM CURRENT (1.5x trigger / 1.0x trim-to -- ADR-048 live rule) | 25 (-16) | 24 (-15) | 23 (-15) | 21 (-12) | -7 (-14) |
| TRIM PRIOR (2.0x / 1.5x -- pre-ADR-048, reference only, NOT current) | 26 (-18) | 25 (-17) | 23 (-15) | 21 (-12) | -7 (-14) |
| TRIM REFERENCE (2.0x / 1.0x -- AGENTS.md standing 3rd variant) | 26 (-18) | 25 (-16) | 23 (-15) | 21 (-12) | -7 (-14) |

Start months: 13y=2013-09, 8y=2018-09, 5y=2021-09, 3y=2023-09, 1y=2025-09. Cell = XIRR%/yr (maxDD%).

## BLEND: not implemented as an overlay -- regime context only

This is a 100%-equity, no-bonds/gold/cash book: there is no internal DEFENSE bucket for the ADR-044 risk-on/off mechanism to rotate weight into without inventing one arbitrarily (see script header for the full rationale). Instead, here is the SAME underlying blend regime's month occupancy per window, purely as context for how "risk-off" the macro backdrop was during each window -- **not applied to any weight in this study**.

| window | RISK_OFF | MID_OFF | NEUTRAL | MID_ON | RISK_ON |
|---|--:|--:|--:|--:|--:|
| 13y | 25% | 6% | 12% | 8% | 49% |
| 8y | 33% | 6% | 7% | 3% | 51% |
| 5y | 37% | 8% | 5% | 2% | 47% |
| 3y | 20% | 9% | 9% | 3% | 60% |
| 1y | 18% | 9% | 9% | 9% | 55% |

## Trim-rule fire-frequency (single continuous pass, study #15's method)

One non-overlapping pass, 2010-07..2026-09 (16.2y).

| config | total fires | fired stocks (count each) |
|---|--:|---|
| CURRENT (1.5x trigger / 1.0x trim-to -- ADR-048 live rule) | 34 | NVDA (8), AVGO (6), TSLA (6), TSM (3), AMZN (2), ASML (2), NVO (2), AAPL (1), GOOGL (1), MSFT (1), SAMSUNG (1), WMT (1) |
| PRIOR (2.0x / 1.5x -- pre-ADR-048, reference only, NOT current) | 21 | NVDA (8), AVGO (5), TSLA (5), AMZN (1), ASML (1), TSM (1) |
| REFERENCE (2.0x / 1.0x -- AGENTS.md standing 3rd variant) | 15 | NVDA (5), AVGO (3), TSLA (3), AMZN (1), ASML (1), SAMSUNG (1), TSM (1) |

## Reference rows: classic portfolios (study #16) and the user's own book (study #14)

Cited, not recomputed here -- same END/DCA/cost/PLN conventions.

| portfolio | variant | 13y | 8y | 5y | 3y | 1y |
|---|---|--:|--:|--:|--:|--:|
| 60/40 (study 16) | FIXED | 10 (-12) | 9 (-12) | 8 (-12) | 10 (-12) | 15 (-3) |
| Permanent Portfolio (study 16) | FIXED | 7 (-11) | 7 (-11) | 7 (-11) | 9 (-4) | 6 (-4) |
| Golden Butterfly (study 16) | FIXED | 8 (-15) | 8 (-15) | 8 (-15) | 11 (-9) | 13 (-1) |
| Ray Dalio All Weather (study 16) | FIXED | 6 (-20) | 5 (-20) | 3 (-20) | 6 (-9) | 9 (-2) |
| User's own book (study 14, CURRENT/ADR-044, older fixed-split DCA approx.) | -- | 34 (-24) | 33 (-20) | 21 (-28) | 24 (-15) | -3 (-11) |

Caveats: overlapping windows share one underlying price history (not independent draws); 1y/3y windows are single-episode reads; equal-weight (5% each) is a simplifying convention, not how any real index or actual investor would size 20 individual stocks (a market-cap-weighted version would look very different, dominated by AAPL/MSFT/NVDA/AMZN/GOOGL); BABA is absent for part of the 13y window only (see above); Samsung's return series involves a two-hop FX chain (KRW->USD->PLN via USDKRW_X and USDPLN_X) not used anywhere else in this folder, a small additional source of FX noise/slippage vs. single-hop USD->PLN tickers; BLEND is NOT an active variant in this study (equity-only book has no internal defensive bucket -- see rationale above), shown as context only; the 60/40/Permanent/Golden Butterfly/All Weather/study-14 reference rows are cited from studies 16/14, not recomputed in this script; study 14's row still uses the older fixed-split DCA approximation, not the real catch-up mechanic used throughout this study (see backtest/AGENTS.md 'Rebalancing rule').
