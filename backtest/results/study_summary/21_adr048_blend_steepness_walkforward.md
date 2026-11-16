# 21 — ADR-048 blend-steepness walk-forward validation

Script: `blend_steepness_walkforward_backtest.py` → `results/blend_steepness_walkforward_backtest.md`,
`results/study21_walkforward_checkpoints.csv` (per-checkpoint divisor-selection log),
`results/study21_variant_returns.csv` (per-variant monthly NAV returns, retained per
`01_data_and_methods.md`'s DSR/PBO-relevant-series convention). Design reviewed by
data-analyst + data-scientist agents before execution (`.claude/tasks/done/
adr-047-048-walkforward-validation.task.md`, "Reviewer's verdict" section) — the walk-forward
protocol below is the reviewer-tightened design, not the task's original one-line "make it
walk-forward" instruction.

## Context

ADR-048 (2026-07-10) tightened `MacroDataService.BLEND_STEEPNESS_DIVISOR` 2.0 → 1.0 (doubling
the ADR-044 blend's sensitivity to the same net-liquidity/Global-M2 composite z-score) on the
architecture doc's own admission: "ad-hoc backtesting... not walk-forward validated" (§7.2.1).
This happened **4 days after** study #19 established the underlying net-liquidity-momentum
signal sits at DSR = 0.926 — just short of the conventional 95% bar — at its widest, most
honest framing. Increasing sensitivity to a signal just shown to sit near the noise floor,
without walk-forward validation, is exactly the failure mode this program's `quant-backtest-
rigor` skill exists to catch. This study builds the walk-forward test ADR-048's own doc admits
was missing.

**Central design point (the reviewer's biggest catch)**: computing the 3 divisors'
full-history XIRR/maxDD across the 5 windows and reporting "which divisor wins" is **still the
ADR-048 mistake**, just tidier — every divisor's own z-scores are expanding/no-lookahead by
construction *regardless of divisor value*, so that property alone does not make picking the
divisor itself walk-forward. This study therefore keeps that 3-way comparison as explicitly
labeled **Part 1 (in-sample/descriptive)**, and adds a genuine **Part 2**: an expanding-window
divisor-**selection** protocol (burn-in → periodic re-pick on trailing data only → chained OOS
blocks), matching study 04/06 Part 3's own walk-forward precedent in this repo.

## What was tested

Reuses study 14's exact scaffold (`portfolio_macro_blend_backtest.py`, unmodified): same demo
portfolio (GLD 30/CASH 10/QQQ 5/FXI 2.5/MSTR 2.5/BTC 17.5/miners 7.5/ETH 13/SOL 5/COIN 4/SUI
2/DOGE 1, risk-on), same real deficit-proportional catch-up DCA (ADR-040/045), same
2.0×-trigger/1.5×-trim-to-gold rule held **fixed throughout** (only the divisor varies), same
5 overlapping windows (1/3/5/8/13y), 1000 PLN + 1000/mo, 15bps.

### Part 1 — fixed-divisor comparison across 5 windows (IN-SAMPLE, NOT the verdict)

Cell = XIRR%/yr (maxDD%). Start months: 13y=2013-06, 8y=2018-06, 5y=2021-06, 3y=2023-06,
1y=2025-06.

| variant | 13y | 8y | 5y | 3y | 1y |
|---|--:|--:|--:|--:|--:|
| BASELINE (fixed target, no macro, WITH 2.0/1.5 trim) | 43 (−39) | 43 (−40) | 26 (−52) | 25 (−22) | −16 (−22) |
| BUYHOLD control (fixed target, NO macro, NO trim of any kind) | 55 (−86) | 40 (−82) | 24 (−52) | 26 (−24) | −16 (−22) |
| BLEND divisor=2.0 (pre-ADR-048) | 38 (−22) | 37 (−22) | 24 (−30) | 22 (−15) | −7 (−14) |
| BLEND divisor=1.0 (ADR-048 live value) | 39 (−24) | 40 (−17) | 26 (−24) | 24 (−16) | −6 (−13) |
| BLEND divisor=0.5 (also ad-hoc-tried, per the doc) | 39 (−26) | 41 (−16) | 28 (−21) | 26 (−12) | −2 (−10) |

**Byte-parity check**: re-running study 14's actual, unmodified script fresh today reproduces
this study's BASELINE (43/−39) and divisor=2.0 (38/−22) cells exactly — confirming this
script's mechanics are a faithful reconstruction, and that the drift vs. study 14's originally
*published* numbers (39/−35, 34/−24) is entirely the documented 2026-07-10 price-history
refresh (the same legitimate cause study 19 already identified), not a bug in this script.

**N=3 data-mining gut check (rough — below the skill's own N≥~5 formal trigger)**: using each
divisor's 13y-window annualized Sharpe (2.0: 1.098, 1.0: 1.040, 0.5: 1.025), cross-sectional
σ_SR (ddof=0) = 0.031, expected max Sharpe under pure luck at N=3 ≈ 0.03. The best-of-3
(divisor=2.0, Sharpe 1.10) clears this bar comfortably — but per the task's own instruction,
**3 points barely describe a cross-sectional spread**; this is reported as a rough indicator,
not a pass/fail bar, and does not substitute for Part 2 below.

### Part 2 — the walk-forward divisor-selection protocol (THE verdict)

Locked before any result was read: **36-month burn-in** (2011-07..2014-06, defaults to
divisor=2.0 — the pre-ADR-048 status quo, mirroring ADR-044's own "insufficient history →
risk-on" convention) → **annual (12-month) checkpoints** → at each checkpoint, all 3 candidate
divisors are independently re-simulated over the **trailing 36 months** (a fresh 1000+1000/mo
portfolio restarting at checkpoint−36mo, using only data available up to that checkpoint), and
the candidate with the best trailing annualized Sharpe (matching study 04 Part 2's own
best-Sharpe re-pick criterion) is selected and applied **out of sample** for the next 12
months → repeat, chaining the resulting OOS blocks into one continuous track record spanning
2011-07..2026-06 (180 months — the full history composite_z/smoothed_z make available).
Burn-in length, checkpoint cadence, and selection lookback were **not adjusted after seeing
how the chain performed.**

**Checkpoint log** (12 annual checkpoints after the 36-month burn-in):

| checkpoint | trailing-36mo Sharpe @2.0 | @1.0 | @0.5 | selected | applied OOS |
|---|--:|--:|--:|--:|---|
| 2014-06 | 0.94 | 0.91 | 0.95 | **0.5** | 2014-07..2015-06 |
| 2015-06 | 0.90 | 0.88 | 0.88 | **2.0** | 2015-07..2016-06 |
| 2016-06 | 1.04 | 1.00 | 1.02 | **2.0** | 2016-07..2017-06 |
| 2017-06 | 1.82 | 1.73 | 1.64 | **2.0** | 2017-07..2018-06 |
| 2018-06 | 1.90 | 1.83 | 1.83 | **2.0** | 2018-07..2019-06 |
| 2019-06 | 1.42 | 1.39 | 1.36 | **2.0** | 2019-07..2020-06 |
| 2020-06 | 0.96 | 0.89 | 0.88 | **2.0** | 2020-07..2021-06 |
| 2021-06 | 1.81 | 1.86 | 1.89 | **0.5** | 2021-07..2022-06 |
| 2022-06 | 1.64 | 1.68 | 1.71 | **0.5** | 2022-07..2023-06 |
| 2023-06 | 1.50 | 1.57 | 1.59 | **0.5** | 2023-07..2024-06 |
| 2024-06 | 0.93 | 1.08 | 1.14 | **0.5** | 2024-07..2025-06 |
| 2025-06 | 1.16 | 1.29 | 1.40 | **0.5** | 2025-07..2026-06 |

**Divisor-selection occupancy table** — the headline, walk-forward-honest answer:

| divisor | # checkpoints selected (of 12) | % of checkpoints | months governed OOS |
|--:|--:|--:|--:|
| 2.0 | 6 | 50% | 72 |
| **1.0 (ADR-048's actual live value)** | **0** | **0%** | **0** |
| 0.5 | 6 | 50% | 72 |

**Divisor=1.0 — ADR-048's actual chosen value — is never selected at any of the 12 annual
checkpoints.** The walk-forward process alternates cleanly between the pre-ADR-048 status quo
(2.0, all six checkpoints from 2015-06 through 2020-06) and the steeper alternative also
mentioned in the architecture doc (0.5, the *most recent six consecutive checkpoints*,
2014-06 and continuously every checkpoint from 2021-06 to the present, 2025-06). At the most
recent checkpoint (2025-06), trailing-36mo Sharpe was 1.16 (@2.0) vs 1.29 (@1.0) vs 1.40
(@0.5) → selected 0.5. **ADR-048's move to /1.0 would not have been the walk-forward-selected
choice at any point in this reconstruction's history, including right now — a genuine
walk-forward process lands on either the old value or a steeper one than ADR-048 chose, never
on 1.0 itself.**

### The walk-forward verdict — one continuous chained track record

**2011-07..2026-06 (180 months): XIRR 38.0%/yr, maxDD −21.4%, annualized Sharpe 0.97.**

Applying the same realized month→divisor schedule to fresh-start sims at the 5 standard
windows (for direct comparison against Part 1's fixed-divisor table):

| variant | 13y | 8y | 5y | 3y | 1y |
|---|--:|--:|--:|--:|--:|
| **WALK-FORWARD SCHEDULE (Part 2)** | 38 (−22) | 37 (−18) | 28 (−21) | 26 (−12) | −2 (−10) |
| BASELINE (Part 1, in-sample) | 43 (−39) | 43 (−40) | 26 (−52) | 25 (−22) | −16 (−22) |
| BLEND divisor=2.0 (Part 1, in-sample) | 38 (−22) | 37 (−22) | 24 (−30) | 22 (−15) | −7 (−14) |
| BLEND divisor=1.0, ADR-048 (Part 1, in-sample) | 39 (−24) | 40 (−17) | 26 (−24) | 24 (−16) | −6 (−13) |
| BLEND divisor=0.5 (Part 1, in-sample) | 39 (−26) | 41 (−16) | 28 (−21) | 26 (−12) | −2 (−10) |

The walk-forward schedule tracks divisor=0.5's Part-1 numbers almost exactly (both selected at
every recent checkpoint) and beats fixed divisor=2.0 on drawdown at 4 of 5 windows and on
return at 3 of 5 — a materially better result than staying at 2.0 forever, and comparable to
(not clearly worse than) divisor=1.0's own fixed-divisor row. **The direction "let the blend
swing further from neutral" is validated by the walk-forward chain; the specific value ADR-048
picked (1.0) is not.**

### ETH trim-overshoot-ratio metric

Disambiguated per the task: this is **this backtest's own modeled 2.0×-trigger/1.5×-trim-to
rule** (the user's own live manual practice), *not* the unrelated frontend-only sell-flag UI
constants (§7.5, 200%/150%→150%/100%, which have zero interaction with any backtest script).
Overshoot ratio = ETH's peak `deviationPct` (actual weight ÷ effective target) reached in any
month, at the pre-trim-check point, holding the trim config fixed at (2.0, 1.5) — only the
divisor varies. Computed over a single continuous pass, 2011-07..2026-06:

| divisor | ETH peak overshoot ratio |
|--:|--:|
| 2.0 (pre-ADR-048) | 5.27× |
| 1.0 (ADR-048 live value) | 4.84× |
| 0.5 | 6.51× |
| walk-forward schedule (Part 2) | 5.28× |

**This does NOT reproduce ADR-048's own claimed ~5×→~10× doubling.** This reconstruction finds
overshoot essentially flat-to-slightly-*lower* going 2.0→1.0 (5.27×→4.84×), and higher only at
the more extreme 0.5 (6.51×). This is a genuine, disclosed discrepancy with ADR-048's own
ad-hoc claim — that ad-hoc comparison was never committed to `backtest/` (confirmed: no such
script exists), so its exact overshoot-ratio definition or portfolio calibration cannot be
diffed against this reconstruction. Plausible explanations (not verified): a different
overshoot-ratio definition (e.g. measured on a re-risk-after-de-risk transition specifically,
vs. this reconstruction's "peak ratio in any month"), or a different, non-committed demo
portfolio. Not chased further, per the anti-tuning rule — reported as found.

## Required verdict

**Does the walk-forward chain support ADR-048's decision? Partially, and not for the reason
ADR-048 gave.**

1. **The general direction ADR-048 took (steepen the blend, let it swing further toward the
   extremes) is validated** — the walk-forward-chained track record (38/−22, 37/−18, 28/−21,
   26/−12, −2/−10 across the 5 windows) beats the pre-ADR-048 status quo (fixed divisor=2.0)
   on drawdown at 4 of 5 windows and on return at 3 of 5, and the walk-forward process itself
   chooses to steepen further than 2.0 at every one of its 6 most recent checkpoints
   (2021-06 onward).
2. **The specific value ADR-048 chose (1.0) is never the walk-forward-selected value at any
   of the 12 checkpoints** — a genuine, honest walk-forward process always lands on either the
   old value (2.0, in the earlier, calmer years 2015-2021) or a steeper value than ADR-048
   picked (0.5, in every recent year including today). 1.0 sits in a selection no-man's-land
   for this specific metric (trailing-36-month Sharpe) at every point tested.
3. **ADR-048's own cited justification for the change (overshoot severity roughly doubling,
   ~5×→~10×) does not reproduce in this reconstruction** — found roughly flat-to-slightly-lower
   at 1.0 vs 2.0, worse only at 0.5. This is reported as a genuine discrepancy with the
   never-committed ad-hoc test, not chased or explained away.
4. **This was never walk-forward validated before shipping, and the walk-forward reconstruction
   now built confirms that gap was real** — not because the direction was wrong, but because
   the specific number landed somewhere a genuine selection process would not have chosen.

**Recommendation (reported, not implemented — per the task's guardrail)**: move
`BLEND_STEEPNESS_DIVISOR` to either **2.0** (revert to the pre-ADR-048, longest-validated
status quo) or **0.5** (the value this walk-forward reconstruction's own protocol has
selected at every checkpoint since 2021-06, including the most recent one) — not leave it at
**1.0**, which this study's honest walk-forward process never once recommends across 12
annual re-checks spanning 2014 to today. This is a recommendation for the user to weigh and
decide on; no live code, Flyway-seeded table, or `MacroDataService` constant was touched by
this task.

## Skill checklist (`quant-backtest-rigor/SKILL.md` "Before writing up the study")

- **In-sample labeling**: Part 1 explicitly labeled in-sample/descriptive throughout, never
  presented as the verdict — Part 2's chain is.
- **Data-mining gut check**: N=3 is below the skill's own N≥~5 trigger; computed anyway as a
  rough indicator, explicitly caveated as such, not treated as a pass/fail bar.
- **Standing baselines**: plain BUYHOLD control (real catch-up DCA, no trim, no macro)
  included per the task's own scoping; a 60/40 cross-check is not required for this
  blend-internal tuning question (required instead for study 22).
- **Sharpe-vs-CAGR check**: the walk-forward schedule's improvement over fixed divisor=2.0
  shows up on both Sharpe (implicit in the checkpoint log) and CAGR/maxDD (explicit table
  above) at most windows — not a Sharpe-only vol-reduction artifact.
- **Macro-signal grounding**: no new macro signal was built (reuses ADR-044's existing
  net-liq/Global-M2 blend construction unchanged, only the divisor parametrized) — N/A.
- **Honest verdict**: see "Required verdict" above — reported plainly in both directions
  (direction validated, specific value not, overshoot claim not reproduced).
- **README updated**: Files-table row + Executive-summary point added.

## Caveats

- Same caveats as study 14 apply unchanged: miners-basket survivorship (MARA/CIFR/IREN/WULF/
  HUT each began trading at different points), gold's multi-year rally through most of this
  sample flatters any variant that trims into gold, overlapping windows share one underlying
  price/macro history (not independent draws), 1y/3y windows are single-episode reads, the
  RISK_OFF table is a judgment-call adaptation of the real production V37/V39 ratios onto this
  demo portfolio.
- **N=3 gut check is rough** — 3 candidate divisors is below the skill's formal-check
  threshold; treat the SR0 comparison as a high-variance indicator only.
- **The trailing-36mo selection-metric sims restart the portfolio fresh at each checkpoint**
  (not a continuation of the actual chain's accumulated state) — this matches study 04 Part
  2's own re-pick-on-trailing-data method exactly, and was decided before any result was seen.
- **Burn-in/checkpoint-cadence/selection-lookback (36mo/12mo/36mo) were locked in advance** and
  not adjusted after seeing the chain's performance, per the anti-tuning discipline this task
  and the skill both require.
- **Only 3 divisor candidates were tested** (2.0/1.0/0.5, matching ADR-048's own doc, which
  mentions trying exactly these three ad-hoc) — not a continuous scan; a finer grid could in
  principle select a value between 0.5 and 2.0 not tested here.
- **This is a blend-internal tuning-constant question, not a general portfolio-construction
  question** — no classic-portfolio (60/40) cross-check was run for this study specifically,
  per the task's own scoping (required instead for study 22, where the change under test is a
  general allocation shift).

## 2026-09-25 correction note (task 28, second pass — re-run, verdict REFINED)
Re-ran under the refreshed `portfolio_spec.py` (live ADR-074/087 weights incl. LINK,
CURRENT 1.5x/1.0x trim held fixed and correctly labeled, live orientation);
`USDPLN_X.csv` restored to full 2003→ history; chain extended through 2026-09 (183mo:
13 checkpoints — 12 full 12-month OOS blocks plus one trailing 3-month partial block;
the 36/12/36 protocol parameters are unchanged). Two corrections to the verdict above,
stated plainly: (1) divisor=1.0 **is now selected at 2 of 13 checkpoints** (2014-06,
2019-06) — the old "never selected at any checkpoint" claim is retired; (2) the live
value 0.5 is selected at 7/13 **including the last six checkpoints in a row**
(2021-06→2026-06), i.e. the current live value now has direct recent walk-forward
support it did not have before. The direction (steepen the blend) is still validated;
the overshoot reconstruction now finds 3.54x @2.0 → 4.66x @1.0 (same direction as
ADR-048's claimed ~5x→~10x, smaller magnitude) and 5.94x @0.5.
