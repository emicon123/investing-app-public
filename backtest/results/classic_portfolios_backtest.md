# Study 16 -- classic textbook portfolios (reference point vs. the user's own book)

Buy-only DCA 1000+1000/mo PLN, 15bps. Four variant families per portfolio, same DCA/cost conventions throughout, differing only in rebalancing discipline: BUYHOLD (contributions routed via the REAL production deficit-proportional catch-up mechanic -- O_total = current+A, P_i = target_w_i*O_total, Q_i = P_i-current_i, buy_i = A*Q_i/sum(positive Q) -- never rebalanced/sold, see backtest/AGENTS.md 'Rebalancing rule'), FIXED (contributions at fixed target-weight proportions + annual full rebalance-to-target -- see script header for the anniversary-month convention), BLEND (ADR-044 sliding risk-on/off target, reused unchanged from study 14, + annual full rebalance to that month's moving target), and TRIM (BUYHOLD's real catch-up DCA + a monthly threshold-trigger partial-sell check, general/portfolio-agnostic redistribution -- backtest/AGENTS.md 'Standing baselines' point 3 -- tested at (2.0,1.5)/(2.0,1.0)/(1.5,1.0)). Latest common month: 2026-09. Cell = XIRR%/yr (maxDD%).

## Portfolio weight tables (FIXED / named, and the BLEND variant's RISK_ON / RISK_OFF endpoints)

### 60/40

| asset | named (FIXED) % | RISK_ON % (BLEND, blend=0) | RISK_OFF % (BLEND, blend=1) |
|---|--:|--:|--:|
| SPY | 60.00 | 80.00 | 40.00 |
| AGG | 40.00 | 20.00 | 60.00 |

### Permanent Portfolio (Harry Browne)

| asset | named (FIXED) % | RISK_ON % (BLEND, blend=0) | RISK_OFF % (BLEND, blend=1) |
|---|--:|--:|--:|
| SPY | 25.00 | 33.33 | 16.67 |
| TLT | 25.00 | 22.22 | 27.78 |
| CASH | 25.00 | 22.22 | 27.78 |
| GLD | 25.00 | 22.22 | 27.78 |

### Golden Butterfly (Portfolio Charts)

| asset | named (FIXED) % | RISK_ON % (BLEND, blend=0) | RISK_OFF % (BLEND, blend=1) |
|---|--:|--:|--:|
| SPY | 20.00 | 26.67 | 13.33 |
| IWN | 20.00 | 26.67 | 13.33 |
| TLT | 20.00 | 15.56 | 24.44 |
| SHY | 20.00 | 15.56 | 24.44 |
| GLD | 20.00 | 15.56 | 24.44 |

### Ray Dalio All Weather

| asset | named (FIXED) % | RISK_ON % (BLEND, blend=0) | RISK_OFF % (BLEND, blend=1) |
|---|--:|--:|--:|
| SPY | 30.00 | 40.00 | 20.00 |
| TLT | 40.00 | 32.00 | 48.00 |
| IEF | 15.00 | 12.00 | 18.00 |
| GLD | 7.50 | 6.00 | 9.00 |
| DBC | 7.50 | 10.00 | 5.00 |

Risk-on/risk-off construction: RISK bucket (SPY equities, IWN small-cap value, DBC commodities) scaled to (1+0.333) of its named weight in RISK_ON and (1-0.333) in RISK_OFF; freed/needed weight moves to/from the DEFENSE bucket (bonds/gold/cash) proportionally to each asset's own named weight. Calibrated to an explicit worked example (60/40: risk-on 80/20 stock/bond, risk-off 40/60) and applied identically (same tilt fraction) to all 4 portfolios -- a judgment call, not independently re-optimized per portfolio (same caveat style as study 14's RISK_OFF table for the user's own book).

## Fetched-ticker inception confirmation (13y-history requirement)

Window start dates: 13y=2013-09, 8y=2018-09, 5y=2021-09, 3y=2023-09, 1y=2025-09 (13y window starts 2013-09). All assets below must clear that date by a wide margin.

| ticker | role | first available (this data file) | true Yahoo inception (confirmed) | years before 13y-window start |
|---|---|---|---|--:|
| SPY | US stock (60/40, Permanent, Golden Butterfly, All Weather) | 2007-01 | 1993-01-29 | 20.7y |
| TLT | LT treasuries (Permanent, Golden Butterfly, All Weather) | 2007-01 | 2002-07-30 | 11.2y |
| GLD | gold (Permanent, Golden Butterfly, All Weather) | 2007-01 | 2004-11-18 | 8.8y |
| AGG | US aggregate bonds (60/40) | 2007-01 | 2003-09-29 | 10.0y |
| IEF | intermediate treasuries (All Weather) | 2007-01 | 2002-07-30 | 11.2y |
| SHY | short-term treasuries (Golden Butterfly) | 2007-01 | 2002-07-30 | 11.2y |
| IWN | small-cap value (Golden Butterfly) | 2007-01 | 2000-07-28 | 13.2y |
| DBC | broad commodities (All Weather) | 2007-01 | 2006-02-06 | 7.6y |

## Headline results

| portfolio | variant | 13y | 8y | 5y | 3y | 1y |
|---|---|--:|--:|--:|--:|--:|
| 60/40 | BUYHOLD | 9 (-12) | 8 (-12) | 7 (-12) | 7 (-12) | -14 (-15) |
| 60/40 | FIXED | 9 (-12) | 8 (-12) | 7 (-12) | 7 (-12) | -14 (-15) |
| 60/40 | BLEND | 11 (-14) | 10 (-14) | 9 (-14) | 9 (-13) | -13 (-15) |
| 60/40 | TRIM CURRENT (1.5x trigger / 1.0x trim-to -- ADR-048 live rule) | 9 (-12) | 8 (-12) | 7 (-12) | 7 (-12) | -14 (-15) |
| 60/40 | TRIM 2.0 / 1.0 (AGENTS.md standing 3rd reference variant) | 9 (-12) | 8 (-12) | 7 (-12) | 7 (-12) | -14 (-15) |
| 60/40 | TRIM PRIOR (2.0x / 1.5x -- pre-ADR-048, reference only, NOT current) | 9 (-12) | 8 (-12) | 7 (-12) | 7 (-12) | -14 (-15) |
| Permanent Portfolio (Harry Browne) | BUYHOLD | 7 (-10) | 7 (-11) | 7 (-11) | 9 (-4) | 5 (-2) |
| Permanent Portfolio (Harry Browne) | FIXED | 15 (-11) | 20 (-11) | 29 (-11) | 48 (-4) | 132 (-2) |
| Permanent Portfolio (Harry Browne) | BLEND | 16 (-11) | 21 (-11) | 30 (-11) | 50 (-4) | 134 (-2) |
| Permanent Portfolio (Harry Browne) | TRIM CURRENT (1.5x trigger / 1.0x trim-to -- ADR-048 live rule) | 7 (-10) | 7 (-11) | 7 (-11) | 9 (-4) | 5 (-2) |
| Permanent Portfolio (Harry Browne) | TRIM 2.0 / 1.0 (AGENTS.md standing 3rd reference variant) | 7 (-10) | 7 (-11) | 7 (-11) | 9 (-4) | 5 (-2) |
| Permanent Portfolio (Harry Browne) | TRIM PRIOR (2.0x / 1.5x -- pre-ADR-048, reference only, NOT current) | 7 (-10) | 7 (-11) | 7 (-11) | 9 (-4) | 5 (-2) |
| Golden Butterfly (Portfolio Charts) | BUYHOLD | 8 (-15) | 8 (-14) | 7 (-14) | 8 (-9) | -16 (-15) |
| Golden Butterfly (Portfolio Charts) | FIXED | 8 (-14) | 8 (-14) | 7 (-15) | 8 (-9) | -16 (-15) |
| Golden Butterfly (Portfolio Charts) | BLEND | 9 (-16) | 8 (-16) | 7 (-16) | 9 (-11) | -16 (-15) |
| Golden Butterfly (Portfolio Charts) | TRIM CURRENT (1.5x trigger / 1.0x trim-to -- ADR-048 live rule) | 8 (-15) | 8 (-14) | 7 (-14) | 8 (-9) | -16 (-15) |
| Golden Butterfly (Portfolio Charts) | TRIM 2.0 / 1.0 (AGENTS.md standing 3rd reference variant) | 8 (-15) | 8 (-14) | 7 (-14) | 8 (-9) | -16 (-15) |
| Golden Butterfly (Portfolio Charts) | TRIM PRIOR (2.0x / 1.5x -- pre-ADR-048, reference only, NOT current) | 8 (-15) | 8 (-14) | 7 (-14) | 8 (-9) | -16 (-15) |
| Ray Dalio All Weather | BUYHOLD | 6 (-19) | 4 (-20) | 3 (-20) | 3 (-10) | -18 (-15) |
| Ray Dalio All Weather | FIXED | 6 (-20) | 4 (-20) | 3 (-20) | 3 (-9) | -18 (-15) |
| Ray Dalio All Weather | BLEND | 7 (-22) | 5 (-22) | 4 (-21) | 5 (-10) | -17 (-15) |
| Ray Dalio All Weather | TRIM CURRENT (1.5x trigger / 1.0x trim-to -- ADR-048 live rule) | 6 (-19) | 4 (-20) | 3 (-20) | 3 (-10) | -18 (-15) |
| Ray Dalio All Weather | TRIM 2.0 / 1.0 (AGENTS.md standing 3rd reference variant) | 6 (-19) | 4 (-20) | 3 (-20) | 3 (-10) | -18 (-15) |
| Ray Dalio All Weather | TRIM PRIOR (2.0x / 1.5x -- pre-ADR-048, reference only, NOT current) | 6 (-19) | 4 (-20) | 3 (-20) | 3 (-10) | -18 (-15) |

Start months: 13y=2013-09, 8y=2018-09, 5y=2021-09, 3y=2023-09, 1y=2025-09. Cell = XIRR%/yr (maxDD%). TRIM variants use BUYHOLD's real catch-up DCA plus a monthly threshold-trigger check (see script header) -- not FIXED's annual-calendar rebalance.

## Trim-rule fire-frequency (single continuous pass per portfolio, study #15's method)

For each portfolio, one non-overlapping pass from the earliest month all of its own assets have valid returns through the latest common month -- gives a clean fire count per (trigger, trim_to) config, independent of the 5 overlapping DCA windows above.

### 60/40 (2007-02..2026-09, 19.6y)

| config | total fires | fired assets (count each) |
|---|--:|---|
| CURRENT (1.5x trigger / 1.0x trim-to -- ADR-048 live rule) | 0 | (no fires) |
| 2.0 / 1.0 (AGENTS.md standing 3rd reference variant) | 0 | (no fires) |
| PRIOR (2.0x / 1.5x -- pre-ADR-048, reference only, NOT current) | 0 | (no fires) |

### Permanent Portfolio (Harry Browne) (2007-02..2026-09, 19.6y)

| config | total fires | fired assets (count each) |
|---|--:|---|
| CURRENT (1.5x trigger / 1.0x trim-to -- ADR-048 live rule) | 2 | GLD (1), SPY (1) |
| 2.0 / 1.0 (AGENTS.md standing 3rd reference variant) | 0 | (no fires) |
| PRIOR (2.0x / 1.5x -- pre-ADR-048, reference only, NOT current) | 0 | (no fires) |

### Golden Butterfly (Portfolio Charts) (2007-02..2026-09, 19.6y)

| config | total fires | fired assets (count each) |
|---|--:|---|
| CURRENT (1.5x trigger / 1.0x trim-to -- ADR-048 live rule) | 1 | GLD (1) |
| 2.0 / 1.0 (AGENTS.md standing 3rd reference variant) | 0 | (no fires) |
| PRIOR (2.0x / 1.5x -- pre-ADR-048, reference only, NOT current) | 0 | (no fires) |

### Ray Dalio All Weather (2007-02..2026-09, 19.6y)

| config | total fires | fired assets (count each) |
|---|--:|---|
| CURRENT (1.5x trigger / 1.0x trim-to -- ADR-048 live rule) | 4 | GLD (2), DBC (1), SPY (1) |
| 2.0 / 1.0 (AGENTS.md standing 3rd reference variant) | 0 | (no fires) |
| PRIOR (2.0x / 1.5x -- pre-ADR-048, reference only, NOT current) | 0 | (no fires) |


## Reference row: the user's own portfolio (NOT recomputed in this script)

Cited from study 14's CURRENT-mode row (`portfolio_macro_blend_backtest.py`, same END=2026-09, same DCA/cost/PLN conventions) -- a different rebalancing mechanic (buy-only + 2x-overshoot-trim-to-gold + ADR-044 macro blend, NOT annual rebalance-to-target) and a different, much more concentrated asset roster (50% crypto / 30% gold tilt). Not literally rerun here; cited as-published for comparison. NOTE: that row still uses the OLDER fixed-percentage-split DCA approximation, not the real catch-up mechanic this script's BUYHOLD now uses (studies 07/14/15 not corrected, per explicit user decision to fix study 16 only for now) -- not apples-to-apples with the BUYHOLD rows above.

| portfolio | 13y | 8y | 5y | 3y | 1y |
|---|--:|--:|--:|--:|--:|
| User's own book (study 14, CURRENT/ADR-044, older fixed-split DCA approx.) | 34 (-24) | 33 (-20) | 21 (-28) | 24 (-15) | -3 (-11) |

Caveats: overlapping windows share one underlying price/macro history (not independent draws); 1y/3y windows are single-episode reads; SPY/TLT/GLD/IWN/SHY/IEF/AGG/DBC data files start 2007-01 by convention (matching fetch_long_history.py) even though several have earlier true Yahoo inception -- irrelevant here since 2007-01 already predates the earliest (13y) window by 6+ years; IWM (blend small-cap, already in data/) was fetched previously but NOT used in Golden Butterfly here -- IWN (value) is used instead per the task's more-faithful ticker mapping; DBC has the shortest runway of the 5 newly-fetched tickers (inception 2006-02) but still clears the 13y window by ~7.3 years; the user's reference row uses a different mechanism and a different (far more concentrated) asset roster, so the comparison is return/drawdown profile vs. profile, not apples-to-apples on holdings; the BLEND variant's RISK_ON/RISK_OFF tables are a judgment-call tilt (TILT_FRACTION=1/3, uniform across all 4 portfolios), not independently calibrated per portfolio or re-optimized against this backtest's own results; BUYHOLD now uses the real deficit-proportional catch-up mechanic (corrected mid-task -- the original version used a naive fixed-percentage split, which materially overstated BUYHOLD's edge over FIXED; see study write-up); studies 07/14/15's own-portfolio figures still use that older fixed-split approximation and were NOT corrected (explicit user decision, study 16 only for now); TRIM's redistribution-to-others rule is a judgment-call generalization of the BLEND overlay's own DEFENSE-bucket redistribution logic, applied portfolio-agnostically (every OTHER position gets a share proportional to its own target weight) per backtest/AGENTS.md's explicit instruction not to route proceeds to one fixed safe-haven asset the way the user's own crypto book does; TRIM's fire-frequency pass uses a per-portfolio FULL_START (earliest month all of that portfolio's own assets have a valid return), so the four portfolios' continuous-pass windows are not identical in length to each other.
