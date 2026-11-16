# 11 — On-chain correlation analysis: redundancy, stability, and LTH/STH vs the macro gate

Script: `onchain_correlation_analysis.py` → `results/onchain_correlation_analysis.md`.
Same data as file 10 (`bitcoin-data.com` native series, 2022-07+, plus the
Coinmetrics-derived 2010+/2015+ MVRV-Z/NUPL cross-check and the shipped macro
net-liq/Global-M2 gate) — no new fetching. This is a correlation/BI pass, not
another pooled-IC backtest: (1) how redundant are the on-chain metrics with each
other, (2) does file 10's pooled "strong 2022+" IC hold steady across the window or
is it concentrated in a sub-period, (3) does LTH/STH supply ratio (the one signal
file 10 didn't reject) add anything beyond the shipped macro gate, and (4) any
signal the pooled-IC approach in file 10 could have missed. Does **not** re-litigate
raw-MVRV-percentile-as-top-timer (file 08) or re-run file 10's portfolio backtest.

## Headline

- **MVRV-Z, NUPL, and LTH-SOPR are effectively one signal**, not three: pairwise
  Spearman rho 0.90–0.99. File 10 reported all three as "significant reversal
  signals" — that's really **one underlying factor** (holder profitability /
  cycle-valuation depth) showing up three ways, not three independent
  confirmations.
- **File 10's pooled "strong 2022+" IC is itself unstable within the window.**
  Rolling 15-month windows show the valuation-cluster IC was near-zero in
  2022-23, peaked strongly negative (−0.75 to −0.82) across the 2023-10→2025-06
  windows, then **collapsed to ~0 (and briefly flipped positive)** in every
  window since late 2024. The "one cycle, not several" caveat in file 10 was an
  understatement — it wasn't even stable *within* the one cycle available.
- **LTH/STH supply ratio is the outlier, in a good way**: the only signal with a
  100%-sign-consistent rolling IC (never flips across all 12 windows), and its
  partial correlation with forward returns (controlling for the macro composite
  score) is **essentially unchanged from its raw correlation** at every horizon —
  the strongest evidence yet that it carries information genuinely independent
  of the shipped macro gate, not a re-expression of it.
- **New candidate ruled out**: SOPR/STH-SOPR minus LTH-SOPR "divergence spread"
  showed a significant 6-12m IC, but it correlates at **rho = −0.999** with
  LTH-SOPR alone — it's a mechanical restatement, not a new signal.
- No new de-risk trigger found. Main value of this pass: formally collapsing
  redundant signals, quantifying instability of the reversal cluster, and giving
  the LTH/STH-vs-macro question (raised qualitatively in file 10) a real answer:
  **orthogonal, worth combining as a conviction overlay — but not as a sell
  trigger**, which also explains (not contradicts) why file 10's OR/AND
  portfolio fusion underperformed.

## 1. Cross-correlation matrix among the on-chain metrics (native, 2022-07 → 2026-07, n=49)

| signal | SOPR | LTH-SOPR | STH-SOPR | MVRV-Z | NUPL | LTH/STH ratio | LTH 3mo%chg | STH 3mo%chg |
|---|--:|--:|--:|--:|--:|--:|--:|--:|
| SOPR | +1.00 | +0.69 | +0.70 | +0.71 | +0.75 | −0.07 | −0.23 | +0.27 |
| LTH-SOPR | +0.69 | +1.00 | +0.21 | **+0.90** | **+0.90** | −0.27 | −0.26 | +0.29 |
| STH-SOPR | +0.70 | +0.21 | +1.00 | +0.28 | +0.34 | +0.26 | −0.03 | +0.05 |
| MVRV-Z (native) | +0.71 | +0.90 | +0.28 | +1.00 | **+0.99** | −0.34 | −0.31 | +0.34 |
| NUPL (native) | +0.75 | +0.90 | +0.34 | +0.99 | +1.00 | −0.30 | −0.32 | +0.34 |
| LTH/STH supply ratio | −0.07 | −0.27 | +0.26 | −0.34 | −0.30 | +1.00 | +0.67 | −0.70 |
| LTH supply 3mo%chg | −0.23 | −0.26 | −0.03 | −0.31 | −0.32 | +0.67 | +1.00 | **−0.99** |
| STH supply 3mo%chg | +0.27 | +0.29 | +0.05 | +0.34 | +0.34 | −0.70 | −0.99 | +1.00 |

**Near-collinear (|rho| ≥ 0.85) — treat as one signal, not several:**
- MVRV-Z vs NUPL: +0.99 (both derived from the same market-cap/realized-cap
  spread — expected, but confirms they add zero incremental information over
  each other in any future ensemble).
- LTH-SOPR vs MVRV-Z / NUPL: +0.90 / +0.90 — this is the less obvious one.
  LTH-SOPR is nominally a *transaction*-based metric (realized profit ratio of
  coins moved) while MVRV-Z/NUPL are *stock*-based (unrealized profit across all
  supply), yet in this sample they move almost in lockstep. Practically:
  **LTH-SOPR, MVRV-Z, and NUPL form a single cluster** — a future signal design
  should pick at most one representative, not stack all three as "confirming"
  evidence.
- LTH supply 3mo-chg vs STH supply 3mo-chg: −0.99 — mechanical complements (LTH
  and STH supply are two sides of the same coin-age split), already implicitly
  known but worth stating: don't treat these as two signals either.

**Genuinely distinct:**
- **STH-SOPR** is the one member of the SOPR family that stands apart (rho
  0.21–0.34 with everything else) — but per file 10's pooled IC (reproduced
  here in the underlying `onchain_signals_study.md`), it also has **no
  significant standalone predictive power at any horizon** (max |t| = 1.2).
  Independence from the other signals doesn't make it useful on its own.
- **LTH/STH supply ratio** is mildly negatively correlated with the valuation
  cluster (−0.27 to −0.34) — related but not redundant, and (see §3) it's the
  one with independent predictive content.

**Net effect**: of the 8 signals tested, there are really only **~4 independent
axes**: (a) the MVRV-Z/NUPL/LTH-SOPR valuation-cycle factor, (b) blended SOPR
(partially overlapping with (a), rho 0.71–0.75), (c) STH-SOPR (independent, but
empty), (d) LTH/STH supply ratio (independent, and the only one with a track
record).

## 2. Is the pooled 2022+ IC stable, or driven by one sub-period?

Rolling 15-month windows, stepped quarterly, Spearman IC vs BTC forward 3-month
return (12 windows spanning 2022-07 → 2026-06):

| window | MVRV-Z | NUPL | LTH-SOPR | SOPR | LTH/STH ratio |
|---|--:|--:|--:|--:|--:|
| 2022-07..2023-09 | −0.11 | −0.12 | −0.02 | −0.19 | +0.23 |
| 2022-10..2023-12 | −0.10 | −0.11 | −0.17 | −0.22 | +0.36 |
| 2023-01..2024-03 | −0.09 | −0.10 | −0.16 | −0.22 | +0.70 |
| 2023-04..2024-06 | −0.34 | −0.34 | −0.42 | −0.16 | +0.75 |
| 2023-07..2024-09 | −0.64 | −0.63 | −0.41 | −0.42 | +0.86 |
| 2023-10..2024-12 | **−0.75** | **−0.74** | −0.57 | −0.48 | +0.80 |
| 2024-01..2025-03 | **−0.80** | **−0.76** | −0.44 | −0.53 | +0.50 |
| 2024-04..2025-06 | **−0.81** | **−0.82** | −0.43 | −0.63 | +0.38 |
| 2024-07..2025-09 | −0.76 | −0.60 | −0.45 | −0.60 | +0.32 |
| 2024-10..2025-12 | +0.06 | +0.18 | +0.19 | +0.02 | +0.30 |
| 2025-01..2026-03 | +0.00 | +0.04 | +0.19 | −0.06 | +0.08 |
| 2025-04..2026-06 | +0.09 | +0.15 | +0.07 | +0.04 | +0.27 |

**Reading**: the valuation-cluster IC was **weak-to-negligible through all of
2022-23**, then **strengthened sharply and peaked** across the windows centered
on late-2023 through mid-2025 (exactly the post-2022-bottom rally → 2024-25 top
→ correction arc), and has since **collapsed to ~0 (briefly flipping positive)**
in every window since Q4 2024. File 10's pooled 2022-2026 number is a blended
average that's carried almost entirely by ~18 months in the middle of the
sample — the most recent ~18 months show essentially no relationship at all.

| signal | sign matches pooled direction | min IC | max IC |
|---|--:|--:|--:|
| MVRV-Z (native) | 75% of windows | −0.81 | +0.09 |
| NUPL (native) | 75% | −0.82 | +0.18 |
| LTH-SOPR | 75% | −0.57 | +0.19 |
| SOPR | 83% | −0.63 | +0.04 |
| LTH/STH supply ratio | **100%** | +0.08 | +0.86 |

LTH/STH ratio is the only signal that never flips sign across all 12 windows —
but note its magnitude is also **fading in the most recent windows** (0.50 →
0.38 → 0.32 → 0.30 → 0.08 → 0.27 in the last six), so "never flips" should not
be read as "guaranteed to hold" — it's the most robust of the bunch, not
bulletproof.

## 3. LTH/STH supply ratio vs the shipped macro gate — orthogonal or redundant?

Sample: 2022-07 → 2026-05 (47 months).

**3a. Contemporaneous correlation** (does LTH/STH track the same underlying
regime as the macro inputs?):

| pair | rho | t |
|---|--:|--:|
| LTH/STH level vs net-liq accel | +0.41 | 3.0 |
| LTH/STH level vs Global-M2 accel | +0.21 | 1.4 |
| LTH/STH level vs macro risk-off (0/1) | −0.38 | −2.7 |
| LTH/STH 3mo-chg vs net-liq accel | +0.22 | 1.4 |
| LTH/STH 3mo-chg vs Global-M2 accel | +0.29 | 2.0 |
| LTH/STH 3mo-chg vs macro risk-off (0/1) | −0.41 | −2.9 |

Moderate, statistically real (t≈3) but far below the ≥0.85 "same signal"
threshold used in §1 — LTH/STH shares some regime information with the macro
gate but is not a restatement of it.

**3b. Conditional IC** — LTH/STH's own IC split by macro state:

| regime | months | fwd 1m | fwd 2m | fwd 3m | fwd 6m | fwd 12m |
|---|--:|--:|--:|--:|--:|--:|
| macro risk-off = 0 (risk-on) | 33 | +0.20 | +0.44 | **+0.59** (t=4.0) | +0.61 (t=3.9) | +0.51 (t=2.8) |
| macro risk-off = 1 | 14 | +0.12 | +0.23 | +0.33 (t=1.2, n.s.) | +0.59 (t=2.5) | +0.55 (t=2.0) |

At short horizons LTH/STH is stronger and clearly significant specifically
*inside risk-on* months (when the macro gate is quiet) and weaker/insignificant
at 1-3m *inside risk-off* months (small n=14 caveat applies); by 6-12m the two
regimes converge to similar IC. Reads as: LTH/STH adds the most differentiation
precisely in the periods where the macro gate has nothing to say.

**3c. Partial correlation** — LTH/STH vs forward return, controlling for the
macro composite score (rank-based partial Spearman):

| horizon | raw IC | partial IC (macro controlled) | n |
|---|--:|--:|--:|
| 1m | +0.29 | +0.24 | 47 |
| 2m | +0.45 | +0.42 | 46 |
| 3m | +0.54 | +0.55 | 45 |
| 6m | +0.54 | **+0.61** | 42 |
| 12m | +0.58 | +0.52 | 36 |

Partial IC tracks raw IC almost exactly at every horizon (and is even slightly
*higher* at 6m) — controlling for the macro signal does **not** explain away
LTH/STH's predictive power. This is the clearest evidence in this study that
the two are complementary, not duplicative.

**3d. Two-way split** (macro state × LTH/STH median split → mean fwd 3m
return; descriptive, full-sample median, not a live rule):

| macro state | LTH/STH state | months | mean fwd 3m |
|---|---|--:|--:|
| risk-off = 0 | below median | 14 | +0.3% |
| risk-off = 0 | at/above median | 17 | **+29.6%** |
| risk-off = 1 | below median | 9 | **−8.6%** |
| risk-off = 1 | at/above median | 5 | +18.2% |

Even conditioning on the macro state, LTH/STH's high/low split still separates
forward returns by a large margin in both regimes (small n in the last two
cells — directional, not conclusive on its own, but consistent with 3a-3c).

**Verdict for §3**: LTH/STH ratio is **orthogonal enough to be worth combining**
with the macro gate, not a redundant restatement of it. This also explains,
rather than contradicts, file 10's portfolio-level finding that OR/AND-fusing
LTH-SOPR/on-chain into the *de-risk trigger* underperformed the macro-alone
gate: the fusion tested there treated an on-chain signal as another sell
trigger. LTH/STH specifically is a **stay-allocated/buy-conviction** signal
(positive IC = momentum continuation, not reversal) — asking it to fire a
de-risk flag is the wrong mechanism for what it measures. If it's ever wired
in, it belongs as an independent conviction overlay (e.g. modulating DCA
tilt/confidence within the crypto sleeve) alongside the existing macro gate,
not fused into the same OR/AND sell trigger.

## 4. Other signals the pooled-IC approach in file 10 might have missed

**4a. SOPR-family "divergence spread" — ruled out as mechanical, not new.**
`SOPR − LTH-SOPR` and `STH-SOPR − LTH-SOPR` both show a significant positive IC
at 6-12m (+0.36 to +0.58, same magnitude as LTH-SOPR alone but sign-flipped)
that looked promising as a fresh "late-cycle divergence" signal. Checked
directly: both spreads correlate at **rho = −0.9995 with LTH-SOPR itself** —
because SOPR and STH-SOPR are comparatively flat/near-1 while LTH-SOPR is the
volatile component, the "spread" is just `constant − LTH-SOPR` in disguise.
**Not independent — reject as a separate candidate**, it inherits LTH-SOPR's
own one-cycle caveat (§1/§2) rather than adding anything.

**4b. Regime-dependent IC (bull vs bear, split on trailing-6m BTC return, not
calendar halves):**

| signal | regime | months | fwd 3m IC | fwd 6m IC |
|---|---|--:|--:|--:|
| MVRV-Z | bear (trail 6m<0) | 18 | −0.05 | −0.46 |
| MVRV-Z | bull (trail 6m≥0) | 30 | −0.48 | −0.55 |
| NUPL | bear | 18 | −0.05 | −0.46 |
| NUPL | bull | 30 | −0.43 | −0.49 |
| LTH-SOPR | bear | 18 | +0.09 | −0.31 |
| LTH-SOPR | bull | 30 | −0.45 | −0.47 |
| SOPR | bear | 18 | +0.04 | −0.10 |
| SOPR | bull | 30 | −0.26 | −0.27 |
| **LTH/STH ratio** | bear | 18 | **+0.70** | **+0.75** |
| **LTH/STH ratio** | bull | 30 | +0.41 | +0.34 |

The valuation-cluster reversal signal (MVRV-Z/NUPL/LTH-SOPR) mostly "works" at
3m only when the market had already been **rising** into the reading (bull
regime IC strongly negative; bear regime IC ≈ 0 at 3m) — i.e. on this data it
reads better as "trim into strength" than "time the exact bottom," converging
with the bear-regime IC only by 6m. **LTH/STH ratio shows the opposite,
favorable pattern**: it's *more* robust in down-trending regimes (+0.70/+0.75)
than in up-trending ones (+0.41/+0.34) — it's most informative exactly when a
stay-allocated/conviction signal is most useful (during drawdowns), a genuinely
complementary property to the reversal cluster.

**4c. ETH cross-check** (derived MVRV-Z/NUPL, full 2015-2026 overlap, n=120-132):
BTC vs ETH MVRV-Z rho=+0.82, NUPL rho=+0.57 — both assets' on-chain valuation
metrics move almost in lockstep (shared market beta dominates the derived
construction). Reinforces file 10's finding that on-chain doesn't differentiate
ETH's risk profile from BTC's — the macro signal does that.

## Verdict

- **Collapse MVRV-Z / NUPL / LTH-SOPR into one signal** in any future design —
  don't count agreement among them as three independent confirmations, it's
  one factor read three ways (rho 0.90-0.99).
- **That one factor's "strong 2022+" IC is concentrated in ~18 months
  (2023-10→2025-06)**, not the full window — it has decayed to ~0 in every
  rolling window since Q4 2024. Sharper, more concrete evidence for file 10's
  "one-cycle, don't wire it in" conclusion, not a reversal of it.
- **LTH/STH supply ratio remains the standout finding**: 100%-sign-stable
  across 12 rolling windows, its IC survives partialling out the macro
  composite score almost unchanged, is stronger specifically in the macro
  gate's quiet (risk-on) periods and in down-trending regimes — real evidence
  of being additive, not redundant. Still: magnitude is fading in the most
  recent year (rolling IC 0.50→0.08→0.27 in the last several windows) — treat
  as "the best on-chain candidate so far," not "proven durable."
- **SOPR-family divergence spread: rejected**, mechanically redundant with
  LTH-SOPR (rho −0.9995), not a new signal.
- **No new automated trigger to wire in.** This pass's value is diagnostic:
  fewer independent signals than file 10 implied, a concrete timeline showing
  *when* the reversal cluster's edge existed (and that it's currently gone),
  and a real (not just plausible) answer that LTH/STH is complementary to,
  not a substitute for, the shipped macro gate — which also explains why
  fusing it as a sell-trigger (file 10's portfolio test) underperformed: wrong
  mechanism for what the signal measures, not evidence the signal is empty.
- **Pending**: the BTC address-balance cohort fetch (>100/>1000 BTC bands) had
  not landed in `data/onchain/` as of this write-up (2026-07-03) — no
  `BALANCE`/`ADDR` files present. Worth a follow-up correlation pass once
  available; not blocking this study's conclusions.

## Caveats

Same small-sample caveats as file 10 apply throughout (native series n=36-49
months, one cycle); rolling-window IC uses overlapping 15-month windows so
adjacent windows are highly autocorrelated (read the *trajectory*, not each
window as an independent draw); the partial-correlation method (§3c) is a
first-order rank-based approximation (removes shared *linear-in-ranks*
variance with a single macro score, not a full multivariate control) —
adequate for an exploratory orthogonality check, not a substitute for a proper
regression/backtest if this is ever taken further; §3d's median split uses the
full-sample (in-sample) median, descriptive only, not a live rule.
