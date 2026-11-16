# 23 — Tax/cost-drag audit: does Belka tax change the trim-rule tightening verdict?

Script: `tax_drag_audit_backtest.py` → `results/tax_drag_audit_backtest.md`,
`results/tax_drag_events.csv`. Direct sequel to study 15 — reuses its exact demo
portfolio, RISK_ON/RISK_OFF tables, ADR-044 blend computation, and `sim()` mechanic
unchanged (does not modify study 15's script, same convention study 15 itself used
relative to study 14). Adds a 19% Belka tax term (Poland) on top of the existing 15bps
trading cost.

**Scope, per the reviewed task** (`.claude/tasks/done/cost-tax-audit-and-ic-framework.task.md`):
study 15 is the primary/only re-run target. **Study #20 (vol-targeted DCA) is not
re-run** — it only changes the buy-side catch-up weighting and never trims/sells, so a
tax overlay is fully predetermined before computing anything: no realized-gain event ever
occurs in that variant, and its already-rejected verdict (no cell showed a joint
CAGR+Sharpe improvement) is completely unaffected by tax. **Studies #07/#14 get a
one-sentence citation, not a re-run**: direct inspection today confirms both
`portfolio_strategy_backtest.py` (07) and `portfolio_macro_blend_backtest.py` (14) already
implement the real deficit-proportional `Q_pos`/`CONTRIB*Q[a]/Q_pos` catch-up mechanic
(the `backtest/AGENTS.md` "Known gap" note claiming otherwise is stale, not the scripts) —
study 07's headline ("2× trim halves drawdown at little/no return cost") and study 14's
momentum-vs-contrarian result would both plausibly see the same qualitative tax effect
found here (a few points of extra XIRR drag, concentrated almost entirely in the
crypto-spot legs, not large enough on its own to flip either headline) — but neither was
re-run under a tax lens; this study's numbers come only from study 15's fire-log
machinery, per the task's explicit scope narrowing.

## Methodology — cost-basis convention and tax-exposure assumption (stated explicitly, not a footnote)

**Cost-basis: average-cost approximation, not true FIFO.** Poland's real Belka-tax rule
for securities disposals uses FIFO cost-basis matching (the standard convention under
`art. 30b` PIT law) — that is the legally correct answer to "what should this be." But
`trim_frequency_backtest.py` (and 07/14/20) model each position as a single running scalar
`hold[a]`, with no per-lot data at all — no unit count, no per-month purchase price, no lot
queue. Implementing genuine FIFO would require restructuring `hold[a]` into a list of
(month, PLN-cost, market-value-share) lots per asset, a materially bigger lift than this
task's scope. Instead, a parallel scalar `basis[a]` tracks running cost basis:

```
basis[a] += CONTRIB * Q[a] / Q_pos      # same month, same amount added to hold[a]
# price return applied to hold[a] only, never to basis[a]
# on a trim event trimming fraction f of hold[a]:
realized_gain = f*hold[a] - f*basis[a]  # proceeds minus proportional cost basis removed
tax = 0.19 * max(realized_gain, 0)      # no loss offset/carryforward modeled
basis[a] -= f*basis[a]
```

The tax leaves the system at the point of the trim: `hold["GLD"] += trimmed - tax` (not
`+= trimmed`) — the after-tax proceeds are what gets reinvested into the gold sink; the
tax itself is real capital removed from the portfolio, same treatment as the existing
15bps trading cost (which is computed on the gross `sold` amount, unchanged from study 15
— tax stacks on top of, not instead of, the existing cost convention).

**Directional bias, quantified, not just asserted.** Trim events fire almost exclusively
on assets in a sustained multi-year uptrend by the time they cross the trigger. True
FIFO — which consumes the oldest, most-appreciated lots first — would show a *larger*
realized-gain fraction than average-cost (which blends in more-recent, less-appreciated
contributions still sitting in the position), so average-cost likely **understates** true
tax drag. **How much headroom does that bias actually have in this fire log?** Measured
directly: BTC_spot's gain-fraction-of-trimmed-dollars ranges 88–100% (median ~100%) at the
live 2.0/1.5 rule, 82–100% at 1.5/1.0; ETH and DOGE sit at 99–100% throughout. These
crypto-spot positions have appreciated so enormously over such long holds that average
cost is **already within a few percentage points of the 100% FIFO ceiling** — the bias is
real and correctly signed, but numerically small (single-digit-to-low-double-digit percent
of the trimmed dollar amount) for exactly the assets that generate almost all the tax in
this study, not a multiple-fold understatement. For the newly-active equity/ETF firers
(FXI/MSTR/QQQ, see below), gain-fraction is near 0% or negative (many of their trims are
near-breakeven or losses under average cost) — here the FIFO-vs-average-cost choice could
matter proportionally more, but the absolute PLN at stake for these assets is trivial (see
below), so it doesn't move the aggregate finding either way.

**Tax-exposure assumption: every fire is treated as REGULAR (fully taxable).** None of
studies 07/14/15/20 model account type (IKE/IKZE/REGULAR) at all. This study uses an
explicit, stated simplifying assumption rather than building a full capacity-aware account
simulator (out of scope): **crypto-spot fires (BTC/ETH/DOGE/SOL) are structurally
REGULAR** — real crypto can never be booked directly under `AccountType.IKZE`
(`docs/architecture/05-rebalancing-algorithm.md` §8), so the backtest's raw crypto-spot
return already IS what a REGULAR disposal would tax, not an approximation for these
assets. **Equity/ETF fires (MSTR, miners-basket proxy, FXI/QQQ) are treated as REGULAR
too, via a capacity-exhaustion argument**: Poland's combined annual IKE+IKZE contribution
cap is modest relative to a 1000+1000 PLN/month, 12–37-line-item buy list spread across
~27 assets, so in steady state most of a multi-year contribution stream ends up in REGULAR
once the shared annual cap is exhausted — a stated **worst-realistic-case** approximation
for these assets, not a precise per-position accounting claim. **This assumption turns out
to matter far less than it might seem**: even under this worst-case REGULAR treatment,
equity/ETF fires contribute only 0.02–4.7% of total tax across the two primary configs
(see below) — the overall verdict is essentially insensitive to whether some of those
specific fires actually happened inside a tax-sheltered account in reality.

## Data-drift discovery (found during this audit's own sanity check, not assumed)

Re-running the tax-free (`tax_on=False`) variant of this study's `sim()` — arithmetically
identical to study 15's own unmodified `trim_frequency_backtest.py` — against **today's**
`data/prices/` produces **54/57 fires at the live 2.0/1.5 rule (BASELINE/CURRENT)**, not
study 15's published **22/25**. Re-running study 15's own script byte-for-byte, unmodified,
reproduces the same 54/57 — this is not a bug in this new script. **Root cause, traced**:
`data/prices/QQQ_full.csv` and `FXI_full.csv` were created 2026-07-10 21:35 (`git log`),
*after* study 15's 2026-07-09 publication and its committed `trim_events.csv` snapshot; the
raw fallback `QQQ.csv`/`FXI.csv` files study 15 originally read no longer exist on disk at
all (removed by the same refresh, commit `2b03ed0`). The loader prefers `_full.csv` when
present, so today's data pulls a materially longer QQQ/FXI history (`QQQ_full.csv` now
runs back to 1999-03) than whatever study 15's original run saw. **QQQ and FXI now fire at
the LIVE 2.0/1.5 rule too** (13 and 11 times respectively over the 19.3y window), not just
at the tighter 1.5/1.0 rule as study 15 reported — study 15's own headline is now stale and
should be refreshed as a separate follow-up (out of scope for this task; study 15's own
committed artifacts are untouched by this script). Every number below is computed against
today's on-disk data for both the no-tax and with-tax variants, so the internal
**WITH-tax-vs-NO-tax comparison remains apples-to-apples valid** even though absolute fire
counts/XIRR/maxDD no longer match study 15's committed table.

## 1. Fire counts and total tax paid, single continuous pass (2007-02..2026-06, 19.3y)

| config | mode | fires | total tax paid (PLN, nominal) | avg tax/fire (PLN) |
|---|---|--:|--:|--:|
| 2.00 / 1.50 (current rule) | BASELINE | 64 | 1,410,350 | 22,037 |
| 2.00 / 1.50 (current rule) | CURRENT | 68 | 1,249,322 | 18,372 |
| 1.75 / 1.25 (midpoint) | BASELINE | 82 | 1,122,788 | 13,693 |
| 1.75 / 1.25 (midpoint) | CURRENT | 85 | 982,694 | 11,561 |
| 1.50 / 1.00 (alternative) | BASELINE | 85 | 862,499 | 10,147 |
| 1.50 / 1.00 (alternative) | CURRENT | 89 | 604,386 | 6,791 |

**Total tax paid falls monotonically as the rule tightens**, despite fire count rising —
each individual trim under a tighter rule crystallizes a smaller absolute gain (it fires
before as much overshoot/appreciation accumulates), which more than offsets the higher
fire count.

### Per-asset breakdown, CURRENT mode, 2.0/1.5 (live rule) — 68 fires

| asset | fires | total tax (PLN) | gain-frac of trimmed $ (min/median/max) |
|---|--:|--:|--:|
| BTC_spot | 15 | 872,755 | 88% / 100% / 100% |
| ETH | 4 | 364,959 | 99% / 100% / 100% |
| DOGE | 2 | 11,369 | 99% / 99% / 99% |
| MSTR | 16 | 109 | -33% / -2% / 41% |
| QQQ | 15 | 80 | -19% / 0% / 14% |
| FXI | 16 | 50 | -21% / 5% / 20% |

Crypto-spot (BTC/ETH/DOGE) = **100.0%** of total tax; equity/ETF proxies (FXI/MSTR/QQQ,
now firing frequently post-data-refresh) = **0.02%** of total tax.

### Per-asset breakdown, CURRENT mode, 1.5/1.0 (tighter alternative) — 89 fires

| asset | fires | total tax (PLN) | gain-frac of trimmed $ (min/median/max) |
|---|--:|--:|--:|
| BTC_spot | 13 | 362,136 | 82% / 99% / 100% |
| ETH | 5 | 187,187 | 99% / 99% / 100% |
| MSTR | 23 | 28,354 | -29% / 0% / 97% |
| SOL | 1 | 13,961 | 99% / 99% / 99% |
| DOGE | 2 | 12,528 | 99% / 99% / 99% |
| QQQ | 22 | 125 | -15% / 0% / 15% |
| FXI | 23 | 96 | -19% / 0% / 21% |

Crypto-spot = **95.3%** of total tax; equity/ETF = **4.7%**. Even though FXI/MSTR/QQQ now
fire far more often at this rule (22–23 times each vs. 13 for BTC_spot), they remain
essentially tax-irrelevant — they trim small, near-breakeven or loss positions under
average cost, not large embedded gains.

## 2. Full continuous-pass XIRR/maxDD/tax, WITH tax vs. WITHOUT tax (single 19.3y number)

The cleanest single comparison — one continuous, non-overlapping 19.3-year compounding
path per config/mode, matching study 15's own frequency-analysis convention:

| config | XIRR no-tax → with-tax | maxDD no-tax → with-tax | total tax paid | tax as % of no-tax final value |
|---|---|---|--:|--:|
| **2.0/1.5 (live rule)** | 39.66% → 36.53% (**-3.13pp**) | -24.8% → -26.9% (**-2.1pp**) | 1,249,322 PLN | 5.24% |
| **1.5/1.0 (tighter alt.)** | 32.83% → 31.06% (**-1.77pp**) | -18.9% → -19.3% (**-0.4pp**) | 604,386 PLN | 5.73% |

**The tighter rule loses less XIRR to tax in absolute pp terms (-1.77pp vs. -3.13pp),
despite firing 31% more often (89 vs. 68 fires) and generating far less total nominal tax
(604K vs. 1.25M PLN).** This directly contradicts the task's own naive prior ("given
1.5x/1.0x fires 232% more often [per study 15's original figures], tax drag should be
expected to widen the gap" against the tighter rule) — the mechanism runs the other way:
trimming sooner caps how much unrealized gain accumulates before each disposal, so smaller,
more-frequent trims are, if anything, more tax-efficient than fewer, larger ones.

**Does the tax-adjusted gap between the two rules widen or narrow?** Using the same clean
full-pass numbers: no-tax XIRR gap (2.0/1.5 minus 1.5/1.0) = +6.83pp → with-tax gap =
+5.47pp (**narrows by 1.36pp** — tax erodes some of the looser rule's return advantage).
No-tax maxDD gap = -5.9pp → with-tax gap = -7.6pp (**widens by 1.7pp in the tighter rule's
favor** — its drawdown advantage grows once tax is counted). **Both axes move (modestly)
toward favoring 1.5/1.0 once tax is included, not away from it** — opposite of what the
task's own prior expected.

*(The 5-window 1/3/5/8/13y table — matching study 15's exact format — is reproduced in
`results/tax_drag_audit_backtest.md` for completeness; it shows the same qualitative
pattern with more window-to-window noise, e.g. the 8y maxDD gap widens sharply — -4.9pp
no-tax → -9.3pp with-tax — while the 1y window is dominated by single-episode noise, as
per study 15's own standing caveats about short/overlapping windows.)*

## 3. Second-order effect: does tax change fire timing/count itself?

Removing tax capital from the GLD credit slightly shrinks total NAV going forward, which
can (very slightly) change *when* a later fire crosses its own trigger. Measured directly
on the identical continuous pass: at **2.0/1.5, fire count rises materially under tax**
(BASELINE 54→64, CURRENT 57→68, +18–19%) — larger than a "rounding" second-order effect,
because this rule fires rarely enough (once every several months per asset) that a small
NAV perturbation can shift a specific historical crossing by a month or two either side of
the check date. At **1.75/1.25 and 1.5/1.0, fire count is essentially unchanged** (+0 to
+1) — these rules already fire so often that individual-crossing timing sensitivity washes
out in aggregate. **Caveat**: this means the with-tax and no-tax runs at 2.0/1.5 are not
literally the same set of historical fire events — a real, if second-order, limitation of
comparing two full simulation paths rather than re-taxing a fixed, shared event log; it
does not change the XIRR/maxDD/total-tax conclusions above (both paths are independently
faithful simulations under their own assumptions), but is flagged for transparency.

## Verdict — does tax drag change study 15's tightening recommendation?

**No — study 15's core conclusion holds, and if anything looks slightly more favorable
toward tightening once tax is counted, not less.** Study 15 (as it stands, now on stale
pre-refresh data — see the data-drift note above) found that tightening 2.0x/1.5x →
1.5x/1.0x trades real return for real but not transformative drawdown reduction, with
2.0x/1.5x "reasonably well-calibrated." Adding a 19% Belka tax term (average-cost basis,
REGULAR-account assumption) on top:

- **Both rules lose a broadly similar, modest amount of XIRR to tax** (-1.77 to -3.13pp/yr
  over the full 19.3y pass) — tax is a real, non-trivial drag (roughly 5–6% of what the
  no-tax final portfolio value would have been), but it is not asymmetric against the
  tighter rule.
- **Total nominal tax paid is actually LOWER under the tighter rule** (604K vs. 1.25M PLN
  over 19.3 years) — the higher fire count does not translate into a bigger tax bill,
  because each individual trim event captures a smaller absolute gain.
- **The XIRR gap between the two rules narrows modestly under tax (+6.83pp → +5.47pp) and
  the drawdown gap widens modestly in the tighter rule's favor (-5.9pp → -7.6pp)** — tax
  makes 1.5x/1.0x look (slightly) more attractive on both axes, not less.
- **Essentially all tax exposure (95–100%) is concentrated in crypto-spot (BTC/ETH/DOGE/
  SOL)** — assets that are structurally REGULAR-taxable by construction (real crypto can't
  sit in IKE/IKZE). The equity/ETF assets that newly dominate fire *count* under the
  tighter rule and post-refresh data (FXI/MSTR/QQQ) are tax-irrelevant (0.02–4.7% of the
  total), so the worst-realistic-case REGULAR assumption for those assets barely matters
  to the bottom line either way.
- **The average-cost-vs-FIFO approximation likely understates true tax drag, but by a
  bounded, small amount for the assets that matter**: BTC/ETH/DOGE gain-fractions are
  already 82–100% (median ~99–100%) of the trimmed dollar amount under average cost — there
  is little numerical headroom left for FIFO to push materially higher for these specific,
  extremely-appreciated, long-held positions. A genuine FIFO implementation (flagged as a
  larger follow-up, not done here) would very likely nudge these same directional findings
  slightly further, not reverse them.

**Bottom line: tax drag does not change the standing recommendation.** 2.0x/1.5x remains a
reasonable live calibration on the return/drawdown/turnover tradeoff even under a tax-aware
lens; the case for tightening to 1.5x/1.0x is, if anything, marginally *strengthened* by
adding tax (lower total tax bill, narrower return gap, wider drawdown-favor gap) — though
this study does not recommend changing the live rule on this basis alone, since study 15's
own operational-cost concerns (83–89 fires vs. 25–68, far more manual trims to execute)
still dominate the tightening decision more than the tax question does.

## Caveats

Carried over from study 15: miners-basket survivorship bias, gold's tailwind through most
of the sample flattering any gold-destination trim rule, overlapping windows in the
5-window table, thin history for SUI/COIN/SOL, the risk-off table being an unverified
adaptation of the real production V37 ratios. **New for this study**: the data-drift
finding above (study 15's headline fire counts are stale vs. today's refreshed
`data/prices/`, a separate follow-up); the average-cost-vs-FIFO bounded-understatement
discussion; no loss offset/carryforward modeled (a realized loss under average cost is
simply untaxed, never banked against a future gain — a further simplification, not modeled
either direction); the second-order NAV-path fire-count sensitivity noted above; account
 type (IKE/IKZE/REGULAR) is a stated simplifying assumption, not a capacity-aware
 simulation — a true FIFO lot-tracking rebuild and/or a full account-capacity simulator
 would sharpen these numbers further but are out of scope for this pass (flagged as
 follow-ups, not blockers, per the task's own reviewer verdict).

## 2026-09-25 correction note (task 28, second pass — re-run, verdict unchanged)
Re-ran under the refreshed `portfolio_spec.py` (live weights, CURRENT trim);
`USDPLN_X.csv` restored; END 2026-09. The pattern holds with new figures: CURRENT fires
48 (matches study 15's re-run exactly — the cross-check still holds) paying ~903K PLN
total tax vs. PRIOR's 41 fires / ~2.22M PLN; the PRIOR-minus-CURRENT XIRR gap narrows
mildly with tax at 13y (+5.0pp → +4.3pp) and widens mildly in CURRENT's favor at 3y
(-1.8pp → -2.0pp). Tax drag still does not change the standing verdict, and still
mildly favors tightening.
