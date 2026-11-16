# Study 15 -- trim-rule (+100%) frequency & depth audit

Single continuous pass 2007-02..2026-09 (19.6y, no overlapping windows) for the fire-count/frequency analysis; the standard 5 start-date windows (1/3/5/8/13y, matching study 14) for XIRR/maxDD comparability. Buy-only DCA 1000+1000/mo PLN, 15bps.

## 1. How often does the CURRENT rule (1.5x trigger / trim to 1.0x, ADR-048 live rule) actually fire?

Continuous pass, 2007-02..2026-09 (19.6 years), non-overlapping. PRIOR (2.0x/1.5x, pre-ADR-048) shown alongside for comparison -- it is NOT the live rule, kept as a reference point only.

### CURRENT (1.5x trigger / 1.0x trim-to -- ADR-048 live rule)

#### BASELINE (fixed target, no macro): 41 total fires

| asset | fires | % of total | avg months between fires | implied fires/yr (while held) | years present |
|---|--:|--:|--:|--:|--:|
| BTC_spot | 13 | 32% | 10 | 0.82 | 15.9 |
| QQQ | 13 | 32% | 3 | 0.67 | 19.5 |
| FXI | 12 | 29% | 3 | 0.62 | 19.4 |
| ETH | 3 | 7% | 25 | 0.28 | 10.8 |

#### CURRENT (ADR-044 blend, live mechanism): 48 total fires

| asset | fires | % of total | avg months between fires | implied fires/yr (while held) | years present |
|---|--:|--:|--:|--:|--:|
| BTC_spot | 13 | 27% | 10 | 0.82 | 15.9 |
| QQQ | 13 | 27% | 3 | 0.67 | 19.5 |
| FXI | 12 | 25% | 3 | 0.62 | 19.4 |
| ETH | 5 | 10% | 14 | 0.46 | 10.8 |
| SOL | 2 | 4% | 3 | 0.32 | 6.2 |
| DOGE | 1 | 2% | n/a (fired once) | 0.12 | 8.6 |
| LINK | 1 | 2% | n/a (fired once) | 0.12 | 8.6 |
| miners | 1 | 2% | n/a (fired once) | 0.07 | 14.2 |

### PRIOR (2.0x / 1.5x -- pre-ADR-048, reference only, NOT current)

#### BASELINE (fixed target, no macro): 37 total fires

| asset | fires | % of total | avg months between fires | implied fires/yr (while held) | years present |
|---|--:|--:|--:|--:|--:|
| BTC_spot | 13 | 35% | 10 | 0.82 | 15.9 |
| QQQ | 12 | 32% | 3 | 0.62 | 19.5 |
| FXI | 11 | 30% | 3 | 0.57 | 19.4 |
| ETH | 1 | 3% | n/a (fired once) | 0.09 | 10.8 |

#### CURRENT (ADR-044 blend, live mechanism): 41 total fires

| asset | fires | % of total | avg months between fires | implied fires/yr (while held) | years present |
|---|--:|--:|--:|--:|--:|
| BTC_spot | 14 | 34% | 9 | 0.88 | 15.9 |
| QQQ | 12 | 29% | 3 | 0.62 | 19.5 |
| FXI | 11 | 27% | 3 | 0.57 | 19.4 |
| ETH | 3 | 7% | 20 | 0.28 | 10.8 |
| DOGE | 1 | 2% | n/a (fired once) | 0.12 | 8.6 |

**BASELINE vs CURRENT, at the CURRENT rule (1.5/1.0):** 41 fires vs 48 fires over the same 19.6-year continuous window (+7 fires under CURRENT-mechanism).

**Same comparison at the PRIOR rule (2.0/1.5), for reference:** 37 fires vs 41 fires (+4).

## 2. Overshoot-at-fire distribution (actual/target ratio at the moment of firing)

Monthly granularity means a position can already be well past the trigger by the time it's checked (it isn't checked continuously). Ratio = actual weight / that month's target weight, at the fire event, CURRENT rule = 1.5x trigger:

| mode | n fires | min | p25 | median | p75 | max | % fires with ratio >= 1.88x |
|---|--:|--:|--:|--:|--:|--:|--:|
| BASELINE (fixed target, no macro) | 41 | 1.50 | 1.56 | 1.70 | 1.93 | 2.95 | 32% |
| CURRENT (ADR-044 blend, live mechanism) | 48 | 1.50 | 1.60 | 1.79 | 2.03 | 9.01 | 40% |

Largest single overshoot (CURRENT mode, 1.5x rule): **DOGE**, 2021-05, ratio 9.01x (target 0.10%, actual 0.9%) -- trimmed 89% of the position that month.

## 3. Regime clustering -- how much of CURRENT's extra firing is target-contraction-driven?

Sustained MID_OFF/RISK_OFF episodes (blend<=0.4 in the live 0=risk-off/1=risk-on orientation -- ADR-071; the same episode set study 14 identified under the old 0=risk-on/1=risk-off convention as blend_old>=0.6), 3+ consecutive months:

| start | end | months |
|---|---|--:|
| 2011-12 | 2012-02 | 3 |
| 2012-05 | 2012-10 | 6 |
| 2014-10 | 2015-07 | 10 |
| 2018-08 | 2019-02 | 7 |
| 2021-05 | 2021-09 | 5 |
| 2021-11 | 2023-02 | 16 |
| 2024-04 | 2024-07 | 4 |
| 2025-02 | 2025-04 | 3 |
| 2025-11 | 2026-01 | 3 |

These episodes cover 57 of 236 months in the full window (24% of history).

**CURRENT**: 9/48 fires (19%) fell inside a sustained MID_OFF/RISK_OFF episode; 39 (81%) fired in ordinary RISK_ON/MID_ON/NEUTRAL months.
**BASELINE**: 1/41 fires (2%) fell inside a sustained MID_OFF/RISK_OFF episode; 40 (98%) fired in ordinary RISK_ON/MID_ON/NEUTRAL months.

## 4. Whipsaw check -- does the CURRENT 1.5x/1.0x rule cause more quick re-fires than the PRIOR rule?

A "quick re-fire" = the same asset fires again within <=6 months of its previous fire (each re-fire costs another round of 15bps trading cost on top of the return effect).

| config | mode | total fires | consecutive same-asset gaps | quick re-fires (<=6mo) | quick re-fire rate |
|---|---|--:|--:|--:|--:|
| PRIOR (2.0x / 1.5x -- pre-ADR-048, reference only, NOT current) | BASELINE | 37 | 33 | 26 | 79% |
| PRIOR (2.0x / 1.5x -- pre-ADR-048, reference only, NOT current) | CURRENT | 41 | 36 | 26 | 72% |
| CURRENT (1.5x trigger / 1.0x trim-to -- ADR-048 live rule) | BASELINE | 41 | 37 | 28 | 76% |
| CURRENT (1.5x trigger / 1.0x trim-to -- ADR-048 live rule) | CURRENT | 48 | 40 | 30 | 75% |

## 5. Sensitivity grid -- fire-frequency vs. return/drawdown tradeoff

Continuous-pass fire counts (CURRENT mode, the live macro-blend mechanism -- not to be confused with the CURRENT trim-rule config, i.e. the live 1.5x/1.0x pair) and 5-window headline results (both modes) at three trim-rule configs spanning CURRENT (1.5x/1.0x, live) through PRIOR (2.0x/1.5x, pre-ADR-048).

### Fire counts, continuous pass, CURRENT mode

| config | total fires | fires/yr |
|---|--:|--:|
| CURRENT (1.5x trigger / 1.0x trim-to -- ADR-048 live rule) | 48 | 2.45 |
| 1.75 / 1.25 (sensitivity midpoint, between CURRENT and PRIOR) | 45 | 2.30 |
| PRIOR (2.0x / 1.5x -- pre-ADR-048, reference only, NOT current) | 41 | 2.09 |

### 5-window XIRR%/yr (maxDD%) by config

| config | mode | 13y | 8y | 5y | 3y | 1y |
|---|---|--:|--:|--:|--:|--:|
| CURRENT (1.5x trigger / 1.0x trim-to -- ADR-048 live rule) | BASELINE | 37 (-31) | 37 (-32) | 22 (-53) | 14 (-21) | -34 (-33) |
| CURRENT (1.5x trigger / 1.0x trim-to -- ADR-048 live rule) | CURRENT | 35 (-14) | 36 (-12) | 27 (-25) | 20 (-10) | -27 (-20) |
| 1.75 / 1.25 (sensitivity midpoint, between CURRENT and PRIOR) | BASELINE | 40 (-33) | 40 (-36) | 21 (-53) | 15 (-22) | -34 (-33) |
| 1.75 / 1.25 (sensitivity midpoint, between CURRENT and PRIOR) | CURRENT | 37 (-25) | 39 (-14) | 26 (-25) | 19 (-11) | -27 (-21) |
| PRIOR (2.0x / 1.5x -- pre-ADR-048, reference only, NOT current) | BASELINE | 43 (-40) | 41 (-39) | 23 (-54) | 15 (-22) | -34 (-33) |
| PRIOR (2.0x / 1.5x -- pre-ADR-048, reference only, NOT current) | CURRENT | 40 (-26) | 39 (-14) | 26 (-25) | 18 (-12) | -28 (-22) |

Start months: 13y=2013-09, 8y=2018-09, 5y=2021-09, 3y=2023-09, 1y=2025-09. Cell = XIRR%/yr (maxDD%).

Caveats: miners basket return overstated (survivorship -- MARA/CIFR/IREN/WULF/HUT each began trading at different points); gold rallied through most of this sample (flatters any variant that trims into gold regardless of trigger quality); newer assets (SUI since 2023-05, COIN since 2021-04, SOL since 2020-04) have thin history so their per-asset fire-frequency stats above are based on a much shorter window than GLD/QQQ/BTC -- their 'years present' column should be read alongside the fire count, not in isolation; overlapping windows / one shared underlying price/macro history in section 5's 5-window table; GLD/QQQ/FXI are long-history proxies for the real Gold/NQSE.DE/ICGA.DE positions (portfolio_spec.py's SIMPLIFIED_* tables, live ADR-067 weights), not the literal tickers -- see AGENTS.md's standing FXI-for-ICGA.DE convention; the 19-position Experiments sleeve (2.5% flat, unaffected by blend/trim either way) is omitted and its weight implicitly redistributed pro-rata by the existing present-renormalization step.
