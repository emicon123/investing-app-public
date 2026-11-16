# 20 — Vol-targeted / risk-parity-weighted catch-up DCA

Script: `vol_targeted_dca_backtest.py` (copy of study 14's
`portfolio_macro_blend_backtest.py` — study 14 itself is unchanged) →
`results/vol_targeted_dca_backtest.md`.

Candidate #20 from the methodology review (`18_methodology_review.md` §C2 /
`.claude/skills/quant-backtest-rigor/references/methodology.md` "Volatility
Targeting"): the time-series-momentum literature found much of the apparent edge
in directional timing strategies comes from vol-scaling the position size, not
the direction call — this study tests whether risk-weighting the real
production catch-up DCA mechanic (instead of dollar-weighting it) does anything
similar here.

## What changed vs. study 14

Only the monthly catch-up **allocation** step. Everything else — RISK_ON/RISK_OFF
tables, ADR-044 blend construction, the 2.0x/1.5x trim-to-gold rule, 15bps cost,
1000 PLN + 1000 PLN/month, the 5 start-date windows (1/3/5/8/13y) — is
byte-for-byte identical to study 14.

- **Dollar-weighted (control, = study 14's existing mechanic):**
  `buy_i = A * Q_i / Q_pos` for every underweight position.
- **Vol-weighted (new):**
  `buy_i = A * (Q_i / sigma_i) / sum_j(Q_j / sigma_j)` for every underweight
  position, where `sigma_i` = trailing realized vol of asset i's own monthly log
  return, 12-month rolling window, **shifted by 1 month so no lookahead** (the
  value used to size month t's contribution only reflects returns realized
  through month t-1).
- **Fallback for <12 months of trailing history** (new listing, or CASH which
  is permanently exempted — no return series, no vol): that asset gets exactly
  the dollar amount the pure dollar-weighted control would have given it (same
  `Q_pos` denominator as the standard mechanic); the remaining budget then
  splits vol-weighted across whichever assets do have 12mo+ of history.

## Variant matrix — fixed N=4, decided up front, not a search

`{BASELINE, CURRENT (ADR-044 blend)} x {dollar-weighted (control), vol-weighted
(new)}`. Only one lookback window (12 months) was tried for `sigma_i` — no
other window was scanned. Because this is a controlled, a-priori 4-cell
comparison (below the skill's N≥~5 threshold for the data-mining gut check),
that check does not formally apply here — noted explicitly rather than run
performatively.

## Sanity check: is the realized-vol ranking plausible?

Median annualized trailing-12mo vol, over each asset's own available history:
GLD 17%, QQQ 15%, FXI 18%, MSTR 40%, BTC 66%, miners 92%, ETH 92%, DOGE 101%,
COIN 88%, SOL 112%, SUI 110%. This is the expected ordering (gold/large-cap
equity-ETF/China-ETF lowest, crypto/crypto-equities highest, MSTR in between) —
the vol-weighting machinery is discounting laggards in the right assets before
looking at any return numbers.

## Headline results

Cell = XIRR%/yr (maxDD%) [annualized Sharpe = mean/std of the monthly
nav-growth-factor return chain x sqrt(12), no risk-free subtraction — see
"Sharpe convention" caveat below]:

| variant | 13y | 8y | 5y | 3y | 1y |
|---|--:|--:|--:|--:|--:|
| BASELINE / dollar (control) | 43 (−39) [1.00] | 43 (−40) [1.07] | 26 (−52) [0.74] | 25 (−22) [1.11] | −16 (−22) [0.18] |
| BASELINE / vol (new) | 42 (−38) [1.07] | 41 (−41) [1.10] | 25 (−49) [0.70] | 24 (−21) [1.06] | −15 (−20) [0.05] |
| CURRENT / dollar (control) | 38 (−22) [1.07] | 37 (−22) [1.24] | 24 (−30) [0.89] | 22 (−15) [1.22] | −7 (−14) [0.54] |
| CURRENT / vol (new) | 36 (−21) [1.18] | 35 (−22) [1.28] | 23 (−27) [0.89] | 20 (−14) [1.14] | −8 (−12) [0.38] |

## CAGR-vs-Sharpe diagnostic (the required check)

| mode | window | ΔXIRR (pp, vol − dollar) | ΔSharpe (vol − dollar) |
|---|---|--:|--:|
| BASELINE | 13y | −0.8 | +0.07 |
| BASELINE | 8y | −1.6 | +0.03 |
| BASELINE | 5y | −1.0 | −0.04 |
| BASELINE | 3y | −1.3 | −0.05 |
| BASELINE | 1y | +0.8 | −0.13 |
| CURRENT | 13y | −1.3 | +0.11 |
| CURRENT | 8y | −1.3 | +0.04 |
| CURRENT | 5y | −1.0 | −0.01 |
| CURRENT | 3y | −1.8 | −0.08 |
| CURRENT | 1y | −0.2 | −0.17 |

Averaged across all 5 windows: BASELINE mode's mean ΔXIRR = −0.78pp, mean
ΔSharpe = −0.02; CURRENT mode's mean ΔXIRR = −1.12pp, mean ΔSharpe = −0.02. In
**8 of 10** mode×window cells, ΔXIRR is negative; **only 4 of 10** cells show a
(small) positive ΔSharpe, and every one of those 4 is at a long window (13y or
8y) — and in every one of those 4 cells, CAGR is simultaneously *down*, never
flat or up. **No cell in the whole grid shows a genuine joint CAGR-and-Sharpe
improvement.**

## Cross-check: 60/40 classic portfolio (SPY/AGG, BUYHOLD only)

Same dollar-vs-vol A/B, reusing `classic_portfolios_backtest.py`'s named
weights/tickers (not an import — a small self-contained function in the new
script), BUYHOLD mode only:

| window | ΔXIRR (pp) | ΔSharpe |
|---|--:|--:|
| 13y | −0.0 | −0.00 |
| 8y | −0.0 | −0.00 |
| 5y | −0.0 | −0.00 |
| 3y | −0.1 | −0.04 |
| 1y | −0.3 | −0.15 |

Effectively no effect at all on this diversified, 2-asset, lower-vol-dispersion
book — reinforcing that whatever small pattern shows up on the user's own
concentrated crypto-heavy portfolio is not a generic property of vol-weighted
catch-up DCA; it needs a wide vol spread across the underweight assets in a
given month to matter, and 60/40's two legs (equity vs. investment-grade bonds)
don't span nearly as wide a vol range as GLD/QQQ/FXI vs. BTC/ETH/SOL/miners/
COIN/SUI/DOGE.

## Why the effect is this small (a plausible mechanism, not just a null result)

The RISK_ON/RISK_OFF target table already assigns much smaller weight to the
low-vol assets (GLD 30%, QQQ 5%, FXI 2.5%) than to the high-vol crypto sleeve
(~50% combined) — so a typical month's dollar deficit `Q_i` for an underweight
crypto position is already several times larger than a gold/QQQ/FXI deficit,
even before dividing by vol. Vol-weighting divides crypto's larger `Q_i` by a
~4-7x larger `sigma_i` too, so the two effects partly cancel — the reallocation
is real (it does shift some catch-up money from crypto toward gold/QQQ/FXI
whenever both are simultaneously underweight) but it's a mild tilt, not a
dramatic reshuffle, given how this particular target table is already sized.

## Verdict: REJECT — no material, robust improvement; the narrow long-window pattern that does appear is the vol-reduction artifact the skill warned about, not genuine skill

Following the required verdict framing:

- **Not (b) — no cell shows a genuine joint CAGR-and-Sharpe improvement.** Every
  window/mode where Sharpe ticks up (BASELINE 13y/8y, CURRENT 13y/8y, +0.03 to
  +0.11) is paired with a CAGR *decline* (−0.8 to −1.6pp), not a flat or
  positive one — textbook case of **(a)**: a Sharpe-only improvement that is a
  vol-reduction artifact (shifting money to lower-vol assets mechanically
  lowers the return path's volatility, which flatters Sharpe's denominator),
  not evidence of genuine allocation skill. Flagged as such, not oversold.
- **At every other window (BASELINE 5y/3y/1y, CURRENT 5y/3y/1y) and on the
  60/40 cross-check almost everywhere, the result is (c) — no material
  difference, and more often mildly negative on both axes** than positive on
  either. Gold's own multi-year rally through most of this sample (same caveat
  as studies 07/09/14) means shifting catch-up money away from crypto and
  toward gold/QQQ/FXI in this particular historical window was, if anything, a
  mild net drag rather than a free lunch — there was no regime in this sample
  where vol-weighting's lower-crypto tilt was rewarded on return, only
  occasionally on smoothness.
- **Recommendation: do not adopt vol-weighted catch-up DCA in production.** The
  realized-vol sanity check confirms the mechanism itself works exactly as
  designed (it discounts the right assets, in the right direction, by a
  plausible magnitude) — this is not a bug or an implementation failure — but
  the resulting XIRR/Sharpe trade-off across all 4 grid variants plus the
  60/40 cross-check is too small and inconsistent across windows to justify
  adding rolling per-asset vol computation to the live `MainRebalancingStrategy`
  algorithm. Matches this program's established honest-rejection tone (studies
  09/12/13): a real, well-implemented idea that simply doesn't earn its
  complexity here, not a flawed one.

## Caveats

Same caveats as study 14 apply unchanged (miners-basket survivorship bias,
gold's rally over this sample flatters any variant that leans into gold,
overlapping windows share one underlying price/macro history — not independent
draws, 1y/3y windows are noisy single-episode reads, the RISK_OFF table is a
judgment-call adaptation of the real production V37 ratios, not independently
re-optimized). Additionally for this study: **sigma_i is computed over each
asset's full available price history** (not reset per backtest window), so by
the time any of the 5 windows starts, only genuinely recently-listed assets
(SUI, COIN, near their own true inception) can still be in the <12mo fallback
state — see the sanity-check table for how many months of valid sigma each
asset actually has (SUI: 25, COIN: 50 — both comfortably populated by the time
the shortest 1y window starts). Only one lookback window (12 months) was tried
for `sigma_i` — if a different window is tried in a future study, it must be
counted and labeled as its own scan, not silently folded into this fixed N=4
comparison. **Sharpe-convention caveat**: study 04's exact original Sharpe
computation could not be independently re-verified against source, because
`liquidity_timing_backtest.py` no longer exists on disk or in git history
(confirmed during this task's own scoping — this is precisely the gap Phase 2
of this program's next task exists to fix for studies 04/06). The convention
used here — annualized mean/std of per-period returns, no risk-free-rate
subtraction — is the standard convention and is named explicitly as an
assumption, not a verified match to study 04's own code.

## Standing-baselines note

This is a variant-comparison study layered on top of study 14's own portfolio,
not a from-scratch portfolio-level study — the dollar-weighted BASELINE variant
above already *is* the standing real buy-and-hold catch-up DCA baseline
(`backtest/AGENTS.md` "Standing baselines" #1). Per the task's own scoping
(step 6), the full 4-classic-portfolio x 3-trim-config standing-baseline matrix
was not re-run here (this study isn't re-auditing the trim rule or the classic
portfolios themselves) — one diversified cross-check point (60/40, above) was
judged sufficient to confirm the effect (or lack of one) isn't an artifact
specific to the user's own concentrated book.

## 2026-09-25 correction note (task 28, second pass — re-run, rejection stands)
Re-ran under the refreshed `portfolio_spec.py` (live weights incl. LINK, CURRENT
1.5x/1.0x trim held fixed, live divisor 0.5 + live orientation); `USDPLN_X.csv`
restored; END 2026-09. Still no joint CAGR+Sharpe improvement anywhere: 13y Sharpe
upticks (+0.32 dollar→vol) pair with CAGR declines; 3y CURRENT loses -3pp XIRR under
vol-weighting (20→17) with Sharpe down too (1.67→1.53); the 60/40 cross-check is
identical to the digit. The rejection above stands — the Sharpe-only uplift remains
the vol-reduction artifact the skill warns about, not directional skill.
