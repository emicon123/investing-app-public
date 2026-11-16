# 19 — Real Deflated Sharpe Ratio (DSR) & Probability of Backtest Overfitting (PBO) on the ETH flagship

Scripts: `liquidity_timing_backtest.py` (study 04, **reconstructed** — see reconstruction
notice below) and `macro_strategy_zoo.py` (study 06, **reconstructed**). Raw fidelity
check: `results/study19_first_pass_diff.md`. Raw DSR/PBO work:
`results/dsr_pbo_computation.md`. Per-variant return series (new — retained this time, see
"process fix" below): `results/study19_variant_returns_04_insample.csv`,
`results/study19_variant_returns_04_walkforward.csv`, `results/study19_variant_returns_06.csv`,
`results/study19_variant_returns_06_part3_walkforward.csv`.

This is file 18's own top-scoped candidate (#19): replace its back-of-envelope Deflated
Sharpe Ratio estimate (`σ_SR≈0.38`, `SR₀≈0.62-0.82`, "right on top of" the observed 0.81)
with an actual computation on real reconstructed per-variant return series, plus a real
Probability of Backtest Overfitting (PBO) pass via combinatorial cross-validation (CSCV)
on study 06's 11×28 rule zoo — the widest scan this program has ever run.

## Why this required reconstructing two scripts first

`liquidity_timing_backtest.py` and `macro_strategy_zoo.py` were never committed to git
(confirmed: `git log --all --follow` returns nothing for either) — the per-study
distillation convention (see the process fix below) had deleted them once studies 04/06
were folded into their summary `.md` files, with no `01_data_and_methods.md`-promised git
history to restore from. Both were rebuilt from scratch from `04_timing_backtest.md`,
`06_strategy_zoo.md`, and `01_data_and_methods.md`'s asset inventory, then validated
against every numeric/pattern gate the task specified **before** any DSR/PBO number was
computed (full comparison table: `results/study19_first_pass_diff.md`). Headline result of
that gate: **both reconstructions passed cleanly** — every walk-forward OOS Sharpe landed
within ±0.06 of published (band was ±0.15), the harder pattern checks (ETH's genuine OOS
edge, SOL's overfitting collapse, BTC's de-risk-only story, NetLiqAccel's #1 rank) held
exactly, and the small uniform drift seen throughout (+1-2pp CAGR, Sharpes ±0.01-0.06) is
fully explained by two known, legitimate, non-bug causes: the 2026-07-10 price-history
refresh post-dating the original runs, and DGS3MO/FRED being actively blocked from this
environment (both scripts fell back to their own documented 0%-cash-leg convention, same
as the originals almost certainly did). **Decision: PROCEED with full confidence, not
"pattern-level only."** One deliberate, flagged substitution carries through everything
below: study 06's 28-asset roster used JPM/HYG (per the task's literal spec) instead of
the original's actual BNB/XRP — this affects the aggregate breadth statistics (and
therefore the N=28/N=308 cross-sectional variance below), not any individual named-asset
check, all of which passed regardless.

## Disambiguating the two ETH results — never averaged, never conflated

Two genuinely different signal constructions each produce their own ETH walk-forward OOS
Sharpe:

- **Primary flagship (study 04, Part 2): net-liquidity L-week-momentum timing, OOS Sharpe
  0.8178** — this is study 18's own framing, the number the N=3/N=15/N=28/N=308 sweep
  below is built on.
- **Secondary companion (study 06, Part 3): Global-M2-acceleration timing, OOS Sharpe
  1.2031** — a related but distinct rule, reported here for context only. SOL is excluded
  from every Part-3-based statistic (checkpoint 1 found a real-but-noisy 12-observation
  SOL number where the original published "n/a, short OOS" — matching that abstention
  rather than reporting a number built on 12 monthly points). An informal N=2 gut-check
  (BTC/ETH only) gives `DSR≈1.000000` — not one of the four required framings, kept
  separate throughout.

## DSR at all four required N framings (primary flagship: study 04's ETH, 0.8178)

Using `expected_max_sharpe()`/`deflated_sharpe_ratio()` **verbatim** from
`.claude/skills/quant-backtest-rigor/references/methodology.md` (2026-07-12 corrected —
`SR_hat` in the PSR denominator, the formula question is resolved, no dual computation
needed):

| N | what N counts | `SR_hat` source | `SR_hat` | `SR₀` (by-chance bar) | **DSR** |
|---|---|---|--:|--:|--:|
| **3** | study 04's 3 walk-forward assets | study04 ETH | 0.818 | 0.303 | **1.000000** (clears by 7.7σ) |
| **15** | study04 Part 1's 5-window×3-asset in-sample scan | study04 ETH | 0.818 | 0.229 | **1.000000** (clears by 8.8σ) |
| **28** | study06's `NetLiqAccel` rule, its own 28-asset breadth | **study06 `NetLiqAccel×ETH`** | **1.102** | 0.662 | **0.997959** |
| **308** | study06's full 11-rule×28-asset zoo | **study06 `NetLiqAccel×ETH`** | **1.102** | 0.881 | **0.925599** |

Per the task's sourcing rule, the N=28/N=308 rows use **study 06's own `NetLiqAccel×ETH`
absolute Sharpe (1.102)** as `SR_hat` — not study 04's 0.818 — since they're testing a
different (though related) signal construction under a study-06-sourced breadth; pulling
study 04's number in under a study-06-sourced N would silently mix two different
strategies into one DSR statement. A `ddof=1` (sample-variance) sensitivity check on the
cross-sectional variance moves every number by ≤0.02 in `SR₀` and ≤0.002 in DSR — no
conclusion changes. Full numbers, per-variant Sharpe lists, and the formula sensitivity
check: `results/dsr_pbo_computation.md`.

**Optional, informal secondary check** (reproducing study 18's original back-of-envelope
literally — `SR_hat`=study04's 0.818 combined with study06's N=308 cross-sectional
variance, explicitly mixing the two signal constructions): `DSR = 0.174`. This is
dramatically lower than the correctly-sourced N=308 answer above (0.926) — the gap itself
is the practical demonstration of why the disambiguation rule matters: combining a weekly
momentum strategy's own Sharpe/skew/kurtosis/T with a monthly zoo's cross-sectional
variance is internally inconsistent and materially understates confidence. Kept only for
continuity with study 18, not treated as the real N=308 number.

## PBO via CSCV

**Study 06's 308-variant zoo (S=10 blocks, NaN-aware panel — 3/10 blocks flagged
low-confidence, pre-2007 rows with 0/28 assets covered since macro-signal history predates
every asset's own price history):**

- **Classic full-panel CSCV** (pick the in-sample raw-Sharpe champion across all 308
  columns each split, rank it OOS): **PBO = 0.2698** (68/252 combinations). `NetLiqAccel×ETH`
  was **never** the in-sample champion (0/252) — champions are dominated by
  `NetLiqAccel×SOL` (57×), `GlobalM2Accel×NVDA` (38×), `ISMRising×SOL` (35×), and others.
  This says something real: if the search process had literally hunted for "the single
  highest raw Sharpe among all 308 combos," it would usually have crowned something other
  than the flagship pair, and that broader "pick the best of 308" selection process
  carries a genuine, moderate (27%) overfitting risk.
- **Targeted CSCV** (the task's literal spec — `NetLiqAccel×ETH`'s test-half Sharpe vs. the
  full 308-column test-half median): **PBO = 0.0000** across all 240 valid combinations
  (mean logit-λ=2.72 — a wide, one-sided margin, not a knife-edge zero). **Honest caveat**:
  the comparison set here includes many structurally bad combos by design (bonds/dollar
  under any rule, contrarian ISM rules on anything), which drags the median down — a PBO
  of exactly 0 mostly says "ETH under a momentum rule beats a mix that deliberately
  includes bad ideas," a real but less demanding statement than "beats other similarly
  reasonable strategies" (which (a) speaks to more directly).

**Study 04's 15-variant in-sample matrix (S=8, weekly, block sizes ≥120 obs — comfortably
clears the ≥20-obs/block rule of thumb):** **PBO = 0.8714** (61/70 combinations). This is
**expected, not alarming** — Part 1 is explicitly in-sample/descriptive only, never the
tradeable claim, and a PBO this high is simply a quantitative confirmation of the
already-known SOL overfitting story (its in-sample champion, `SOL_L26`/`SOL_L52`, wins 20
of the 70 in-sample splits and collapses OOS every time this program has actually traded
it). **This number does not speak to Part 2's walk-forward result at all** — CSCV applies
to Part 1's static scan, not to Part 2's expanding-window re-pick protocol, which is a
single realized track record, not a set of candidate variants.

## Required verdict

**The primary ETH net-liquidity-momentum edge (study 04, OOS Sharpe 0.82) survives every
narrow-to-moderate framing overwhelmingly, and survives the widest framing narrowly.**

- **N=3, N=15**: DSR effectively 1.0 (clears by 7.7-8.8 standard errors) — not a close
  call at all under either of the framings closest to "just look at the walk-forward runs
  actually done."
- **N=28** (study 06's own honest reframing — "was ETH really the best of the 28 assets
  the winning rule was tested on"): DSR = **0.998** — still comfortably above any
  conventional threshold (95% or even 99%).
- **N=308** (the widest, most honest framing — "was ETH really the best of the entire
  11-rule×28-asset search that led here"): DSR = **0.926**. Using the conventional 95%
  bar from the DSR literature, this is **just short of "significant"** — a genuinely
  borderline number, not a clean pass. Using a looser 90% bar it clears; using a stricter
  99% bar it does not.

**Honest bottom line, matching this program's house style (studies 09/12/13's
non-oversold rejection tone) rather than rounding a borderline number up**: at the widest,
most punishing framing this study computed, the ETH edge sits **right on the edge of
"real" vs. "not cleanly distinguishable from a lucky draw out of 308 tries"** — this is
exactly the qualitative call study 18's back-of-envelope estimate made ("probably real but
thinner than it first looks"), now backed by an actual number (0.926, not a
guessed range) instead of an estimate. The PBO evidence is more reassuring in one sense
(NetLiqAccel×ETH beats the full zoo's OOS median 100% of the time it's tested, PBO=0.00)
and more sobering in another (it was never the single best-Sharpe combo in any of the 252
resampled searches, and the generic "pick the best of 308" process has a real 27%
overfitting rate) — both are true simultaneously and neither should be quoted alone. **Do
not treat N=308/DSR=0.926 as a clean pass, and do not treat it as a rejection either** —
report it exactly as computed, at the threshold stated, and let the reader weigh a
borderline 92.6% against whatever bar they need.

## Skill checklist (`quant-backtest-rigor/SKILL.md` "Before writing up the study")

- **In-sample labeling**: study 04 Part 1 (the 15-variant window scan) and its CSCV result
  (PBO=0.87) are explicitly labeled descriptive/in-sample above, never the tradeable claim
  — that remains Part 2's walk-forward chain, matching the original studies' own framing.
- **Data-mining gut check**: this study *is* the formal version of that check (DSR at 4
  N framings + CSCV/PBO), not a separate step to additionally run.
- **Standing baselines** (`backtest/AGENTS.md`): do not apply here — this is a statistical
  multiple-testing audit of an existing timing rule's already-reported Sharpe, not a new
  portfolio-level backtest reporting XIRR/maxDD, so the real buy-and-hold DCA / 4 classic
  portfolios / 3 trim configs baselines are out of scope by construction, not silently
  skipped.
- **Sharpe-only-vs-CAGR check**: already satisfied by study 04's own original result (ETH's
  walk-forward edge shows up on **both** Sharpe, 0.65→0.81, **and** CAGR, 22%→36%) — not
  re-litigated here, but worth restating since this study's whole purpose is stress-testing
  that same result's statistical significance, not its vol-vs-return character.
- **Macro-signal grounding**: no new macro signal was built in this study (it reuses study
  04/06's existing net-liquidity/Global-M2 signals unchanged) — N/A.
- **Honest verdict**: see "Required verdict" above — the N=308 borderline number is reported
  as borderline, not rounded either direction.
- **README updated**: Files-table row + Executive-summary point 15 added, per convention.

## Caveats

- T-bill cash leg: DGS3MO/FRED was actively blocked from this environment for both
  reconstructions (confirmed by an independent probe, not assumed); both scripts logged
  the failure and fell back to their own documented 0%-cash-leg convention. Very likely
  matches what the original studies experienced too, but not independently confirmed.
- Study 06's 28-asset roster substitutes JPM/HYG for the original's BNB/XRP (task's
  literal spec, not a reconstruction choice) — this means the N=28/N=308 cross-sectional
  variance (and therefore `SR₀`/DSR at those framings, and the whole PBO pass) is computed
  over a 2-asset-different population than the one that produced the original published
  20/28 breadth number. All individually-named-asset fidelity checks passed regardless.
- MSTR's ΔSharpe in the fidelity check drifted more than any other named asset (+0.40
  reconstructed vs. +0.24 published, correct sign) — not chased per the anti-tuning rule;
  MSTR does not appear in any DSR/PBO number reported here (ETH is the only asset used as
  `SR_hat`), so this caveat is inherited but not load-bearing for this study's verdict.
- The targeted CSCV's PBO=0.0000 compares `NetLiqAccel×ETH` against the full 308-column
  zoo's median, which includes many rule×asset combos that were never expected to be good
  (bonds/dollar under any rule, contrarian ISM rules on anything) — a stricter,
  apples-to-apples ΔSharpe-vs-buy&hold version of this same check (matching the metric
  study 06's own paper actually ranked rules by) was not attempted; flagged as a natural
  follow-up.
- `deflated_sharpe_ratio()`'s cross-sectional-variance convention (`ddof=0` vs. `ddof=1`)
  is not specified in `methodology.md` itself; `ddof=0` was chosen here because it exactly
  reproduces the skill file's own worked example (`σ_SR≈0.38` from the published
  1.00/0.81/0.12 triple) — the `ddof=1` sensitivity check (see
  `results/dsr_pbo_computation.md`) confirms this choice isn't load-bearing for the
  verdict either way.
- Binary long/cash rules throughout (no vol-targeting) — study #20 already tested and
  rejected vol-weighted position sizing on the production DCA mechanic as a separate
  question; not re-litigated here.

## Process fix (task step 10)

`results/study_summary/01_data_and_methods.md`'s "Note on the study scripts" convention is
updated by this study: **per-variant return series (and the scripts producing them) that
feed a DSR/PBO-relevant flagship claim are now retained**, not deleted once a study is
distilled into its summary `.md` — this entire Phase 2 effort (reconstructing two scripts
from prose descriptions, under an explicit anti-tuning rule, as its own multi-step
checkpointed task) only existed because that discipline wasn't followed for studies 02-06.
See `01_data_and_methods.md` for the updated convention text.
