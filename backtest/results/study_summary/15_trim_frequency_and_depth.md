# 15 — Trim-rule (+100%) frequency & depth audit, and a tighter 1.5x/1.0x alternative

Script: `trim_frequency_backtest.py` → `results/trim_frequency_backtest.md`,
`results/trim_events.csv`. Direct sequel to file 14 — reuses its exact data loading,
RISK_ON/RISK_OFF tables, and ADR-044 blend computation unchanged (does not modify file
14's script). Two new pieces of machinery: (1) `TRIGGER`/`TRIM_TO` are now parameters,
not hard-coded constants, and (2) every trim event is logged (month, mode, asset, target
weight, actual weight, ratio, trimmed fraction, blend, whether the month fell inside a
sustained MID_OFF/RISK_OFF episode). A **single continuous pass over the full available
history (2007-02..2026-06, 19.3y, non-overlapping)** drives the frequency analysis; the
standard 5 start-date windows (1/3/5/8/13y, same DCA/cost conventions) drive the
XIRR/maxDD comparison, matching file 14's table format.

## Correction (2026-07-09): monthly contribution allocation now uses the real catch-up DCA mechanic

**Everything below was re-run.** The original version of this study allocated each
month's contribution with a **naive fixed-percentage split**
(`hold[a] += CONTRIB * target_weight_i`) — every asset gets a constant proportional
slice of new money every month regardless of how far it has actually drifted from
target. That is **not** what the production app does. The real algorithm
(`MainRebalancingStrategy.java`, ADR-040/045, spec in `backtest/AGENTS.md`
"Rebalancing rule") is a **deficit-proportional "catch-up"**: each month,
`O_total = current_total + contribution`; `P_i = target_weight_i × O_total` (what
position *i* should be worth after the deposit); `Q_i = P_i − current_value_i`
(deficit if positive); the whole contribution is split across every
**currently-underweight** position in proportion to the size of its own deficit —
positions already at/above target get **zero** new money that month. This is the same
correction already applied to study #16 (`classic_portfolios_backtest.py`); there it
reversed a real conclusion (buy-and-hold's apparent edge over annual rebalancing nearly
vanished). It matters here too, for a specific reason: **the naive split constantly
"dilutes" an overweight position with fresh proportional cash every month, acting as an
unintended brake that slows how fast it can re-cross a trim trigger. The real catch-up
mechanic removes that brake entirely** — once a position is trimmed back to its exact
target, it gets **zero** further cash until something else falls behind, so its weight
afterward is driven by price returns alone. That structural difference turns out to
matter far more at the **tight** end of the rule (trim-to-1.0x, no cushion at all) than
at the current 2.0x/1.5x rule (which still leaves 50% headroom after a trim). The figures
this correction produced (BASELINE 22 / CURRENT 25 fires at the live 2.0x/1.5x rule) were
published on 2026-07-09 and are the numbers this section originally described — **they
are themselves now superseded by an unrelated, later data issue; see "Price-data-drift
refresh (2026-07-13)" immediately below for the current figures.** This section is kept
as historical record: the DCA-mechanic correction it documents is real, separate, and
still valid — only the specific fire counts it originally reported are stale.

## Price-data-drift refresh (2026-07-13): QQQ/FXI price-history change supersedes every fire-count-derived number below

**All figures in sections 1-3 below were regenerated on 2026-07-13 and are current.**
One day after the 2026-07-09 correction above was published, an unrelated repo-wide data
reorganization (`2b03ed0`, "chore(backtest): split data/ into prices/macro/onchain,
refresh price history", 2026-07-10 21:35) added `data/prices/QQQ_full.csv` and
`data/prices/FXI_full.csv` for the first time — these files did not exist anywhere in
this repo's history before that commit. `trim_frequency_backtest.py`'s loader
(`usd_close()`) prefers a `*_full.csv` file whenever one is present, falling back to the
plain `QQQ.csv`/`FXI.csv` only if no `_full` variant exists; that same commit also
**removed** the plain `QQQ.csv`/`FXI.csv` fallback files entirely, so as of 2026-07-10
the script has no choice but to read the new `_full.csv` series (now starting 1999-03 for
QQQ) — a different price input than whatever the plain fallback contained when this study
originally ran on 2026-07-09. **The deficit-proportional `Q_pos` catch-up mechanic
(section above) is completely unchanged and was independently re-verified correct in the
current script before this refresh** (matches `backtest/AGENTS.md`'s spec exactly,
line-by-line) — this is a pure price-data-input change, not a second mechanic bug.

This was first surfaced as a side effect of study #23's own sanity check
(`23_tax_drag_audit.md`, "Data-drift discovery" section), which found that re-running
`trim_frequency_backtest.py` unmodified against today's data produces **54 BASELINE / 57
CURRENT fires at the live 2.0/1.5 rule, not the published 22/25** — flagged there as "a
separate follow-up, not fixed here." This section is that follow-up: `trim_frequency_backtest.py`
was re-run fresh on 2026-07-13 (script itself unmodified since 2026-07-09, only the
underlying `data/prices/` changed), reproducing that exact **54/57** ground truth, and
every downstream table this study reports — per-asset breakdown, overshoot-at-fire
distribution, regime clustering, whipsaw/re-fire rates, and the 3-point sensitivity grid —
has been regenerated from that same fresh run, since all of them are derived from the
identical fire-event log (`results/trim_events.csv`, now 451 rows, up from the
2026-07-09 pass). **Sections 1-3 below present only the current (2026-07-13) numbers;
where a figure moved meaningfully from the 2026-07-09 pass, the delta and its cause are
called out explicitly in the prose** rather than left as a silent replacement.

**Headline mechanism of the drift, traced precisely from the fire log itself (not
inferred):** BTC_spot's and ETH's own fire counts, overshoot ratios, and per-asset years-
present are **identical or within one fire** of the 2026-07-09 pass at every tested
config — their own price series were untouched by this refresh. The entire jump in total
fires is attributable to three assets newly firing far more often — **MSTR, QQQ, and
FXI** — plus one asset that stops firing entirely — **miners** (2 fires at 2.0/1.5 and 16
at 1.5/1.0 in the 2026-07-09 pass; **zero at every config tested here**, alongside COIN
and SUI, which never fired in either pass). Because every asset draws from the same
shared `Q_pos` catch-up pool each month, adding two long-history, meaningfully-drifting
assets (QQQ, FXI) into the simulation from 2007 onward changes how much of every month's
contribution *every other* asset receives, not just QQQ/FXI's own fire counts — this
most plausibly explains why MSTR's fire count also jumped sharply and miners' dropped to
zero, even though neither of those two assets' own price data changed. (The pre-refresh
plain `QQQ.csv`/`FXI.csv` files no longer exist on disk, so this can't be re-verified
against the exact old input — but the fact that BTC/ETH/DOGE/SOL fire counts are
essentially untouched while QQQ/FXI/MSTR/miners move in lockstep is strong internal
evidence for a cash-flow-competition effect, not a coincidence.)

## 1. How often does the current rule (2.0x trigger → trim to 1.5x) actually fire?

Over 19.3 years, non-overlapping:

| mode | total fires |
|---|--:|
| BASELINE (fixed target, no macro) | 54 |
| CURRENT (ADR-044 blend, live mechanism) | 57 |

(2026-07-09 corrected-mechanic pass, now stale due to the price-data drift above: 22
BASELINE / 25 CURRENT — today's figures are +32 fires at both, +145%/+128%. Original
naive-split pass, doubly stale: 18 BASELINE / 21 CURRENT.)

Per-asset breakdown, CURRENT mode:

| asset | fires | % of total | avg months between fires | fires/yr (while held) | years present |
|---|--:|--:|--:|--:|--:|
| BTC_spot | 15 | 26% | 9 | 0.94 | 15.9 |
| MSTR | 14 | 25% | 3 | 0.72 | 19.4 |
| QQQ | 13 | 23% | 3 | 0.67 | 19.4 |
| FXI | 11 | 19% | 3 | 0.57 | 19.4 |
| ETH | 3 | 5% | 20 | 0.28 | 10.8 |
| DOGE | 1 | 2% | fired once | 0.12 | 8.6 |

BASELINE (54 fires): BTC_spot 14 (26%), MSTR 14 (26%), QQQ 13 (24%), FXI 11 (20%), ETH 2
(4%). **miners, SOL, COIN, and SUI never fired even once** at this (2.0/1.5) rule in
19.3 years under either mode.

**Verdict, materially changed from both prior passes: the rule is no longer meaningfully
"BTC-specific" even at its live, un-tightened calibration.** BTC_spot's own fire count
(14-15) is essentially unchanged from the 2026-07-09 pass, but its **share of total
fires collapsed from 60-64% to 26%** — not because BTC fires less, but because MSTR (25-
26%), QQQ (23-24%, previously zero at this rule), and FXI (19-20%) now fire nearly as
often as BTC does. At the live rule, four assets (BTC_spot/MSTR/QQQ/FXI) each contribute
19-26% of total fires — a genuinely broad, multi-asset mechanism, not a single-asset
brake. This reframes the section-2 finding below (that the *tighter* 1.5x/1.0x rule
becomes broad-based) — under today's data, breadth is already present at the *current*
live rule, not just at the tightened alternative. **miners is a new, complete reversal**:
it fired twice at this exact rule in the 2026-07-09 pass and fires zero times here.

## 2. 1.5x trigger / trim-to-1.0x alternative vs. the current 2.0x/1.5x rule

Cell = XIRR%/yr (maxDD%), same 5 start-date windows as file 14:

| config | mode | 13y | 8y | 5y | 3y | 1y |
|---|---|--:|--:|--:|--:|--:|--:|
| **2.00 / 1.50 (current)** | BASELINE | 43 (−39) | 43 (−40) | 26 (−52) | 25 (−22) | −16 (−22) |
| **2.00 / 1.50 (current)** | CURRENT | 38 (−22) | 37 (−22) | 24 (−30) | 22 (−15) | −7 (−14) |
| 1.75 / 1.25 (midpoint) | BASELINE | 42 (−32) | 42 (−35) | 24 (−52) | 24 (−21) | −16 (−22) |
| 1.75 / 1.25 (midpoint) | CURRENT | 36 (−21) | 35 (−16) | 23 (−30) | 21 (−15) | −7 (−13) |
| **1.50 / 1.00 (alternative)** | BASELINE | 38 (−31) | 39 (−33) | 24 (−51) | 23 (−21) | −16 (−22) |
| **1.50 / 1.00 (alternative)** | CURRENT | 33 (−17) | 33 (−17) | 23 (−29) | 20 (−14) | −7 (−13) |

Start months: 13y=2013-06, 8y=2018-06, 5y=2021-06, 3y=2023-06, 1y=2025-06.

**This XIRR/maxDD table is, cell for cell, within ±1pp of the 2026-07-09 pass — the
price-data drift barely touches the return/drawdown headline, even though it more than
doubled the fire count (see below).** The likely reason: all 5 standard windows start in
2013-06 or later, a period for which the pre-refresh QQQ/FXI data (whatever it was) most
plausibly already had usable price coverage; the drift's real bite is concentrated in the
2007-2013 stretch that only the long continuous pass (section 1, and the fire counts
below) covers, not the 1/3/5/8/13y windows. This can't be independently re-verified since
the pre-refresh input files no longer exist, but it is consistent with everything
observed: the windowed return/drawdown numbers are stable while the continuous-pass fire
counts are not.

Fire counts, continuous 19.3y pass, CURRENT mode: 2.0/1.5 → **57 fires (2.95/yr)**;
1.75/1.25 → 85 (4.40/yr); 1.5/1.0 → **89 fires (4.60/yr, +56% vs. the live rule)**.

(2026-07-09 pass, now stale: 25 → 35 → 83, i.e. +232% 2.0/1.5→1.5/1.0. Today's *relative*
growth from the live rule to the tight alternative is far smaller — +56%, not +232% —
because the live rule itself now already captures most of the broad-based firing that
previously only appeared once the rule was tightened; see section 1's verdict.)

Per-asset breakdown at 1.5/1.0 (CURRENT mode, 89 fires total): **FXI 23 (26%), MSTR 23
(26%), QQQ 22 (25%), BTC_spot 13 (15%), ETH 5 (6%), DOGE 2 (2%), SOL 1 (1%).** BASELINE
(84 fires): FXI 23 (27%), MSTR 22 (26%), QQQ 22 (26%), BTC_spot 13 (16%), ETH 4 (5%) — no
DOGE/SOL fires under BASELINE. **miners fires zero times at 1.5/1.0 too** (16 fires, 19%
of the 2026-07-09 pass's total, now gone entirely) — combined with its zero fires at
2.0/1.5 and 1.75/1.25, miners never crosses its trigger at *any* tested config in today's
data, joining COIN and SUI as assets this rule structurally never touches. BTC_spot,
ETH, DOGE, and SOL's fire counts at 1.5/1.0 are **identical** to the 2026-07-09 pass
(13/5/2/1) — the entire +6-fire net change here (83→89) nets out from FXI +7, MSTR +8,
QQQ +7, miners −16.

**In short: 2.0x/1.5x is now already a broad, multi-asset mechanism (section 1), and
1.5x/1.0x tightens it further but by a much smaller relative margin than the 2026-07-09
pass suggested** — not because the tighter rule fires less than previously thought (89 is
still more than 25's old 1.5/1.0 comparator, 83, was), but because the *live* rule now
absorbs most of what used to look like a "tightening effect." BTC_spot's role has also
inverted more starkly than the 2026-07-09 pass showed: it was described there as "the
asset that does 60% of the work at the live rule" and "now only the 5th-most-frequent
firer" at the tight rule — today, BTC_spot is neither dominant at the live rule (26%,
tied closely with MSTR/QQQ/FXI) nor even close to dominant at the tight one (15%, now
4th of 7 firing assets, essentially unchanged in rank from the live rule).

Despite the mechanism shift, the headline return/drawdown tradeoff is essentially
unchanged in size from both prior passes: 13y XIRR gives up 5pp (38→33) for 5pp less
drawdown (−22→−17) going from 2.0/1.5 to 1.5/1.0 in CURRENT mode; 8y gives up 4pp
(37→33) for 5pp less drawdown (−22→−17). 1.75/1.25 again sits close to the midpoint on
both axes. **Verdict on the return/drawdown axis is unchanged from both prior passes:
tightening the rule trades real return for real but not transformative drawdown
reduction, and 2.0x/1.5x remains reasonably well-calibrated on this specific axis.** But
the *operational* picture has changed materially since 2026-07-09: the live rule itself
is now firing 57 times over 19.3 years (roughly 3/yr) across four different assets, not
25 times (roughly 1.3/yr) dominated by one — anyone executing this rule by hand today
faces a meaningfully busier manual-trim workload at the *current* calibration than the
2026-07-09 writeup implied, even before considering the tighter alternative at all.

## 3. Additional angles (fire-event instrumentation, folded in)

**Overshoot-at-fire ratio** (checks are monthly, so a position can already be well past
2.0x by the time it's caught): BASELINE fires (n=54) median 2.02x, max 3.85x, 9% at
≥2.5x; CURRENT fires (n=57) median 2.03x, **max 5.78x** (DOGE, 2021-06 — target had
shrunk to 0.10% that month while DOGE's actual weight hit 0.6%, so 74% of the position
was trimmed in one event), 14% at ≥2.5x. **Both the BASELINE max (3.85x, BTC_spot,
2013-12) and the CURRENT max (5.78x, DOGE, 2021-06) are the exact same events as the
2026-07-09 pass** (5.77x there, a negligible rounding-level difference from the changed
Q_pos cash-flow path elsewhere in the sim that month) — crypto's tail overshoots are
untouched by the QQQ/FXI drift, as expected. **What did move: the median and the ≥2.5x
share both fell sharply** (BASELINE median 2.22→2.02, 18%→9%; CURRENT median 2.39→2.03,
32%→14%) — the new QQQ/FXI/MSTR fires cluster very tightly just above the 2.00x trigger
(their `p25` is exactly 2.00 in both modes) rather than overshooting deeply the way
crypto does, so they dilute the overall distribution toward the trigger threshold rather
than adding to its tail. **Revised verdict: the rule's worst-case overshoot depth is
unchanged (still crypto-driven, still ~3.9-5.8x), but its typical/median overshoot is now
much shallower than the 2026-07-09 pass suggested**, because most fires today are
ordinary, small QQQ/FXI/MSTR crossings caught close to the trigger, not the
blow-off-top-style crypto events the median used to be anchored to.

**Regime clustering** — do CURRENT's extra fires (57 vs. BASELINE's 54) come from
target contraction during risk-off regimes? Sustained MID_OFF/RISK_OFF episodes (blend
≥0.6, 3+ months, recomputed identically to file 14: 2012-06→08, 2014-11→2015-03,
2018-08→2019-01, 2021-05→09, 2021-12→2023-01) cover 33/233 months (14% of history) —
identical episode table to the 2026-07-09 pass (unaffected by QQQ/FXI, which don't feed
the blend calculation). Of CURRENT's 57 fires, **4 (7%)** fall inside those episodes;
BASELINE's 54 fires are **1 (2%)** inside episodes. **The absolute counts (4 and 1) are
identical to the 2026-07-09 pass** — every regime-linked fire in this dataset is one of
the original BTC/ETH-driven events, and none of the new QQQ/FXI/MSTR fires happen to fall
inside a sustained risk-off episode. The percentage dropped (16%→7% CURRENT, 5%→2%
BASELINE) purely because the denominator grew. **This conclusion is, if anything,
strengthened by the correction: an even smaller share of CURRENT's total firing — now
7%, not 16% — is target-contraction-driven; the vast majority remains ordinary
price-growth firing.**

**Whipsaw/re-fire check**: quick re-fires (same asset firing again within ≤6 months) —
under 2.0/1.5: **84%/84%** (BASELINE/CURRENT) of consecutive same-asset gaps are quick
re-fires; under 1.5/1.0: **90%/90%**. The tighter rule still has a **higher**, not lower,
quick-re-fire rate than the live rule, at both configs — the same direction the
2026-07-09 correction found (65%/68% at 2.0/1.5, 81%/83% at 1.5/1.0) — but **the absolute
level rose sharply at both configs** (roughly +16-19pp at 2.0/1.5, +7-9pp at 1.5/1.0).
QQQ/FXI/MSTR turn out to whipsaw at least as readily as BTC/ETH once they're firing at
all — their many-times-per-19-years fire pattern (avg gap of only 3 months between fires,
per section 1's table) is itself evidence of frequent re-firing, not isolated one-off
events. **Verdict direction unchanged from the 2026-07-09 pass (tighter rule = more
whipsaw-prone, no offsetting silver lining), but the base rate of whipsawing across the
whole rule — at both configs — is now higher than either prior pass showed.**

**Sensitivity grid** (3 points, CURRENT mode): fires/yr rises 2.95 → 4.40 → 4.60 across
2.0/1.5 → 1.75/1.25 → 1.5/1.0. **This is now front-loaded (concave), the opposite shape
from the 2026-07-09 pass's convex characterization** (which found +0.52 fires/yr for the
first step, +2.48 for the second — nearly 5x steeper at the tight end). Today's steps are
**+1.45 fires/yr for the first step (2.0/1.5→1.75/1.25), only +0.20 for the second
(1.75/1.25→1.5/1.0)** — almost the entire jump in fire frequency now happens just from
loosening the trigger from 2.0x to 1.75x; tightening further to 1.5x/1.0x adds
comparatively little extra firing. This makes sense given section 1's finding: QQQ/FXI/
MSTR already fire heavily at the live 2.0/1.5 rule, so there's much less additional
"newly-crossing" territory left for them to contribute as the rule tightens further —
most of their fire-count growth already happened at the loosest step. 13y maxDD still
improves smoothly (CURRENT: −22 → −21 → −17) and 13y XIRR still degrades smoothly
(38 → 36 → 33) — **the return/drawdown tradeoff curve remains monotonic and smooth, but
the fire-frequency curve's shape has flipped from convex to concave under the corrected
data** — a materially different picture from what the 2026-07-09 pass reported for how
firing cost scales as the trigger tightens.

## Caveats

Carried over from files 07/14: miners-basket survivorship bias, gold's tailwind through
most of the sample flattering any gold-destination trim rule, overlapping windows sharing
one price/macro history, 1y/3y windows as single-episode reads, and the risk-off table
being an unverified adaptation of the real production V37 ratios.

**Updated for the 2026-07-13 price-data-drift refresh** (superseding the 2026-07-09
pass's own caveat update): the claim that "QQQ, miners, SOL, COIN, SUI never fired even
once" (2026-07-09 pass) is now doubly wrong for QQQ specifically — QQQ fires 13 times
even at the live 2.0/1.5 rule today, and 21-22 times at the two tighter configs — and
**miners has flipped from "fires twice at 2.0/1.5, 16 times at 1.5/1.0" back to "never
fires at any tested config,"** the opposite direction of the 2026-07-09 correction's own
finding. The set of assets that genuinely never cross this rule at any tested severity in
today's data is now: **miners, COIN, SUI** (SOL fires once, only at the tightest 1.5/1.0
config). SOL/COIN/SUI's thin histories (since 2020-04, 2021-04, 2023-05 respectively)
remain a genuine low-power caveat independent of this refresh — "never fired" for them
should still be read as low-power, not strong evidence, and this now applies to miners
too by a different mechanism (not thin history, but apparently being crowded out of new
contribution cash by QQQ/FXI's now-longer, more-competitive drift — see the "Headline
mechanism" note above, itself not independently re-verifiable since the pre-refresh input
files no longer exist on disk). **The rule is no longer well-described as "a BTC-specific
brake at its live calibration"** — that framing held under both the original naive-split
pass and the 2026-07-09 mechanic-corrected pass, but does not hold under today's data:
BTC_spot's fire count is unchanged from the 2026-07-09 pass, yet its *share* of total
firing at the live rule fell from 60-64% to 26% because three other assets (MSTR/QQQ/FXI)
now fire nearly as often. Any future work that tunes this rule's parameters, or cites its
"who fires" breakdown, should use this refresh's figures, not the 2026-07-09 or original
pass's.

## 2026-09-25 correction note (task 28, second pass — re-run, framing now correct)
Re-ran under the refreshed `portfolio_spec.py` (live ADR-074/087 weights, LINK-USD added;
CURRENT 1.5x/1.0x held fixed and correctly labeled live throughout — the mislabeling this
task exists to fix no longer appears anywhere in the script or its output);
`USDPLN_X.csv` restored to full 2003→ history; END 2026-09 (continuous pass 2007-02..2026-09,
19.6y). CURRENT now fires 41 (BASELINE) / 48 (CURRENT mechanism); LINK fires once in 8.6y
present. BTC's share dilutes further. Study 23's cross-check (CURRENT fires = 48) holds
exactly — see the 2026-09-25 addendum there.
