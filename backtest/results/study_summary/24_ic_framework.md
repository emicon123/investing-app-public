# 24 — IC/ICIR/IC-decay framework: formalizing studies 03/06 in Grinold-Kahn vocabulary

Script: `ic_framework_backtest.py` → `results/ic_framework_backtest.md`,
`results/ic_time_series_h1_summary.csv`. Reuses already-fetched data only (`data/macro/`,
`data/prices/`, `results/study19_variant_returns_06.csv`, `results/liquidity_beta_ranking.csv`)
— no new fetch — but is a genuine new analysis script (block-IC/ICIR/decay-curve
machinery + a cross-sectional bootstrap), per the reviewed task's clarification that "no
new data" does not mean "no new code."

## 0. Why a literal cross-sectional IC across the 28-asset universe is mathematically undefined here

The Grinold-Kahn Information Coefficient is normally computed **cross-sectionally**: rank
the forecast across many assets at one point in time, correlate against their forward
returns, repeat every period → a time series of period-by-period ICs. The task's original
wording asked for exactly this, applied to study 06's 11-rule × 28-asset zoo. **Confirmed
by direct inspection, not assumed**: `macro_strategy_zoo.py`'s `build_rules()` constructs
every one of the 11 rules as a **single macro-level time series applied identically to all
28 assets each month** (`signals[name] = sig`, the same ON/OFF value for SPY, BTC, GLD,
everything, at any given time step). Spot-checked directly: `NetLiqAccel`'s continuous
driver reading at 2013-07 is a single scalar (0.1119) — identical for all 28 assets that
month by construction. Rank-correlating a 28-element vector that is a **constant** against
28 different realized returns has an **undefined** correlation coefficient (zero
cross-sectional variance in the forecast column) — not a small or weak IC, an undefined
one (`NaN`, not a number close to zero).

Two legitimate reframings are used instead, per the task's own guidance:

- **(a) PRIMARY — asset-level time-series IC per (rule, asset) pair.** Correlate each
  rule's own *continuous* macro-driver reading (not the binarized ON/OFF signal) against
  that asset's own forward return, over time, using non-overlapping calendar-year blocks
  (one Spearman IC value per year with ≥6 valid monthly observations → a genuine IC series
  with a real mean/std → ICIR = mean/std, t-stat = ICIR×√n). This is mechanically the same
  design study 03 already used for 3 drivers × 3 assets as single whole-sample numbers —
  Part 2 extends it to **9 continuous-driver rules × all 28 assets**, with the formal
  ICIR/t-stat/decay-curve packaging study 03 never had. This is the study's main content.
- **(b) BONUS — a genuinely cross-sectional (single-point, not time-series) check.**
  Correlate study 06's per-asset Sharpe-uplift for the `NetLiqAccel` rule against that
  asset's own net-liquidity beta (study 05's ranking) across the ~18 assets present in
  both artifacts. This has real cross-sectional variance (unlike the raw signal) and gets
  an i.i.d. bootstrap CI over assets — **not** the block-bootstrap convention
  (`01_data_and_methods.md`), which exists specifically to handle time-series
  autocorrelation; there is no time axis in a single snapshot, so a plain
  resample-with-replacement over assets is the correct adaptation, and this is reported as
  a **cross-sectional Spearman correlation with a bootstrap CI**, never as an "ICIR" (ICIR
  requires a time series of IC values; this is one number).

Two of the 11 rules (`ComboM2AccelNetLiqYoY`, `ISMLt50Rising`) are logical ANDs of two
other rules' conditions and have no natural single continuous driver reading — excluded
from (a)'s table below (their component rules, `GlobalM2Accel`/`NetLiqYoY` and
`ISMGt50`/`ISMRising`, ARE included individually).

**A note on driver definitions vs. study 03's own originals.** The 9 continuous drivers
used here are `macro_strategy_zoo.py` (study 06)'s own rule constructions (e.g.
`NetLiqAccel` = YoY-then-3-month-delta of net liquidity, lagged 1mo), not always identical
to study 03's own bespoke transforms for the same underlying series (e.g. study 03's
"net-liq momentum" was a raw N-week % change on the *weekly* grid, a related but distinct
quantity). This is a deliberate choice — it lets part (a) extend to the full 9-rule roster
study 06 already validated, at the cost of not literally reproducing study 03's specific
published numbers. See the whole-sample cross-reference below for how much this — plus a
separately-discovered data-refresh issue (see study #23's data-drift note; some of the
same underlying price series were also refreshed 2026-07-10) — moves the numbers.

## 1a. Study-03 spotlight — BTC/ETH/SOL × the 4 liquidity drivers, IC-decay curve

Annual-block IC/ICIR/t-stat at h=1/3/6/12 months (full table in
`results/ic_framework_backtest.md`; representative rows below):

| driver | asset | h=1 mean IC (ICIR, t) | h=12 mean IC (ICIR, t) |
|---|---|---|---|
| NetLiqAccel | BTC | -0.12 (-0.30, -1.21) | +0.02 (+0.06, +0.24) |
| NetLiqAccel | ETH | -0.00 (-0.00, -0.01) | +0.19 (+0.59, +1.88) |
| NetLiqAccel | SOL | +0.18 (+0.42, +1.02) | +0.18 (+0.55, +1.35) |
| NetLiqYoY | BTC | -0.11 (-0.40, -1.60) | -0.30 (-0.56, -2.23) |
| NetLiqYoY | ETH | -0.02 (-0.11, -0.35) | -0.23 (-0.39, -1.23) |
| NetLiqYoY | SOL | +0.23 (+0.77, +1.88) | -0.11 (-0.23, -0.56) |
| GlobalM2Accel | BTC | +0.07 (+0.18, +0.73) | -0.23 (-0.39, -1.57) |
| GlobalM2Accel | ETH | -0.06 (-0.22, -0.68) | -0.13 (-0.20, -0.63) |
| GlobalM2Accel | SOL | -0.04 (-0.13, -0.32) | -0.06 (-0.12, -0.29) |
| GlobalM2YoY | BTC | +0.02 (+0.06, +0.24) | -0.26 (-0.48, -1.93) |
| GlobalM2YoY | ETH | +0.06 (+0.22, +0.71) | -0.07 (-0.10, -0.32) |
| GlobalM2YoY | SOL | +0.16 (+0.70, +1.71) | -0.11 (-0.15, -0.37) |

n = 16 annual blocks for BTC, 10 for ETH, 6 for SOL (bounded by each asset's own price
history — SOL/ETH's shorter n makes their ICIR/t-stat noisier and lower-power, same
caveat this program already applies elsewhere to short-history assets).

**Whole-sample (single-shot, pooled) IC, for cross-reference against study 03's originally
published numbers:**

| driver | asset | h=1 | h=3 | h=6 | h=12 |
|---|---|--:|--:|--:|--:|
| NetLiqAccel | BTC | +0.12 | +0.14 | +0.14 | +0.14 |
| NetLiqAccel | ETH | +0.28 | +0.28 | +0.21 | +0.26 |
| NetLiqAccel | SOL | +0.37 | +0.37 | +0.31 | +0.41 |
| NetLiqYoY | SOL | +0.26 | +0.32 | +0.38 | +0.24 |
| GlobalM2Accel | SOL | +0.12 | +0.18 | +0.39 | +0.56 |

**These pooled numbers reproduce study 03's central qualitative claim cleanly (SOL > ETH >
BTC, monotonic, at essentially every driver/horizon combination) but not its exact
magnitudes** (e.g. study 03 originally published net-liq-momentum→ETH ≈ +0.33, →SOL ≈
+0.54; this recomputation's closest analog, `NetLiqAccel`, shows ETH +0.26–0.28, SOL
+0.31–0.41 — same direction/ranking, smaller magnitude). Two identified, non-mutually-
exclusive reasons, not fully reconciled here: (1) `NetLiqAccel` is study 06's own driver
definition (YoY-then-3mo-delta), not study 03's own bespoke "raw N-week % momentum"
transform for the same underlying series — related quantities, not identical ones; (2)
some of the same underlying price data was independently found to have been refreshed
2026-07-10 (study #23's data-drift discovery), which could plausibly also shift these
correlations modestly. Flagged as a fidelity gap for a future reconciliation pass, not
silently smoothed over — but it does not change the qualitative ranking, which is the
part of study 03's finding this section set out to formalize.

## 1b. Full breadth — 9 continuous-driver rules × all 28 assets, h=1 (primary, non-overlapping)

Per-rule aggregate across all 28 assets (median across assets with ≥3 valid annual IC
blocks) — same format as study 06's median-dSharpe breadth table, now in formal IC
vocabulary:

| rule | n assets | median mean-IC | median ICIR | median t-stat | n assets \|t\|≥2 |
|---|--:|--:|--:|--:|--:|
| GlobalM2 YoY>0 | 28 | +0.056 | **+0.198** | +0.61 | 5 |
| NetLiq accel>0 | 28 | +0.036 | +0.118 | +0.46 | 1 |
| NetLiq rising(3m) | 28 | +0.023 | +0.073 | +0.31 | 2 |
| Gold YoY>0 (risk proxy) | 28 | +0.014 | +0.050 | +0.20 | 3 |
| NetLiq YoY>0 | 28 | +0.013 | +0.042 | +0.18 | 2 |
| ISM rising | 28 | -0.016 | -0.055 | -0.23 | 0 |
| GlobalM2 YoY accel>0 | 28 | -0.021 | -0.073 | -0.30 | 2 |
| ISM>50 | 28 | -0.020 | -0.082 | -0.35 | 2 |
| USM2 YoY accel>0 | 28 | -0.039 | -0.146 | -0.57 | 3 |

**Two things worth flagging plainly:**

1. **The rule ranking reorders vs. study 06's own median-dSharpe breadth ranking.**
   Study 06 crowned `NetLiqAccel` (net-liq acceleration → risk-on) as the #1, most
   broadly-robust rule by median Sharpe uplift across the 28-asset universe. Here, ranked
   by median ICIR of the *raw continuous driver's* correlation with forward returns,
   **`GlobalM2YoY` edges it out** (ICIR +0.198 vs. +0.118). This is a real, if modest,
   divergence — not a contradiction, because the two measures are genuinely different
   things: study 06's dSharpe is the realized performance of a *thresholded, traded*
   binary rule (which folds in volatility-reduction effects, not just directional
   accuracy), while this table is the *raw predictive correlation* of the underlying
   continuous signal. They don't have to agree, and the fact that they only mostly agree
   (both rules are top-2 either way; `ISM*` rules are weak/negative in both) is itself a
   useful, non-trivial thing the formal IC framing surfaces that a plain restatement of
   study 06 would not have.
2. **Almost nothing here clears a conventional significance bar.** Summed across the whole
   9×28 grid, only **21 of 252 (rule, asset) pairs (8.3%)** reach `|t-stat| ≥ 2` — barely
   above the ~5% you'd expect from chance alone under a two-sided normal-approximation
   threshold with no real signal at all. This is a materially more cautious picture than
   the block-bootstrap-CI-flagged (✓) correlations reported in studies 01-06, which are
   computed on the full pooled monthly sample (much higher effective n, hence tighter
   CIs) — the annual-block IC series, by design, has far fewer degrees of freedom (6-18
   points per asset) and is correspondingly noisier. **This does not mean studies 01-06's
   pooled correlations are wrong** — the pooled numbers and their CIs are still valid,
   larger-sample estimates — but it does mean that, viewed as a genuine period-by-period
   time series with a formal ICIR, the "edge" found by any single rule/asset pair is far
   less consistently repeatable year-to-year than a single strong pooled correlation might
   suggest. This is a real methodological sharpening, not a restatement.

Full CSV: `results/ic_time_series_h1_summary.csv` (252 rows, one per rule×asset).

## 1c. Named-asset check (same assets study 06/19's guardrail used)

`NetLiqAccel`, h=1, annual-block IC/ICIR — direction should match study 06's dSharpe sign
(COIN/SOL/MARA/TSLA/ADA/MSTR positive; UUP/GLD negative):

| asset | mean IC | ICIR | t-stat | n years |
|---|--:|--:|--:|--:|
| COIN | +0.231 | +0.770 | +1.72 | 5 |
| SOL | +0.182 | +0.417 | +1.02 | 6 |
| MARA | +0.035 | +0.114 | +0.42 | 14 |
| TSLA | +0.151 | +0.373 | +1.54 | 17 |
| ADA | -0.008 | -0.024 | -0.07 | 8 |
| MSTR | -0.015 | -0.047 | -0.20 | 17 |
| UUP | -0.025 | -0.077 | -0.33 | 18 |
| GLD | +0.031 | +0.097 | +0.41 | 18 |

**6 of 8 signs match the guardrail direction** (COIN/SOL/MARA/TSLA positive as expected,
UUP negative as expected); **ADA and MSTR are essentially zero and flip sign** (both
`|t| < 0.5`, well within noise, not a real contradiction); **GLD flips weakly positive**
(`t=+0.41`, also noise-level). None of the "flipped" cases carry any statistical weight —
consistent with section 1b's broader finding that most individual pairs don't clear
significance at this granularity.

## 2. Bonus (b) — cross-sectional Spearman: `NetLiqAccel` dSharpe vs. net-liquidity beta

Recomputed each asset's `NetLiqAccel`-rule Sharpe uplift vs. buy&hold directly from
`study19_variant_returns_06.csv` (same formula as `macro_strategy_zoo.py`'s own Part 1),
correlated against each asset's own net-liquidity beta (study 05,
`liquidity_beta_ranking.csv`) across the 18 assets present in both artifacts (SPY, GLD,
SLV, NVDA, AAPL, MSFT, TSLA, AMZN, JPM, BTC, ETH, SOL, ADA, LINK, DOGE, MSTR, COIN, MARA):

**Cross-sectional Spearman ρ = +0.523 [+0.068, +0.843] (95% i.i.d. bootstrap CI, n=18,
5000 draws) ✓ — CI excludes 0.**

This directly confirms, with an actual number and a real CI (rather than an informal
claim), study 06's own qualitative statement that `NetLiqAccel` "helps risk assets... hurts/
ignores defensives": the highest-beta assets (SOL β=181, MARA β=20, ADA β=16, COIN β=14)
mostly show strongly positive dSharpe (+0.26 to +0.87), while the lowest/negative-beta
assets (GLD, SLV, JPM, AMZN) show flat-to-negative dSharpe (-0.21 to +0.05). The CI is
wide (n=18 is a modest cross-section) but excludes zero. This is one number, not a time
series — reported as a cross-sectional Spearman correlation with a bootstrap CI, not an
ICIR, per the task's own labeling requirement.

## Verdict — does the formal IC framing sharpen anything, or mostly restate?

**Both — it restates the big picture but genuinely sharpens two specific things.**

**What it restates (no new finding):** the broad-strokes story is unchanged. Net-liquidity
and Global-M2 drivers remain the strongest, most broadly-positive signals; ISM rules
remain weak-to-negative; crypto and high-liquidity-beta names (SOL, COIN, MARA, TSLA)
respond most strongly; BTC shows the weakest/most-decaying forward predictability
(consistent with study 03's "contemporaneous, not a genuine lead" finding, and with study
19's DSR analysis showing the flagship edge is borderline even at its strongest framing).
Nothing here overturns any standing conclusion in the README's Executive summary.

**What it genuinely sharpens (two specific, non-obvious findings):**

1. **The formal ICIR framing reorders the breadth ranking** — `GlobalM2YoY` edges out
   `NetLiqAccel` for the #1 spot by median ICIR of the raw continuous signal, even though
   `NetLiqAccel` remains #1 by study 06's realized-strategy dSharpe. This is a genuine,
   quantified divergence between "predictive correlation of the raw signal" and "realized
   trading-strategy Sharpe uplift of the thresholded rule" — a distinction the program's
   prior single-number reporting didn't separately surface.
2. **Only ~8% of (rule, asset) pairs clear a conventional |t|≥2 significance bar under a
   genuine, non-overlapping annual-block IC series** — barely above chance. This is a
   materially more cautious read on signal *stability* than the pooled, full-sample
   block-bootstrap CIs (✓ markers) reported throughout studies 01-06 convey on their own;
   it does not invalidate those pooled estimates, but it does add an honest caveat about
   how repeatable any single rule/asset edge has actually been, year to year, that a plain
   re-labeling exercise would not have produced.

The bonus cross-sectional check (b) adds a formal number and CI to a claim (study 06's
"helps risk assets, hurts defensives") that was previously only asserted qualitatively —
useful confirmation, not new information in direction, but a genuine upgrade in rigor
(ρ=+0.523, CI excludes 0, vs. an unquantified prose claim before).

## Caveats

Two of the 11 rules excluded from the continuous-driver table (`ComboM2AccelNetLiqYoY`,
`ISMLt50Rising`, no natural single continuous reading — see §0). Annual-block granularity
trades statistical power for genuine non-overlap; shorter-history assets (SOL n=6, ETH
n=10) have correspondingly noisier ICIR/t-stat estimates, same low-power caveat this
program already applies elsewhere. The whole-sample pooled IC numbers in §1a do not
exactly reproduce study 03's originally-published magnitudes (driver-definition and
data-refresh reasons identified, not fully reconciled — flagged as a follow-up). The
h=3/6/12 decay-curve blocks share the same overlapping-window caveat this program already
applies to its own multi-horizon lead-lag tables (a 12-month forward return inside a
12-month block is nearly one degree of freedom, not twelve). This is not a portfolio-level
backtest (no new trading rule is proposed or simulated) — the standing-baselines
requirement (`backtest/AGENTS.md`) does not apply.
