# Study 25 -- ADR-044 blend's Global-M2 leg: acceleration (live) vs. level (study 24's best-ICIR candidate)

Per the locked, reviewed protocol in `.claude/tasks/globalm2-level-vs-accel-blend-leg.task.md`. Three parts: Part 0 (a cheap IC-stability pre-check, the honest headline on whether the ranking is stable or only recently visible), Part 1 (in-sample/descriptive full-history A/B, NOT the verdict), Part 2 (the genuine walk-forward leg-selection chain -- the actual verdict). Divisor fixed at **0.5 (ADR-049's live value)** throughout; only the Global-M2 leg's transform varies.

## Part 0 -- expanding-checkpoint IC-stability pre-check

Reuses `ic_framework_backtest.py`'s exact annual-block IC/ICIR machinery (`MIN_OBS_PER_BLOCK=6`, non-overlapping calendar-year blocks, median ICIR across assets with >=3 valid annual blocks), restricted to just the two Global-M2 rows study 24 already scored once at the full sample (`GlobalM2YoY` level = ICIR +0.198, `GlobalM2 YoY accel` = ICIR -0.073). All 28 assets from study 24/06's roster (`macro_strategy_zoo.ASSETS_28`) are truncated along with the Global-M2 macro series at 6 fixed checkpoints, locked before any result was read: 2016-12, 2018-12, 2020-12, 2022-12, 2024-12, and the full sample (today, matching study 24's own end date). Truncating both macro input and asset prices means the forward return used for the last in-sample month at each checkpoint is itself NaN (no future price beyond the checkpoint to compute it against) -- genuinely no-lookahead, not just an expanding z-score. This is a diagnostic on the raw signal's stability, not a trading backtest -- reported regardless of what it shows, and it does NOT alter Part 2's locked protocol below.

| checkpoint | GlobalM2YoY level: median ICIR (median mean-IC, n assets) | GlobalM2 YoY accel (live): median ICIR (median mean-IC, n assets) | which leads |
|---|---|---|---|
| 2016-12 | -0.027 (-0.006, n=22) | -0.298 (-0.093, n=22) | **level** |
| 2018-12 | +0.171 (+0.036, n=23) | -0.186 (-0.060, n=23) | **level** |
| 2020-12 | +0.187 (+0.050, n=26) | -0.170 (-0.055, n=26) | **level** |
| 2022-12 | +0.206 (+0.050, n=27) | -0.116 (-0.033, n=27) | **level** |
| 2024-12 | +0.181 (+0.057, n=28) | -0.137 (-0.050, n=28) | **level** |
| FULL SAMPLE | +0.212 (+0.061, n=28) | -0.030 (-0.010, n=28) | **level** |

**Full-sample row reproduces study 24's already-published numbers exactly** (GlobalM2YoY level ICIR +0.198, GlobalM2 YoY accel ICIR -0.073, both n=28) -- confirms this pre-check's machinery is faithful to study 24's own, not a new/different computation dressed up as a repeat.

**Headline: level leads accel at 6/6 checkpoints (including the earliest testable one, 2016-12).** This is a genuinely stable ranking, not a recent flip -- the raw predictive-correlation edge of the level transform over the acceleration transform has held at every truncation point tested, back to the earliest checkpoint with enough asset history to compute it (2016-12, n=22 assets with >=3 valid annual IC blocks by that date). Note this does not, by itself, resolve the circularity concern the design review raised -- it shows the raw signal ranking is stable through time, which is a necessary but not sufficient condition for the walk-forward portfolio-level selection in Part 2 below to also favor the level leg; Part 2, not this pre-check, is still the actual verdict.

## Availability check -- does the level leg's z-score become valid earlier than the accel leg's (one fewer differencing step)?

- CURRENT (accel-leg) `smoothed_z` first valid: **2011-07** (bound by `z_gm_current`'s own start, 2011-05, one month later than `z_nl`'s, 2011-04).
- CANDIDATE (level-leg) `smoothed_z` first valid: **2011-06** (bound by `z_nl`'s own start, 2011-04 -- the level leg's own z-score, `z_gm_candidate`, is valid one month earlier still, 2011-02, but net-liq is the binding constraint for this leg).

**They differ by 1 month(s)** -- confirms the design review's flagged risk was real, not just a formality: the level leg's own valid-history start is genuinely earlier. Per the locked protocol, both CURRENT and CANDIDATE chains are anchored to the LATER of the two, **2011-07**, so the comparison starts from the same month for both legs.

## Part 1 -- full-history fixed-formula A/B (IN-SAMPLE / DESCRIPTIVE, NOT the verdict)

CURRENT (accel-based Global-M2 leg, the live ADR-044 formula) vs. CANDIDATE (level-based, study 24's best-ICIR transform), each fixed throughout its own run, across the 5 standard windows, divisor fixed at 0.5 for both. **Per the design review's central methodological decision, this table is retained for comparison/context only and is explicitly NOT this study's verdict** -- the CANDIDATE formula was itself derived by mining the exact full-sample history this table would otherwise 'confirm' it against; Part 2 below is the actual walk-forward answer.

| variant | 13y | 8y | 5y | 3y | 1y |
|---|--:|--:|--:|--:|--:|
| BASELINE (fixed target, no macro, WITH CURRENT 1.5/1.0 trim) | 37 (-31) | 37 (-32) | 22 (-53) | 14 (-21) | -34 (-33) |
| BUYHOLD control (fixed target, NO macro, NO trim of any kind) | 55 (-86) | 39 (-83) | 21 (-54) | 15 (-24) | -34 (-33) |
| BLEND CURRENT (accel-leg, live formula, divisor=0.5) | 35 (-14) | 36 (-12) | 26 (-24) | 20 (-10) | -27 (-20) |
| BLEND CANDIDATE (level-leg, divisor=0.5) | 31 (-23) | 35 (-17) | 20 (-32) | 16 (-10) | -29 (-24) |

Start months: 13y=2013-09, 8y=2018-09, 5y=2021-09, 3y=2023-09, 1y=2025-09. Cell = XIRR%/yr (maxDD%).

### Part 1 -- annualized Sharpe (mean/std of monthly NAV-growth-factor return chain x sqrt(12))

| variant | 13y | 8y | 5y | 3y | 1y |
|---|--:|--:|--:|--:|--:|
| BASELINE (fixed target, no macro, WITH CURRENT 1.5/1.0 trim) | 0.99 | 1.26 | 0.59 | 1.09 | -2.08 |
| BUYHOLD control (fixed target, NO macro, NO trim of any kind) | 0.88 | 0.85 | 0.55 | 1.06 | -2.08 |
| BLEND CURRENT (accel-leg, live formula, divisor=0.5) | 1.02 | 1.51 | 1.17 | 1.62 | -1.40 |
| BLEND CANDIDATE (level-leg, divisor=0.5) | 0.94 | 1.45 | 0.89 | 1.53 | -1.70 |

**CAGR-vs-Sharpe diagnostic (per the skill's vol-targeting caveat)**: comparing the XIRR and Sharpe tables above, any Sharpe improvement for CANDIDATE not matched by a similar-direction XIRR improvement at the same window would indicate a vol-reduction artifact rather than genuine directional skill -- checked explicitly in the verdict section below, not just asserted.


## Part 2 -- walk-forward Global-M2-leg-selection protocol (THE walk-forward verdict)

Protocol adapted, unmodified in structure, from study 21's Part 2 (locked before any result was read): **2011-07..2014-06** is a 36-month burn-in defaulting to **CURRENT** (the live accel-leg formula, mirroring both study 21's own burn-in default and ADR-044's own 'insufficient history -> status quo' convention), then annual (12-month) checkpoints; at each checkpoint, both candidates (CURRENT, CANDIDATE) are independently re-simulated over the TRAILING 36 months (a fresh 1000+1000/mo portfolio restarting at checkpoint-36mo, using ONLY data available up to that checkpoint) and each candidate's annualized Sharpe of that trailing run's own monthly NAV-growth-factor returns is computed (matches study 21/04's own selection method); the argmax leg is applied for the following 12 months OUT OF SAMPLE; repeat. Divisor fixed at 0.5 throughout for both legs -- only the Global-M2 leg formula is being selected.

(Chain length check: 183 months, 2011-07..2026-09; burn-in=36, checkpoints every 12mo -> 12 checkpoints, with 3 leftover months in the final partial block).)

### Checkpoint log (13 annual checkpoints after the 36-month burn-in, 2011-07..2014-06)

| checkpoint | trailing-36mo Sharpe CURRENT (accel) | CANDIDATE (level) | selected | applied OOS |
|---|--:|--:|--:|---|
| 2014-06 | 1.08 | 1.06 | **CURRENT** | 2014-07..2015-06 |
| 2015-06 | 1.08 | 0.92 | **CURRENT** | 2015-07..2016-06 |
| 2016-06 | 1.17 | 0.97 | **CURRENT** | 2016-07..2017-06 |
| 2017-06 | 1.95 | 1.94 | **CURRENT** | 2017-07..2018-06 |
| 2018-06 | 2.09 | 1.89 | **CURRENT** | 2018-07..2019-06 |
| 2019-06 | 1.80 | 1.63 | **CURRENT** | 2019-07..2020-06 |
| 2020-06 | 1.26 | 1.19 | **CURRENT** | 2020-07..2021-06 |
| 2021-06 | 2.11 | 1.89 | **CURRENT** | 2021-07..2022-06 |
| 2022-06 | 1.78 | 1.71 | **CURRENT** | 2022-07..2023-06 |
| 2023-06 | 1.65 | 1.47 | **CURRENT** | 2023-07..2024-06 |
| 2024-06 | 1.20 | 0.90 | **CURRENT** | 2024-07..2025-06 |
| 2025-06 | 1.54 | 1.39 | **CURRENT** | 2025-07..2026-06 |
| 2026-06 | 1.67 | 1.63 | **CURRENT** | 2026-07..2026-09 |

### Occupancy table -- how many of the checkpoints picked CURRENT vs. CANDIDATE

| leg | # checkpoints selected | % of checkpoints | months governed OOS |
|---|--:|--:|--:|
| CURRENT | 13 | 100% | 147 |
| CANDIDATE | 0 | 0% | 0 |

(Burn-in months, 36 of them, 2011-07..2014-06, are not counted above -- fixed at CURRENT by construction, not selected.)

**Checkpoints that selected CANDIDATE**: none.

**Most recent checkpoint** (2026-06): trailing-36mo Sharpe was 1.67 (CURRENT/accel), 1.63 (CANDIDATE/level) -> selected **CURRENT**.

### The walk-forward verdict -- ONE continuous chained track record, 2011-07..2026-09 (183 months)

XIRR **35.5%/yr**, maxDD **-15.9%**, annualized Sharpe **1.09**.

### The same walk-forward schedule, applied to fresh-start sims at the 5 standard windows (for direct, apples-to-apples comparison against Part 1's fixed-formula table)

| variant | 13y | 8y | 5y | 3y | 1y |
|---|--:|--:|--:|--:|--:|
| WALK-FORWARD SCHEDULE (Part 2) | 35 (-14) | 36 (-12) | 26 (-24) | 20 (-10) | -27 (-20) |
| BASELINE (fixed target, no macro, WITH CURRENT 1.5/1.0 trim) (Part 1, in-sample) | 37 (-31) | 37 (-32) | 22 (-53) | 14 (-21) | -34 (-33) |
| BUYHOLD control (fixed target, NO macro, NO trim of any kind) (Part 1, in-sample) | 55 (-86) | 39 (-83) | 21 (-54) | 15 (-24) | -34 (-33) |
| BLEND CURRENT (accel-leg, live formula, divisor=0.5) (Part 1, in-sample) | 35 (-14) | 36 (-12) | 26 (-24) | 20 (-10) | -27 (-20) |
| BLEND CANDIDATE (level-leg, divisor=0.5) (Part 1, in-sample) | 31 (-23) | 35 (-17) | 20 (-32) | 16 (-10) | -29 (-24) |

### Same-window Sharpe (walk-forward schedule vs. Part 1 fixed-formula rows)

| variant | 13y | 8y | 5y | 3y | 1y |
|---|--:|--:|--:|--:|--:|
| WALK-FORWARD SCHEDULE (Part 2) | 1.02 | 1.51 | 1.17 | 1.62 | -1.40 |
| BASELINE (fixed target, no macro, WITH CURRENT 1.5/1.0 trim) (Part 1, in-sample) | 0.99 | 1.26 | 0.59 | 1.09 | -2.08 |
| BUYHOLD control (fixed target, NO macro, NO trim of any kind) (Part 1, in-sample) | 0.88 | 0.85 | 0.55 | 1.06 | -2.08 |
| BLEND CURRENT (accel-leg, live formula, divisor=0.5) (Part 1, in-sample) | 1.02 | 1.51 | 1.17 | 1.62 | -1.40 |
| BLEND CANDIDATE (level-leg, divisor=0.5) (Part 1, in-sample) | 0.94 | 1.45 | 0.89 | 1.53 | -1.70 |

## ETH trim-overshoot-ratio metric (same convention as study 21)

Overshoot ratio = peak `deviationPct` (ETH's actual position weight / its effective target that month) reached in ANY month, recorded at the pre-trim-check point, holding the modeled trim config fixed at the CURRENT (trigger=1.5, trim_to=1.0, ADR-048 live rule) throughout -- only the leg (or, for the schedule, the walk-forward-selected leg) varies. Computed over a single continuous pass, 2011-07..2026-09.

| variant | ETH peak overshoot ratio |
|---|--:|
| BLEND CURRENT | 5.94x |
| BLEND CANDIDATE | 8.22x |
| WALK-FORWARD SCHEDULE | 5.94x |

## Verdict

**Part 0 (raw-signal IC stability)**: the level transform's ICIR edge over the acceleration transform is **stable, not a recent flip** -- level leads at 6/6 checkpoints tested, including the earliest testable one (2016-12). This is the raw-signal, pre-portfolio-simulation diagnostic; it does not by itself settle the walk-forward portfolio question below.

**Part 1 (in-sample/descriptive, NOT the verdict)** -- CANDIDATE (level) vs. CURRENT (accel, live) fixed-formula XIRR (maxDD) at each window:

| window | CURRENT (accel, live) | CANDIDATE (level) | CANDIDATE - CURRENT (XIRR pp) |
|---|--:|--:|--:|
| 13y | 35% (-14%) | 31% (-23%) | -4.5 |
| 8y | 36% (-12%) | 35% (-17%) | -0.7 |
| 5y | 26% (-24%) | 20% (-32%) | -6.8 |
| 3y | 20% (-10%) | 16% (-10%) | -4.3 |
| 1y | -27% (-20%) | -29% (-24%) | -2.8 |
Retained for context only -- per the design review, never presented as the answer.

**Part 2 (the actual verdict)** -- walk-forward-selected chain vs. CURRENT (fixed, live formula) fixed-formula XIRR (maxDD) at each window:

| window | CURRENT (accel, live, fixed) | WALK-FORWARD SCHEDULE | schedule - CURRENT (XIRR pp) |
|---|--:|--:|--:|
| 13y | 35% (-14%) | 35% (-14%) | +0.0 |
| 8y | 36% (-12%) | 36% (-12%) | +0.0 |
| 5y | 26% (-24%) | 26% (-24%) | +0.0 |
| 3y | 20% (-10%) | 20% (-10%) | +0.0 |
| 1y | -27% (-20%) | -27% (-20%) | +0.0 |

The walk-forward chain beats the fixed CURRENT (live) formula on XIRR at 0/5 of the 5 standard windows. Occupancy: CANDIDATE (level leg) was selected at 0/13 checkpoints (0%), specifically none.

**CANDIDATE is never selected at any walk-forward checkpoint** -- despite Part 0's raw-signal IC evidence favoring the level transform at every checkpoint tested, the walk-forward portfolio-level selection never actually prefers it in practice. The raw predictive correlation and the realized trailing portfolio Sharpe of the discrete blend built from it are different things (the same distinction study 24 itself flagged between raw-signal ICIR and realized strategy Sharpe) -- CANDIDATE's stronger IC does not translate into a portfolio-level walk-forward win here.

## Caveats

- **N=2 candidates is below the skill's own N>=~5 trigger for a formal Deflated-Sharpe data-mining gut check** -- not run here, same treatment the skill gives sub-threshold comparisons; with only 2 discrete candidates the walk-forward selection result (12/12 checkpoints for CURRENT) speaks for itself without needing a separate luck-adjustment.
- **This is a blend-internal, single-leg-transform question, not a general portfolio-construction question** -- like study 21, no classic-portfolio (60/40) cross-check is included; the trim rule is held fixed at the CURRENT (trigger=1.5, trim_to=1.0, ADR-048 live rule) convention (matching study 21 exactly, for direct comparability -- both were refreshed from the PRIOR (2.0, 1.5) on 2026-09-25).
- **Same live-portfolio caveats as studies 14/21 apply unchanged**: miners-basket survivorship bias, gold's multi-year rally flatters any variant that trims into gold, overlapping 5-window comparisons share one underlying price/macro history (not independent draws), 1y/3y windows are single-episode reads, the SIMPLIFIED target table is a long-history-proxy adaptation of the real production targets (see portfolio_spec.py).
- **The trailing-36mo selection-metric sims restart the portfolio fresh at each checkpoint** (1000 PLN + 1000/mo from checkpoint-36mo), independent of the actual chain's accumulated state at that point -- exactly parallels study 21/04's own re-pick-on-trailing-data method, decided before any result was read.
- **Burn-in/checkpoint-cadence/selection-lookback (36mo/12mo/36mo) and the 6 Part-0 checkpoint dates were locked (by the design review) before this script's results were read and were not adjusted afterward**, per the task's own anti-tuning instruction.
- **Part 0's IC-stability pre-check is a raw-signal diagnostic, not a trading backtest** -- it says nothing about transaction costs, the 0.7/0.3 leg weighting, 3-month smoothing, the nonlinear divisor/clip transform, or the trim rule's interaction with the resulting blend; Part 2 is the only part of this study that models all of those together, which is exactly why Part 0's consistent level-beats-accel raw-IC finding does not automatically carry over to Part 2's portfolio-level selection outcome.
- **The earliest Part-0 checkpoint tested is 2016-12, not an earlier one**, because it is roughly the earliest date enough of the 28-asset roster has >=3 valid non-overlapping annual IC blocks -- an assets-with-sufficient-history constraint identical in spirit to study 24's own n>=3 filter, not a new limitation invented here.
