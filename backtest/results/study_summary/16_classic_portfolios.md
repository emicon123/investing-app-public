# 16 — Classic textbook portfolios: a reference point, not the user's own strategy

Script: `classic_portfolios_backtest.py` → `results/classic_portfolios_backtest.md`.
Fetch: `fetch_classic_portfolio_data.py` (5 new tickers, `data/prices/<TICKER>_full.csv`).
Question: how would well-known, canonically-weighted **named** portfolios (60/40,
Permanent Portfolio, Golden Butterfly, All Weather) have performed under the exact
same DCA/cost/PLN conventions as the user's own book (files 07/14/15), as a
contrast/sanity-check baseline? These are **not** the user's strategy and the weights
are taken as published, not re-derived.

**Key methodological difference from 07/14/15**: no 2x-overshoot-trim-to-gold rule
here — that's specific to the user's own aggressive crypto book. Instead, standard
**annual full rebalance-to-target** (on the 12-month anniversary of each window's own
start month) plus monthly DCA contributions allocated pro-rata at that month's target
weights (same convention as the existing scripts' `w = tt/sum()` pattern). Turnover
at each annual rebalance is costed the same way as existing scripts cost trim
turnover (one-way turnover × 2 × 15bps). Same 5 overlapping start windows (1/3/5/8/
13y back from 2026-06, the latest common month) as files 07/14/15.

**Per three explicit mid-task requests, every portfolio was also tested with a
buy-and-hold baseline and a sliding risk-on/off overlay** — three points on the
same spectrum, from no rebalancing discipline at all to a moving annually-
rebalanced target:
- **BUYHOLD** — monthly DCA contributions routed via the **real production
  deficit-proportional "catch-up" mechanic**, not a naive fixed-percentage split
  (corrected mid-task — see `backtest/AGENTS.md` "Rebalancing rule" and
  `MainRebalancingStrategy.java`, ADR-040/045): each month, `O_total = current
  total + this month's contribution A`; `P_i = target_weight_i × O_total` (what
  position i *should* be worth after the deposit); `Q_i = P_i − current_value_i`
  (deficit if positive); `Q_pos = sum of positive Q_i`; `buy_i = A × Q_i / Q_pos`
  for every `Q_i > 0`, **zero** for any position already at/above target. The
  whole contribution chases whatever is most underweight — **no rebalancing or
  selling of any kind, ever**; existing holdings just drift with price returns,
  and a position that's run ahead of target simply stops receiving new money
  until the rest of the portfolio catches up. (The first version of this study
  used a naive fixed-percentage-split approximation for BUYHOLD — corrected once
  the user clarified "buy and hold DCA" must mean the real production algorithm;
  see the "Correction" note below for how the numbers changed.)
- **FIXED** — constant named weights every month, contributions allocated at
  fixed target-weight proportions, **with** the annual full rebalance-to-target
  (the pure textbook "rebalance with selling" portfolio) — unchanged by the
  BUYHOLD correction, since it's a distinct, real textbook technique in its own
  right, not an approximation of anything.
- **BLEND** — the same ADR-044 mechanism used for the user's own book in study 14,
  reused unchanged (net-liq + Global-M2 YoY-acceleration composite z-score, 3-month
  smoothing, `blend = clip(0.5 − smoothed/2, 0, 1)`): monthly target =
  `RISK_ON×(1−blend) + RISK_OFF×blend`, where RISK_ON/RISK_OFF are a symmetric tilt
  built from the named table (see below) — same annual full-rebalance mechanic as
  FIXED, just against a moving target instead of a fixed one. Also unchanged by
  the BUYHOLD correction.
- **TRIM** — added later mid-task as a new **standing baseline**
  (`backtest/AGENTS.md` "Standing baselines" point 3, 2026-07-09): BUYHOLD's real
  catch-up DCA mechanic **plus** a monthly threshold-triggered partial-sell check
  layered on top. Any position whose actual weight reaches `trigger × its own
  current target weight` gets trimmed back to `trim_to × target`; the freed value
  is redistributed pro-rata across every **other** present position in proportion
  to *that* position's own target weight — general/portfolio-agnostic, **not**
  dumped into one fixed "safe haven" asset like gold (that was the user's own
  personal choice for their own crypto book, not a general rule). Tested at three
  `(trigger, trim_to)` pairs on every portfolio: **(2.0, 1.5)** (the user's own
  live rule, same numbers as studies 07/14/15), **(2.0, 1.0)**, **(1.5, 1.0)**.
  This is "BUYHOLD + threshold trim," **not** FIXED's annual-calendar rebalance —
  the only correction mechanism is the threshold check itself (evaluated monthly,
  same cadence convention as study #15).

Parallel worth noting: for the user's own aggressive book, an equivalent buy-and-
hold reference already exists — study 07's no-trim/pure-buy variant (13y
46%/−80%, 8y 32%/−73%). It is the same *style* of construct (never sells or
rebalances) but **still uses the older fixed-percentage-split approximation**
for its contribution step, not the real catch-up mechanic BUYHOLD uses here —
per an explicit user decision, studies 07/14/15 are **not** being corrected right
now (only study 16). So that row is **not apples-to-apples** with this study's
corrected BUYHOLD figures; treat it as a same-style-but-uncorrected reference,
not a literal comparison point (also flagged next to its citation below).

### Correction: BUYHOLD now uses the real catch-up algorithm, not a fixed split

The original version of this study approximated BUYHOLD's contribution step as
`contribution × target_weight_i` (a naive fixed percentage split) — the same
simplification studies 07/14/15 use for the user's own portfolio. The user
clarified mid-task that "buy and hold DCA" must mean the **real production
algorithm**, which is deficit-proportional catch-up (described above), not a
fixed split. Re-running BUYHOLD with the real mechanic **materially changes the
BUYHOLD-vs-FIXED comparison** — see the dedicated section below. All BUYHOLD
figures in this document are now the corrected (real-mechanic) numbers.

## Portfolios (canonical published weights, FIXED variant)

| portfolio | weights |
|---|---|
| **60/40** | SPY 60 / AGG 40 |
| **Permanent Portfolio** (Harry Browne, 1981) | SPY 25 / TLT 25 / CASH 25 / GLD 25 |
| **Golden Butterfly** (Portfolio Charts) | SPY 20 / IWN 20 / TLT 20 / SHY 20 / GLD 20 |
| **Ray Dalio All Weather** (Robbins book version) | SPY 30 / TLT 40 / IEF 15 / GLD 7.5 / DBC 7.5 |

Ticker substitutions vs. the most literal reading of each named portfolio: **IWN**
(Russell 2000 Value) used for Golden Butterfly's small-cap-value leg instead of the
already-available **IWM** (Russell 2000 blend) — more faithful to the named
portfolio, per task brief. SPY stands in for "total US stock" throughout (a
large-cap proxy, not a total-market fund) — standard practice for this class of
backtest, minor tracking difference vs. VTI-style funds.

## RISK_ON / RISK_OFF construction (BLEND variant)

Every asset is classified **RISK** (equities SPY/IWN, commodities DBC) or
**DEFENSE** (bonds TLT/IEF/SHY/AGG, gold GLD, cash CASH), matching the standard
risk-on = lean-into-risk / risk-off = lean-defensive semantics used everywhere else
in this folder (ADR-044). RISK_ON scales the portfolio's total RISK-bucket weight to
**(1 + 1/3)** of its named value, RISK_OFF to **(1 − 1/3)**; the freed/needed weight
moves to/from the DEFENSE bucket proportionally to each asset's own named weight.
The 1/3 tilt fraction is a judgment call calibrated to an explicit worked example
(60/40: risk-on 80/20 stock/bond, risk-off 40/60 stock/bond) and applied identically
to all 4 portfolios — not independently re-optimized per portfolio.

| portfolio | asset | named (FIXED) % | RISK_ON % | RISK_OFF % |
|---|---|--:|--:|--:|
| 60/40 | SPY | 60.00 | 80.00 | 40.00 |
| 60/40 | AGG | 40.00 | 20.00 | 60.00 |
| Permanent Portfolio | SPY | 25.00 | 33.33 | 16.67 |
| Permanent Portfolio | TLT / CASH / GLD (each) | 25.00 | 22.22 | 27.78 |
| Golden Butterfly | SPY / IWN (each) | 20.00 | 26.67 | 13.33 |
| Golden Butterfly | TLT / SHY / GLD (each) | 20.00 | 15.56 | 24.44 |
| All Weather | SPY | 30.00 | 40.00 | 20.00 |
| All Weather | TLT | 40.00 | 32.00 | 48.00 |
| All Weather | IEF | 15.00 | 12.00 | 18.00 |
| All Weather | GLD | 7.50 | 6.00 | 9.00 |
| All Weather | DBC | 7.50 | 10.00 | 5.00 |

## 13-year-history requirement — confirmed for every asset used

| ticker | role | true Yahoo inception | margin before 13y-window start (2013-06) |
|---|---|---|--:|
| SPY | US stock | 1993-01-29 | 20.4y |
| IWN | small-cap value | 2000-07-28 | 12.9y |
| TLT | LT treasuries | 2002-07-30 | 10.9y |
| IEF | intermediate treasuries | 2002-07-30 | 10.9y |
| SHY | short-term treasuries | 2002-07-30 | 10.9y |
| AGG | US aggregate bonds | 2003-09-29 | 9.8y |
| GLD | gold | 2004-11-18 | 8.6y |
| DBC | broad commodities | 2006-02-06 | **7.3y** |

All 8 tickers clear the 13-year requirement comfortably (7.3–20.4y of margin before
the earliest window even starts). **DBC has the least margin** (as flagged in the
task brief) but is still ~7.3 years clear. Data files themselves are fetched/saved
starting 2007-01 (matching `fetch_long_history.py`'s existing convention) regardless
of true inception — irrelevant here since 2007-01 already predates the 13y window
by 6+ years for every ticker.

## Headline results — cell = XIRR%/yr (maxDD%)

| portfolio | variant | 13y | 8y | 5y | 3y | 1y |
|---|---|--:|--:|--:|--:|--:|
| 60/40 | BUYHOLD (real catch-up) | 10 (−12) | 9 (−12) | 8 (−12) | 10 (−12) | 14 (−3) |
| 60/40 | FIXED | 10 (−12) | 9 (−12) | 8 (−12) | 10 (−12) | 15 (−3) |
| 60/40 | BLEND | 11 (−12) | 10 (−12) | 9 (−12) | 11 (−12) | 15 (−3) |
| 60/40 | TRIM 2.0/1.5 (user's own live rule) | 10 (−12) | 9 (−12) | 8 (−12) | 10 (−12) | 14 (−3) |
| 60/40 | TRIM 2.0/1.0 | 10 (−12) | 9 (−12) | 8 (−12) | 10 (−12) | 14 (−3) |
| 60/40 | TRIM 1.5/1.0 | 10 (−12) | 9 (−12) | 8 (−12) | 10 (−12) | 14 (−3) |
| Permanent Portfolio | BUYHOLD (real catch-up) | 7 (**−10**) | 7 (−11) | 6 (−11) | 9 (−4) | 6 (−4) |
| Permanent Portfolio | FIXED | 7 (−11) | 7 (−11) | 7 (−11) | 9 (−4) | 6 (−4) |
| Permanent Portfolio | BLEND | 7 (−11) | 7 (−11) | 7 (−11) | 9 (−4) | 7 (−4) |
| Permanent Portfolio | TRIM 2.0/1.5 (user's own live rule) | 7 (−10) | 7 (−11) | 6 (−11) | 9 (−4) | 6 (−4) |
| Permanent Portfolio | TRIM 2.0/1.0 | 7 (−10) | 7 (−11) | 6 (−11) | 9 (−4) | 6 (−4) |
| Permanent Portfolio | TRIM 1.5/1.0 | 7 (−10) | 7 (−11) | 6 (−11) | 9 (−4) | 6 (−4) |
| Golden Butterfly | BUYHOLD (real catch-up) | 8 (−15) | 8 (**−14**) | 7 (−14) | 11 (−9) | 13 (−1) |
| Golden Butterfly | FIXED | 8 (−15) | 8 (−15) | 8 (−15) | 11 (−9) | 13 (−1) |
| Golden Butterfly | BLEND | 9 (−14) | 9 (−14) | 8 (−14) | 12 (−8) | 15 (−1) |
| Golden Butterfly | TRIM 2.0/1.5 (user's own live rule) | 8 (−15) | 8 (−14) | 7 (−14) | 11 (−9) | 13 (−1) |
| Golden Butterfly | TRIM 2.0/1.0 | 8 (−15) | 8 (−14) | 7 (−14) | 11 (−9) | 13 (−1) |
| Golden Butterfly | TRIM 1.5/1.0 | **9** (−15) | 8 (−14) | 7 (−14) | 11 (−9) | 13 (−1) |
| Ray Dalio All Weather | BUYHOLD (real catch-up) | 6 (**−19**) | 4 (−20) | 3 (−20) | 6 (−10) | 9 (−3) |
| Ray Dalio All Weather | FIXED | 6 (−20) | 5 (−20) | 3 (−20) | 6 (−9) | 9 (−2) |
| Ray Dalio All Weather | BLEND | 7 (−20) | 5 (−20) | 4 (−20) | 6 (−9) | 10 (−2) |
| Ray Dalio All Weather | TRIM 2.0/1.5 (user's own live rule) | 6 (−19) | 4 (−20) | 3 (−20) | 6 (−10) | 9 (−3) |
| Ray Dalio All Weather | TRIM 2.0/1.0 | 6 (−19) | 4 (−20) | 3 (−20) | 6 (−10) | 9 (−3) |
| Ray Dalio All Weather | TRIM 1.5/1.0 | 6 (−19) | 4 (−20) | 3 (−20) | 6 (−10) | 9 (−3) |
| **User's own book** (study 14, CURRENT/ADR-044 — cited, not rerun, still uses the older fixed-split contribution approximation, not the real catch-up mechanic — not apples-to-apples with this study's corrected BUYHOLD/TRIM rows) | — | 34 (−24) | 33 (−20) | 21 (−28) | 24 (−15) | −3 (−11) |

Start months: 13y=2013-06, 8y=2018-06, 5y=2021-06, 3y=2023-06, 1y=2025-06 — identical
window definitions to files 14/15 (same `USDPLN_X` end date, 2026-06), so the user's
own row is a valid same-period citation even though it comes from a separate script
(`portfolio_macro_blend_backtest.py`) using a different rebalancing mechanic (buy-only
+ 2x-trim-to-gold + ADR-044 macro blend) and a far more concentrated 12-asset,
50%-crypto/30%-gold roster — not literally rerun here, cited as-published, and (per
the correction above) still on the older fixed-split DCA approximation rather than
this study's now-corrected real catch-up mechanic.

## Threshold-trim fire-frequency (single continuous pass per portfolio, study #15's method)

One non-overlapping pass per portfolio, from the earliest month all of its own
assets have a valid return through the latest common month (2026-06) — same
method study #15 used for the user's own book (which found 21 fires over 19.3y
under CURRENT mode, 2.0/1.5, ~67% BTC).

| portfolio (window) | config | total fires | fired assets |
|---|---|--:|---|
| 60/40 (2007-02..2026-06, 19.3y) | 2.0/1.5 | 0 | (none) |
| 60/40 | 2.0/1.0 | 0 | (none) |
| 60/40 | 1.5/1.0 | 0 | (none) |
| Permanent Portfolio (2007-02..2026-06, 19.3y) | 2.0/1.5 | 0 | (none) |
| Permanent Portfolio | 2.0/1.0 | 0 | (none) |
| Permanent Portfolio | 1.5/1.0 | 2 | GLD (1), SPY (1) |
| Golden Butterfly (2007-02..2026-06, 19.3y) | 2.0/1.5 | 0 | (none) |
| Golden Butterfly | 2.0/1.0 | 0 | (none) |
| Golden Butterfly | 1.5/1.0 | 1 | GLD (1) |
| Ray Dalio All Weather (2007-02..2026-06, 19.3y) | 2.0/1.5 | 0 | (none) |
| Ray Dalio All Weather | 2.0/1.0 | 0 | (none) |
| Ray Dalio All Weather | 1.5/1.0 | 4 | GLD (2), DBC (1), SPY (1) |

**The coordinator's expectation is confirmed, and even more starkly than
anticipated: the user's own live rule (2.0/1.5) never fires, for any of the 4
classic portfolios, over a full 19.3-year continuous pass.** Even the two looser
configs (2.0/1.0, 1.5/1.0) barely fire — 0–4 times total per portfolio across
19.3 years, vs. the user's crypto book's 21 fires (2.0/1.5, same window length,
study #15) or 33 fires (1.5/1.0, study #15's tightened variant). Only the
loosest config tested (1.5/1.0) fires at all, and only for 3 of 4 portfolios
(never for 60/40), each only 1–4 times total. Gold is the most frequent single
firer where anything fires at all (GLD: 1 of Permanent's 2, all of Golden
Butterfly's 1, 2 of All Weather's 4) — consistent with gold's strong multi-year
rally through this sample (same tailwind flagged as a caveat in files 07/09/14)
occasionally outrunning its fixed 20–30% target enough to cross even a loose
1.5× trigger, but never close to 2×.

## Does threshold-trim add anything over BUYHOLD/FIXED for these portfolios? No — it's nearly inert

Matching the fire-frequency finding directly, **TRIM's headline XIRR/maxDD
numbers are identical or within 1pp of BUYHOLD at every window for every
portfolio and every (trigger, trim_to) config** — because the rule essentially
never fires, "BUYHOLD + trim" collapses to just BUYHOLD in practice for 60/40,
Permanent Portfolio (at 2.0/1.5 and 2.0/1.0), Golden Butterfly (at 2.0/1.5 and
2.0/1.0), and All Weather (at all three configs, since its handful of 1.5/1.0
fires land outside every 5-window DCA period tested — the continuous pass
covers 2007-2026, but the windows only go back to 2013-06). The **only** place a
trim event visibly moves a headline number is Golden Butterfly's loosest
config (1.5/1.0) at the 13y window: 9% vs BUYHOLD's 8%, a 1pp return gain from
its single GLD fire, with **no** drawdown change (−15% both) — trimming a small
gold overshoot into other targets happened to help marginally, but this is a
single-fire, single-window effect, not a systematic pattern.

**Why this is the opposite of study #15's finding for the user's own book**:
there, the 2x rule is a BTC-specific brake (67–72% of all fires are BTC) because
a single concentrated, high-beta asset can run to 2×+ its target inside a single
market cycle. These four classic portfolios have no such asset — even their
largest single positions (60% SPY, 40% TLT, 30% SPY, 20% GLD, etc.) are
diversified, moderate-volatility, non-blow-off assets that essentially never
double relative to their own target over 19+ years (gold's steady, non-parabolic
multi-year rally gets it occasionally past 1.5×, but not remotely close to 2×).
**Threshold-triggered trim rules are a mechanism calibrated for concentrated,
high-beta portfolios (like the user's own crypto book) — for diversified
textbook portfolios like these four, the rule is a no-op at the user's own
calibration (2.0/1.5) and barely more than a no-op even loosened to 1.5/1.0.**
This mirrors, and reinforces, the BLEND finding above (the sliding overlay was
also nearly inert / occasionally counterproductive for these portfolios) —
**both of the user's own risk-control mechanisms (the macro blend and the
threshold trim) were built for, and calibrated to, a concentrated crypto book,
and neither transfers meaningfully to a diversified textbook portfolio.** For
these four portfolios, essentially all of the risk/return profile is determined
by the asset mix and the BUYHOLD-vs-FIXED choice (itself nearly a wash, see
below) — not by which of these three overlay mechanisms sits on top.

## Does the sliding overlay help the classic portfolios? Barely, and in one case it can't

- **BLEND adds +0.5 to +1.5pp XIRR at most windows, essentially for free** (maxDD
  unchanged or 1pp better) — 60/40 and Golden Butterfly both pick up a small, free
  return improvement across almost every window; Permanent Portfolio is flat
  (its 25%-cash/25%-gold ballast already dominates the drawdown profile, leaving
  little for a stock/bond tilt to improve).
- **All Weather's BLEND drawdown is completely unchanged (−20% at 13y/8y/5y,
  identical to FIXED)** — this is the interesting result. All Weather's dominant
  drawdown event in this sample is the 2022 bond-rate-hike crash (TLT/IEF), and the
  risk-off state of this overlay **increases** bond weight (TLT 40→48, IEF 15→18)
  while cutting stock (SPY 30→20) — exactly backwards for a bond-driven drawdown.
  A macro overlay built to fade *equity/crypto* risk does not protect against a
  *bond-duration* risk-off event; it can even mechanically leave the portfolio more
  exposed to it. This mirrors, in miniature, why All Weather was the worst FIXED
  performer here (see below) — the overlay doesn't fix the portfolio's actual
  vulnerability because that vulnerability is on the "safe" side of the ledger.
- **Net read: the sliding overlay is a modest, low-cost return enhancer for the
  more equity-heavy classic portfolios (60/40, Golden Butterfly), but not a
  drawdown-reduction tool here** — unlike its effect on the user's own book (studies
  07/14), where it materially cuts drawdown (13y −35%→−24%) because risk-off there
  moves weight OUT of the actual volatile assets (crypto) and INTO true zero/low-vol
  ballast (gold/cash). For classic portfolios already balanced toward bonds, the
  same mechanism has much less (or, for All Weather, no) drawdown-reduction value,
  because bonds are both a large chunk of the "safe" bucket and, in this specific
  sample, one of the actual sources of the drawdown.

## Did annual rebalancing earn its keep vs. real buy-and-hold DCA? Essentially a wash — the two mechanisms converge

**This conclusion reversed once BUYHOLD was corrected to the real catch-up
mechanic.** The first pass of this study (naive fixed-percentage-split BUYHOLD)
showed buy-and-hold clearly beating annual rebalancing on both axes for 3 of 4
portfolios. With the **real** production algorithm, that gap almost entirely
disappears: across all 4 portfolios × 5 windows (20 cells), BUYHOLD and FIXED are
**within 0–1 percentage point of each other on both XIRR and maxDD, in both
directions, with no consistent winner** —
- 60/40: identical at 13y/8y/5y/3y (10/9/8/10, DD −12 all four); FIXED edges ahead
  by 1pp return at 1y (15 vs 14, same −3% DD).
- Permanent Portfolio: BUYHOLD 1pp better DD at 13y (−10 vs −11, same 7% return);
  FIXED 1pp better return at 5y (7 vs 6, same −11% DD); identical elsewhere.
- Golden Butterfly: BUYHOLD 1pp better DD at 8y (−14 vs −15, same 8% return); at
  5y BUYHOLD trades 1pp less return (7 vs 8) for 1pp better DD (−14 vs −15) — the
  one genuine two-sided micro-trade-off in the whole table; identical elsewhere.
- All Weather: BUYHOLD 1pp better DD at 13y (−19 vs −20, same 6% return); FIXED
  1pp better return at 8y (5 vs 4, same −20% DD) and 1pp better DD at 3y/1y;
  identical at 5y.

**Why they converge**: the real catch-up mechanic is itself a form of continuous,
cash-flow-only rebalancing — every single month, 100% of the new contribution is
routed toward whatever position is furthest below target, so weights are
constantly nudged back toward target *without ever needing to sell*. For a
diversified, moderate-volatility portfolio like these four (unlike the user's own
concentrated crypto book), no single position drifts far enough above target
within a 12-month span for the annual full-rebalance-with-selling step to have
much left to correct — the monthly cash flow has usually already done most of
that work. **The extra machinery of "sell the winner, buy the laggard" earns its
keep only at the margin here (≤1pp either way, no consistent direction)** — for
these classic portfolios in this sample, real buy-and-hold DCA (no selling, ever)
is a legitimate, essentially equivalent substitute for annual rebalance-with-
selling, not a materially worse or better alternative. This is a **materially
different, and more interesting, finding than the original (naive-BUYHOLD)
pass** — the earlier "buy-and-hold clearly wins" result was an artifact of the
fixed-percentage-split approximation (which keeps feeding a fixed % to a winner
regardless of drift, amplifying "let it ride"), not a property of genuine
buy-and-hold DCA. (Caveat, unchanged: this is one 13-year, heavily overlapping-
window sample — a regime with larger/faster single-asset drift, or a much longer
horizon where cash flow becomes a shrinking fraction of AUM, could plausibly favor
FIXED's periodic hard correction more than it does here.) The parallel to the
user's own book (study 07) remains instructive but is **not** apples-to-apples,
and doubly so now: that no-trim/pure-buy row still uses the older fixed-split
approximation (not yet corrected, by explicit user decision), and even if it were
corrected, crypto's blow-off rallies are large and fast enough that a single
position can run far ahead of what monthly cash flow alone could ever catch up —
unlike this study's classic portfolios, where cash flow alone was already doing
essentially all the rebalancing work needed.

## Verdict: classic portfolios (FIXED) trade a lot of return for a lot less drawdown — Permanent Portfolio is the mildest, 60/40 the best risk-adjusted among the classics

- **All 4 classic portfolios (FIXED or BLEND) land in single-digit-to-low-teens
  XIRR with single-digit-to−20% max drawdown** — a completely different risk/return
  regime from the user's crypto-heavy book (double-digit-to-30s XIRR, −20/−28%
  drawdown at long windows).
- **Permanent Portfolio is the mildest** (smallest absolute drawdown at every
  window: −11% at 13y/8y/5y, −4% at 3y/1y) — matches its real-world reputation as
  the lowest-volatility named portfolio here, thanks to 25% zero-vol cash + 25%
  gold cushioning both equity and bond drawdowns.
- **60/40 has the best risk-adjusted (return-per-unit-drawdown) profile among the
  classics on the reliable long windows** (FIXED, 13y: 10/−12 ≈0.83; 8y: 9/−12
  ≈0.75; 5y: 8/−12 ≈0.67) — modestly ahead of Permanent Portfolio's ratio (≈0.64 at
  all three) and well ahead of Golden Butterfly (≈0.53) and All Weather (≈0.15–0.30).
- **All Weather is the worst performer here, not the best** — surprising given its
  "smoothest ride" reputation, but explained by its **55% combined long+intermediate
  treasury weight (TLT 40 + IEF 15)** getting hit by the historic 2022 bond-rate-hike
  drawdown with no equity/gold convexity large enough to offset it (only 30%
  stock/7.5% gold/7.5% commodities). Its −20% max drawdown at 13y/8y/5y is worse than
  every other classic portfolio tested (FIXED or BLEND), and its return is the
  lowest too — the 2022 bond crash is the dominant event across all three long
  windows.
- **Quantified trade-off, user's book vs. the mildest classic (Permanent Portfolio,
  FIXED)**:
  - 13y: +27pp XIRR (34 vs 7) for −13pp more drawdown (−24 vs −11)
  - 8y: +26pp XIRR (33 vs 7) for −9pp more drawdown (−20 vs −11)
  - 5y: +14pp XIRR (21 vs 7) for −17pp more drawdown (−28 vs −11)
  - 3y: +15pp XIRR (24 vs 9) for −11pp more drawdown (−15 vs −4)
  - 1y: **−9pp XIRR** (−3 vs 6, i.e. worse return too) for −7pp more drawdown (−11 vs −4)
  — at the 4 long-run windows the user's book buys real extra return (14–27pp/yr) at
  the cost of materially deeper drawdowns (9–17pp), a defensible aggressive-growth
  trade; the 1y window is the one case where the user's book was worse on **both**
  axes simultaneously — a reminder that the concentrated/crypto tilt can underperform
  even a maximally boring reference portfolio over short, adverse stretches.

## Caveats
- **Overlapping windows share one underlying price history** (not independent
  draws), same caveat as every prior study in this folder.
- **1y/3y windows are single-episode reads**, not statistically robust.
- **All Weather's 2022 bond-crash exposure is itself a single historical episode**
  (the worst bond bear market in decades) landing inside 3 of the 5 windows (13y, 8y,
  5y) — a different macro regime (e.g., a bond bull market during the test period)
  would likely flip its relative ranking versus 60/40/Permanent/Golden Butterfly, and
  would likely also flip whether the BLEND overlay helps or hurts it.
- **SPY stands in for "total US stock"** and **IWM (already fetched, not used) vs.
  IWN (used)** is a deliberate substitution favoring fidelity to Golden Butterfly's
  named small-cap-**value** leg over reusing already-available data — flagged per
  task brief.
- **DBC (commodities) has the shortest data runway** of the newly-fetched tickers
  (2006-02 inception) — still clears 13y by ~7.3 years, but has the least margin of
  any ticker used across all 4 portfolios.
- **The BLEND variant's RISK_ON/RISK_OFF tables are a judgment-call tilt**
  (TILT_FRACTION=1/3, uniform across all 4 portfolios, calibrated to a single worked
  example) — not independently optimized per portfolio or re-derived from any
  published source, unlike the FIXED variant's weights (which are the real published
  numbers). Treat the BLEND results as illustrative of "what a macro overlay does to
  a classic portfolio," not as a recommended real-world enhancement to these
  portfolios.
- **The user's-book reference row is cited, not recomputed in this script** — same
  END date and DCA/cost/PLN conventions as this study, but a structurally different
  rebalancing mechanic and asset roster, so it is a profile-vs-profile comparison,
  not an apples-to-apples holdings comparison.
- **Annual-rebalance anchor month is the anniversary of each window's own start
  month** (not a fixed calendar month like January) — chosen for reproducibility
  across 5 different overlapping windows without an arbitrary shared cutoff; a
  fixed-calendar-month convention would shift exact rebalance timing by up to 11
  months per window and could change results at the margin, though not the
  broad-strokes verdict above.
- **BUYHOLD never sells, so any drift the monthly catch-up mechanic can't fully
  correct (because cumulative cash flow is a shrinking fraction of total AUM as
  the portfolio compounds) persists indefinitely** — unlike FIXED, which forces a
  hard correction every 12 months regardless of how much cash flow could achieve
  on its own. In this sample the two stayed within ~1pp of each other at every
  window (see above), but a longer horizon, a larger single-asset blow-off, or a
  much bigger monthly-contribution-to-AUM ratio shift could open a larger gap
  between them than shown here — the BUYHOLD-vs-FIXED convergence above should be
  read as "what happened in this specific 2013–2026 sample for these four
  diversified portfolios," not as a general law that rebalancing never adds value.
- **Studies 07/14/15 (the user's own portfolio) still use the older
  fixed-percentage-split DCA approximation, not the real catch-up mechanic used
  by this study's (corrected) BUYHOLD** — per explicit user decision, only study
  16 was corrected for now (see `backtest/AGENTS.md` "Rebalancing rule" for the
  full note). Do not read the "User's own book" reference row above as using the
  same, now-corrected contribution mechanic as this study's BUYHOLD/FIXED/BLEND
  rows.
- **TRIM's near-zero fire rate is itself a one-sample finding** — these four
  portfolios happened not to contain any asset that ran to 2× (or even 1.5×, for
  60/40) its target over this specific 19.3-year window. A different sample
  period containing a genuine single-asset blow-off in one of these tickers
  (e.g., a repeat of gold's 2008-2011 or 2019-2020 run concentrated harder, or a
  small-cap-value or commodities supercycle) could make the rule bind more often
  — the "nearly inert for diversified portfolios" finding should be read as
  "true in this sample for these four named weight mixes," not as a permanent
  structural property of all diversified portfolios everywhere.
- **TRIM's redistribution-to-others rule is a judgment-call generalization**
  (every other position gets a share of freed value proportional to its own
  target weight) — a reasonable, portfolio-agnostic default per
  `backtest/AGENTS.md`'s explicit instruction, but not the only possible design
  (e.g., redistributing only to underweight positions, or weighting by recent
  performance, are alternatives not tested here).
- **TRIM's fire-frequency pass uses a per-portfolio start date** (the earliest
   month all of that portfolio's own assets have a valid return) rather than one
   shared start — so the four portfolios' continuous-pass windows are not
   identical in length to each other, though all are 19.3y here since every
   ticker used clears the shared 2007-01 data-file start.

## 2026-09-25 correction note (task 28, second pass — re-run, reference rows only)
Re-ran with END 2026-09 (`USDPLN_X.csv` restored to full 2003→ history after the 2b03ed0
truncation). The classic portfolios' own tables are untouched by the spec refresh (only
the BLEND tilt variant reads the user's book, and the tilt fraction is unchanged) —
numbers moved only with the window shift. No verdict change: these remain reference
points, not a claim about the user's strategy.
