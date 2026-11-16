Agents to use when working inside this folder:
- data-analyst
- data-scientist
You have to spawn those two subagents before running any study and ask them to enforce the study (how to get better conclusions, how to drive the study correctly, is the study valid, etc.) 

Every test you do in this directory should assume:
- 1000 PLN starting cash + 1000 PLN added each month - regularly to the test portfolio
- studies and backtest conclusions are saved in results/study_summary — `results/study_summary/README.md` is the master index of every study; when a study finishes, add a numbered `NN_slug.md` file there plus a row/line in the README's Files table and Executive summary
- studies should have multiple starting dates in order to mitigate the risk of starting the backtest at the very bottom or top of a market. I.e. 1, 3, 5, 8, 13 yrs (if possible - some asset prices and/or macro data might not have 13 yrs of history - if that happens, you should look into APIs like yahoo finance or macro sites in order to fetch missing data)
- for studies you can either use asset data from /data/prices or use main portfolio as portfolio for testing

## Rebalancing rule — the main portfolio is BUY-ONLY, no auto-sell at all (ADR-045, 2026-07-05)
- **The real DCA contribution algorithm is deficit-proportional "catch-up," NOT a fixed-weight
  split** (corrected 2026-07-09 — several earlier backtest scripts, incl. studies 07/14/15,
  approximated it as `contribution × target_weight_i`, which is NOT what production does and
  is a known, flagged-but-not-yet-corrected gap in those studies' figures). The real algorithm
  (`MainRebalancingStrategy.java`, ADR-040/045, 12-step):
  1. `O_total = current_total_value + A` (A = this month's contribution)
  2. `P_i = target_weight_i × O_total` (what position i *should* be worth after the deposit)
  3. `Q_i = P_i − current_value_i` (deficit if positive, surplus if negative)
  4. `Q_pos = sum of all positive Q_i`
  5. `buy_i = A × Q_i / Q_pos` for every `Q_i > 0`; **zero** for any position at/above target
     (`Q_i <= 0`). **This is NOT winner-take-all to a single most-underweight asset** — the whole
     contribution is spread across *every currently-underweight position*, each getting a share
     proportional to the size of its own deficit (`Q_i`) relative to the total deficit (`Q_pos`).
     The biggest laggard gets the biggest slice, but every below-target position gets something;
     it's also not a fixed evenly/target-weighted split across everything (positions already
     at/above target get exactly zero). This is "buy only, try to catch up with the rest," never
     a sell.
  - Implement this exact mechanic (not the fixed-weight-split approximation) in any new backtest
    that claims to model "the real DCA" or a buy-and-hold/no-sell baseline for either the user's
    own portfolio or a comparison portfolio.
- **There is no automatic sell/trim mechanism of any kind.** The 2×-trim-to-gold rule described
  in earlier versions of this file existed under ADR-042/043/044 and was **removed entirely** by
  ADR-045 ("Purchases Endpoint Is Buy-Only") after the user found it silently selling positions
  they hadn't asked to sell — see `docs/architecture/05-rebalancing-algorithm.md` §7.4 (`07-adrs.md`
  is retired, merged into the numbered docs) and the hard rule in project memory
  (`manual-sell-only-rule`): **only the user's manual "Sprzedaj" button ever sells**, never an
  algorithm. A backtest modeling automatic trim-selling is modeling a mechanism that no longer
  exists in production — clearly label any such variant "historical / pre-ADR-045 reference,"
  never "current" or "the real algo." The manual trim itself is now UI-assisted at **150%
  trigger / 100% (exact target) trim-to** (ADR-048, 2026-07-10, tightened from 200%/150% — see
  §7.5) — a frontend-only flag/suggestion, still zero automation.
- **The regime that DOES still exist (ADR-044, live and computing since 2026-07-06 — see
  `sliding-macro-regime-targets` memory / ADR-046)**: every MAIN position has a risk-on target
  and a risk-off target; a continuous `blend_factor` (0=full risk-on, 1=full risk-off) derived
  from net-liquidity + Global-M2 YoY-acceleration z-scores interpolates between them
  (`effective = riskOn×(1−blend) + riskOff×blend`). This changes **what proportion each asset's
  monthly contribution buy targets**, never triggers a sell — a risk-off tilt just means the
  DCA engine buys less of the de-risked asset (and more of gold/bonds/cash) until the blend
  moves back, not that anything gets sold. Model this shifting-target behavior in any backtest
  that claims to reflect the current live mechanism; see `05-rebalancing-algorithm.md` §7.2 for
  the exact blend formula (steepness divisor tightened 2.0→1.0 by ADR-048, 2026-07-10) and §7
  generally for the implementation spec.
- A "rebalanced to target (with selling)" variant may be shown only as an explicitly-labeled
  reference benchmark — it is NOT the real algo, past or present.

## Standing baselines — required in every portfolio-level backtest (added 2026-07-09, study #16)

Any study that reports return/drawdown for a rebalancing rule or overlay must include these
reference points alongside it, not just compare rule variants against each other in isolation:

1. **Plain buy-and-hold DCA baseline — using the REAL catch-up algorithm above, not a
   fixed-weight split.** Monthly contributions are routed via the deficit-proportional `Q_i/Q_pos`
   mechanic (catches underweight positions up using new money only) — but **no selling/trimming
   of any kind, ever**; positions at/above target simply get zero new money and otherwise just
   drift with price returns. This is the "what if I did nothing clever besides my own real
   DCA" control — distinct from a naive fixed-percentage-split DCA, which is a *weaker*, less
   accurate baseline (see correction above). **Resolved** (verified 2026-07-13, study #23):
   `portfolio_strategy_backtest.py` (07) and `portfolio_macro_blend_backtest.py` (14) both
   already implement the real `Q_pos` catch-up mechanic in their current committed form —
   the gap flagged 2026-07-09 (which named 07/14/15 as still using the naive fixed-split)
   is itself now stale for 07/14; study 16's BUYHOLD was corrected the same day. Study 15's
   headline fire counts separately went stale for an unrelated reason (a 2026-07-10 price-data
   refresh extending `QQQ_full.csv`/`FXI_full.csv` history, not a DCA-mechanic bug) — see
   study #23 and study #15's own refresh note. Use the real mechanic for any new work.
2. **Classic reference portfolios**, so the user's own strategy is judged against genuine
   textbook alternatives, not just against tuning variants of itself. Use these four canonical,
   published-weight portfolios (don't improvise the ratios — see study #16,
   `results/study_summary/16_classic_portfolios.md`, and `classic_portfolios_backtest.py` /
   `fetch_classic_portfolio_data.py` for the reusable implementation and ticker mapping):
   - **60/40**: SPY 60 / AGG 40
   - **Permanent Portfolio** (Harry Browne): SPY 25 / TLT 25 / CASH 25 / GLD 25
   - **Golden Butterfly** (Portfolio Charts): SPY 20 / IWN 20 / TLT 20 / SHY 20 / GLD 20
   - **Ray Dalio All Weather**: SPY 30 / TLT 40 / IEF 15 / GLD 7.5 / DBC 7.5

   Tickers `AGG`/`IEF`/`SHY`/`IWN`/`DBC` are already fetched into `data/prices/*_full.csv`
   (all clear 13y of history by 7.3y+ margin — reuse, don't refetch). These classics use
   **standard annual full rebalance-to-target** by default (their own textbook convention), plus
   optionally the plain buy-and-hold variant above and/or the live ADR-044 sliding blend
   (study #16 tested all three: FIXED/BLEND/BUYHOLD) — pick whichever variants are relevant to
   the question being asked, but don't drop the classic-portfolio comparison itself.

   **ICGA.DE proxy for long-history backtests**: `data/prices/FXI_full.csv` (iShares China
   Large-Cap, 2004-10-08→, 21.7y, USD-native) is the standing proxy for ICGA.DE (Amundi/Lyxor
   MSCI China UCITS, real history only 2019-07→, 7.0y) in any backtest needing 13y+ history —
   already used in study #22 Part A's portfolio-level A/B, and again in study #27's extended
   literal-methodology check (`icga_fxi_extended_defensiveness_check.py`). Note FXI tracks
   **FTSE China 25**, not **MSCI China** (ICGA.DE's actual index) — a close but not identical
   China-market proxy, not a literal ICGA.DE substitute; study #27 Part 5 found strong (0.95-
   0.99) return correlation between the two over their common 2019-08→2026-06 window, but
   confirm that's an acceptable approximation for whatever new question is being asked before
   reusing it further back than that.
3. **Threshold-triggered trim-rule variants — ALWAYS test these too, on every portfolio, not
   just the user's own book** (explicit standing instruction, 2026-07-09): any position that
   reaches `trigger × its current target` gets trimmed back down to `trim_to × target`,
   proceeds redistributed pro-rata across every *other* position's own target-weight share
   (general/portfolio-agnostic version — NOT dumped into one fixed "safe haven" asset like the
   user's own gold-specific version, unless a study is specifically about the user's own book).
   Test at minimum these three (trigger, trim_to) pairs: **(1.5, 1.0)** — the user's own live
   rule (tightened from (2.0, 1.5) by ADR-048, 2026-07-10 — see
   `docs/architecture/05-rebalancing-algorithm.md` §7.5; this is a manual UI sell-assist
   threshold, not an automated mechanism — ADR-045 removed all auto-selling, the user still
   trims by hand via "Sprzedaj", the UI just now flags/suggests at 150%/100% instead of
   200%/150%), **(2.0, 1.5)** — the prior value, kept as a reference point, not the current
   live rule, and **(2.0, 1.0)** — alongside BUYHOLD/FIXED/BLEND above, so every
   portfolio-level study compares: no correction ever (BUYHOLD) vs. calendar-based full rebalance
   (FIXED/BLEND) vs. threshold-triggered partial trim (these three). Report fire-frequency
   per-asset too (per study #15's method), not just XIRR/maxDD, since a trim rule that never
   fires for a given portfolio is a materially different result than one with equal returns that
   fired often.