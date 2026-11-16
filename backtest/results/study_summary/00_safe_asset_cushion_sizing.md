# Safe-asset cushion sizing for a new, standalone glide-path portfolio

**Date:** 2026-07-27
**Script:** `safe_cushion_glidepath_backtest.py` -> `results/safe_cushion_glidepath_backtest.json`
**Scope:** this is explicitly **not** a MAIN/retirement-portfolio study. The user is
starting a brand-new, separate, manually-managed portfolio; none of ADR-040
(deficit-proportional catch-up DCA), ADR-044 (sliding macro blend), or ADR-045/048
(buy-only, trim-suggestion UI) apply here.

## The question

Target end-state is US100/China at **66/33** (also tested: **75/25**). Worried that
current valuations are a bull-market peak, the user wants to start with a safe-asset
cushion (Gold + bonds: ZPR1.DE, ETFBCASH.PL) instead of deploying straight to target,
DCA monthly, and manually flip 100% into the target mix the moment "the index" (US100)
drops 20% from its peak. Question: **what safe% is needed so that, buying at the worst
plausible historical peak, the account is still at or above its contributed capital 24
months later?**

Reviewed by data-analyst + data-scientist before running (per `backtest/AGENTS.md`).
Their input shaped the design: gold and bonds are modeled as separate sub-allocations
(not blended) because ETFBCASH.PL turned out to have a real ~-35% drawdown in the 2022
rate shock -- not the low-vol "cash" its ticker implies; results are reported
per-scenario, never as one blended number; the China-peak scenario is called out
separately as a concentration-risk finding, not folded into "worst case."

## Method

- 1000 PLN initial + 1000 PLN/month, 24 months, no rebalancing beyond the described
  trigger (this is a clean-slate strategy, not MAIN's real algorithm).
- Risk sleeve: 66% QQQ (US100 proxy) / 33% FXI (China proxy -- FXI has 21.7y of history
  vs. ICGA.DE's 7y, the repo's standing long-history substitute per `AGENTS.md`).
  75/25 tested as an alternative split.
- Safe sleeve compositions: **gold100**, **gold70/bond30** (bonds = flat 3.5%/yr
  synthetic, the user's own suggested fallback for the pre-2021/2023 data gap), and
  **gold60/bond40-realstress** (bonds = ETFBCASH.PL's actual price path wherever it
  existed, spliced onto the synthetic path before its 2021-06 inception) -- a
  deliberately harsh sensitivity given that fund's real 2022 behavior.
- Trigger: QQQ (USD) drawdown from its own running high **since the portfolio's start
  date** (not the 2000 dot-com all-time-high, which wasn't retested until ~2015 and
  would wrongly mark 2007 as already -50% off-peak) hits -20% -> convert the entire
  safe-sleeve balance into the 66/33 (or 75/25) target that month, all future
  contributions go 100% to target permanently.
- All USD/EUR assets converted to PLN via USDPLN_X/EURPLN_X.

### Scenarios (chosen a priori on macro/historical grounds, not cherry-picked)

| Key | Start (peak) | Why |
|---|---|---|
| A | 2007-10-31 | GFC -- US and China equities peaked together; real QQQ/FXI/GLD data |
| B | 2021-12-27 | QQQ all-time-high before the 2022 bear -- the stock/bond correlation-breakdown year |
| C | 2021-02-17 | FXI's own peak -- China's structural derate (Beijing tech crackdown), not V-shaped |
| D | 2015-04-27 | 2015 China deval/crash -- a distinct mechanism from C |
| E | 2020-02-19 | COVID -- fast V-shaped crash, the recovery counterexample |
| F | 2018-12-24 | No-crash grinding bull -- opportunity-cost ceiling, not a downside case |
| dot-com (sensitivity) | 2000-03-27 | Worst U.S. tech drawdown ever (-83%). No FXI/GLD data this far back -- QQQ-only, 100% synthetic-bond safe sleeve, **not a primary scenario** |

## Results -- minimum safe% for month-24 breakeven (gold70/bond30 synthetic, primary)

| Scenario | 66/33 min safe% | 75/25 min safe% | ratio@0% | ratio@100% | trigger fires |
|---|---|---|---|---|---|
| A GFC 2007 | **0%** | 0% | 1.163 | 1.218 | month 4 |
| B QQQ ATH 2021 | **0%** | 0% | 1.026 | 1.048 | month 4 |
| C FXI peak 2021 | **35%** | 30% | 0.969 | 1.065 | month 15 |
| D China deval 2015 | **0%** | 0% | 1.159 | 1.039 | never |
| E COVID 2020 | **0%** | 0% | 1.082 | 1.094 | month 1 |
| F grinding bull (opp. cost) | 0% (n/a, no downside) | 0% | 1.318 | 1.424 | month 15 |
| dot-com (sensitivity) | **cushion doesn't work** -- see below | -- | 0.682 | 0.685 | **month 1** |

## Two findings that matter more than the headline number

**1. Monthly DCA already does most of the protective work.** In every real,
fully-data-backed scenario except C, breakeven at month 24 is achieved with **0% safe
cushion** -- because only the first month's 1000 PLN is exposed at the actual peak;
every later contribution buys into an already-cheaper market. The cushion doesn't
change *solvency* in these cases, it reduces the ride's *volatility* -- the path still
dips well below contributed capital mid-way even where it recovers by month 24 (e.g.
scenario A's min-on-path ratio is 0.87 even with a 100%-safe start, before the GFC
trigger fires and deploys the cushion into the recovering-but-still-crashed sleeve).

**2. The fixed "-20% -> flip everything" rule has a real blind spot: slow,
multi-year bear markets.** It works fine for V-shaped (COVID), moderate (2022), and
GFC-style crashes because the trigger fires only 1-4 months in, when there isn't much
cushion built up yet to lose, or the ratio needing an exact solve is manageable. But
run against dot-com (QQQ-only sensitivity, since China/gold have no data that far
back): the -20% level was breached in the **first month**, so almost the entire
cushion -- regardless of how large you set safe% -- gets dumped into an asset that then
fell for another ~2.5 years (peak-to-trough -83%). Result: final ratio is ~0.68
*whether safe% is 0% or 100%* (0.682 vs. 0.685). Re-running the same window with the
trigger disabled (holding the cushion the full 24 months, no manual flip) shows the
cushion working exactly as intended -- 100% safe reaches breakeven (1.031) instead of
0.685. **The mechanism that protects you against a fast-triggering shallow bear is the
same one that defeats you in a slow deep one: it commits your safety money right as
the crash is just getting started, not after it's over.** This is a property of the
fixed 20%-drawdown rule itself, not of any safe% choice -- not something more cushion
can fix.

**3. Scenario C (China's own 2021-22 structural derate) is a concentration-risk
finding, not a sizing one.** The trigger is QQQ-based, so it doesn't fire on an
FXI-only shock until QQQ's *own*, later (2022) peak eventually breaks -20% (month 15)
-- by which point FXI has already done most of its damage on its own schedule. 25-35%
safe% gets this scenario to almost-exactly breakeven; with the harsher
gold60/bond40-realstress mix, even 100% safe caps out at 0.999 -- a rounding error
short, not a real shortfall, but a reminder that 33% in a single foreign-market ETF
carries a risk this cushion structure can only partially offset.

## Recommendation

- **25-35% initial safe allocation, gold-heavy (70/30 or 100% gold over bonds)**
  covers every real, fully-data-backed joint-crash scenario tested (A/B/C/D/E) with
  room to spare, and the opportunity cost of holding that much cushion in a grinding
  bull (F) is a fairly small give-up (ratio 1.32 vs. 1.42 at month 24 for 0% vs. 100%
  safe -- linear in between).
- **Gold over bonds** for the safe sleeve: ETFBCASH.PL had a real ~-35% drawdown
  exactly during 2022's equity selloff (positively correlated with the crash it's
  supposed to hedge), while GLD was down only ~10% at the same QQQ trough and was
  +13.7% by month 24. ZPR1.DE looks genuinely low-vol so far, but only has 2.7 years of
  history (unstress-tested, not proven safe).
- **Don't treat the -20%-flip rule as protection against a genuine bubble-peak
  unwind.** If the specific fear is "this looks like 2000," the fixed rule as described
  doesn't deliver the safety the cushion is meant to provide -- it just delays full
  exposure by one month. That's a design tradeoff for the user to weigh (e.g. flipping
  only half the cushion at -20% and holding the rest for a deeper confirmation), not
  something back-tested/optimized here -- tuning the trigger threshold to fit one
  historical window would be exactly the kind of single-scenario overfit the
  `quant-backtest-rigor` skill warns against.

## Scope notes / what this is NOT

- Not a Sharpe/edge search -- one monotonic parameter (safe%) scanned against six fixed,
  a-priori scenarios. The DSR/PBO data-mining check doesn't apply.
- Nominal breakeven only, no inflation adjustment.
- Bond history gap: ZPR1.DE (2.7y) and ETFBCASH.PL (4y) don't cover 2007 or 2015;
  those periods use the user's own suggested ~3.5%/yr flat assumption, spliced
  continuously into real prices from each fund's inception.
- Dot-com is a QQQ-only sensitivity (no FXI/GLD data pre-2007) -- directionally
  important (see finding #2) but not put on equal footing with the primary scenarios.
