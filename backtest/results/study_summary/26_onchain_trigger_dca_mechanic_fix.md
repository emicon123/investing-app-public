# 26 — On-chain trigger portfolio test: DCA-mechanic fix and index correction (not new research)

Script: `onchain_portfolio_backtest.py` → `results/onchain_portfolio_backtest.md`. **This
is a correction/refresh of an existing, previously-unindexed script, not a new empirical
question.** `onchain_portfolio_backtest.py` (dated 2026-07-03, part of the file 10
on-chain-signals thread) tests whether adding an on-chain LTH-SOPR trigger to the
shipped macro dynamic-target de-risk mechanism (ADR-042, files 07/09) improves portfolio
outcomes vs. macro-only / on-chain-only / composite-AND / composite-OR / no-trigger-at-all.
Its result (`results/onchain_portfolio_backtest.md`) has always existed on disk but was
**never folded into this numbered index** — no row in the Files table, no Executive-
summary point, referenced from nowhere. This file both indexes it (as #26) and corrects a
real bug found while doing so.

## What was wrong

The script's monthly contribution-allocation step (`sim()`, previously around line 100-101)
used a **naive fixed-percentage split**: `hold[a] += CONTRIB * w[a]`, where `w[a]` is the
current (risk-off-adjusted) target weight renormalized over present assets — every asset
gets a constant proportional slice of new money every month, regardless of how far it has
actually drifted from its own target. This is **not** what the production app does (see
`backtest/AGENTS.md` "Rebalancing rule" and ADR-040/045) — the real algorithm is a
**deficit-proportional "catch-up"**: `O_total = current_total + contribution`;
`P_i = target_weight_i × O_total`; `Q_i = P_i − current_value_i` (deficit if positive);
the whole contribution is split across every currently-underweight position in proportion
to the size of its own deficit, and positions already at/above target get zero new money
that month. A 2026-07-09 pass fixed this exact bug across studies 07, 14, 15, 16, 17, and
20 — but `onchain_portfolio_backtest.py` predates that pass by six days (2026-07-03 vs.
2026-07-09) and, because it was never indexed here, was never swept up in it. It sat as an
orphan result with a known-stale mechanic until this fix.

**The fix** (matching the exact pattern already used in `portfolio_strategy_backtest.py`
and `portfolio_macro_blend_backtest.py`): the deficit calculation now reuses the same
risk-off-adjusted `tt` target dict that `cur_target(ro, CUT)` already computes each month
for the trim-trigger check a few lines below it, so the catch-up deficit and the trim
check always agree on "what's the target this month" — no second, inconsistent target
definition was introduced. No other part of the script (the +100% trim rule itself, the
macro/on-chain risk-off definitions, the LTH-SOPR threshold, the CUT=0.33 crypto sub-cap,
the cost model) was touched.

## Corrected results (2026-07-13)

Same format as the original `results/onchain_portfolio_backtest.md`. Real algo (buy-only
Q_pos catch-up DCA + 2x-trim-to-1.5x-target, cut=0.33). Only the risk-off TRIGGER
definition varies across rows. **On-chain data only exists from 2022-07 — 5/8/13y start
dates from files 07/09 are not testable here; only 3y and 1y** (same limitation as the
original, unaffected by this fix).

| config | 3y | 1y |
|---|--:|--:|
| NO de-risk (baseline, always risk-on) | 25 (−22) | −16 (−22) |
| Macro only (shipped, ADR-042) | 30 (−13) | −3 (−9) |
| On-chain only (LTH-SOPR≥1.86) | 25 (−12) | −10 (−16) |
| Composite OR (macro or on-chain) | 25 (−13) | −2 (−9) |
| Composite AND (macro and on-chain) | 28 (−13) | −16 (−22) |

Start months: 3y=2023-06, 1y=2025-06. Cell = XIRR%/yr (maxDD%). Trigger frequency (48
months, 2022-07..2026-06, identical to the pre-fix pass since it doesn't depend on the DCA
mechanic): macro risk-off 14 months, on-chain risk-off 13 months, both 2 months, either 25
months.

For reference, the **naive-split (pre-fix, now superseded) numbers** were:

| config | 3y | 1y |
|---|--:|--:|
| NO de-risk (baseline, always risk-on) | 25 (−23) | −5 (−17) |
| Macro only (shipped, ADR-042) | 31 (−11) | 3 (−8) |
| On-chain only (LTH-SOPR≥1.86) | 28 (−9) | −2 (−9) |
| Composite OR (macro or on-chain) | 27 (−11) | 2 (−9) |
| Composite AND (macro and on-chain) | 30 (−14) | −5 (−17) |

## What changed, and what didn't

**The 3y window (the less noisy of the only two testable windows) shows the clearest,
most consequential shift: on-chain-only's and composite-OR's small apparent edges over
doing nothing at all vanish entirely.** Under the naive split, on-chain-only beat the
no-derisk baseline by +3pp XIRR (28 vs. 25) and composite-OR beat it by +2pp (27 vs. 25);
under the corrected catch-up mechanic, both are in a dead 3-way tie with the baseline
(25 vs. 25 vs. 25). Macro-only's edge over baseline also shrank, but survives (+6pp →
+5pp, 30 vs. 25), and composite-AND's edge shrank similarly but survives too (+5pp →
+3pp, 28 vs. 25). **The top-of-ranking answer is unchanged — macro-only remains the
single best-performing trigger definition at 3y, composite-AND remains second — but the
weaker two configs (on-chain-only, composite-OR) go from "modestly better than nothing"
to "indistinguishable from nothing"** once the more realistic DCA mechanic is used.

**The 1y window moves in the opposite direction, and should not be read as contradicting
the 3y result — it's a single, noisy, non-overlapping 12-month episode** (2025-06 to
2026-06), the shortest and least reliable window this program tests anywhere. Every
triggered variant's *edge over the no-derisk baseline* widens substantially at 1y, purely
because the corrected no-derisk baseline itself performs far worse under the real
mechanic (−16% XIRR / −22% maxDD, vs. −5% / −17% under the naive split) — a large,
mechanic-driven swing in the baseline, not something specific to any de-risk trigger.
Macro-only and composite-OR remain very close to each other for the top spot in both
passes (they're effectively tied, within 1pp, both before and after the fix — the exact
order between them flips by a fraction of a point, not a meaningful reordering).
On-chain-only's absolute XIRR at 1y worsens (−2% → −10%) and its maxDD protection
degrades noticeably relative to the other triggers (from tied-best −9% to a clearly
worse −16%, now closer to the undefended baseline's −22% than to macro-only's −9%) —
the one genuinely new, somewhat concerning observation in this 1y slice, though it rests
on a single noisy 12-month window and should be weighted accordingly.

**Composite AND is numerically identical to the no-derisk baseline at 1y in both passes**
(−16/−22 both times) — not a coincidence or an error: verified directly against the
underlying risk-off series that neither the macro nor the on-chain gauge triggers
simultaneously ("AND") at any point in the 2025-06→2026-06 window, so `sim()` receives
the exact same all-False risk-off series for both configs and necessarily produces the
exact same path. This is a real property of the trigger-overlap data for this specific
recent window, unrelated to the DCA-mechanic fix.

**Trigger frequency itself is completely unaffected** (macro 14/48, on-chain 13/48, both
2/48, either 25/48 months) — as expected, since `MACRO_RISK_OFF`/`ONCHAIN_RISK_OFF` are
computed independently of `sim()`'s DCA allocation logic; only the portfolio-level
XIRR/maxDD outcomes for a given trigger series changed.

## Verdict — does the corrected mechanic change which trigger configuration looks best?

**No, not at the top of the ranking — macro-only (the shipped ADR-042 mechanism) and
composite-AND remain the best-performing configurations at the only reliable (3y)
window, both before and after the fix.** What the correction does change is the case
*for* adding the on-chain layer at all: on-chain-only's and composite-OR's already-modest
apparent edges over doing nothing evaporate at 3y under the more realistic DCA mechanic,
and on-chain-only's drawdown protection looks materially weaker in the noisy 1y slice
too. **If anything, this correction makes the case for adding an on-chain LTH-SOPR
trigger on top of (or instead of) the macro gate weaker than the pre-fix numbers
suggested, not stronger** — consistent with, and reinforcing, study #10's own original
verdict that rejected LTH-SOPR (and the wider on-chain toolkit) as a portfolio-level
de-risk trigger.

**Study #10's own caveats about this exact threshold still apply in full, and are
repeated here rather than assumed:** the LTH-SOPR threshold (1.86, the 75th percentile)
is calibrated **in-sample on the same ~4-year 2022-2026 history being backtested** — a
real limitation given how little on-chain history exists (bitcoin-data.com only goes back
to 2022-07). Only two start dates are testable at all (3y, 1y) — far short of this
program's standard 1/3/5/8/13y windows — and both overlap heavily with each other and
with the single 2022-2026 sample the threshold itself was tuned on. Nothing about the
DCA-mechanic fix changes this data-availability constraint; it remains the dominant
caveat for this specific study, arguably more binding than the mechanic bug that
motivated this refresh.

## Caveats

Same as the original write-up: only 2 start dates testable (on-chain history is ~4
years); LTH-SOPR threshold calibrated on the same full sample being backtested — real
in-sample bias; see file 10 for the signal-level caveats (native-2022+ IC is strong but
doesn't replicate over 2010+ history). **New for this refresh**: the corrected mechanic
makes the no-derisk baseline itself materially more volatile at the 1y window than the
naive-split pass showed (−16%/−22% vs. −5%/−17%) — a reminder that this study's baseline
row is not a neutral reference point independent of the DCA-mechanic choice, the same
point already established for the standing BUYHOLD baseline elsewhere in this program
(`backtest/AGENTS.md` "Standing baselines" point 1).

## 2026-09-25 correction note (task 28, second pass — re-run, rejection stands, tie broken)
Re-ran under the refreshed `portfolio_spec.py` (live weights, CURRENT trim — the script
already used both; only its emitted header still said "2x-trim", fixed);
`USDPLN_X.csv` restored to full 2003→ history; END 2026-09 (3y=2023-09, 1y=2025-09). The
old 25/25/25 three-way tie at 3y **broke mildly**: macro-only 27 > composite-AND 21 >
composite-OR 20 > on-chain-only 18 > baseline 14. The rejection stands on the same
methodological grounds (in-sample-calibrated 1.86 threshold, one on-chain cycle,
macro-only still best by +6-9pp) — but the note above should now read "on-chain-only's
and composite-OR's edges over doing nothing reappeared mildly (+4/+6pp at 3y) under
refreshed weights," not "vanish entirely." 1y remains a single noisy window, not
over-read.
