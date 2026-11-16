# Study 21 -- ADR-048 BLEND_STEEPNESS_DIVISOR walk-forward validation

Buy-only DCA 1000+1000/mo PLN, 15bps, CURRENT 1.5x/1.0x-trim-to-gold rule held fixed throughout (ADR-048 live rule; was 2.0x/1.5x PRIOR before the 2026-09-25 refresh) -- only the blend steepness divisor varies. Live portfolio weights via portfolio_spec.py. Latest month: 2026-09.

## Part 1 -- fixed-divisor comparison across 5 windows (IN-SAMPLE / DESCRIPTIVE, NOT the walk-forward verdict)

Each divisor's own composite_z/smoothed_z construction is expanding and no-lookahead by definition (true at every divisor value) -- but picking 'which divisor wins' by reading this table is full-sample hindsight, structurally identical to ADR-048's own ad-hoc comparison, just tidier. Kept for continuity with that comparison and as Part 1 context, per study 04/06/19's own Part 1-vs-Part 2 convention -- never the verdict.

| variant | 13y | 8y | 5y | 3y | 1y |
|---|--:|--:|--:|--:|--:|
| BASELINE (fixed target, no macro, WITH CURRENT 1.5/1.0 trim) | 37 (-31) | 37 (-32) | 22 (-53) | 14 (-21) | -34 (-33) |
| BUYHOLD control (fixed target, NO macro, NO trim of any kind -- standing baseline) | 55 (-86) | 39 (-83) | 21 (-54) | 15 (-24) | -34 (-33) |
| BLEND divisor=2.0 (pre-ADR-048 original) | 33 (-15) | 32 (-19) | 23 (-32) | 16 (-14) | -28 (-24) |
| BLEND divisor=1.0 (ADR-048, superseded -- PRIOR) | 34 (-18) | 35 (-13) | 25 (-26) | 19 (-12) | -28 (-23) |
| BLEND divisor=0.5 (ADR-049 LIVE value) | 35 (-14) | 36 (-12) | 26 (-24) | 20 (-10) | -27 (-20) |

Start months: 13y=2013-09, 8y=2018-09, 5y=2021-09, 3y=2023-09, 1y=2025-09. Cell = XIRR%/yr (maxDD%).

### Part 1 -- annualized Sharpe (mean/std of monthly NAV-growth-factor return chain x sqrt(12))

| variant | 13y | 8y | 5y | 3y | 1y |
|---|--:|--:|--:|--:|--:|
| BASELINE (fixed target, no macro, WITH CURRENT 1.5/1.0 trim) | 0.99 | 1.26 | 0.59 | 1.09 | -2.08 |
| BUYHOLD control (fixed target, NO macro, NO trim of any kind -- standing baseline) | 0.88 | 0.85 | 0.55 | 1.06 | -2.08 |
| BLEND divisor=2.0 (pre-ADR-048 original) | 1.05 | 1.41 | 0.93 | 1.32 | -1.62 |
| BLEND divisor=1.0 (ADR-048, superseded -- PRIOR) | 1.00 | 1.49 | 1.08 | 1.46 | -1.59 |
| BLEND divisor=0.5 (ADR-049 LIVE value) | 1.02 | 1.51 | 1.17 | 1.62 | -1.40 |

### N=3 data-mining gut check on the 3-way divisor comparison (rough, below the skill's N>=~5 formal-check trigger)

Per the task's own instruction: N=3 is below `.claude/skills/quant-backtest-rigor/SKILL.md`'s own 'N >= ~5' trigger for formally requiring this check -- three points barely describe a cross-sectional spread. Computed anyway as a rough indicator, using each divisor's 13y-window Sharpe (longest, least noisy single number per variant, matching study 19's convention of picking one representative number per candidate rather than averaging across overlapping windows).

| divisor | 13y Sharpe |
|--:|--:|
| 2.0 | 1.048 |
| 1.0 | 0.996 |
| 0.5 | 1.016 |

Cross-sectional sigma_SR (N=3, ddof=0) = 0.0216. Expected max Sharpe under pure luck at N=3, SR0 = 0.018. Best-of-3 (divisor=2.0, Sharpe=1.048) clears SR0 by 1.030. **Read with heavy caution**: with only 3 points, sigma_SR itself is a high-variance estimate, and this is not a substitute for the real walk-forward verdict in Part 2 below -- reported as a rough, informal indicator only, per the task's explicit instruction not to present an N=3 comparison with more confidence than 3 data points can support.

## Part 2 -- walk-forward divisor-selection protocol (THE walk-forward verdict)

Protocol LOCKED before any result was read (not tuned afterward): 36-month burn-in (defaults to divisor=2.0, the pre-ADR-048 status quo, mirroring ADR-044's own 'insufficient history -> risk-on' convention), then annual (12-month) checkpoints; at each checkpoint, every one of the 3 candidate divisors is independently re-simulated over the TRAILING 36 months (a fresh 1000+1000/mo portfolio restarting at checkpoint-36mo, using ONLY data available up to that checkpoint) and its annualized Sharpe of that trailing run's own monthly NAV-growth-factor returns is computed (matches study 04 Part 2's own re-pick-on-trailing-data selection method); the argmax divisor is applied for the following 12 months OUT OF SAMPLE; repeat. The resulting month->divisor schedule spans the full available history, 2011-07 (first month composite_z/smoothed_z exist) through 2026-09.

### Checkpoint log (13 annual checkpoints after the 36-month burn-in, 2011-07..2014-06)

| checkpoint | trailing-36mo Sharpe @2.0 | @1.0 | @0.5 | selected | applied OOS |
|---|--:|--:|--:|--:|---|
| 2014-06 | 1.08 | 1.10 | 1.08 | **1.0** | 2014-07..2015-06 |
| 2015-06 | 1.09 | 1.07 | 1.08 | **2.0** | 2015-07..2016-06 |
| 2016-06 | 1.14 | 1.13 | 1.17 | **0.5** | 2016-07..2017-06 |
| 2017-06 | 1.99 | 1.98 | 1.95 | **2.0** | 2017-07..2018-06 |
| 2018-06 | 2.16 | 2.11 | 2.09 | **2.0** | 2018-07..2019-06 |
| 2019-06 | 1.69 | 1.82 | 1.80 | **1.0** | 2019-07..2020-06 |
| 2020-06 | 1.30 | 1.28 | 1.26 | **2.0** | 2020-07..2021-06 |
| 2021-06 | 1.95 | 2.04 | 2.11 | **0.5** | 2021-07..2022-06 |
| 2022-06 | 1.66 | 1.74 | 1.78 | **0.5** | 2022-07..2023-06 |
| 2023-06 | 1.43 | 1.59 | 1.65 | **0.5** | 2023-07..2024-06 |
| 2024-06 | 0.96 | 1.15 | 1.20 | **0.5** | 2024-07..2025-06 |
| 2025-06 | 1.25 | 1.44 | 1.54 | **0.5** | 2025-07..2026-06 |
| 2026-06 | 1.37 | 1.46 | 1.67 | **0.5** | 2026-07..2026-09 |

### Divisor-selection occupancy table -- how often each divisor was actually selected across the 13 checkpoints

| divisor | # checkpoints selected | % of checkpoints | months governed OOS |
|--:|--:|--:|--:|
| 2.0 | 4 | 31% | 48 |
| 1.0 | 2 | 15% | 24 |
| 0.5 | 7 | 54% | 75 |

(Burn-in months, 36 of them, 2011-07..2014-06, are not counted above -- they are fixed at divisor=2.0 by construction, not selected.)

**Most recent checkpoint** (2026-06): trailing-36mo Sharpe was 1.37 (@2.0), 1.46 (@1.0), 1.67 (@0.5) -> selected **0.5**, applied 2026-07..2026-09. This is the direct, walk-forward-honest answer to 'would ADR-048's move to /1.0 have been the walk-forward-selected choice at the most recent checkpoint, or only in hindsight.'

### The walk-forward verdict -- ONE continuous chained track record, 2011-07..2026-09 (183 months)

XIRR **34.1%/yr**, maxDD **-19.1%**, annualized Sharpe **1.13**. This is the number that answers 'what would ADR-048's kind of annual re-tuning decision have actually produced, honestly walk-forward, instead of picking /1.0 once with full-sample hindsight' -- not the Part 1 fixed-divisor table.

### The same walk-forward schedule, applied to fresh-start sims at the 5 standard windows (for direct, apples-to-apples comparison against Part 1's fixed-divisor table)

| variant | 13y | 8y | 5y | 3y | 1y |
|---|--:|--:|--:|--:|--:|
| WALK-FORWARD SCHEDULE (Part 2) | 33 (-15) | 33 (-13) | 26 (-24) | 20 (-10) | -27 (-20) |
| BASELINE (fixed target, no macro, WITH CURRENT 1.5/1.0 trim) (Part 1, in-sample) | 37 (-31) | 37 (-32) | 22 (-53) | 14 (-21) | -34 (-33) |
| BUYHOLD control (fixed target, NO macro, NO trim of any kind -- standing baseline) (Part 1, in-sample) | 55 (-86) | 39 (-83) | 21 (-54) | 15 (-24) | -34 (-33) |
| BLEND divisor=2.0 (pre-ADR-048 original) (Part 1, in-sample) | 33 (-15) | 32 (-19) | 23 (-32) | 16 (-14) | -28 (-24) |
| BLEND divisor=1.0 (ADR-048, superseded -- PRIOR) (Part 1, in-sample) | 34 (-18) | 35 (-13) | 25 (-26) | 19 (-12) | -28 (-23) |
| BLEND divisor=0.5 (ADR-049 LIVE value) (Part 1, in-sample) | 35 (-14) | 36 (-12) | 26 (-24) | 20 (-10) | -27 (-20) |

## ETH trim-overshoot-ratio metric -- disambiguated

ADR-048's own doc (Sec 7.2.1) reports ETH's 'max overshoot ratio... rose from ~5x under /2.0 to ~10x under /1.0.' Per the task's explicit disambiguation: this is **this backtest's own modeled CURRENT 1.5x-trigger/1.0x-trim-to rule** (ADR-048's live rule -- this reconstruction held (2.0, 1.5) PRIOR fixed before the 2026-09-25 refresh, now holds (1.5, 1.0) fixed), NOT the unrelated frontend-only sell-flag thresholds (Sec 7.5, 200%/150% -> 150%/100%) -- that UI constant has zero interaction with `MainRebalancingStrategy` or any backtest script and has no equivalent computed here. Overshoot ratio = peak `deviationPct` (ETH's actual position weight / its effective target that month) reached in ANY month, recorded at the pre-trim-check point (the value that would trigger the rule to fire), holding the modeled trim config fixed at (trigger=1.5, trim_to=1.0) throughout -- only the divisor (or, for Part 2, the walk-forward-selected divisor) varies. Computed over a single continuous pass, 2011-07..2026-09, matching the walk-forward chain's own window for direct comparability.

### Part 1 (fixed divisors, single continuous pass)

| divisor | ETH peak overshoot ratio |
|--:|--:|
| 2.0 | 3.54x |
| 1.0 | 4.66x |
| 0.5 | 5.94x |

### Part 2 (walk-forward schedule, same continuous pass)

| walk-forward schedule | 3.54x |

ADR-048's own claimed doubling (~5x @2.0 -> ~10x @1.0): this reconstruction finds 3.54x @2.0 -> 4.66x @1.0 (confirms the same direction and rough magnitude), and 5.94x @0.5. Under the actual walk-forward-selected schedule (not a full-sample-hindsight /1.0 pick), the realized peak overshoot is 3.54x.

## Caveats

- **Part 1 is explicitly in-sample/descriptive** -- never the walk-forward claim, kept only for continuity with ADR-048's own ad-hoc comparison and study 04/06/19's Part-1 convention.
- **The N=3 gut check is rough** -- 3 candidate divisors is below the skill's own N>=~5 trigger for a formal data-mining check; treat the SR0 comparison above as a high-variance indicator, not a pass/fail bar.
- **Same caveats as study 14 apply unchanged**: miners-basket survivorship bias (MARA/CIFR/IREN/WULF/HUT each began trading at different points), gold's multi-year rally through most of this sample flatters any variant that trims into gold regardless of trigger quality, overlapping 5-window comparisons share one underlying price/macro history (not independent draws), 1y/3y windows are single-episode reads, the SIMPLIFIED target table is a long-history-proxy adaptation of the real production targets (GLD/QQQ/FXI stand in for Gold/Nasdaq-100/China; Experiments sleeve, ONDO-USD and India absorbed by present-renormalization -- see portfolio_spec.py).
- **The trailing-36mo selection-metric sims restart the portfolio fresh at each checkpoint** (1000 PLN + 1000/mo from checkpoint-36mo), independent of the actual chain's accumulated state at that point -- this exactly parallels study 04 Part 2's own re-pick-on-trailing-data method (a fresh Sharpe computed per candidate window over the trailing period, not a continuation of one realized position), and was decided before any result was read.
- **Burn-in/checkpoint-cadence/selection-lookback (36mo/12mo/36mo) were locked before this script's results were read and were not adjusted afterward**, per the task's own anti-tuning instruction and the skill's 'decide before looking at results' rule.
- **This is a blend-internal tuning-constant question**, not a general portfolio-construction question -- per the task's own scoping, no classic-portfolio (60/40) cross-check is included for Study A (required for Study B instead, where the change under test is a general allocation shift).
