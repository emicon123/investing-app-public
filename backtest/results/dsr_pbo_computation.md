# Study #19, Phase 2 checkpoint 2 — raw DSR/PBO computation (audit trail)

This is the raw work behind `results/study_summary/19_dsr_pbo.md`. Computed once, from the
already-validated (checkpoint 1) reconstructed CSVs — no reconstruction script was re-run,
no parameter was tuned to move any number below. `expected_max_sharpe()` and
`deflated_sharpe_ratio()` are copied **verbatim** from
`.claude/skills/quant-backtest-rigor/references/methodology.md` (2026-07-12 corrected
version — `SR_hat` in the PSR denominator, not `SR_0`). Sharpe convention matches each
reconstructed script exactly: `mean/std * sqrt(periods_per_year)`, no risk-free
subtraction (weekly `sqrt(52)` for study 04, monthly `sqrt(12)` for study 06).

Analysis script: scratch, not retained in `backtest/` (this repo's convention is that
`results/*.md` + the underlying CSVs are the durable record — the analysis script itself
is throwaway plumbing around functions that already live in `references/methodology.md`).
Run inside a local venv (`python3 -m venv`) because `scipy` is not installed system-wide
here (`numpy`/`pandas` are; `scipy` needed for `norm.ppf`/`norm.cdf` and skew/kurtosis) —
no system packages were touched, no cost incurred (still free/open-source).

## Inputs (all from checkpoint 1, unmodified)

| file | shape | role |
|---|---|---|
| `study19_variant_returns_04_walkforward.csv` | 673×4 (date,BTC,ETH,SOL) | study 04 Part 2 — primary ETH flagship source |
| `study19_variant_returns_04_insample.csv` | 964×16 (date + 15 cols) | study 04 Part 1 — N=15 framing's variance source |
| `study19_variant_returns_06.csv` | 329×309 (date + 308 cols) | study 06 — N=28/N=308 framing + PBO source |
| `study19_variant_returns_06_part3_walkforward.csv` | 129×4 (date,BTC,ETH,SOL) | study 06 Part 3 — secondary ETH companion |

## Step 1 — per-series stats used as `SR_hat`/skew/kurtosis/T inputs

**Study 04 walk-forward (weekly, `sqrt(52)`):**

| asset | Sharpe | skew | kurtosis (Pearson, normal=3) | T |
|---|--:|--:|--:|--:|
| BTC | 1.0050 | 1.857 | 14.93 | 673 |
| **ETH** | **0.8178** | **1.447** | **13.02** | **409** |
| SOL | 0.1753 | 1.468 | 12.00 | 165 |

(All three land within checkpoint 1's fidelity band of the published 1.00/0.81/0.12.)

**Study 06 — `NetLiqAccel__ETH` absolute Sharpe (monthly, `sqrt(12)`)**, needed as `SR_hat`
for the N=28/N=308 framings per the task's explicit sourcing rule (never study 04's 0.81
under a study-06-sourced N):

| variant | Sharpe | skew | kurtosis (Pearson) | T |
|---|--:|--:|--:|--:|
| `NetLiqAccel__ETH` | **1.1020** | 3.553 | 20.59 | 130 |

This absolute Sharpe (1.10) has never been individually published before — `06_strategy_zoo.md`
only reports ΔSharpe-vs-buy&hold and %beat aggregates, not each rule×asset cell's own level.

**Study 06 Part 3 (Global-M2-accel walk-forward, monthly) — secondary companion, own numbers:**

| asset | Sharpe | skew | kurtosis | T |
|---|--:|--:|--:|--:|
| BTC | 1.0436 | 1.369 | 5.84 | 129 |
| **ETH** | **1.2031** | **1.963** | **7.19** | **68** |
| SOL | −0.3916 | −0.210 | 2.94 | 12 |

SOL: 12 monthly observations, matches checkpoint 1's flag exactly (published = "n/a, short
OOS") — **excluded from any Part-3-based statistic below**, per the task's instruction.

## Step 2 — cross-sectional variance (`V_SR`) at each N, and the resulting `SR_0`

Population variance (`ddof=0`) used throughout — verified against `methodology.md`'s own
worked example: plugging its cited published Sharpes (BTC 1.00/ETH 0.81/SOL 0.12) through
`ddof=0` reproduces its stated `σ_SR≈0.38` almost exactly (0.378), confirming this is the
convention the skill's own example used. **Sensitivity check** (`ddof=1`, sample variance)
run alongside — results below, does not flip any conclusion (max SR₀ drift ≈ +0.02, max DSR
drift ≈ 0.002).

| N | trial set | V_SR (ddof=0) | SR₀ = expected_max_sharpe | V_SR (ddof=1) | SR₀ (ddof=1) |
|---|---|--:|--:|--:|--:|
| 3 | study04's 3 walk-forward Sharpes {1.005, 0.818, 0.175} | 0.1262 | 0.3030 | 0.1894 | 0.3711 |
| 15 | study04 Part1's 5-window×3-asset in-sample grid | 0.0167 | 0.2286 | 0.0179 | 0.2366 |
| 28 | study06's `NetLiqAccel` row, all 28 assets (all valid, no NaN) | 0.1047 | 0.6617 | 0.1086 | 0.6739 |
| 308 | study06's full 11×28 rule×asset zoo (all valid, no NaN) | 0.0920 | 0.8806 | 0.0923 | 0.8821 |

Full 15-variant Sharpe list (study 04 Part 1, weekly): BTC_L4 1.233, BTC_L8 1.204, BTC_L13
1.069, BTC_L26 1.074, BTC_L52 1.153, ETH_L4 1.206, ETH_L8 0.920, ETH_L13 1.054, ETH_L26
1.052, ETH_L52 0.947, SOL_L4 1.154, SOL_L8 1.102, SOL_L13 1.297, SOL_L26 1.311, SOL_L52
1.399.

Full 28-asset `NetLiqAccel` Sharpe list (study 06, monthly): SPY 0.929, QQQ 1.195, IWM
0.709, EEM 0.483, TLT 0.361, HYG 0.579, LQD 0.620, UUP −0.098, GLD 0.398, SLV 0.203, NVDA
1.069, AAPL 0.849, MSFT 1.000, TSLA 1.133, AMZN 0.948, GOOGL 0.835, JPM 0.630, XLF 0.581,
XLE 0.340, BTC 0.847, **ETH 1.102**, SOL 1.371, ADA 0.770, LINK 0.917, DOGE 0.638, MSTR
0.782, COIN 1.137, MARA 0.710.

## Step 3 — DSR at each of the 4 required N framings (primary ETH flagship, study 04's 0.81)

Using `deflated_sharpe_ratio(SR_hat, SR_0, skew, kurtosis, T)` verbatim:

| N | SR_hat source | SR_hat | SR₀ | skew/kurt/T source | z | **DSR** |
|---|---|--:|--:|---|--:|--:|
| **3** | study04 ETH walk-forward | 0.8178 | 0.3030 | study04 ETH walk-forward | 7.69 | **1.0000000000** (1−DSR = 7.1e-15) |
| **15** | study04 ETH walk-forward | 0.8178 | 0.2286 | study04 ETH walk-forward | 8.81 | **1.0000000000** (1−DSR ≈ 0) |
| **28** | **study06 `NetLiqAccel__ETH`** | 1.1020 | 0.6617 | study06 `NetLiqAccel__ETH` | 2.87 | **0.997959** |
| **308** | **study06 `NetLiqAccel__ETH`** | 1.1020 | 0.8806 | study06 `NetLiqAccel__ETH` | 1.44 | **0.925599** |

`ddof=1` sensitivity: N=3 → DSR≈1.000000; N=15 → DSR≈1.000000; N=28 → DSR=0.997386; N=308
→ DSR=0.924275. **No conclusion changes under the sensitivity check.**

**Optional/informal secondary check** (study 18's original back-of-envelope framing,
explicitly mixing constructions — `SR_hat`=study04's 0.81 but `V_SR`/`SR₀` from study06's
308-variant zoo): `DSR = 0.173706` — dramatically lower than the "clean" N=308 number
(0.9256) above. This large gap is the whole reason step 5's disambiguation rule
(`SR_hat` must be sourced from the same signal construction as the N framing) matters: naively
combining study 04's weekly-momentum ETH Sharpe with study 06's monthly-zoo cross-sectional
variance is internally inconsistent (different frequencies, different T, different
underlying strategies) and materially understates confidence relative to the correctly-
sourced N=28/N=308 numbers above. Kept only for continuity with study 18, not treated as
the real N=308 answer.

**Secondary companion (study06 Part 3, Global-M2-accel ETH, informal N=2 since SOL
excluded):** BTC/ETH Sharpes {1.044, 1.203}, `V_SR`(ddof=0)=0.00636, `SR₀(N=2)`=0.0414,
`DSR(ETH, N=2, informal)=1.000000`. Not one of the four required framings (task only
asked for study 04's 0.81 to be swept across N=3/15/28/308) — reported for completeness
only, and explicitly **not averaged or conflated** with the primary numbers above.

## Step 4 — PBO via CSCV, study 06's 308-variant matrix (S=10, NaN-aware panel)

T=329 monthly rows, `date` 1999-03→2026-07 (macro-signal history extends further back
than any of the 28 assets' price history — the earliest ~8 years of rows are 100% NaN
across all 308 columns; this doesn't corrupt anything since NaN-aware Sharpe/coverage
checks exclude them, but it does mean 3 of 10 contiguous blocks are entirely empty).

10 contiguous blocks, sizes [32,33,33,33,33,33,33,33,33,33]. Per-block asset coverage (of
28, via `NetLiqAccel__<ticker>` non-null as a coverage proxy — every rule for a given asset
shares that asset's own price-return NaN pattern):

| block | date range | assets covered / 28 |
|---|---|--:|
| 0 | 1999-03 .. 2001-10 | 0 — **LOW CONFIDENCE** |
| 1 | 2001-11 .. 2004-07 | 0 — **LOW CONFIDENCE** |
| 2 | 2004-08 .. 2007-04 | 0 — **LOW CONFIDENCE** |
| 3 | 2007-05 .. 2010-01 | 19 |
| 4 | 2010-02 .. 2012-10 | 22 |
| 5 | 2012-11 .. 2015-07 | 22 |
| 6 | 2015-08 .. 2018-04 | 26 |
| 7 | 2018-05 .. 2021-01 | 27 |
| 8 | 2021-02 .. 2023-10 | 28 |
| 9 | 2023-11 .. 2026-07 | 28 |

3/10 blocks (30%) flagged low-confidence — a direct consequence of the mismatched-inception
issue the task flagged in advance. `C(10,5)=252` train/test combinations; a (train,test)
pair is only scored for a given variant if that variant has ≥6 non-null monthly
observations in **both** halves (floor for a non-degenerate half-sample Sharpe) — 252/252
combinations produced ≥2 commonly-valid variants, so all 252 were usable.

**(a) Classic CSCV — full-panel in-sample-champion selection (methodology.md's literal
recipe: pick the train-half raw-Sharpe champion among all 308 columns, rank it OOS):**

- **PBO = 0.2698** (68/252 combinations: the in-sample raw-Sharpe champion underperformed
  the test-half cross-sectional median).
- `NetLiqAccel__ETH` was **never** the champion (0/252) — the raw-Sharpe champion is
  dominated by `NetLiqAccel__SOL` (57/252), `GlobalM2Accel__NVDA` (38/252),
  `ISMRising__SOL` (35/252), `NetLiqYoY__AAPL` (19/252), `GlobalM2YoY__ETH`/`GlobalM2YoY__QQQ`
  (14/252 each), and others. **This means picking "whatever combo happens to have the
  single highest raw Sharpe in a given train half" would usually crown something other
  than the flagship rule×asset pair** — a materially different and harsher search-process
  question than "does NetLiqAccel×ETH specifically hold up" (see (b)).

**(b) Targeted CSCV — `NetLiqAccel__ETH` vs. the cross-sectional OOS median (task's literal
step-6 spec):** valid in 240/252 combinations (both halves have ≥6 obs).

- **PBO (targeted) = 0.0000** — `NetLiqAccel__ETH`'s test-half Sharpe beat the full
  308-column test-half median in **all 240** valid combinations (mean λ=2.72, median
  λ=2.62 — a large, one-sided margin, not a knife-edge 0).
- **Honest caveat on this number**: the comparison benchmark here is the median of *all*
  308 columns, which includes many structurally low/negative-Sharpe combos by design
  (bond/dollar assets under any rule, contrarian ISM rules applied to anything) — these
  drag the cross-sectional median well below what "a reasonable crypto/equity strategy"
  should be compared against. A PBO of exactly 0 here is a real, computed result, not a
  typo, but it mostly says "ETH under a momentum rule beats a random mix that deliberately
  includes bad combos" rather than "ETH under NetLiqAccel specifically beats other
  similarly-reasonable strategies." (a) is the more informative overfitting statistic for
  that harder question.
- A stricter apples-to-apples version (ranking by ΔSharpe-vs-buy&hold, the metric study
  06's own paper actually used to rank rules, rather than raw Sharpe) was not attempted —
  would require reconstructing each of the 28 assets' own raw buy&hold monthly return
  series independently; flagged as a natural follow-up, not computed here to avoid scope
  creep beyond the task's literal spec.

## Step 5 — PBO via CSCV, study 04's 15-variant in-sample matrix (S=8, weekly)

T=964 weekly rows, S=8 → block sizes [120,121,120,121,120,121,120,121] (min 120 ≫ the
task's ≥20-obs/block rule of thumb — **PASS**, safe to run). `C(8,4)=70` combinations, all
70 produced ≥2 commonly-valid variants at a ≥20-obs/half floor.

**PBO (study 04, 15-variant in-sample-champion selection) = 0.8714** (61/70 combinations:
the in-sample champion underperformed the OOS median). Champion frequency across the 70
combinations: `SOL_L26` 20, `ETH_L4` 16, `BTC_L4` 12, `SOL_L13` 10, `BTC_L8` 8, `ETH_L52` 2,
`ETH_L26` 1, `BTC_L52` 1.

**This is expected, not alarming** — Part 1 of study 04 is explicitly documented (both in
`04_timing_backtest.md` and the walk-forward guardrail above) as **in-sample/descriptive
only**, never the tradeable claim. A PBO this high (87%) on that in-sample scan is a
quantitative confirmation of exactly what the walk-forward test already showed
qualitatively: SOL's in-sample-best window (SOL_L52, Sharpe 1.40, per the fidelity check)
collapsed OOS (0.12/0.18). It does **not** speak to the walk-forward (Part 2) OOS result at
all — Part 2 uses a completely different, expanding-window re-pick protocol specifically
designed to avoid this exact failure mode, and is not re-tested by this CSCV pass (a
walk-forward chained series is a single realized track record, not a set of N candidate
variants to run CSCV over — CSCV is the right tool for Part 1's static, fixed-window scan,
not for Part 2's protocol).

## Raw stdout

Full script output (scratch, not committed):
`/tmp/claude-1000/-home-wojtekrpi-Claude-Code-investing-app/c7fbccfd-8bc7-4ac0-9afc-a22b8a762a19/scratchpad/dsr_pbo_output.log`
