# Quant backtest rigor — methodology reference

Deeper technical material backing `SKILL.md`'s checklist. Read the section you were
pointed to, not the whole file, unless doing a first-time deep read. All techniques here
are free/open-source (`numpy`/`scipy`/`pandas`, and `hmmlearn` for the optional HMM
note) — nothing here requires a paid API or dataset, consistent with this project's
cost-free constraint.

## Table of contents

1. [Deflated Sharpe Ratio & PBO](#deflated-sharpe-ratio--pbo)
2. [Volatility Targeting](#volatility-targeting)
3. [Macro-Liquidity Frameworks](#macro-liquidity-frameworks)
4. [IC Framework](#ic-framework)
5. [Regime detection (HMM) — optional, lower priority](#regime-detection-hmm--optional-lower-priority)
6. [Further reading](#further-reading)

## Deflated Sharpe Ratio & PBO

### Deflated Sharpe Ratio (DSR) — Bailey & López de Prado, 2014

**Plain terms:** an ordinary Sharpe ratio overstates skill whenever several variants
were tried and the best one was kept — the *maximum* of several noisy estimates is
biased upward even when every variant has zero true edge. DSR asks: "given I tried N
variants, and my returns aren't perfectly normal (skewed/fat-tailed — true of crypto
returns, which is exactly why this repo already prefers Spearman over Pearson, see
`01_data_and_methods.md`), does my observed Sharpe clear the bar I'd expect from pure
luck?" It returns a probability (0-1) that the true Sharpe exceeds that by-chance
threshold.

**Exact formula** (`SR₀` = expected max Sharpe under N independent zero-skill trials
with cross-sectional Sharpe variance `V̂[SR]`; `γ ≈ 0.5772` Euler-Mascheroni):

```python
import numpy as np
from scipy.stats import norm

def expected_max_sharpe(V_SR, N):
    """V_SR: cross-sectional variance of the N trials' Sharpe ratios."""
    gamma = 0.5772156649
    sigma = V_SR ** 0.5
    term1 = (1 - gamma) * norm.ppf(1 - 1 / N)
    term2 = gamma * norm.ppf(1 - 1 / (N * np.e))
    return sigma * (term1 + term2)

def deflated_sharpe_ratio(SR_hat, SR_0, skew, kurtosis, T):
    """SR_hat: observed per-period Sharpe. SR_0: expected_max_sharpe() output.
    skew/kurtosis: sample skew/kurtosis of the chosen variant's returns.
    T: number of return observations for the chosen variant."""
    numerator = (SR_hat - SR_0) * (T - 1) ** 0.5
    denominator = (1 - skew * SR_hat + (kurtosis - 1) / 4 * SR_hat ** 2) ** 0.5
    return norm.cdf(numerator / denominator)
```

**Corrected 2026-07-12** (caught during Phase-2/study-19 design review): the skew/kurtosis
variance-adjustment term in the denominator uses `SR_hat` (the observed strategy's own
Sharpe), not `SR_0` (the benchmark being tested against). This is the Probabilistic Sharpe
Ratio (PSR) formula from Bailey & López de Prado — `DSR = PSR[SR_0]`, i.e. PSR evaluated at
a benchmark of `SR_0`, but the standard-error adjustment inside it always reflects the
*strategy's own* return distribution (its own skew/kurtosis apply to its own Sharpe
estimator), never the benchmark's. The earlier version of this file had `SR_0` in the
denominator, which would silently shift the reported probability — fixed here before any
study computes a real DSR number against it.

Both functions are the entire implementation — no dependency beyond `numpy`/`scipy`.

**Worked example already run for this repo** (`study 18`, back-of-envelope, not a fresh
computation — the underlying per-variant return series were not preserved once earlier
studies were distilled, see `01_data_and_methods.md`'s note on script lifecycle): study
04's three walk-forward OOS Sharpes (BTC 1.00, ETH 0.81, SOL 0.12) give `σ_SR≈0.38`. At
the narrow framing (N=3, just these three runs), `SR₀≈0.32` — ETH's 0.81 clears this
comfortably (~2.5×). But ETH was spotlighted only after study 06's 308-combination rule
zoo (11 rules × 28 assets) and study 03's lag scan pointed at it — a more honest N is
closer to 28 (study 06's asset breadth), which with a plausible `σ_SR≈0.3-0.4` (study
06's own median-ΔSharpe spread) gives `SR₀` in the range **0.62-0.82** — right on top of
the observed 0.81. **Conclusion for future studies: always ask which framing of N is
honest before trusting a walk-forward Sharpe uplift, and prefer computing this properly
(over the real per-variant series) rather than reasoning about it after the fact — keep
the return series a study is built from until this check has been run, not just the
summary table.**

### Probability of Backtest Overfitting (PBO) / Combinatorially Symmetric Cross-Validation (CSCV)

**When to reach for this instead of/alongside DSR:** when N is large enough (dozens+)
that a single SR₀ threshold feels too coarse, or when the concern is specifically
"would the *process* that selected the winning rule have picked a fluke instead" rather
than just "is this one Sharpe number too high." Study 06's 11×28 rule zoo is the
textbook candidate in this repo.

**Recipe** (all free — `pandas`/`numpy`/`scipy`, reuses return series already produced
by the fetch scripts in `backtest/`):

1. Build a T×N matrix of per-period returns, one column per variant tried (rule ×
   asset, or rule × window — whatever the actual search space was).
2. Split the T periods into S contiguous, non-overlapping blocks (S even, e.g. 10-16;
   more blocks = more combinations = smoother PBO estimate, at more compute cost).
3. For every combination of S/2 blocks as "train" and the complementary S/2 as "test"
   (`C(S, S/2)` combinations total — 252 for S=10), compute every variant's Sharpe on
   both halves.
4. Find the best-Sharpe variant on train; find its **rank** among all variants' test
   Sharpes; convert to relative rank `ω ∈ (0,1)`; logit-transform: `λ = log(ω/(1-ω))`.
5. Repeat across all combinations → a distribution of `λ`. **PBO = fraction of that
   distribution below 0** — i.e., how often the in-sample champion did *worse* than the
   OOS median. Lower PBO = less overfitting risk.

**Note on scope:** for the 3-point trim-parameter grids already in this program (studies
12/15 — `(2.0,1.5)/(1.75,1.25)/(1.5,1.0)`), full CSCV is likely overkill; it earns its
keep on wider scans (dozens+ of variants), not small, a-priori grids.

### What's explicitly *not* recommended here

**White's Reality Check (2000) / Hansen's SPA test (2005)** — a bootstrap framework for
"is the single best of many strategies truly beating a benchmark, once you account for
having tried all of them." Real, well-established technique, but the heaviest machinery
of the three multiple-testing tools researched for this program. For a single-user
personal project, DSR + a PBO pass on the widest scans (study 06, and study 04's window
scan) gets most of the same insurance at a fraction of the implementation/maintenance
cost. Skip building this unless a future study specifically needs to defend a result
against an external/adversarial audience — it's disproportionate machinery otherwise,
the same judgment call this program has already made for other over-engineered ideas
(see studies 12/13's rejections).

## Volatility Targeting

**Plain terms:** instead of a binary all-in/all-cash rule or a purely directional blend,
scale the *size* of a position (or, in this repo's buy-only DCA context, the
*contribution weight*) inversely to the asset's own realized volatility — calmer assets
get relatively more allocation, choppier ones relatively less, independent of the
directional call. This is the standard risk-parity idea, and is the mechanism that later
research found actually drives most of the well-known "time-series momentum" academic
result (Moskowitz, Ooi & Pedersen, 2012) — without vol-scaling, that strategy's returns
were statistically similar to plain buy-and-hold.

**Free/cheap:** yes — realized vol is a rolling standard deviation of log returns from
the close-price CSVs already in `data/prices/`. No paid implied-vol index (VIX-style)
needed; realized vol from free daily/weekly closes is the standard substitute the
academic literature itself validates on.

**Concrete design compatible with this repo's real DCA mechanic:** the production
algorithm (`backtest/AGENTS.md` "Rebalancing rule") is deficit-proportional:
`buy_i = A × Q_i / Q_pos` for every underweight position, zero otherwise. A vol-targeted
variant risk-weights the catch-up instead of dollar-weighting it:

```
buy_i = A × (Q_i / σ_i) / Σ_j (Q_j / σ_j)      for every Q_i > 0, else 0
```

where `σ_i` is asset i's trailing realized volatility. Still buy-only, still respects
ADR-045 (no auto-sell), still zero for at/above-target positions — just changes *how
much* of the catch-up money a laggard gets per unit of deficit, discounting laggards in
historically volatile assets (BTC, miners) relative to equally-underweight but calmer
ones (gold, FXI). Not yet tested anywhere in this program (scoped as candidate study #20
in `18_methodology_review.md`).

**Diagnostic to run on any existing/new timing rule:** if a rule's reported edge is
Sharpe-only and doesn't also show up as a CAGR improvement, treat that as a flag that the
"edge" may be a vol-reduction artifact (lower realized vol mechanically inflates Sharpe's
denominator) rather than genuine directional skill.

## Macro-Liquidity Frameworks

### Lyn Alden's "Fed net liquidity"

`Fed balance sheet (WALCL) − Treasury General Account (TGA/WDTGAL) − Reverse Repo
(RRP/RRPONTSYD)`. This is a well-known, widely-cited public framework — and this repo's
`data/macro/FED_NET_LIQUIDITY.csv` (`WALCL - WDTGAL - RRPONTSYD`, per
`data/macro/README.md`) already matches it **exactly**. No change needed; just know it's
grounded in a named framework, not an invented proxy, when writing this up.

### Michael Howell / CrossBorder Capital's Global Liquidity Index (GLI)

A materially richer construction than a simple M2 sum: aggregates ~80 economies' **(1)
official liquidity** (central bank reserves/balance sheets), **(2) financial-sector
liquidity** (shadow banking, repo market conditions, collateral availability), and **(3)
private-sector liquidity** (cross-border capital flows) — Howell frames "liquidity" as
*the willingness and ability of credit providers to lend*, closer to a credit-conditions
index than a monetary aggregate. **It is a paid product — do not attempt to scrape or
fully replicate it**; this repo's README already correctly avoids that
(`data/macro/README.md`: "the bgeometrics chart... is a paid product and is not
scraped").

Two concrete, known gaps between this repo's free `GLOBAL_M2.csv` (5-country: US/Euro
area/China/Japan/UK, summed at monthly-mean spot FX) and Howell's GLI, worth naming
explicitly in any writeup rather than conflating the two:
1. **Coverage** — 5 countries vs. ~80, and no repo-market/shadow-banking/collateral
   dimension at all (no free, reliable, keyless proxy exists for those — an
   acknowledged, currently-unaddressable limitation, not a bug).
2. **FX conversion** — this repo converts each leg at the raw monthly-mean spot rate,
   not trade-weighted or otherwise adjusted; a currency move (e.g. JPY depreciation) can
   mechanically shrink the USD-converted Japan leg even when yen-denominated liquidity
   didn't change, contaminating the liquidity signal with FX noise. **An untested,
   free, concrete partial fix**: blend each leg's own published *local-currency YoY %*
   (already computed for the Japan leg via YoY-chaining, per `data/macro/README.md`)
   into a GDP- or trade-weighted composite growth rate, instead of only differencing the
   raw FX-converted USD level — no new data source required, every input already exists
   in `data/macro/`.

## IC Framework

**Plain terms** (Grinold & Kahn's Fundamental Law of Active Management,
`IR = IC × √Breadth`): the Information Coefficient is the (typically Spearman) rank
correlation between a signal's forecast and the realized forward return, computed
**cross-sectionally at each point in time** (many assets, one time step), giving a
**time series of period-by-period ICs**. From that series: mean IC (average skill), IC
standard deviation, the **IC Information Ratio** (`ICIR = mean(IC) / std(IC)`,
annualized by `√(periods/yr)`), an **IC t-stat** (`ICIR × √n`) for formal significance,
and an **IC-decay curve** — IC at multiple forward horizons (1/3/6/12mo) plotted
together to see how long a signal stays predictive.

**How this differs from what this repo already does:** the existing Spearman +
block-bootstrap-CI machinery (`01_data_and_methods.md`) computes mostly **single-shot,
whole-sample** correlations (one number for "macro driver vs. forward return" over the
full history), not a period-by-period cross-sectional IC time series. Study 06's
11-rule × 28-asset breadth test is the closest existing design to a proper
cross-sectional IC computation, but it reports Sharpe uplift / %-of-assets-beaten, not
IC/ICIR/t-stat.

**Free/cheap — the cheapest item in this whole reference:** needs no new data, and in
study 03's case may need no new computation at all — just relabel/replot already-
computed lag-by-lag Spearman correlations as an IC-decay chart, and add mean/ICIR/t-stat
on top of numbers that mostly already exist. Concretely: (a) replot study 03's lead-lag
findings (BTC contemporaneous, ETH ~1-2mo lead, SOL ~3mo lead) as an IC-decay curve per
asset; (b) recompute study 06's 11-rule zoo as a proper cross-sectional Spearman IC per
rule per month (signal vs. next-month return, ranked across the 28-asset universe) to
get a formally-tested per-rule IC/ICIR/t-stat, directly comparable across rules and a
natural companion to a PBO pass on the same rule zoo.

## Regime detection (HMM) — optional, lower priority

**Plain terms:** fit a 2-state Gaussian Hidden Markov Model directly to the same inputs
ADR-044 already z-scores (net-liq YoY acceleration, Global-M2 YoY acceleration), letting
the model estimate transition probabilities and each state's mean/variance from data,
outputting `P(risk-off state)` each month as a probabilistic alternative to the manual
`blend = clip(0.5 - smoothed_z/2, 0, 1)` formula. Free (`hmmlearn`, open source), no new
data (same two monthly macro series ADR-044 already computes).

**Important caveat before treating this as an obvious upgrade:** study 14 already flagged
its own result as driven by essentially **one** long, deep risk-off episode
(2021-12→2023-01) in the ~220-month net-liquidity history since 2008. An HMM's risk-off
state would be estimated from that *same* single dominant episode — fitting a fancier
model does not solve a small-sample problem, it just moves the same small-sample
estimation into a fancier box. Worth trying as an alternative functional form (may
produce smoother/more responsive transitions than the manual clip formula), but should
not be sold as "more rigorous, therefore fixes the n≈1 caveat" — it doesn't. Lowest
priority of the ideas in this reference for exactly this reason.

## Further reading

Full citations and the complete methodology review this skill was distilled from:
`backtest/results/study_summary/18_methodology_review.md`. That file also contains the
prioritized list of scoped-but-not-yet-executed follow-up studies (#19-22) referenced
above.
