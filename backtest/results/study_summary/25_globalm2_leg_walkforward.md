# 25 — does the ADR-044 blend's Global-M2 leg use the wrong transform? (level vs. acceleration)

Script: `globalm2_leg_walkforward_backtest.py` → `results/globalm2_leg_walkforward_backtest.md`,
`results/study25_part0_ic_stability.csv`, `results/study25_walkforward_checkpoints.csv`,
`results/study25_variant_returns.csv`. Per
`.claude/tasks/globalm2-level-vs-accel-blend-leg.task.md` (data-analyst + data-scientist design
review, 2026-07-13, fully reproduced in the task file's own "Reviewer's verdict").

## Why this study exists

Study #24 formalized studies 03/06 in IC/ICIR vocabulary and found something that directly
concerns the *live* ADR-044 blend, not just an academic reframing: of 9 continuous macro
drivers tested against all 28 assets, **`GlobalM2 YoY>0` (the plain level of Global-M2 YoY
growth) has the best median ICIR of anything tested (+0.198)**, while **`GlobalM2 YoY
accel>0` (the acceleration transform — YoY-then-3-month-delta — that the live blend actually
uses for its 30%-weighted Global-M2 leg) has *negative* median ICIR (−0.073)**. The live
formula (`docs/architecture/05-rebalancing-algorithm.md` §7.2) is:

```
composite_z = 0.7×zscore(net-liq YoY accel) + 0.3×zscore(Global-M2 YoY accel)
smoothed_z  = 3-month rolling average of composite_z
blend       = clip(0.5 − smoothed_z / 0.5, 0, 1)      [divisor = 0.5, ADR-049]
```

This study asks: does swapping the Global-M2 leg from acceleration to level (holding
everything else — the 0.7/0.3 weights, the net-liq leg, the 3-month smoothing, the 0.5
divisor — fixed) actually improve the blend's *walk-forward portfolio* performance?

## The circularity problem, and how the locked protocol handles it

The CANDIDATE (level leg) was not proposed independently of this program's own historical
data — it is the direct output of a full-sample IC statistic (study 24) computed on the exact
data this backtest would otherwise reuse to "confirm" it. A naive full-history A/B here would
substantially launder, not test, the same full-sample-hindsight mistake ADR-048's divisor
choice was rightly criticized for (study 21). The design review therefore locked a 3-part
protocol, **demoting the full-history A/B to explicitly in-sample/descriptive context (Part
1)** and requiring two genuine tests instead:

- **Part 0** — an expanding-checkpoint IC-stability pre-check (does "level beats accel" hold
  up at past truncation points, or is it only visible with today's full-sample hindsight?).
- **Part 2** — a genuine walk-forward leg-*selection* portfolio backtest, adapting study 21's
  exact burn-in/annual-checkpoint/trailing-36mo-Sharpe-selection/OOS-chaining machinery
  (generalizing its candidate axis from "which divisor" to "which Global-M2 leg formula",
  divisor fixed at 0.5 for both) — **this is the actual verdict**, not Part 1.

## Availability check (done before locking `CHAIN_START`)

Directly verified, not assumed: the level leg's own z-score (`z_gm_candidate`, from
`gm.pct_change(12).shift(2)`) becomes valid at **2011-02**, one fewer differencing step
earlier than the accel leg's (`z_gm_current`, valid **2011-05**). But `smoothed_z` for each
variant is additionally bound by `z_nl` (net-liq accel z-score, valid **2011-04**), so:

- CURRENT (accel leg): `smoothed_z` first valid **2011-07** — bound by `z_gm_current`
  (2011-05), one month later than `z_nl`.
- CANDIDATE (level leg): `smoothed_z` first valid **2011-06** — bound by `z_nl` (2011-04)
  itself, since the level leg's own z-score is already valid earlier (2011-02).

**They genuinely differ by 1 month** — the design review's flagged risk was real, not a
formality. Per the locked protocol, both chains are anchored to the later of the two,
**2011-07**, matching study 21's own `CHAIN_START` exactly (a coincidence confirmed by direct
computation, not assumed).

## Part 0 — expanding-checkpoint IC-stability pre-check (headline diagnostic)

Reuses `ic_framework_backtest.py`'s exact annual-block IC/ICIR machinery
(`MIN_OBS_PER_BLOCK=6`, non-overlapping calendar-year blocks, median ICIR across assets with
≥3 valid annual blocks), restricted to just the two Global-M2 rows, at 6 fixed truncation
checkpoints (both the Global-M2 macro series and all 28 assets' price histories truncated
together, so forward returns near each checkpoint are genuinely undefined/no-lookahead, not
just an expanding z-score):

| checkpoint | GlobalM2 YoY level: median ICIR (n assets) | GlobalM2 YoY accel (live): median ICIR (n assets) | which leads |
|---|--:|--:|---|
| 2016-12 | −0.027 (n=22) | −0.298 (n=22) | **level** |
| 2018-12 | +0.171 (n=23) | −0.186 (n=23) | **level** |
| 2020-12 | +0.187 (n=26) | −0.170 (n=26) | **level** |
| 2022-12 | +0.206 (n=27) | −0.116 (n=27) | **level** |
| 2024-12 | +0.181 (n=28) | −0.137 (n=28) | **level** |
| FULL SAMPLE | +0.198 (n=28) | −0.073 (n=28) | **level** |

The full-sample row reproduces study 24's already-published numbers exactly (+0.198 / −0.073,
both n=28) — confirms the pre-check's machinery is faithful to study 24's own, not a
different computation dressed up as a repeat.

**Headline: level leads accel at 6/6 checkpoints tested, including the earliest testable one
(2016-12, n=22 assets).** This is a genuinely **stable** ranking, not a recent flip — the raw
predictive-correlation edge of the level transform over the acceleration transform has held
at every truncation point this pre-check could test, all the way back to the earliest
checkpoint with enough asset history to compute it. This is a raw-signal, pre-portfolio
diagnostic only — it does not, by itself, resolve whether that edge survives the far more
processed composite-blend/trim-rule/portfolio machinery Part 2 actually simulates.

## Part 1 — full-history fixed-formula A/B (in-sample/descriptive, NOT the verdict)

CURRENT (accel-leg, live) vs. CANDIDATE (level-leg), each fixed throughout, divisor=0.5 fixed
for both, across the 5 standard windows:

| window | CURRENT (accel, live) | CANDIDATE (level) | CANDIDATE − CURRENT (XIRR pp) |
|---|--:|--:|--:|
| 13y | 39% (−26%) | 33% (−31%) | −5.6 |
| 8y | 41% (−16%) | 38% (−21%) | −3.1 |
| 5y | 28% (−21%) | 21% (−31%) | −6.9 |
| 3y | 26% (−12%) | 19% (−12%) | −6.9 |
| 1y | −2% (−10%) | −8% (−15%) | −5.8 |

CANDIDATE loses to CURRENT at **every single window**, on both XIRR and maxDD — not a
Sharpe-only mirage: annualized Sharpe also favors CURRENT at every window (13y 1.02 vs. 0.96,
8y 1.34 vs. 1.24, 5y 1.11 vs. 0.93, 3y 1.37 vs. 1.32, 1y 0.84 vs. 0.45), so this is not the
CAGR-vs-Sharpe vol-artifact pattern the skill warns about — CANDIDATE is simply weaker on
both axes throughout, even before accounting for the circularity concern that makes this
table non-authoritative in the first place. **Per the locked protocol, this is retained for
context only and is explicitly NOT this study's verdict.**

## Part 2 — walk-forward Global-M2-leg-selection protocol (THE verdict)

36-month burn-in (2011-07..2014-06, defaulting to CURRENT), then 12 annual checkpoints
(2014-06..2025-06, identical dates to study 21's own schedule), trailing-36-month-Sharpe
selection between CURRENT and CANDIDATE, divisor fixed at 0.5 for both, chained OOS
2011-07..2026-06:

| checkpoint | trailing-36mo Sharpe CURRENT (accel) | CANDIDATE (level) | selected |
|---|--:|--:|--:|
| 2014-06 | 0.95 | 0.92 | **CURRENT** |
| 2015-06 | 0.88 | 0.87 | **CURRENT** |
| 2016-06 | 1.02 | 0.82 | **CURRENT** |
| 2017-06 | 1.64 | 1.64 | **CURRENT** |
| 2018-06 | 1.83 | 1.61 | **CURRENT** |
| 2019-06 | 1.36 | 1.26 | **CURRENT** |
| 2020-06 | 0.88 | 0.72 | **CURRENT** |
| 2021-06 | 1.89 | 1.71 | **CURRENT** |
| 2022-06 | 1.71 | 1.66 | **CURRENT** |
| 2023-06 | 1.59 | 1.46 | **CURRENT** |
| 2024-06 | 1.14 | 0.88 | **CURRENT** |
| 2025-06 | 1.40 | 1.31 | **CURRENT** |

**Occupancy: CURRENT selected at 12/12 checkpoints (100%); CANDIDATE never once selected**,
at any checkpoint, in any period — early (2014-2016), middle (2017-2021), or most recent
(2022-2025). CURRENT's trailing-36mo Sharpe is higher than CANDIDATE's at literally every
single checkpoint tested, including 2025-06 (the most recent one: 1.40 vs. 1.31). By
construction, the chained walk-forward track record is therefore **numerically identical** to
CURRENT's own fixed-formula track record at all 5 standard windows (39%/13y, 41%/8y, 28%/5y,
26%/3y, −2%/1y — 0.0pp difference at every window) — the walk-forward chain does not beat the
current live formula anywhere, because the process never once had reason to leave it.

ETH trim-overshoot-ratio cross-check (same convention as study 21): CURRENT (accel) peaks at
**6.51×**, matching `05-rebalancing-algorithm.md` §7.2.2's own published divisor=0.5 figure
exactly (a good faithfulness check on this script's blend reconstruction); CANDIDATE (level)
peaks *higher* still, at **9.81×** — the level leg would also let ETH overshoot further before
the manual sell-assist would flag it, a second, independent strike against it beyond the
return numbers.

## Verdict — is this a durable finding, or a recency artifact?

**Neither of the task's two speculative "recency" framings apply here — the actual result is
cleaner and more unambiguous than either.** Part 0 shows the raw-signal ICIR ranking has been
**stable in level's favor since at least 2016-12**, not a recent flip. Part 2 shows the
walk-forward portfolio-level selection has favored CURRENT (accel) at **every single
checkpoint**, old and recent alike — not a recency-driven pattern either, in the opposite
direction from what a "level's raw edge only shows up in backtest hindsight" story would
predict. **The two results are honestly in tension, and that tension is itself the finding**:
the level transform has a stronger, temporally stable *raw predictive correlation* with
forward returns than the acceleration transform does — but that raw-signal advantage never
once translates into a better trailing 36-month portfolio Sharpe once it is folded into the
actual composite (0.7/0.3-weighted, z-scored, 3-month-smoothed, divisor-clipped, trim-rule-
interacting) blend the live formula, and this backtest, actually trade. Study 24 itself
already flagged this exact distinction in the abstract (raw ICIR reordering vs. realized
strategy Sharpe not always agreeing) — this study is a concrete, walk-forward-honest
demonstration of that same gap, on the specific candidate change study 24's own table
appeared to motivate.

**Bottom line: do not swap the Global-M2 leg from acceleration to level.** The walk-forward
process — which is this study's actual verdict, per the locked protocol — never selects the
level leg, at any of 12 checkpoints spanning 2014-2025; the in-sample table (context only)
shows CANDIDATE losing on both return and risk axes at every window too; and CANDIDATE's ETH
trim-overshoot risk is worse, not better. Study 24's raw-ICIR finding was real and reproduces
exactly here, and its temporal stability (Part 0) is a genuinely new, useful fact about the
signal — but it does not, on its own, indicate the live blend formula should change. Per the
task's own guardrail, this is a report-only recommendation (do not swap the leg); no change
was made to `MacroDataService` or `docs/architecture/05-rebalancing-algorithm.md`.

## Caveats

N=2 candidates is below the skill's own N≥~5 trigger for a formal Deflated-Sharpe data-mining
gut check (not run — with only 2 discrete candidates and a 12/12 unanimous walk-forward
result, a luck-adjustment adds little). This is a blend-internal, single-leg-transform
question, not a general portfolio-construction one — like study 21, no classic-portfolio
(60/40) cross-check is included, and the trim rule is held fixed at the backtest's own
modeled (trigger=2.0, trim_to=1.5) convention (matching study 21 exactly for comparability),
which is unrelated to the current live UI-only manual sell-assist threshold (150%/100%,
ADR-048 §7.5). Same demo-portfolio caveats as studies 14/21 apply unchanged (miners-basket
survivorship bias, gold's rally flatters gold-trimming variants, overlapping windows share one
underlying history, 1y/3y are single-episode reads). The trailing-36mo selection sims restart
the portfolio fresh at each checkpoint (study 21/04's own convention), independent of the
chain's actual accumulated state. Burn-in/cadence/lookback (36/12/36mo) and the 6 Part-0
checkpoint dates were locked by the design review before any result was read. Part 0 is a
raw-signal diagnostic only — it says nothing about transaction costs, the composite's
weighting/smoothing/clip transform, or trim-rule interaction, which is exactly why its
consistent finding does not automatically carry over to Part 2's portfolio-level result. The
earliest Part-0 checkpoint (2016-12) is set by asset-history availability (≥3 valid annual IC
blocks per asset), the same constraint study 24 itself already applies.

## 2026-09-25 correction note (task 28, second pass — re-run, verdict unchanged)
Re-ran under the refreshed `portfolio_spec.py` (live weights incl. LINK, CURRENT
1.5x/1.0x trim, live orientation) — plus a packaging fix the re-run required:
`macro_strategy_zoo.py` still resolved data paths relative to its own pre-task-159
location (`backtest/lib/data/...`, nonexistent); now resolves to `backtest/data/`.
Chain extended through 2026-09 (183mo: 13 checkpoints — 12 full plus one trailing
3-month partial block; 36/12/36 protocol unchanged). CANDIDATE (level leg) is still
**never selected at any checkpoint**; most recent (2026-06): 1.67 (CURRENT/accel) vs
1.63 (CANDIDATE/level). Part 0 still 6/6 for level. Rejection stands.
