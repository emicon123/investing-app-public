# 18 — Methodology review: multiple-testing control, macro-liquidity frameworks, signal design, and the IC framework

**This is a METHODOLOGY REVIEW, not a new empirical trading result.** No new backtest was
run and no new script was written for this file — it re-reads studies 01-17 and
`backtest/AGENTS.md`, then researches established quant-finance technique (web sources
cited inline) to ask: given everything already found, is the *process* rigorous enough,
and where would real technique sharpen or discipline it? Verdicts below are judgment
calls grounded in the cited literature, not new numbers from this repo's data — where a
number is estimated, it's flagged as a rough, order-of-magnitude back-of-envelope
calculation from already-published study figures, not a fresh computation.

## Why this file exists

The program has now run 17 studies that, cumulatively, scanned a *lot* of variants
without ever formally correcting for it: 11 rules × 28 assets (study 06) = 308
rule-asset combinations; a 5-window scan (4/8/13/26/52w) × 3 assets in study 04; multiple
lag scans in study 03; trigger/trim-depth grids in studies 12/15; portfolio variant grids
in studies 07/14/16/17. The house style already catches some of this by hand — study 04
explicitly calls SOL's in-sample 1.40 Sharpe "an overfitting trap" once walk-forward
collapsed it to 0.12, and study 06's breadth-across-28-assets design is itself an
informal anti-overfitting device (a rule that helps 20/28 unrelated assets is less likely
to be pure noise than one cherry-picked for one asset). But none of this has been put in
formal statistical terms. Sections A-D below cover the four areas the user asked about;
each ends with (i) what the technique is, (ii) whether it's doable for free with data
already in `backtest/data/`, (iii) what it would concretely change for named existing
studies, and a verdict.

## A. Multiple-testing / overfitting control

### A1. Deflated Sharpe Ratio (DSR) — Bailey & López de Prado, 2014

**What it is, plainly:** an ordinary Sharpe ratio overstates skill whenever you tried
several variants and kept the best one — the *maximum* of several noisy estimates is
biased upward even if every variant has zero true edge. DSR asks "what Sharpe would I
expect from pure luck, given I tried N variants and my returns aren't perfectly normal
(they're skewed/fat-tailed, which crypto returns are — see study 01's own note on why
Spearman not Pearson is used here), and does my observed Sharpe clear that bar?" It
outputs a probability (0-1) that the true Sharpe ratio exceeds the "by-chance" threshold.

**Formula** (`SR₀` = expected max Sharpe under the null of N independent zero-skill
trials with cross-sectional Sharpe variance `V̂[SR]`, `γ` = Euler-Mascheroni ≈ 0.5772):

```
SR₀ ≈ √V̂[SR] · [(1-γ)·Φ⁻¹(1-1/N) + γ·Φ⁻¹(1-1/(N·e))]

DSR = Φ( (ŜR - SR₀)·√(T-1)  /  √(1 - γ₃·ŜR + (γ₄-1)/4 · ŜR²) )
```
`ŜR` = observed (per-period) Sharpe, `T` = number of return observations, `γ₃`/`γ₄` =
sample skewness/kurtosis of the strategy's returns, `Φ` = standard normal CDF. Both
halves are ~15 lines of `numpy`/`scipy.stats` — no paid tooling, and every input
(monthly/weekly return series, N = count of variants tried, T = sample length) already
exists in this repo's backtest outputs or is trivially derivable from the tables already
published in studies 04/06.

**Free/cheap:** yes, fully — `scipy.stats.norm`, nothing else. The only real cost is
bookkeeping: you have to have kept (or be willing to regenerate) the *per-variant*
return series, not just the summary Sharpe in the markdown table, since `V̂[SR]` needs
the cross-sectional spread of Sharpes across trials, and `γ₃`/`γ₄` need the actual return
distribution of the *chosen* variant.

**Rough back-of-envelope check on the flagship result (ETH net-liquidity timing, study
04):** the published OOS Sharpes for the three walk-forward runs in study 04 are BTC
1.00, ETH 0.81, SOL 0.12 (mean 0.64, sample std ≈0.38). Treating these three OOS runs
as the trial set (N=3) and plugging into the `SR₀` formula: `Φ⁻¹(1-1/3)≈0.43`,
`Φ⁻¹(1-1/(3e))≈1.16`, giving `SR₀ ≈ 0.38·[0.42·0.43 + 0.58·1.16] ≈ 0.32`. ETH's observed
0.81 clears that by a healthy margin (≈2.5×) — **at the narrow framing of "just these
3 walk-forward runs," ETH's edge looks real, not a lucky draw.**

But that N=3 framing understates the actual search. ETH wasn't picked out of 3 blind
draws — it was spotlighted only *after* study 06's 308-combination rule zoo (11 rules ×
28 assets) surfaced "net-liq acceleration → risk-on" as the best rule, and after study 03
scanned multiple lag windows to find where ETH/SOL's lead lives. If the effective trial
count behind "something in this liquidity-timing family works" is closer to N≈28 (study
06's asset breadth) with a plausible cross-sectional Sharpe-uplift spread of σ≈0.3-0.4
(study 06's own median ΔSharpe ranged from −0.34 to +0.10, with per-asset swings up to
+0.87 for COIN), the same formula gives `Φ⁻¹(1-1/28)≈1.81`, `Φ⁻¹(1-1/(28e))≈2.23`, so
`SR₀ ≈ σ·[0.42·1.81 + 0.58·2.23] ≈ σ·2.05` — **≈0.62 at σ=0.3, or ≈0.82 at σ=0.4.** That
upper estimate sits right on top of ETH's observed 0.81 OOS Sharpe.

**Honest reading:** this is exactly the kind of gap the user asked to check for, and the
napkin math says the margin is *real but thinner than a first glance at "0.65→0.81
Sharpe, survives walk-forward" suggests.* Under the narrowest, most literal framing (3
walk-forward runs) it clears easily; under a wider, more honest framing that counts the
whole search that led to spotlighting ETH, the by-chance bar and the observed result are
uncomfortably close. This is a rough estimate from published summary numbers, not a real
DSR computation (that needs the actual per-variant return series, which weren't
preserved once studies were distilled — see study 01's note that per-study scripts were
deleted after their findings were folded in). **Candidate #19 below scopes doing this
properly.**

### A2. Probability of Backtest Overfitting (PBO) / Combinatorially (Symmetric) Purged Cross-Validation (CSCV/CPCV) — Bailey, Borwein, López de Prado et al.

**What it is, plainly:** instead of one in-sample/out-of-sample split, CSCV carves the
full history into S contiguous blocks (S even, e.g. 10-16), forms every combination of
S/2 blocks as a "training" half and the complementary S/2 as "testing," and for **every**
combination asks: does the variant that looked best on the training half still rank above
the *median* variant on the testing half? Do this for all `C(S, S/2)` combinations (252
for S=10), and the fraction of combinations where the in-sample winner falls **below**
median out-of-sample is the PBO — literally "how often would picking the backtest's
best performer have burned you." A logit transform of the OOS-rank makes this a clean
distribution to integrate below zero.

**Recipe (concrete, ~1 page of Python, all free libraries — pandas/numpy/scipy):**
1. Build a T×N matrix of period returns for every variant tried (N = rule/window/asset
   combos, T = time periods).
2. Split into S contiguous blocks; for each of the `C(S, S/2)` combinations, concatenate
   its half into "train," the complement into "test."
3. Compute each variant's Sharpe on train and on test for that split.
4. Find the best-Sharpe variant on train; find its **rank** among all variants' Sharpes
   on test; convert to a relative rank ω ∈ (0,1); `λ = log(ω/(1-ω))`.
5. Repeat for all combinations → a distribution of λ. PBO = the fraction of that
   distribution below 0 (i.e., where the in-sample champion did *worse* than the OOS
   median).

**Free/cheap:** yes, entirely — no dependency beyond pandas/numpy/scipy, and it reuses
return series already produced by the fetch scripts in `backtest/`.

**What it would concretely change:**
- **Study 06** (11 rules × 28 assets) is the single best candidate: CSCV would let each
  asset's own history be block-split, giving a formal PBO for "would the process that
  picked NetLiq-accel as the winning rule have picked a fluke instead?" — a rigorous
  version of the breadth test that's already run informally.
- **Study 04**'s 5-window scan (4/8/13/26/52w) per asset is exactly the CSCV use case:
  window selection is a parameter search, and CSCV would quantify whether ETH's
  window-adaptive walk-forward process (re-picking best-Sharpe L each 13-week block) is
  meaningfully different from picking a lucky window by chance.
- **Studies 12/15** (trigger/trim-depth grids: (2.0,1.5)/(1.75,1.25)/(1.5,1.0)) are
  smaller grids (3 points) where CSCV is probably overkill — see A3's overkill
  discussion below.

### A3. White's Reality Check (2000) / Hansen's SPA test (2005)

**What it is, plainly:** a bootstrap procedure built for exactly this program's shape of
problem — "I tried a whole universe of rules against one benchmark (buy&hold); is the
single best one truly beating the benchmark, once I account for having tried all of
them?" White's Reality Check resamples the *joint* distribution of all rules' excess
returns over the benchmark and asks whether the best one's edge survives; Hansen's SPA
improves power by re-centering the null distribution (only including rules that
plausibly could win, not obviously-bad ones, which inflates power without inflating false
positives).

**Free/cheap:** yes in principle (stationary/block bootstrap over already-existing return
series, `arch` or hand-rolled block bootstrap — matches the block-bootstrap machinery
already used for correlation CIs in study 01/02), but **it's the heaviest-machinery
option of the three, and for a single-user personal project it's probably overkill.**
DSR + a PBO/CSCV pass on the two or three studies that actually did wide parameter
scanning (06, 04) gets 90% of the same insurance for a fraction of the implementation
and maintenance cost. Reality Check/SPA earns its keep in an institutional context
running dozens of live strategies against a compliance/due-diligence bar; it's not
proportionate here. **Verdict: skip for now, note it exists, don't build it** — the same
"don't gold-plate a single-user project" judgment call this program has already made
elsewhere (e.g. study 12/13's rejections of machinery that didn't earn its complexity).

## B. Established macro-liquidity frameworks — does this repo's construction match practice?

**Lyn Alden's "Fed net liquidity"** is defined exactly as `Fed balance sheet (WALCL) −
Treasury General Account (TGA) − Reverse Repo (RRP)` — this is *precisely*
`backtest/data/macro/FED_NET_LIQUIDITY.csv`'s formula (`WALCL - WDTGAL - RRPONTSYD`, per
`data/macro/README.md`). **Good news, not a gap: this repo's net-liquidity construction
already matches a well-known, widely-cited practitioner framework exactly, not an
invented proxy.** No change needed here.

**Michael Howell / CrossBorder Capital's Global Liquidity Index (GLI)** is a materially
richer construction than this repo's Global M2: it aggregates ~80 economies' **official
liquidity** (central bank reserves/balance sheets), **financial-sector liquidity**
(shadow banking, repo market conditions, collateral availability), and **private-sector
liquidity** (cross-border capital flows), not just central-bank M2 aggregates, and Howell
frames "liquidity" as *the willingness and ability of credit providers to lend* rather
than a monetary stock — closer to a credit-conditions index than an M2 sum. Two concrete
gaps this surfaces for `data/macro/GLOBAL_M2.csv`:

1. **Coverage is 5 countries (US/Euro/China/Japan/UK) vs. Howell's ~80** — this repo's
   README already flags the GLI itself as a **paid product, correctly not scraped**; the
   5-country sum is a reasonable, transparent, free substitute, but it's missing repo
   market conditions, shadow-banking credit, and collateral availability entirely — none
   of which have a free, reliable, keyless proxy the way M2 does via FRED/ECB/BoE APIs.
   This is a real, currently-unaddressable gap given the cost-free constraint, not a bug.
2. **FX conversion is a straight monthly-mean spot-rate sum, not trade-weighted** — per
   `data/macro/README.md`, each country leg is "converted to USD at the monthly-mean FX
   rate." A currency move (e.g. a JPY depreciation) can shrink the USD-converted Japan
   M2 leg even when yen-denominated liquidity didn't change at all — this is FX noise
   contaminating a liquidity signal, and it's a real, cheaply-fixable-in-part nuance
   Howell's methodology (which uses more nuanced weighting, not just raw FX-converted
   levels) implicitly avoids. **A partial free fix**: report/blend each leg's own
   *published, local-currency YoY %* (already computed for the Japan leg per the
   README's YoY-chaining note) into a GDP- or trade-weighted composite growth rate,
   rather than only differencing the raw FX-converted USD level. This wouldn't require
   any new data source — every input series already exists in `data/macro/`.

**Net verdict:** the net-liquidity leg is already textbook-correct; the Global M2 leg is
a reasonable, honestly-labeled, free approximation of a genuinely richer paid framework,
with one concrete, cheap improvement available (weight by published local-currency YoY
rather than raw FX-level differencing) that a future study could test without any new
data fetch.

## C. Signal design upgrades

### C1. Time-series momentum literature — a real caveat for this program's own findings

Moskowitz, Ooi & Pedersen (2012, "Time Series Momentum") is the canonical reference for
"trend/direction as a systematic signal" — directly the same shape of claim as "net-liq
acceleration → risk-on." The important nuance from later literature revisiting that
paper: **subsequent work found the original result was driven largely by
volatility-scaling the positions (sizing each contract to a constant ~40% annualized
vol), not by the momentum signal itself** — without vol-scaling, time-series momentum and
plain buy-and-hold have statistically similar returns. **This is directly relevant self-
critique for studies 04/06/14**: none of this program's timing rules use vol-targeted
position sizing — they're binary (asset vs. cash) or a continuous directional blend, but
never scaled by the asset's own realized volatility. It's worth an explicit caveat added
to studies 04/06/14's verdicts: **some of the apparent edge from "de-risking during
net-liq deceleration" could be a vol-reduction effect in disguise** (moving to cash
mechanically lowers realized vol, which lowers Sharpe's denominator, which flatters the
Sharpe ratio even absent genuine directional skill) **rather than pure directional
timing skill** — the walk-forward CAGR uplift (ETH 22%→36%) is less ambiguous evidence
of genuine skill than the Sharpe uplift alone, since CAGR isn't mechanically inflated by
lower vol the way Sharpe is.

### C2. Volatility-targeting / vol-scaled position sizing as an overlay

**What it is, plainly:** instead of a binary "all-in / all-cash" or a directional
0-100% blend, scale the *size* of the position (or, in this program's buy-only DCA
context, the *contribution weight*) inversely to the asset's own realized volatility —
calmer assets get bigger allocations, choppier ones get smaller, independent of the
directional call.

**Free/cheap:** yes, trivially — realized vol is computable from the close-price CSVs
already in `data/prices/` (rolling std of daily/weekly log returns), no new data source,
no paid vol index needed (a VIX-style implied-vol input would cost money; realized vol
from free daily closes is the standard low-cost substitute and is what the TSMOM
literature itself validates on).

**Concrete design for this repo:** the real production DCA mechanic
(`backtest/AGENTS.md` "Rebalancing rule") is deficit-proportional: `buy_i = A × Q_i /
Q_pos`. A vol-targeted variant would risk-weight the catch-up instead of pure-dollar-
weighting it: `buy_i = A × (Q_i / σ_i) / Σⱼ(Qⱼ / σⱼ)` for every underweight position —
still buy-only, still zero for at/above-target positions (respects ADR-045's hard
buy-only constraint), but a laggard in a historically volatile asset (BTC, miners) would
get relatively less of the catch-up money per unit of deficit than an equally-underweight
but calmer asset (gold, FXI), on the theory that a dollar of "catch-up" in a 80%-annualized-
vol asset carries more risk than a dollar of catch-up in a 15%-vol one. This is a
genuinely new, well-scoped, free-data-computable idea — not yet tested anywhere in this
program. **Scoped as candidate #20 below.**

### C3. HMM / Markov-switching regime detection as an alternative to the manual z-score blend

**What it is, plainly:** instead of a hand-built continuous formula
(`blend = clip(0.5 - smoothed_z/2, 0, 1)`, ADR-044), fit a 2-state Gaussian Hidden Markov
Model directly to the same two inputs already used (net-liq YoY acceleration z-score,
Global-M2 YoY acceleration z-score) and let the model estimate transition probabilities
and each state's own mean/variance from the data, outputting `P(risk-off state)` each
month as a probabilistic, data-driven blend factor instead of an arbitrary linear clip
formula.

**Free/cheap:** yes — `hmmlearn` is free/open-source (`pip install hmmlearn`), and the
inputs are the exact same two z-scored monthly macro series ADR-044 already computes; no
new data fetch.

**Important caveat, not just an upside:** study 14 already flagged its own single-episode
problem — "n≈1 real episode drives the result" (the 2021-12→2023-01 stretch is the only
long, deep risk-off episode the whole net-liq history (2008→now, ~220 months) contains).
An HMM's risk-off state would be estimated from that *same* one dominant episode — it
does not solve the small-sample problem, it just moves the same small-sample estimation
into a fancier box. This is worth trying as an alternative functional form (maybe it
produces smoother or more responsive transitions than the manual clip formula), but it
should not be sold as "more rigorous, therefore fixes the n≈1 caveat" — it doesn't.
**Scoped as the lowest-priority candidate (#22) below**, specifically because of this
caveat.

## D. IC (information coefficient) framework — formalizing, not rebuilding

**What it is, plainly:** in factor investing (Grinold & Kahn's "Fundamental Law of Active
Management," `IR = IC × √Breadth`), the Information Coefficient is the (typically
Spearman) rank correlation between a signal's forecast and the realized forward return,
computed **cross-sectionally at each point in time** (across many assets, one time step),
producing a **time series of period-by-period ICs**. From that time series you get: mean
IC (average skill), IC standard deviation, the **IC Information Ratio** (`ICIR = mean(IC)
/ std(IC)`, annualized by `√(periods/yr)`), an IC **t-stat** (`ICIR × √n`) for a formal
significance test, and an **IC decay curve** — IC computed at multiple forward horizons
(1mo, 3mo, 6mo, 12mo) plotted together to see over what horizon a signal stays predictive
before decaying to noise.

**How this differs from what's already been done here:** this program's correlations
(study 01's stated method: Spearman + block-bootstrap 95% CI) are mostly **single-shot,
whole-sample statistics** — one correlation number for "macro driver vs. forward asset
return" over the full history, not a period-by-period cross-sectional time series. Study
06's breadth test (11 rules × 28 assets) is the closest thing to a cross-sectional
design already in this repo, but it reports **Sharpe uplift and %-of-assets-beaten**, not
a formal IC/ICIR/t-stat. Study 03's lead-lag scan (BTC contemporaneous, ETH ~1-2mo, SOL
~3mo, Global-M2-accel leads BTC ~2-3mo) is *exactly* an IC-decay-curve's worth of
information, just not labeled or plotted that way.

**Free/cheap:** yes, and cheaper than any other item in this review — **it needs no new
data and, in study 03's case, may need no new computation at all**, just relabeling and
replotting already-computed lag-by-lag Spearman correlations as a standard IC-decay
chart, plus adding the mean/ICIR/t-stat formalism on top of numbers that mostly already
exist. This is the highest ratio of "sharpens existing findings" to "implementation
cost" of anything in this review.

**Concrete changes this would make:**
- **Study 03**: replot its lead-lag findings as a proper IC-decay curve per asset (IC on
  the y-axis, forward horizon in months on the x-axis) — turns "ETH leads ~1-2mo, SOL
  leads ~3mo" from a descriptive claim into a chart with a formal peak-IC horizon and a
  t-stat at that peak, directly comparable to how a factor-investing report would present
  the same finding.
- **Study 06**: recompute its 11-rule zoo as **cross-sectional Spearman IC per rule per
  month** (signal value vs. next-month return, ranked across the 28-asset universe),
  producing a per-rule IC time series → mean IC, ICIR, t-stat. This gives a single,
  formally-tested number to rank the 11 rules by, replacing (or sitting alongside) the
  current "median ΔSharpe / %beat" heuristic — and it is the natural companion diagnostic
  to run *together with* a DSR/PBO pass on the same rule zoo (candidate #19), since IC
  gives the signal-quality read and DSR/PBO gives the overfitting-risk read on the same
  search.

## Prioritized next studies (scope only — not executed in this file)

1. **#19 — CSCV/PBO + real DSR on studies 04 and 06's parameter/rule searches.**
   Rebuild the per-variant return series for study 04's 5-window scan (BTC/ETH/SOL) and
   study 06's 11-rule × 28-asset zoo (both fetchable from data already in
   `backtest/data/`, no new sources), then run the CSCV recipe from A2 and compute a real
   DSR (A1) for the ETH flagship result — replacing this file's back-of-envelope
   estimate with an actual number. **Highest priority**: it directly stress-tests the
   single most load-bearing empirical claim in the whole program (README's "one-line
   takeaway").
2. **#20 — Vol-targeted / risk-parity-weighted catch-up DCA** (C2's concrete design:
   `buy_i = A × (Qᵢ/σᵢ) / Σ(Qⱼ/σⱼ)`, buy-only, still zero at/above target). Benchmark
   against the real deficit-proportional mechanic (`Qᵢ/Q_pos`, the current standing
   baseline per `backtest/AGENTS.md`) on the user's own portfolio and, per the standing
   baselines convention, the study 16/17 reference portfolios too. Cheap, well-
   established in the literature, and directly touches how the live algorithm could
   evolve — second priority because it's both low-effort and could matter for production,
   not just for research hygiene.
3. **#21 — Cross-sectional IC framework** applied to study 06's rule zoo (per-rule
   IC/ICIR/t-stat, replacing/supplementing the ΔSharpe-based ranking) and a replotted
   IC-decay curve for study 03's lead-lag findings. Cheapest item on this list (largely
   relabeling + light new computation on existing data) and pairs naturally with #19
   (same rule zoo, two complementary diagnostics).
4. **#22 — 2-state Gaussian HMM as an alternative `blend_factor` construction**,
   benchmarked against the live ADR-044 z-score formula on the exact same walk-forward
   protocol study 14 used (BASELINE/CURRENT/REVERSED-style 3-way comparison, 5 start-date
   windows). **Lowest priority, explicitly exploratory** — per C3's caveat, this doesn't
   solve the "n≈1 real risk-off episode" small-sample problem that already limits study
   14's confidence; it's worth trying as an alternative functional form, not as a fix for
   that caveat.

## Caveats

- This review's DSR back-of-envelope numbers (section A1) are **order-of-magnitude
  estimates built from already-published summary statistics** (the three OOS Sharpes in
  study 04, the ΔSharpe spread in study 06), not a computation over raw per-variant
  return series — those series were deleted once their studies were distilled (per study
  01's own note on script lifecycle). Treat the ≈0.62-0.82 "by-chance" range as
  illustrative of the *order of magnitude* of the multiple-testing problem, not as a
  citable result — candidate #19 exists specifically to replace it with a real number.
- Section B's critique of the Global M2 FX-conversion approach is a real, identified gap,
  but the proposed partial fix (blend published local-currency YoY rather than raw FX-
  level differencing) has not been tested — it's a hypothesis for a future study, not a
  validated improvement.
- None of sections A-D constitute a recommendation to add all of this machinery
  permanently to every study — A3 (Reality Check/SPA) is explicitly rejected as overkill
  for a single-user project, matching this program's existing house style of rejecting
  machinery that doesn't earn its complexity (see studies 12/13). The other items are
  scoped as candidate studies, not standing requirements — `backtest/AGENTS.md`'s
  existing standing-baseline conventions are not superseded by anything in this file.

## Verdict

The program's empirical instincts have already been sound — it caught its own
overfitting once (SOL, study 04), rejected several ideas honestly when they didn't pan
out (studies 09/12/13), and self-corrected a real mechanic bug across three studies
(the fixed-split-vs-catch-up DCA correction). What's been missing is **formal language
and a formal bar for "how many things did I try to find this"** — Deflated Sharpe and
CSCV/PBO are cheap (pure Python, existing data), directly answer that question, and the
back-of-envelope check above suggests the program's single flagship tradeable claim (ETH
net-liquidity timing) is **probably real but has a thinner margin over chance than it
looks at first glance** once the *whole* search that led to it is counted, not just the
final 3-asset walk-forward comparison. The macro-liquidity framework check was
reassuring rather than alarming: Fed net liquidity already matches Lyn Alden's published
definition exactly, and Global M2's gap vs. Howell's richer GLI is a known, currently
unaddressable (paid-data) limitation rather than a bug — with one concrete, free,
untested improvement available (FX-noise reduction via local-currency-weighted
blending). Vol-targeting and the IC framework are the two highest-value, lowest-cost
upgrades here — both reuse data and computation this repo already has; a full HMM regime
model and White's Reality Check are the two items most likely to be more machinery than
this project's single-user scale actually needs.

_Written 2026-07-10. Sources consulted (web research, not scraped into this repo):
Bailey & López de Prado, "The Deflated Sharpe Ratio" (davidhbailey.com preprint,
SSRN 2460551); Bailey, Borwein, López de Prado & Zhu, "The Probability of Backtest
Overfitting" (davidhbailey.com preprint); White, "A Reality Check for Data Snooping"
(2000) and Hansen, "A Test for Superior Predictive Ability" (2005), as summarized in
Hsu & Kuan's technical-analysis re-examination; Michael Howell / CrossBorder Capital's
Global Liquidity Index methodology (public commentary, e.g. SimpleVisor's "Is Global
Liquidity Peaking?"); Lyn Alden's Fed net-liquidity framework (public commentary);
Moskowitz, Ooi & Pedersen, "Time Series Momentum" (2012, JFE) and its vol-scaling
follow-up critique; Grinold & Kahn's Fundamental Law of Active Management (IC/ICIR/
Fundamental Law literature, CFA-level summaries)._
