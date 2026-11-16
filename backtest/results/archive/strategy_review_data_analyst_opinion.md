# Data-analyst opinion: is the live strategy worth its complexity?

**Status: advisory opinion, not an empirical result.** Not part of the numbered study
sequence, not listed in `study_summary/README.md`'s Files table. Written by the
`data-analyst` agent at the user's request, grounded in `backtest/AGENTS.md`,
`docs/architecture/05-rebalancing-algorithm.md` (the ADR log was folded into this doc —
`docs/architecture/07-adrs.md` was deleted 2026-07-10, see its own history note), the 20
studies indexed in `study_summary/README.md`, and `CLAUDE.md`. No code run, no new study.
Written 2026-07-12.

**Note on scope correction:** the brief pointed me at `docs/architecture/07-adrs.md` for
ADR text; that file no longer exists — it was consolidated into
`docs/architecture/05-rebalancing-algorithm.md` on 2026-07-10 ("consume ADRs, don't let
them pile up"). Reading that doc surfaced two live decisions (**ADR-047**, **ADR-048**,
both dated within the last week) that aren't yet reflected in the study README or in
project memory. They turn out to be the single most important input to this opinion —
see Q1 and Q6.

---

## Q1 — Does the current strategy make sense for a solo operator?

**Partially, and less so than it did a week ago.** Split it into its two live pieces:

**The 2.0x/1.5x trim rule as a *manual* practice** — this is fine and should stay. It's
one number to remember, the user already executes it by hand via "Sprzedaj," and studies
07/14/15 confirm it does what the user's own mental model (CLAUDE.md: "when a target
rises to 20%+, I reduce to 15% and swap to Gold") says it does: it caps BTC-driven
drawdown, it doesn't add return, and post-correction (study 15) it fires rarely (25 times
in a 19.3-year pass, 60-64% BTC). Low cognitive load, understood benefit, matches intent.
Keep it.

**The ADR-044 blend as an *automated* target-shifting engine** — this is the piece I'd
push back on. It works (study 19: DSR≈1.0 at N=3/N=15, 0.998 at N=28, but only 0.926 —
below the conventional 0.95 bar — at the widest, most honest N=308 framing that counts the
full rule/asset search that led here). "Real but borderline" is an honest characterization,
not a green light. A blend_factor computed from a five-legged Global-M2 series where the
US leg (FRED) has been silently broken since 2026-07-06 (confirmed 8/8 reproducible,
ADR-046 §6) and has therefore likely not moved the user's actual targets at all in
production, for weeks, is a lot of moving parts — z-scores, 3-month smoothing, two full
100%-summing target tables per regime, a steepness divisor, five HTTP integrations of
varying reliability — for an edge that is itself right at the statistical noise floor and
that, while it was silently not computing, cost the user nothing because the system
degrades to pure risk-on. That degrade-gracefully property is good design. But it also
means the user has been living, unknowingly, on the *simpler* strategy (fixed risk-on
targets) for over a month and, per the studies, hasn't obviously suffered for it.

**The complexity-vs-benefit read, plainly:** if the ADR-044 blend disappeared tomorrow and
MAIN reverted to fixed risk-on targets + the manual 2.0x/1.5x trim, the user would lose an
edge that even its own DSR computation calls borderline, and would gain a system they can
fully explain to themselves in one sentence. That's a real trade, and on a spare-time solo
project I'd lean further toward simplicity than the current build does — not necessarily
by ripping the blend out (it's shipped, it's cheap to leave running, and study 16 shows it
helps more diversified books with no offsetting downside there), but by being honest that
its risk-management value is now doing double duty for something that isn't fully
earning its complexity on the evidence in hand.

**What actually concerns me is not the blend's math — it's what changed around it in the
last week without going through the study process at all (see Q6).**

---

## Q2 — What would I change?

1. **Don't let live parameter changes bypass the rigor process that exists for exactly
   this.** ADR-048 (2026-07-10) tightened `BLEND_STEEPNESS_DIVISOR` from 2.0 to 1.0 — a
   2x sensitivity increase to the exact mechanism study 19 just spent an entire study
   proving is statistically borderline — based on the doc's own words, "ad-hoc
   backtesting... not separately committed to `backtest/`," "not walk-forward validated."
   That is precisely the failure mode the `quant-backtest-rigor` skill and studies 18/19
   exist to catch (in-sample tuning of a parameter that's already close to the noise
   floor), and it happened on the live system anyway, four days after study 19 shipped.
   I'd unwind this to a documented, numbered study (or revert to 2.0 until one exists) —
   not because /1.0 is necessarily wrong, but because the program has a real, working
   process for exactly this question and it wasn't used.
2. **ADR-047's gold cut (30%→10% risk-on, freed weight into NQSE.DE/ICGA.DE) isn't backed
   by any study either.** The task file for it says plainly the numbers were "worked out
   interactively with the user" — reasonable, but the only cited backtest support
   (`onchain-and-derisk-destination`: gold beats bond ETFs *as a de-risk destination*) is
   being used to justify a much bigger, different claim: that gold's steady-state risk-on
   weight itself should be cut two-thirds in favor of leveraged-adjacent equity ETFs. That
   evidence doesn't speak to this question. This is the kind of thing worth a half-day
   check against the same 5-window standing-baseline convention (`AGENTS.md`) before it
   sits live for months.
3. **Fix the flagged crypto-migration double-buy** (`05-rebalancing-algorithm.md` §8,
   "Known concern, flagged but not fixed"). This is not a research question — it's a
   suspected live bug where a crypto position can get bought twice in one purchase session
   (once via IKZE capacity freed by an ETF sale, once via the compensating Binance/REGULAR
   buy) instead of the sale and buy netting to one unit of exposure moved. If real, this is
   guaranteed, quantifiable money — over-buying crypto beyond the intended allocation —
   and it's more certain to matter to actual XIRR than any of the 20 studies' findings.
   I'd prioritize this over #21/#22 below without hesitation.
4. **Simplify the manual sell-assist story, don't re-tune it further.** ADR-048 also
   tightened the UI's sell-flag/suggested-sell-down thresholds from 200%/150% to 150%/100%,
   citing study #15's finding that 200%/150% "hasn't fired... since April 2022." True, but
   study #15 also found the tighter pair "fires 232% more often," has a **higher** quick-
   re-fire/whipsaw rate (81-83% vs 65-68%), and recruits FXI/miners/MSTR/QQQ into frequent
   flagging alongside BTC — i.e., the same study is both the justification cited and a
   caution against the change, and only half of it made it into the decision. This is
   UI-only (the user still decides), so the downside is bounded to "more false alarms," not
   mis-trading — but it's a second instance of the same pattern as #1: citing a study's
   convenient half.

---

## Q3 — What additional opportunities matter most for actual outcomes?

Not another signal. The evidence across 20 studies already says the "what to buy, and
when" lever is close to fully explored and mostly saturated (one thin edge left in ETH
timing, everything else tested and rejected). The higher-value, lower-effort levers I see:

- **The IKE/IKZE capacity-fill correctness (Q2.3) is the single biggest lever on the
  table** — it's about whether tax-free (Belka 19%) capacity is being used correctly, and
  a bug there directly reduces after-tax return with certainty, unlike any signal study.
- **Data-source reliability, not new data sources.** `FredClient` has been silently broken
  since 2026-07-06 and is one of two inputs to a decision engine already flagged as
  borderline (ADR-046 §6). A monitoring/alert on "has `blend_factor` actually updated in
  the last N days" would have caught this immediately instead of it being discovered
  as an aside while writing architecture docs. Cheap, mechanical, protects an already-live
  system rather than researching a new one.
- **None of the 20 studies model trading costs, FX-conversion spread, or Belka tax at
  all** (confirmed: no study file mentions fees/commission/spread/slippage/tax). Every
  XIRR number in the whole program is gross, pre-cost. For a book that fires 25 manual
  trims plus 12-37+ monthly buy-list line items across PLN/EUR/USD/CHF and three
  exchanges, transaction/FX friction is a real, uncounted drag that could easily be
  larger than the ETH-timing edge study 19 spent a whole study qualifying.

---

## Q4 — What studies are worth doing next, prioritized by effort vs. impact?

From `18_methodology_review.md`'s scoped candidates #19-22, plus my own read:

1. **Highest, not yet scoped as a number: audit the ADR-047/048 changes properly**
   (see Q2.1/Q2.2) — small effort (reuses `AGENTS.md`'s standing-baseline machinery
   already built for studies 16/17/19/20), directly protects the live system, and closes
   the two live gaps that currently sit outside the rigor process entirely.
2. **#21 — IC framework.** File 18 already calls this "cheapest item on this list," and
   I agree it's worth doing — but treat it as tidying/relabeling existing study 03/06
   findings, not as a source of new edge. Do it opportunistically, not urgently.
3. **A real trading-cost/tax-drag pass over the existing studies' return numbers** — not
   a new backtest, a correction pass: what does study 19's ETH edge look like net of
   realistic Binance/XTB fees and FX spread? This could single-handedly move the DSR
   verdict in either direction and is more decision-relevant than #21 or #22.
4. **#20 (vol-targeted DCA) is done and correctly rejected — don't revisit.** #22 (HMM
   blend) — I agree with file 18's own "lowest priority, explicitly exploratory" framing;
   it doesn't fix the small-sample problem it would supposedly address (one real risk-off
   episode in the data), so it would swap one under-validated mechanism for another. Skip
   unless there's genuinely nothing higher-value left to do.
5. **Not worth doing at all:** any further on-chain avenue (file 11's own conclusion — the
   sell-side avenue is closed; LTH/STH ratio is "fading"), White's Reality Check/SPA (file
   18 already rejects this as overkill for one user), a second HMM-vs-blend pass, or
   anything that re-litigates the trim-rule parameters (already covered three times over,
   files 07/14/15).

---

## Q5 — Is chasing more signals (EMA/RSI/volume) where the missing return is?

**No, and the evidence already on hand says so fairly directly.** Study #13 already
tested the closest technical-analysis cousin available here — per-asset mean-reversion
against a rolling regression channel (a moving-average-band-style rule) — and it lost to
buy-and-hold for every trending asset (mega-cap tech, miners), which is most of this
portfolio; its only edge was on genuinely range-bound assets like bonds, which the user
barely holds (ADR-047 just cut bonds to 0% in both regimes). RSI and volume-based rules
are, structurally, more of the same family (range/reversal signals on a book dominated by
persistently trending, high-beta names) and study 06's 11-rule×28-asset breadth test
already found momentum-style rules (net-liquidity acceleration) beat mean-reversion-style
rules broadly across this exact asset set. I would not spend the next study slot on
EMA/RSI/volume — the prior evidence points the wrong way for a trend-heavy book, and it's
the same conclusion the program has already reached three separate times (07/13/06). The
higher-value places to look are exactly the ones the user already flagged and the program
has under-covered: **studies 16/17's classic-portfolio comparisons are the most useful
context-setting work done so far** (the user's book earns +14-27pp/yr extra XIRR for
−9-17pp more drawdown vs. the mildest classic — "a defensible aggressive-growth trade,"
per file 16 — but loses on both axes at the noisy 1-year window), and cost/tax efficiency
(Q3) is genuinely unexplored, not just under-explored.

---

## Q6 — What else is being missed?

- **The research-for-its-own-sake risk isn't hypothetical — it already happened, twice,
  in the last week, just not through the numbered-study door.** ADR-047 and ADR-048 are
  exactly the pattern a 20-study rigor program exists to prevent (untested structural
  reallocation; a parameter retuned on ad-hoc, non-committed, in-sample backtesting of the
  very mechanism study 19 just proved is borderline) — and they happened by going around
  the process rather than through it. The risk isn't "the user does more studies than
  needed," it's "a decision that skips the studies entirely because it's framed as
  'architecture' rather than 'research.'" I'd suggest a simple house rule: **any change to
  a live number that has ever been the subject of a numbered study (blend steepness, trim
  thresholds, regime-table weights) requires a study or an explicit, logged
  "we're overriding the evidence, here's why" note** — not just an ADR that cites half of
  an old study's finding.
- **20 studies is a lot for a spare-time program, and the marginal study is now cheaper to
  write than to properly interpret.** Studies 19/20 (DSR/PBO, vol-targeting) show the
  program maturing in the right direction — asking "is this edge even real" instead of
  "what's the next lever" — which is exactly the right instinct at this stage. Keep doing
  that kind of study, not more zoo-breadth studies.
- **The blend has been quietly running on stale/broken data for over a month
  (`FredClient` outage since 2026-07-06) without anyone noticing until this doc got
  written.** That's a bigger practical risk than any signal-design question: an automated
  system silently degrading and nobody finding out except by accident. A trivial
  freshness check/alert would have caught it same-day.
- **Nobody has asked "what does the user actually do with this every month?"** All 20
  studies are outcome-focused (XIRR, drawdown). None ask the operational question a data
  analyst would ask of any dashboard: does the user actually look at `blend_factor` or the
  regime panel monthly, trust it, and understand why it moved? If the honest answer is
  "not really, I just run the purchase session," then the blend's operational value is
  close to zero regardless of its statistical borderline-ness — it's overhead for a number
  the user doesn't act on beyond what the buy-list already encodes for them.
