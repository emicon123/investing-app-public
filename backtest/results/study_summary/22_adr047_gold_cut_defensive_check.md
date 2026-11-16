# 22 — ADR-047 gold-cut / equity-destination defensive-behavior check

Scripts: `gold_cut_defensive_check_backtest.py` (proxy-based portfolio-level A/B) →
`results/gold_cut_defensive_check_backtest.md`; `nqse_icga_defensiveness_check.py` (literal
NQSE.DE/ICGA.DE check) → `results/nqse_icga_defensiveness_check.md`. Both retained per this
task's explicit instruction. Design reviewed by data-analyst + data-scientist agents before
execution (`.claude/tasks/done/adr-047-048-walkforward-validation.task.md`, "Reviewer's
verdict" section) — the proxy-vs-literal split and metric package below are the
reviewer-tightened design, not the task's original "what did the price do" framing.

## Context

ADR-047 (2026-07-07) cut Gold's risk-on target 30%→10% (risk-off unchanged at 30%) and cut
Bonds to 0%/0% (was 10%/10% each, a 20-point combined risk-off allocation), routing the freed
weight into **NQSE.DE** (Nasdaq-100 UCITS) and **ICGA.DE** (MSCI China UCITS) — their combined
risk-off target rose from 22.5% to 42.5% (`05-rebalancing-algorithm.md` §7.3.1). **Never
backtested anywhere in this program before this task.** The data-scientist review flagged a
specific, untested, contrary hypothesis: per study #05's own liquidity-beta ranking, equities
(β≈1) are not obviously "safer" than gold in a liquidity-driven risk-off regime — increasing
Nasdaq-100/China exposure specifically when the macro gate says risk-off could be
**procyclical, not defensive**, the opposite of what the freed weight is supposed to do.

## Proxy-vs-literal split

NQSE.DE (2018-09→) and ICGA.DE (2019-07→) are too short for the 8y (2018-06) and 13y (2013-06)
windows — spot-checked directly against `backtest/data/prices/`, not assumed. This study
therefore runs two separate, clearly labeled pieces, per the reviewer's tightening:

- **Part A (proxy-based, full 5 windows)**: the portfolio-level A/B uses **QQQ/FXI** as
  stand-ins for NQSE.DE/ICGA.DE, exactly matching study 14's own scaffold/analogy convention.
  Answers *"is a ~20pp gold-to-equity reallocation of this rough shape defensible"*.
- **Part B (literal-ticker check)**: uses the **actual** NQSE.DE/ICGA.DE price series (~7-8y,
  enough for a targeted check, not a full 5-window backtest). Answers *"do the two actual ETFs
  behave defensively"*. These are two different claims from two different data sources — a
  passing Part A does not prove Part B, and vice versa; both are stated plainly below.

## Part A — proxy-based portfolio-level A/B

Reuses study 14's demo portfolio, real deficit-proportional catch-up DCA, 2.0×/1.5× trim rule
(held fixed), 5 windows, 15bps, 1000+1000/mo PLN.

**OLD table** (pre-ADR-047, = study 14's existing RISK_ON/RISK_OFF unchanged) **vs. NEW table**
(ADR-047 applied: Gold risk-on 30→10; QQQ/FXI, as the demo's NQSE.DE/ICGA.DE analogs, take
ADR-047's real absolute new values 18.5/28.5 and 9.0/14.0 directly — the demo's OLD QQQ/FXI
targets already numerically matched real production's OLD NQSE.DE/ICGA.DE targets, so no
rescaling was needed; every other bucket untouched):

| asset | OLD risk-on | OLD risk-off | NEW risk-on | NEW risk-off |
|---|--:|--:|--:|--:|
| GLD | 30.0 | 30.0 | 10.0 | 30.0 |
| CASH | 10.0 | 30.625 | 10.0 | 10.625 |
| QQQ (NQSE.DE proxy) | 5.0 | 15.0 | 18.5 | 28.5 |
| FXI (ICGA.DE proxy) | 2.5 | 7.5 | 9.0 | 14.0 |
| (crypto sleeve, MSTR) | unchanged | unchanged | unchanged | unchanged |

**Divisor used for the "new" cell**: not ADR-048's literal 1.0 — per study #21's own finding
(this task's companion study) that its walk-forward chain **never** selects divisor=1.0 at any
of its 12 checkpoints, this study uses **0.5**, the value study 21's walk-forward protocol
actually selects at its most recent checkpoint (2025-06) — reusing that result rather than
re-litigating it here, per the task's own instruction.

**2×2 matrix (N=4)** — cell = XIRR%/yr (maxDD%):

| table | divisor | 13y | 8y | 5y | 3y | 1y |
|---|--:|--:|--:|--:|--:|--:|
| OLD (pre-ADR-047) | 2.0 | 38 (−22) | 37 (−22) | 24 (−30) | 22 (−15) | −7 (−14) |
| OLD (pre-ADR-047) | 0.5 | 39 (−26) | 41 (−16) | 28 (−21) | 26 (−12) | −2 (−10) |
| NEW (post-ADR-047) | 2.0 | 38 (−22) | 37 (−24) | 25 (−34) | 23 (−17) | −4 (−13) |
| NEW (post-ADR-047) | 0.5 | 39 (−26) | 41 (−17) | 29 (−26) | 28 (−13) | −1 (−9) |

**Table effect (NEW − OLD), holding divisor fixed at 2.0**: +0.0pp XIRR / −0.2pp maxDD at 13y
(negligible), **+0.3pp XIRR / −2.1pp maxDD at 8y** (worse drawdown), and (from the full table
above) **+1pp XIRR / −4pp maxDD at 5y** (25 vs 24, −34 vs −30 — worse drawdown again). The
table change is close to a wash at the longest (13y) and shortest (1y, better) windows, but
consistently makes drawdown **worse**, not better, specifically at the 8y and 5y windows — the
two windows that contain the dominant 2021-12→2023-01 risk-off episode (5y, start 2021-06) and
the 2018-08→2019-01 episode (8y, start 2018-06). This is directionally consistent with, though
modest in absolute magnitude compared to, the procyclicality concern — the aggregate portfolio
effect is muted because gold is a modest share of a crypto-dominated book and QQQ/FXI's own
bull-market returns partly offset the risk-off cost over long compounding windows (see Part B
for why this proxy-level muting is misleading about the literal tickers specifically).

**Standing baseline — 60/40 classic-portfolio cross-check** (SPY/AGG, BUYHOLD real catch-up
DCA): 13y 10 (−12), 8y 9 (−12), 5y 8 (−12), 3y 10 (−12), 1y 14 (−3) — included per
`backtest/AGENTS.md`'s standing-baseline rule (required here, unlike study 21, since a ~20pp
gold-to-equity reallocation is a general portfolio-construction question).

## Part B — literal-ticker defensiveness check (the decisive evidence)

Uses NQSE.DE (95mo, 2018-09..2026-07) and ICGA.DE (85mo, 2019-07..2026-07) directly, GLD via
`GLD_full.csv` (the real production Gold ticker `ETCGLDRMAU_WA.csv` has only ~1.5y of history
and is unusable for this or any of this program's windows).

### 1. Liquidity-beta regression (extends study #05's own HAC/Newey-West methodology)

`asset YoY return = a + β·(driver YoY)`, HAC/Newey-West SEs, min 24mo — genuinely new
computation (confirmed NQSE.DE/ICGA.DE are not in study 05's existing output; both price files
were only added 2026-07-10):

| ticker | driver | β | t | R² | n |
|---|---|--:|--:|--:|--:|
| NQSE.DE | net-liq YoY | 0.60 | 3.52 | 0.33 | 82 |
| NQSE.DE | Global-M2 YoY | 2.47 | 4.39 | 0.46 | 80 |
| ICGA.DE | net-liq YoY | 0.41 | 3.01 | 0.14 | 72 |
| ICGA.DE | Global-M2 YoY | 2.04 | 7.25 | 0.35 | 70 |

For scale, study 05's own category aggregates (mean net-liq β): Crypto ≈24, Crypto-equity ≈6,
**Equity ≈1**, FX ≈0.2, **Metal (gold) ≈0.1**. Both tickers land statistically significantly
positive (all 4 |t| > 3), and both are clearly on the **equity** side of that spectrum (at or
above the equity aggregate on Global-M2, comfortably above the metals aggregate on both
drivers) — they behave like ordinary liquidity-sensitive equity-index ETFs, not like gold.

### 2. Risk-off-conditional split (blend≥0.6, original /2.0-divisor classification)

| ticker | subsample | β (net-liq YoY) | n | corr(ret, GLD ret) |
|---|---|--:|--:|--:|
| NQSE.DE | risk-off (blend≥0.6) | **1.40** | 25 | −0.03 |
| NQSE.DE | all other months | 0.35 | 57 | −0.04 |
| ICGA.DE | risk-off (blend≥0.6) | 0.38 | 25 | 0.26 |
| ICGA.DE | all other months | 0.33 | 47 | 0.18 |

**NQSE.DE's liquidity beta roughly quadruples in risk-off months (1.40 vs 0.35)** — it becomes
*more* liquidity-sensitive exactly when the macro gate says risk-off, the opposite of "acts
like gold when it matters." ICGA.DE's conditional beta is roughly flat. Small-sample caveat:
most risk-off months in either ticker's short history belong to the single 2021-12→2023-01
episode — treat as descriptive, not statistically decisive (see Caveats).

### 3. Episode-level table (GLD vs NQSE.DE vs ICGA.DE, PLN, cumulative return / maxDD)

Study 14's own 5 named episodes; only 3 are covered at all by either ticker (2012 and 2014-15
predate both):

| episode | GLD | NQSE.DE | ICGA.DE |
|---|---|---|---|
| 2018-08..2019-01 (NQSE.DE only, truncated to actual 2018-10 start) | +10.0% (maxDD −1.1%) | **−10.2%** (maxDD −10.6%) | no data |
| 2021-05..2021-09 | +5.1% (maxDD −4.1%) | +7.9% (maxDD −3.6%) | **−13.2%** (maxDD −14.2%) |
| **2021-12..2023-01 (14mo — the same dominant episode study 14 already flagged)** | **+13.5%** (maxDD −8.8%) | **−27.8%** (maxDD −35.1%) | **−10.8%** (maxDD −31.9%) |

In the two largest and most decisive covered episodes, gold rose while both ETFs fell hard,
with drawdowns 3-4× deeper than gold's. Only in the smallest episode (2021-05..09, 5 months)
did NQSE.DE modestly beat gold; ICGA.DE underperformed gold in every single covered episode.

### 4. Correlation to GLD, risk-off months only

| ticker | corr(ret, GLD ret), risk-off months | n |
|---|--:|--:|
| NQSE.DE | −0.03 | 29 |
| ICGA.DE | +0.26 | 25 |

Neither shows the strong positive co-movement with gold that "acting like a defensive asset
alongside gold" would imply.

## Required verdict

**Across every metric in the literal-ticker check — unconditional β, conditional (risk-off)
β, episode-level return/drawdown, and correlation-to-gold — NQSE.DE and ICGA.DE behave like
ordinary liquidity-sensitive equity-index ETFs, not like defensive assets, and this is most
starkly true in exactly the regime ADR-047 asks them to absorb more weight in.**

- **The single largest, most decisive risk-off episode in this program's entire dataset**
  (2021-12→2023-01, 14 months — the same episode study 14 already identified as driving
  essentially its entire headline macro-blend result) shows gold rising +13.5% (maxDD only
  −8.8%) while NQSE.DE fell −27.8% (maxDD −35.1%) and ICGA.DE fell −10.8% (maxDD −31.9%). This
  is not a marginal or noisy read — it is the dominant available evidence in this whole
  22-study program's dataset, and it points the **opposite direction** from what ADR-047
  assumes.
- **NQSE.DE's liquidity beta roughly quadruples specifically in risk-off months** (1.40 vs
  0.35) — the mechanism gets *more* procyclical exactly when the macro gate flags risk-off,
  not less.
- **ADR-047 increases the combined NQSE.DE+ICGA.DE risk-off target from 22.5% to 42.5%** —
  i.e., it routes *more* money into these two ETFs specifically in the regime where, per this
  data, they fall hardest and gold protects best.
- **The proxy-based portfolio-level backtest (Part A) shows this cost as modest at the
  aggregate level** (a few pp of extra drawdown concentrated at the 5y/8y windows, roughly
  neutral or mildly positive at 13y/1y) — this is not a contradiction of the finding above, it
  is why a portfolio-level backtest alone would have understated the problem: gold is a modest
  fraction of a crypto-dominated book, so its behavior gets diluted at the aggregate level even
  though the literal-ticker evidence for the specific mechanism ADR-047 relies on is
  unambiguous.

**This finding directly confirms the data-scientist's flagged, untested, contrary hypothesis**:
increasing Nasdaq-100/China exposure specifically when the macro gate says risk-off is
**procyclical, not defensive**, in the one large historical sample this program has to test it
against.

**Per the task's explicit guardrail, this requires the user's own explicit decision, not an
automatic revert**: whether to change ADR-047's risk-off destination for the freed gold/bond
weight (e.g. back toward Gold, which this program has repeatedly found to be the best-tested
de-risk destination — study #09 already rejected bond ETFs for the same reason gold beats them
here: real duration/market risk that correlates with the drawdown it's meant to buffer) is a
live-production decision affecting ~20 percentage points of target weight. This report
recommends flagging it for that decision and **stops here** — no `MacroDataService` constant,
Flyway-seeded regime table, or other backend code was touched by this task.

## Skill checklist (`quant-backtest-rigor/SKILL.md` "Before writing up the study")

- **In-sample labeling**: N/A in the DSR sense (this is not a parameter-selection study) — the
  proxy-vs-literal distinction is instead the key "don't over-claim" discipline here, stated
  plainly throughout.
- **Data-mining gut check**: the 2×2 matrix (N=4) and the 4-cell regression table are both
  small, a-priori, decided-up-front comparisons (matching study 20's precedent) — below the
  skill's N≥~5 formal trigger; not performatively run.
- **Standing baselines**: 60/40 classic-portfolio cross-check included (required here, per
  `backtest/AGENTS.md`, since this is a general allocation-shift question).
- **Sharpe-vs-CAGR check**: not directly applicable to the literal-ticker regression/episode
  metrics (not a timing-rule Sharpe uplift claim); Part A's table-effect deltas move return and
  drawdown in the same (worse-for-NEW) direction at 5y/8y, not a Sharpe-only artifact.
- **Macro-signal grounding**: no new macro signal was built (reuses the existing net-liq/
  Global-M2 series and study 14's fixed /2.0-divisor blend classification for episode
  boundaries) — N/A.
- **Honest verdict**: see "Required verdict" above — reported plainly, flagged for the user's
  explicit decision per the task's own guardrail, not implemented.
- **README updated**: Files-table row + Executive-summary point added.

## Caveats

- **Part A (proxy) and Part B (literal) answer different questions from different data
  sources** — a passing Part A does not prove the literal tickers are defensive, and Part B's
  clear finding does not mean the general reallocation *shape* is indefensible at the
  aggregate-portfolio level; both are stated separately above, per the task's own instruction.
- **Episode coverage is genuinely thin** (at most 2-3 episodes; the largest, 2021-12→2023-01,
  is the same single episode study 14 already flagged as driving essentially all of its own
  headline result) — every conditional/episode statistic above rests on this n≈1 episode more
  than on a general pattern; reported as descriptive, not statistically decisive, per the
  task's explicit instruction not to oversell a 2-3-episode sample.
- **NQSE.DE/ICGA.DE are EUR-denominated** — Part B's Part 1 (liquidity-beta regression) uses
  each ticker's native EUR return, unconverted, matching study 05's own convention (its
  published ranking includes raw FX-pair rows, implying no secondary PLN normalization was
  applied); Parts 2-4 (conditional split, episode table, correlation) convert to PLN via
  EURPLN_X/USDPLN_X for apples-to-apples comparability with the rest of this repo — a
  deliberate design choice, not a re-derivation of study 05's own convention.
- **The new-table derivation (Part A) is a judgment-call analogy** — ADR-047's real absolute
  point changes for NQSE.DE/ICGA.DE were reused unscaled for QQQ/FXI (valid since the demo
  portfolio's OLD values already numerically matched real production's OLD values), not
  independently re-optimized for this demo portfolio's own weight structure.
- **"New divisor" = 0.5, not ADR-048's literal live value of 1.0** — per study 21's finding
  that the walk-forward-selected divisor is never 1.0 at any checkpoint; Part A's matrix
  therefore does not include a literal "1.0" cell, by design, reusing study 21's result rather
  than re-litigating it.
- **Newey-West lag** (L = floor(4·(T/100)^(2/9)), Newey & West 1994's standard rule of thumb)
  is a named convention, not a confirmed match to study 05's own (now-unrecoverable, deleted)
  exact choice.
- Same caveats as study 14 apply to Part A unchanged: miners-basket survivorship, gold's
  multi-year rally through most of this sample, overlapping windows sharing one underlying
  price/macro history, 1y/3y windows as single-episode reads.

## 2026-09-25 correction note (task 28, second pass — re-run, framing changed, verdict unchanged)
Re-ran with NEW = the **live** tables (`portfolio_spec.py` SIMPLIFIED_*: QQQ 22.5/27.5,
FXI 2.5/7.5, ETH 10.0/1.0, LINK 1.5/0.15, Bonds risk-off-only, no CASH) — no longer just
ADR-047's own application, but the cumulative live state through ADR-074/087. The
"table effect" delta therefore measures pre-ADR-047 vs. live: NEW-OLD @0.5 (live
divisor) is +0.6pp (13y) / +1.4pp (8y) XIRR — direction-consistent, same verdict (the
reallocation shape is defensible; the literal tickers remain procyclical per the
companion script, which needed no change). CURRENT 1.5x/1.0x trim throughout;
`USDPLN_X.csv` restored; END 2026-09.
