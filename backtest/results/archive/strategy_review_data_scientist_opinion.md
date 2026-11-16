# Data-scientist opinion: is the live strategy statistically sound?

**Status: advisory opinion, not an empirical result.** No code was run, no file under
`backtest/` was modified, nothing was added to `study_summary/README.md`. Grounded in a
full read of `backtest/AGENTS.md`, `docs/architecture/05-rebalancing-algorithm.md` (see
note below on `07-adrs.md`), `study_summary/README.md`'s Executive summary (all 20
studies), `.claude/skills/quant-backtest-rigor/SKILL.md` +
`references/methodology.md`, `.claude/tasks/done/20-vol-targeting-then-19-dsr-pbo.task.md`,
and `19_dsr_pbo.md`. Written 2026-07-12.

**Scope note.** This memo is deliberately statistical/quantitative, not
operational/portfolio-management — a `data-analyst` agent covered that angle
independently (`strategy_review_data_analyst_opinion.md`, which I skimmed only to avoid
duplicating ground). Where our conclusions land in the same place (e.g. ADR-047/048
lacking backtest support), I'm citing the underlying evidence in more statistical detail
rather than repeating the operational framing. Where my read genuinely differs — most
notably on whether new signal research is worth pursuing (Q4) — I say so plainly.

**Doc note.** The brief asked me to read `docs/architecture/07-adrs.md` — it no longer
exists; it was folded into `05-rebalancing-algorithm.md` on 2026-07-10. Flagging this
once, per CLAUDE.md's instruction to note when a prompt doesn't match the repo, and not
dwelling on it further.

---

## 1. Does the current strategy make sense?

**Partially.** The macro thesis behind ADR-044 is the single best-supported empirical
claim in the whole 20-study program. But the specific mechanism actually running in
production today (post ADR-047/048) has outrun that evidence base in three concrete,
checkable ways.

**What's genuinely solid:** net-liquidity / Global-M2 *acceleration* as a risk-on/off
signal is bootstrap-significant, monotonic in beta (BTC < ETH < SOL, study 05), replicated
across 20/28 assets in study 06's breadth test, and it is the *only* claim in this program
that has been put through a real Deflated Sharpe Ratio computation: DSR ≈ 1.0 at N=3/N=15,
0.998 at N=28, and 0.926 at the widest N=308 framing (study 19) — borderline, not a clean
pass, but a real signal, not noise. If any macro-timing mechanism deserves to be live in
some form, it's this family.

**Three gaps between that finding and what's live:**

1. **Study 19's DSR was computed on a different artifact than the live blend.** DSR=0.926
   describes study 04's specific L-week net-liquidity-*momentum* rule (an expanding-window
   walk-forward binary switch, re-picked out-of-sample, tested on ETH specifically). The
   live `blend_factor` is a different statistical object: fixed 0.7/0.3 z-score weights, a
   fixed 3-month smoothing window, a fixed steepness divisor, applied as a continuous
   interpolation across *every* MAIN position, none of it re-picked out-of-sample. Related
   inputs, genuinely different construction. The rigor study 19 earned doesn't
   automatically transfer to the mechanism that's actually deployed — nobody has run
   DSR/PBO on the blend formula itself.

2. **ADR-048 doubled the blend's sensitivity (`BLEND_STEEPNESS_DIVISOR` 2.0→1.0,
   2026-07-10) on an explicitly non-walk-forward-validated ad-hoc comparison** — the doc's
   own words: "not walk-forward validated," "not separately committed to `backtest/`."
   This happened four days after study 19 concluded the underlying signal sits right at
   the edge of statistical significance at the widest honest framing. Increasing
   responsiveness to a signal you've just learned is close to the noise floor is the wrong
   direction from a pure overfitting-risk standpoint: it makes the interpolated targets
   swing further per unit of a read that's ~93% (not ~100%) likely to be real.

3. **ADR-047's target-table rewrite (Gold's risk-on target cut 30%→10%, freed weight
   routed to NQSE.DE/ICGA.DE) has no backtest attached to it anywhere in `backtest/`.** I
   found no study, script, or result file that tests this specific reallocation. The one
   cited backtest support (`onchain-and-derisk-destination`) answers "where should
   de-risk *proceeds* go, gold or bonds" — a destination question. ADR-047 answers a
   different question — "should gold's *baseline* weight be cut two-thirds in favor of two
   equity-index ETFs" — that evidence doesn't speak to it.

**A specific, untested directional concern with ADR-047's own table.** The risk-off regime
*increases* Nasdaq-100 (NQSE.DE) and MSCI China (ICGA.DE) targets (27.5%→42.5% combined)
at the same time it cuts crypto (50%→15%) — i.e. it treats those two equity ETFs as a
partial safe-haven alongside gold. But study 05's own liquidity-beta ranking puts equities
at β≈1 — nonzero, not "insensitive" — and separately flags TSLA as "the most
liquidity-levered equity"; Nasdaq-100 is a concentrated mega-cap-tech index with similar
composition risk. The 2022 tightening cycle — the single dominant risk-off episode this
whole thesis is calibrated against (see below) — saw the Nasdaq-100 fall roughly a third
(a well-known market fact, not a number computed in this repo), concurrently with exactly
the net-liquidity contraction that would have been pushing the blend toward risk-off and
*increasing* NQSE.DE's target at that same time. I have not tested this — it's a specific,
checkable hypothesis, not a finding — but it should be checked before trusting that this
table is defensive rather than procyclical (see Q3/Q4).

**The "n≈1 episode" problem is broader than the program has acknowledged.** Study 18 named
this caveat specifically for the HMM candidate (#22) — the only long, deep risk-off stretch
in ~220 months of net-liquidity history (2008→now) is 2021-12→2023-01, and any model fit
to "risk-off" is really fit to that one episode. That caveat applies with equal force to
the linear blend that's *already* live, which was retuned twice in the same week
(ADR-047/048) without walk-forward validation on either change. A mechanism whose main
support is one historical episode, tightened without out-of-sample checking right after
its underlying signal was shown to be borderline, is not wrong — but it is running ahead
of what a 20-study evidence base can actually defend today.

**The 2.0x/1.5x trim rule, on its own, is fine.** As a coarse manual guardrail it's
well-calibrated on the return/drawdown axis (studies 15/16/17): smooth, monotonic
tradeoffs at long windows, correctly inert for diversified books (0-4 fires across 19.3y
in study 16), doing real work on concentrated single-name-driven books (25 fires for the
user's own crypto book, 20 for the "Global 20" stock book — study 17). One documentation
note: ADR-048 tightened the *UI's* sell-flag/suggested-amount thresholds to 150%/100% two
days ago, but `backtest/AGENTS.md`'s "Standing baselines" section (written one day
*before* ADR-048) still labels "(2.0, 1.5)" as "the user's own live rule" — that line is
now stale relative to the authoritative architecture doc, and the next study that copies
AGENTS.md's baseline list verbatim will mislabel which config is actually live.

---

## 2. What would I change?

Ranked by how directly each ties to evidence already in hand:

1. **Roll back or formally test ADR-048's steepness change.** Revert
   `BLEND_STEEPNESS_DIVISOR` to 2.0 until a real walk-forward comparison exists, or run
   that comparison now — the machinery (study 19's DSR/PBO code, study 14/16's walk-forward
   harness) already exists. This is a cheap, well-scoped follow-up, not new research.
2. **Backtest ADR-047's table rewrite before treating it as settled**, using the same
   5-window standing-baseline harness from studies 16/17/19/20 (old tables vs. new tables,
   BASELINE vs. BLEND). This is the single largest unvalidated live decision in the system
   by dollar exposure — it moves ~20 percentage points of target weight.
3. **Test the NQSE.DE/ICGA.DE risk-off-defensiveness hypothesis directly** — a Spearman IC
   (or a simple conditional-return check) of each ETF's forward return during net-liq-
   defined risk-off windows vs. risk-on windows. Cheap: reuses price data already fetched.
4. Given the n≈1-episode caveat, my own instinct is the opposite of ADR-048's direction:
   reduce, not increase, sensitivity to a signal this close to the noise floor, until more
   full liquidity cycles exist in the data. This is a judgment call on my part, not a study
   result — flagging it as such.
5. **Actually exploit the one validated edge** — see Q4, the highest-value idea here.

---

## 3. Additional opportunities not yet tested

- **DSR/PBO on the actual `blend_factor` construction**, not study 04's related-but-distinct
  L-week rule — closes the gap in Q1.1. Would need its own per-variant return series
  (retained per the process fix study 19 already made to `01_data_and_methods.md`).
- **A leave-one-episode-out robustness check**: rerun studies 07/14's headline
  blend-vs-no-blend comparison excluding 2021-23 entirely, to see how much of the measured
  benefit survives without its single dominant driver. This turns the "n≈1" caveat from a
  named risk into a quantified one.
- **#21 — the IC framework** (already scoped in study 18): cheapest available upgrade,
  formalizes study 03/06's existing findings (IC/ICIR/t-stat, IC-decay curves) with no new
  data or backtest.
- **The NQSE.DE/ICGA.DE procyclicality question** above.
- **A meta-level multiple-testing question the program hasn't asked of itself** — see Q6.

---

## 4. What studies would most improve XIRR specifically, prioritized

1. **Build a small tactical ETH satellite overlay using study 04's actual walk-forward
   net-liquidity-momentum rule.** This is, to me, the single biggest miss in the whole
   program. The one edge with real (if borderline) statistical support — ETH net-liq
   momentum, walk-forward Sharpe 0.65→0.81, CAGR 22%→36% (study 04), DSR=0.926 at the
   widest framing (study 19) — has never been implemented in production at all. The live
   ADR-044 mechanism only slowly shifts target *weights*; it never applies study 04's
   binary in/out timing rule to any position. Every other idea that tested as "real" in
   this program is already live in some form (the macro blend); the one idea that
   specifically tested as *tradeable* in isolation is sitting completely unused. Given the
   DSR is borderline, I would not swap the whole ETH position onto this rule — a small,
   explicitly-sized satellite sleeve (or a vol-targeted variant, addressing study 18/20's
   own vol-scaling caveat) is the honest way to try capturing it without betting the
   portfolio on a 92.6%-confidence read of one study.
2. **Test ADR-047's table rewrite (Q2.2).** A wrong equity/gold split is a larger, more
   certain drag or gain than any new signal-design question, purely by dollar-weight.
3. **Test whether NQSE.DE/ICGA.DE are actually defensive in risk-off regimes (Q3).** If
   they're procyclical, ADR-047 is making the portfolio *more* exposed exactly when the
   blend believes it's de-risking — the opposite of its intent.
4. **Re-validate ADR-048's steepness change with a real walk-forward comparison.**
5. **The leave-one-episode-out robustness check (Q3)** on the blend's overall value.

---

## 5. Technical indicators (RSI / EMA / SMA crossovers / MACD / volume) — worth testing?

**On-chain:** already extensively tested (studies 08/10/11/12) and mostly rejected as
sell/timing triggers; LTH/STH supply ratio survives as a fading buy-conviction signal, not
a trigger. Nothing to add.

**Classic technical indicators — my honest prediction, before running anything, is that
these mostly fail, and for reasons this program has already established twice over:**

- **RSI and other bounded oscillator-style rules are structurally the closest existing
  test to study 13** (per-asset mean-reversion against a rolling ±1.5σ regression channel),
  which lost to buy-and-hold on every persistently trending asset in this book — mega-cap
  tech and miners never fired at all over 10y (TSLA/GLD/SLV) or missed the bulk of the move
  (NVDA, −35.6pp/yr) — and only won on genuinely range-bound assets like bonds, which
  ADR-047 just cut to 0% target in both regimes. A 14-day RSI mean-reversion rule on
  BTC/ETH/NVDA/TSLA is very likely to fail the same way, for the same structural reason:
  these are trending, not mean-reverting, assets over the relevant holding horizons.
- **EMA/SMA crossovers are trend-following, not mean-reverting**, so they sit on the
  "momentum beats mean-reversion" side of the pattern this program keeps finding (studies
  06, 14) — a better prior than RSI. But they're still **price-level** signals at typical
  20/50/200-day lookbacks, and the program's own finding is that month-to-month
  price/liquidity links are weak — the one thing that works is a slow, ~annual-regime
  acceleration measure (README point 2). I'd predict a short-to-medium-window EMA crossover
  underperforms for exactly the reason MoM correlations are weak throughout this program —
  the edge this book has access to lives at a lower frequency than crossovers typically
  operate at. A very slow variant (200-day/50-week, closer to the ~3-month-smoothed cadence
  that does work) is the only version I'd expect has a fighting chance, and even that is a
  guess, not a finding.
- **Volume-based signals (OBV, volume-weighted momentum) are the weakest candidate of the
  three**, for a reason distinct from the pattern above: this book is crypto-heavy, and
  free crypto volume data (Yahoo Finance / exchange aggregates) is known to carry material
  wash-trading and reporting noise, especially for the smaller Experiments-bucket
  altcoins. Any volume signal here starts from a noisier input than price or
  macro-liquidity series, on top of already sitting in a frequency band this program's own
  evidence says is weak.

**Bottom line: I would not spend a study slot on RSI/EMA/MACD/volume as a new-edge
search.** If tested at all, frame it explicitly as "closing the loop because the user
asked," expect a negative or null result with moderate-to-high confidence given studies
06/13/14's pattern, and only bother with the slowest-cadence variants where there's at
least a plausible frequency-matching mechanism to the ~3-month regime signal that does
work.

---

## 6. Other considerations being missed

- **A meta-multiple-testing question study 19 didn't ask of itself.** Study 19 computed
  DSR at N=308 — study 06's explicit rule×asset zoo — and called that "the widest, most
  honest framing." But the *cumulative* 20-study program has repeatedly probed the same
  fixed ~2008-2026 crypto/macro history from many other angles beyond study 06's zoo
  (different trim thresholds in 12/15, different de-risk destinations in 09, different
  portfolio constructions in 07/14/16/17, different on-chain metrics in 08/10/11/12) —
  much of it touching the same underlying net-liquidity/crypto relationship that eventually
  got distilled into "ETH net-liq momentum." If the true effective search space that led to
  spotlighting ETH is wider than 308 (a reasonable but unverified suspicion — not something
  I've computed), 0.926 could itself be an overestimate of how special the ETH result
  really is. I can't quantify this without new computation, and I'm flagging it as an open
  methodological question, not a finding — but it's the natural next iteration of the exact
  logic study 19 already applied one level down, and it's worth someone eventually running.
- **The DSR formula itself needed a correction mid-program** (`SR_hat` vs. `SR_0` in the
  PSR denominator, fixed 2026-07-12, the same day study 19 ran) — a reminder that even the
  rigor tooling has had at least one real formula bug caught right before first use. I'd
  adopt a standing convention of reporting a small sensitivity range for any new DSR/PBO
  number (the repo already does this informally — `ddof=0` vs. `ddof=1`, S=8 vs. S=10
  blocks) rather than a single point estimate, given how young and lightly battle-tested
  this specific implementation still is.
- **A process point, stated once, since the parallel data-analyst memo covers the
  operational angle in more depth**: ADR-047/048 both happened outside the numbered-study
  process in the same week study 19 established real DSR/PBO rigor for the first time.
  From a pure statistics standpoint, the concerning part isn't that a manual,
  non-statistical judgment call was made — that's fine, not everything needs a backtest —
  it's that ADR-048 specifically *did* cite a backtest ("ad-hoc... not walk-forward
  validated") as partial justification, which is exactly the thing this program's own house
  style (studies 09/12/13's honest-rejection tone) says not to do: present an
  under-validated number with more confidence than it earned.
- **On whether more signal-hunting is worth it at all (a place my read may differ from a
  more operationally-focused view)**: I'd push back on the instinct that "the what-to-buy
  lever is fully explored, stop looking for signals." The ETH edge isn't unused because
  it's redundant with what's already live — it's unused because implementation never
  followed research. The gap between "20 studies of signal research" and "0 tactical
  overlays actually deployed from that research" is, in my view, the most concrete,
  quantifiable opportunity in this whole review (Q4.1), not a reason to stop looking.
