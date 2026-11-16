# 08 — On-chain (MVRV) as a top-timing signal

Script: `onchain_mvrv_study.py` → `results/onchain_mvrv_study.md`. Data: Coinmetrics
community API `CapMVRVCur` (free tier, no paid RealizedCap needed — MVRV ships
pre-computed), daily since inception (BTC 2010-07-18, ETH 2015-08-08) →
`data/onchain/{BTC,ETH}_MVRV.csv`. Question: does on-chain sharpen the "start
selling" trigger beyond the macro de-risk gate already shipped (net-liq accel &
Global-M2 accel, file 07)?

## Method
Raw MVRV thresholds decay across cycles (BTC yearly max: 2013 ≈5.9, 2017 ≈4.7, 2021
≈4.0 — market matures faster than euphoria re-inflates MVRV), so an absolute cutoff
isn't stable over a 15y study. Used a **no-lookahead expanding percentile rank** of
monthly MVRV vs its own history to date (12mo burn-in) as the primary signal, with
raw-level thresholds as a cross-check. Spearman IC vs forward return (1/2/3/6/12mo),
computed over the *same* window as the macro signal for a fair side-by-side.

## Result: MVRV percentile does NOT clear the bar
| signal (fwd 3m) | BTC IC | ETH IC | significant? |
|---|--:|--:|---|
| MVRV percentile | +0.10 | +0.09 | no (t<1.5, **wrong sign** too — momentum, not reversal) |
| macro risk-off (shipped) | −0.17 | −0.33 | **yes** (t=−2.2 / −3.7) |

MVRV-percentile IC is small, statistically insignificant at every horizon out to 6
months, and **positive** (stretched MVRV → *higher*, not lower, near-term forward
return — euphoria feeds euphoria before it breaks). It only flips to the expected
negative sign at 12 months, and only for ETH, marginally (t=−2.1). The already-shipped
macro risk-off signal is far stronger across the board (BTC t up to −2.4, ETH up to
−3.7) — this re-confirms 07's finding that net-liq/Global-M2 deceleration is the real
signal, this time against a genuinely different candidate.

**Threshold sensitivity (fwd 3m) confirms it's not a usable rule:**
| BTC MVRV pctl | n fired | mean fwd 3m | ETH MVRV pctl | n fired | mean fwd 3m |
|---|--:|--:|---|--:|--:|
| ≥0.70 | 35 | +33% | ≥0.70 | 12 | +59% |
| ≥0.85 | 8 | +34% | ≥0.85 | 2 | +110% |
| ≥0.90 | **2** | **−46%** | ≥0.90 | 0 | — |

Same pattern with raw MVRV level (≥2.0 → mostly still-positive forward returns; only
≥3.5 flips negative, at n=2). **The exact cycle tops (2013, 2017/2021) do show up** at
the most extreme percentile/level — but at n=2 firings across 15 years of BTC history,
that's not a statistically usable trigger, it's "the model correctly labels the two
tops with hindsight-grade precision and nothing in between." Every looser threshold
that fires often enough to matter still has strongly positive average forward returns
— using it live would mean selling into ongoing rallies most of the time it fires.

**Composite test (macro risk-off OR MVRV pctl≥0.85):** the two signals **never
co-occur in-sample** (BTC: 59 macro-risk-off months vs 8 MVRV-extreme months, 0
overlap; ETH: 40 vs 2, 0 overlap) — they're catching different things at different
times. OR-ing them in doesn't sharpen the macro trigger, it **dilutes** it: ETH
macro-alone mean fwd 3m = −4.3%, composite = +1.2% (worse — the added MVRV-only
months aren't actually bad months, they pull the average up).

## Verdict: reject as an automated trigger
Same standard applied to ISM (rejected, file 02-06) and the miners-cap ISM schedule
([[btc-equity-ism-rule-backtest]], rejected): insufficient signal, insufficient
sample at the threshold where it would matter, and it would **weaken** the shipped
macro gate if fused in via OR. **Do not wire MVRV into `MacroDeriskGate`.**

**What MVRV IS good for:** a qualitative confirming flag for *manual* judgment — the
two BTC cycle tops in this data (2013, 2017/2021) both show up at the extreme end of
this metric, matching the user's own stated approach ("I'm selling when I decide to").
Worth surfacing as a **read-only display metric** (current BTC/ETH MVRV + percentile)
in the app's macro/rebalancing view, alongside the macro state panel, as one more
input for the user's own discretionary sell decisions — not as something that moves
targets automatically.

## Caveats
BTC pre-2011 excluded (realized cap too thin, MVRV noise up to 146×); ETH history is
short (one full cycle + one partial); Coinmetrics community tier serves MVRV directly
but not the realized-cap/market-cap components, so no independent robustness check
against a self-computed Z-score was possible without a paid tier.
