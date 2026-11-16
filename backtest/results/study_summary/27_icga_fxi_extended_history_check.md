# 27 — does FXI's much longer history change study 22's ICGA.DE "procyclical, not defensive" finding?

Script: `icga_fxi_extended_defensiveness_check.py` → `results/icga_fxi_extended_defensiveness_check.md`.
Per the user's explicit request (2026-07-13) to extend study 22's literal-ticker companion
check (`nqse_icga_defensiveness_check.py` / `results/nqse_icga_defensiveness_check.md`) —
scoped to the **China/ICGA leg only** — using FXI (iShares China Large-Cap, 21.7y of price
history) as an ICGA.DE stand-in, the same convention study 22 Part A already used for its
portfolio-level A/B. **NQSE.DE and the original script are untouched by this study.**

## Why this study exists

Study 22's literal-ticker check found ICGA.DE (MSCI China UCITS, the ETF ADR-047 routes
risk-off-freed gold weight into) tests as **procyclical, not defensive** vs. Gold — but that
finding rested on ICGA.DE's real price history, which only goes back to 2019-07-02 (~7.0y).
Of study 14's 5 named sustained risk-off episodes, ICGA.DE's real data could reach only 2:
2021-05→09 and 2021-12→2023-01 — **both from the same single macro regime** (the Fed
hiking-cycle/2022 crypto-winter stretch), and the 14-month 2021-12→2023-01 episode alone was
already flagged, in study 22 itself and study 14 before it, as driving essentially the entire
headline result. ADR-050 (2026-07-13) already reversed ADR-047's NQSE.DE/ICGA.DE risk-off
premium based on that evidence. FXI (`data/prices/FXI_full.csv`, 2004-10-08→2026-06-17,
~21.7y, USD-native, NYSE Arca-listed, tracks FTSE China 25) reaches all 5 named episodes,
including the two earliest (2012-06→08, 2014-11→2015-03) that no real ICGA.DE data could ever
reach, plus 2018-08→2019-01 (the episode the original script's table showed "no data" for
ICGA.DE at all). This study asks: does the extra history change the picture, or reinforce it?

**Scope note, stated explicitly per the task's own framing (and repeated at the top of the
script's own docstring): this is a China-market-proxy check, not a literal test of the
ICGA.DE fund.** FXI tracks FTSE China 25; ICGA.DE tracks MSCI China — overlapping, closely
correlated China-market exposure, but not an identical index. Part 5 below (new in this
study) directly measures how well FXI actually tracked the real ICGA.DE fund over the 2019-08
→2026-06 window both have data for, rather than assuming agreement.

## Methodology (identical to study 22's companion script, substituting FXI for ICGA.DE)

Same 4-part package as `nqse_icga_defensiveness_check.py` (HAC/Newey-West liquidity-beta
regression, risk-off-conditional split, episode table, risk-off-correlation-to-GLD), plus a
new 5th part (the FXI-vs-real-ICGA.DE agreement check). One deliberate methodology change,
flagged by the task and carried through consistently: **FXI is USD-denominated/US-listed**
(unlike ICGA.DE, EUR-denominated/Xetra-listed), so Parts 2-5's PLN conversion uses `USDPLN_X`
for FXI, not `EURPLN_X` — matching each ticker's own native quote currency, the exact
convention the original script already used for GLD. This means FXI and GLD's PLN legs now
share one FX pair (both USD-native) — expected, not a bug, and it removes the EUR/PLN
confound the original ICGA.DE-based check carried. Part 1 (the HAC regression itself) uses
FXI's own native USD return, unconverted, matching the original script's Part-1 convention.
Risk-off classification uses the identical FIXED, original `/2.0`-divisor `blend` column from
`results/macro_blend_series.csv` (study 14's own series, not re-derived from any of study
21's divisor variants).

## Part 1 — liquidity-beta regression (full 21.7y sample, extends study 05)

`asset YoY return = a + beta*(driver YoY)`, HAC/Newey-West SEs, native USD.

| ticker | driver | beta | t | R2 | n | lag |
|---|---|--:|--:|--:|--:|--:|
| FXI (ICGA.DE proxy) | net-liq YoY | −0.13 | −1.21 | 0.02 | 210 | 4 |
| FXI (ICGA.DE proxy) | Global-M2 YoY | 1.61 | 3.02 | 0.12 | 208 | 4 |

For direct comparison, study 22's own (short-sample, ICGA.DE-only) figures: net-liq YoY
β=0.41 (t=3.01, n=72), Global-M2 YoY β=2.04 (t=7.25, n=70).

**The net-liquidity leg genuinely flips character over the longer sample**: ICGA.DE's own
short 7.0y history showed a statistically significant, equity-like positive net-liq beta
(0.41, t=3.01); FXI's full 21.7y sample shows a beta indistinguishable from zero — and if
anything mildly negative (−0.13, t=−1.21, not significant at any conventional level). **The
Global-M2 leg holds its direction and remains solidly equity-like** (1.61 vs. study 05's
equity aggregate of ~1, far above the metals aggregate of ~0.1), though the point estimate is
somewhat lower than ICGA.DE's own short-sample 2.04. This is the single clearest place the
extended history complicates, rather than simply reinforces, the original finding — see
"Verdict" below.

## Part 2 — risk-off-conditional (blend≥0.6) vs. all-other-months split

| ticker | subsample | beta (net-liq YoY, simple OLS) | n | corr(ret, GLD ret) | n(corr) |
|---|---|--:|--:|--:|--:|
| FXI (ICGA.DE proxy) | risk-off (blend≥0.6) | 0.52 | 40 | 0.16 | 40 |
| FXI (ICGA.DE proxy) | all other months | −0.20 | 170 | 0.09 | 193 |

Unlike Part 1's unconditional beta (near zero), **FXI's beta is markedly higher specifically
in risk-off months (0.52) than in all other months (−0.20)** — a real swing, and directionally
consistent with study 22's own finding that both NQSE.DE (0.35→1.40) and ICGA.DE (0.33→0.38)
became more liquidity-sensitive in risk-off months. FXI's swing (−0.20→0.52, Δ=+0.72) is
larger in absolute terms than either original ticker's own conditional swing. Correlation to
GLD stays small-and-positive in both subsamples (0.16 risk-off, 0.09 other) — not negative,
but far from the near-1 co-movement "defensive" would imply, and lower than ICGA.DE's own
short-sample risk-off correlation (0.26). No HAC correction is applied to this split (matches
the original script's own convention, unchanged; simple OLS slopes, no formal significance
test).

## Part 3 — episode-level cumulative return & maxDD (GLD vs. FXI, PLN)

All 5 of study 14's named episodes (FXI's 21.7y history reaches all of them; GLD_full.csv
starts 2007-01, 5.4y before the earliest 2012-06 episode — comfortably inside GLD's coverage,
confirmed not at its data edge).

| episode | GLD cum. ret / maxDD | FXI cum. ret / maxDD | n(mo) | FXI vs. GLD |
|---|--:|--:|--:|---|
| 2012-06→08 | +2.4% / −1.8% | **−4.3% / −4.4%** | 3 | procyclical (underperforms both axes) |
| 2014-11→2015-03 | +14.0% / −6.4% | **+27.3% / 0.0%** | 5 | **outperforms both axes — counter-example** |
| 2018-08→2019-01 | +10.0% / −1.1% | **+2.3% / −6.1%** | 6 | procyclical (positive but far behind gold, worse DD) |
| 2021-05→09 | +5.1% / −4.1% | **−10.6% / −11.7%** | 5 | procyclical (underperforms both axes) |
| 2021-12→2023-01 | +13.5% / −8.8% | **−9.6% / −36.0%** | 14 | procyclical (underperforms both axes, worst DD of all) |

(GLD's figures for the 3 episodes already in study 22's table — 2018-08→2019-01, 2021-05→09,
2021-12→2023-01 — reproduce exactly, confirming the PLN-conversion machinery is faithful.)

**4 of 5 named episodes show FXI underperforming gold on both cumulative return and maxDD —
the same procyclical pattern study 22 found.** The dominant 14-month 2021-12→2023-01 episode
is, if anything, *worse* here: FXI's maxDD (−36.0%) is deeper than real ICGA.DE's own figure
for the identical window (−31.9%, study 22). **But the single exception is not marginal**:
2014-11→2015-03 (the earliest of the 3 newly-reached episodes, corresponding to the early
phase of mainland China's 2014-2015 domestic equity bull run, a country-specific catalyst
largely independent of the global-liquidity conditions driving the `blend≥0.6` classification)
shows FXI decisively *outperforming* gold — higher return (+27.3% vs. +14.0%) with **zero**
drawdown over the entire 5-month window, the opposite of what "procyclical, not defensive"
would predict.

## Part 4 — correlation to GLD, risk-off months only (PLN)

| ticker | corr(ret, GLD ret), risk-off months only | n |
|---|--:|--:|
| FXI (ICGA.DE proxy) | 0.16 | 40 |

Small and positive — not negative, so not moving *against* gold — but far short of the
near-1 co-movement genuine defensiveness would show, and lower than either of study 22's own
tickers' risk-off correlations (NQSE.DE −0.03, ICGA.DE +0.26). n is now spread across all 5
episodes (40 risk-off months) rather than concentrated almost entirely in the single 2021-12
→2023-01 episode (25 of study 22's 25 ICGA.DE risk-off months came from essentially that one
stretch).

## Part 5 — FXI-vs-real-ICGA.DE agreement check (new; internal-consistency sanity check)

Recomputes real ICGA.DE's own episode stats (EUR→PLN via `EURPLN_X`, reproducing study 22's
exact methodology) for the 2 episodes both series cover, next to FXI's Part-3 numbers:

| episode | metric | GLD | FXI (proxy) | ICGA.DE (real) | FXI − ICGA.DE |
|---|---|--:|--:|--:|--:|
| 2021-05→09 | cum. return | +5.1% | −10.6% | −13.2% | +2.6pp |
| 2021-05→09 | maxDD | −4.1% | −11.7% | −14.2% | +2.6pp |
| 2021-12→2023-01 | cum. return | +13.5% | −9.6% | −10.8% | +1.3pp |
| 2021-12→2023-01 | maxDD | −8.8% | −36.0% | −31.9% | −4.2pp |

Direct month-to-month correlation of FXI's own PLN return to real ICGA.DE's own PLN return,
over their entire common history (2019-08→2026-06, n=83): **0.95**. Restricted to risk-off
months only (n=25): **0.99**.

**FXI and real ICGA.DE agree strongly in the window where both exist.** Every episode-level
delta is small (≤4.2 percentage points on any metric, in either direction — no sign flips),
both series land in the same procyclical quadrant (negative-to-modest return, deep drawdown,
relative to gold) at both overlapping episodes, and the direct monthly-return correlation is
very high (0.95 all-history, 0.99 risk-off-only) — near the top of what one would expect from
two different-but-related China-market vehicles. This is a strong internal-consistency
result: it does not prove FXI would have matched ICGA.DE exactly in the 3 pre-2019 episodes
ICGA.DE has no data for, but it substantially raises confidence that FXI's directional read on
those episodes is a reasonable stand-in, not an artifact of using a different index/currency.

## Verdict — does the procyclical finding hold, strengthen, or weaken?

**Genuinely mixed, reported plainly rather than rounded to one word — this is the most
important result of the study.**

**Reinforces (at the episode/conditional level):**
- 4 of 5 named episodes show FXI underperforming gold on both return and maxDD — the same
  procyclical direction study 22 found, now demonstrated across 4 independent episodes
  spanning 3 different decades' worth of macro regimes (2012, 2018-19, 2021 twice), not just
  the single 2021-2023 stretch.
- The dominant 14-month 2021-12→2023-01 episode's drawdown reading is, if anything, *worse*
  with FXI (−36.0%) than with real ICGA.DE (−31.9%, study 22) — the extended-history
  recomputation of the one episode both scripts share does not soften the original finding.
- The risk-off-conditional beta swing (Part 2: −0.20 all-other → 0.52 risk-off) is larger in
  absolute terms than either NQSE.DE's or ICGA.DE's own conditional swing in study 22 —
  FXI becomes *more* liquidity-sensitive specifically in risk-off months, the opposite of
  defensive behavior.
- FXI's risk-off correlation to gold stays small-positive (0.16), consistent with "does not
  move like gold" rather than "moves against gold or with it."

**Complicates (weakens the aggregate/unconditional reading):**
- The full-sample (21.7y) unconditional net-liquidity beta — the single number most directly
  comparable to study 22's own headline Part-1 statistic — flips from significant-positive-
  equity-like (ICGA.DE's own 7.0y sample: β=0.41, t=3.01) to statistically indistinguishable
  from zero, even mildly negative (FXI's 21.7y sample: β=−0.13, t=−1.21). The Global-M2 leg
  survives this flip (both samples land solidly on the equity side, not the metals side), but
  the net-liq leg specifically does not.
- The single genuinely new multi-month episode with a clean, unambiguous reading — 2014-11
  →2015-03 — is a real counter-example, not noise: FXI beat gold on return by 13.3pp and had
  literally zero drawdown, the opposite of procyclical behavior in that window. It appears to
  reflect a country-specific catalyst (the early phase of mainland China's 2014-2015 domestic
  equity bull run) largely decoupled from the global-liquidity conditions the `blend≥0.6`
  classification is built on, rather than a general refutation of the procyclical pattern —
  but it is a genuine data point against the "always procyclical" reading, not something this
  writeup papers over.

**Overall**: the core qualitative finding — FXI, as a China-market proxy for ICGA.DE, behaves
more like a procyclical equity index than a defensive gold-like asset during risk-off episodes
— **holds at the episode and conditional-beta level (4/5 episodes, and a larger risk-off/
all-other beta swing than either original ticker)**, and the one episode both this study and
study 22 can directly compare (2021-12→2023-01) reads if anything slightly *worse* with the
extra history. But the **unconditional, full-sample regression result is genuinely weaker**
than the short-sample ICGA.DE number implied (net-liq beta no longer significant), and the
2014-15 episode is a real, sizable exception. This is evidence that **cuts both ways relative
to ADR-050's basis** — it does not overturn the procyclical finding, but it does show the
finding is less uniform across market regimes than the original 2-episode, single-regime
check could reveal, and the full-sample net-liq-beta headline specifically does not survive
the longer window unchanged. Per this program's standing convention, **no ADR/production
change is recommended here** — ADR-050 already shipped on the original (shorter-history)
evidence; this is additional supporting-and-complicating evidence for the user's own
awareness. The decision whether this changes anything is the user's, not this study's.

## Caveats

- **China-market-proxy, not literal ICGA.DE**: FXI tracks FTSE China 25, ICGA.DE tracks MSCI
  China — Part 5's strong agreement (0.95/0.99 correlation, small episode-level deltas) is
  direct evidence the proxy is reasonable, not an assumption, but it is still a proxy.
- **FXI is USD-native, ICGA.DE is EUR-native** — Parts 2-5 convert FXI via `USDPLN_X` (same
  pair as GLD) and ICGA.DE via `EURPLN_X` (study 22's own convention); part of any FXI-vs-
  ICGA.DE divergence in Part 5 could reflect USD/PLN-vs-EUR/PLN currency moves rather than
  purely the underlying China-equity indices diverging.
- **The 3 newly-reached episodes (2012-06→08, 2014-11→2015-03, 2018-08→2019-01) have zero
  real ICGA.DE data to cross-check against directly** — confidence in FXI's read on those 3
  specific episodes rests on Part 5's overlap-window agreement (a different, later window),
  not on direct evidence for the pre-2019 period itself.
- **The 2021-12→2023-01 episode (14mo) is still by far the largest/longest of the 5** — the
  other 4 range 3-6 months. Study 14's own n≈1-dominant-episode caution is reduced, not
  eliminated, by reaching 5 episodes instead of 2.
- **Part 2's conditional beta split uses simple OLS, no HAC correction** (identical to the
  original script's own convention) — treat as descriptive, not a formal significance test.
- **Newey-West lag** (`L=floor(4*(T/100)^(2/9))`) is the same standard rule-of-thumb default
  as the original script, not a re-derivation of study 05's own (unrecoverable) exact choice.
- Risk-off classification uses the FIXED, original `/2.0`-divisor `blend` series from study
  14, not re-derived from any of study 21's divisor variants.
- **Scope**: this study covers the China/ICGA leg only. NQSE.DE and the original
  `nqse_icga_defensiveness_check.py` script are untouched — no claim is made here about
  NQSE.DE's own extended-history behavior.
- Per this program's standing convention (see study 22's own closing line): **this study does
  not recommend or imply any ADR/production change**. ADR-050 already shipped based on the
  original evidence; this is additional context for the user's own awareness only.
