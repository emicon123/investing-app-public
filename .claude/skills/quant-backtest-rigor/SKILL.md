---
name: quant-backtest-rigor
description: Checklist and reference for adding statistical rigor to backtest studies in backtest/ (walk-forward vs. in-sample validation, multiple-testing/data-mining checks via deflated Sharpe and PBO, vol-targeted position sizing, macro-liquidity signal grounding via the Howell/Alden frameworks, and IC-based signal formalization). Use this BEFORE designing a new backtest study in backtest/ (to build the checks in from the start) and AFTER finishing one (to stress-test the result before writing it up) — especially when a study scans more than a handful of rule/parameter/asset variants, when a result looks like a genuinely strong edge, or when building/tuning a macro timing signal such as the live ADR-044 blend_factor.
---

# Quant Backtest Rigor

## Overview

This repo's `backtest/` folder has run 17+ studies scanning macro-timing rules, trim
parameters, and portfolio variants. This skill is a checklist that catches the same
classes of mistake this program has already made once (in-sample parameter picks that
collapse out-of-sample, DCA-mechanic bugs, uncorrected multiple testing) so they aren't
repeated in study #19+. It does not replace `backtest/AGENTS.md` (the folder's standing
conventions — DCA mechanic, required baselines, trim-rule configs) — read that file
first for the mechanics; this skill is about the *statistical rigor* layered on top.

## Before starting a new study

1. **Read `backtest/AGENTS.md` in full first.** It owns: the real deficit-proportional
   catch-up DCA mechanic (not a fixed-weight split), the buy-only/no-auto-sell rule
   (ADR-045), the ADR-044 sliding blend, and the standing baselines every portfolio study
   must include (real buy-and-hold DCA, the 4 classic reference portfolios, the 3
   trim-rule configs). Do not re-derive or duplicate any of that here.
2. **Read `backtest/results/study_summary/README.md`'s Executive summary** so the new
   study builds on, rather than re-discovers, what's already established (e.g. net-liq
   acceleration → risk-on is the one broadly-validated edge; ISM rules are rejected;
   on-chain sell triggers are rejected; see the README for the current full list).
   Some studies and/or conclusion however might be incorrect.
3. **Decide up front how many variants you're about to try** (rule types × assets ×
   parameter/window values). Write the count down before running anything — you'll need
   it for the data-mining check below, and deciding it in advance stops the count from
   quietly growing as you go looking for something that works.
4. **Decide the walk-forward protocol before looking at results**, not after — see
   below.

## Walk-forward vs. in-sample: how to tell the difference

A result is **in-sample / descriptive** if any parameter (window length, threshold,
lookback) was chosen by looking at the full history and picking what performed best over
it. That's a valid first pass (this repo calls it "Part 1" in several studies) but it is
**not** a tradeable claim by itself — study 04's SOL result (in-sample Sharpe 1.40) is
the cautionary tale: it collapsed to 0.12 out-of-sample.

A result is a **walk-forward / OOS claim** only if: the parameter was re-picked using
*only* data available up to that point (expanding or rolling window), the OOS periods
that follow are then chained together to form the reported track record, and no
parameter, threshold, or rule was touched after seeing how the OOS chain performed.
Studies 04 and 06 (Part 3) are this repo's reference implementations of this pattern —
match their structure for any new timing claim.

**Rule: never report a Sharpe/CAGR/drawdown number as "the edge" unless it's the
walk-forward number.** In-sample numbers may be shown alongside for comparison, but must
be labeled in-sample and never used as the study's verdict.

## Data-mining gut check: "did I just find this by chance?"

If the study scans **N ≥ ~5 variants** (rule types, parameter values, assets, or windows)
before reporting the best one, run this gut check before writing the verdict:

1. Take the N variants' Sharpe ratios (or the relevant OOS metric) as your trial set.
2. Compute their cross-sectional standard deviation, `σ_SR`.
3. Estimate the **expected max Sharpe under pure luck** (Bailey & López de Prado's
   approximation, `γ ≈ 0.5772` = Euler-Mascheroni):
   ```
   SR₀ ≈ σ_SR · [(1-γ)·Φ⁻¹(1-1/N) + γ·Φ⁻¹(1-1/(N·e))]
   ```
   (`Φ⁻¹` = inverse standard normal CDF — `scipy.stats.norm.ppf`.)
4. **If the reported variant's Sharpe is not comfortably above `SR₀`, the result is
   indistinguishable from noise** — say so plainly in the writeup, the same way this
   program has rejected ideas before (see studies 09/12/13's rejections for the tone).
5. Be honest about what counts as "N" — it's not just the final comparison table, it's
   every variant that was *implicitly* tried on the way to spotlighting the winner
   (earlier scans, adjacent studies, related assets). Undercounting N is the single
   easiest way to fool yourself here.

For the exact worked example (this repo's own flagship ETH net-liquidity timing result,
checked both narrowly and broadly), and the fuller **Probability of Backtest Overfitting
(PBO) / combinatorial cross-validation recipe** for when N is large enough that a single
SR₀ number isn't enough (e.g. study 06's 11×28 rule zoo), see
**[references/methodology.md § Deflated Sharpe & PBO](references/methodology.md)**.

## Position sizing: when to consider vol-targeting

Every timing rule in this program so far is either binary (asset vs. cash) or a
continuous directional blend (ADR-044) — none scale the position/contribution by the
asset's own realized volatility. This matters for two reasons:
- It's a well-established, free-data upgrade (realized vol from the close-price CSVs
  already in `data/prices/`, no new data source, no paid vol index needed).
- The academic time-series-momentum literature found that **much of the apparent edge in
  directional timing strategies comes from the vol-scaling itself, not the direction
  call** — so a rule that "beats buy & hold" without vol-scaling may partly just be
  benefiting from lower realized vol (which mechanically inflates Sharpe), not genuine
  timing skill. **Check whether a study's Sharpe uplift also shows up in CAGR** (CAGR
  isn't mechanically inflated by lower vol the way Sharpe is) before crediting a rule
  with real directional skill.

Consider proposing a vol-targeted variant (position or contribution size scaled
inversely to realized vol) whenever a new study's edge is Sharpe-only and doesn't show a
matching CAGR improvement. See **[references/methodology.md § Volatility
Targeting](references/methodology.md)** for a concrete, buy-only-compatible design that
fits this repo's real deficit-proportional catch-up DCA mechanic.

## Signal construction: ground it in practitioner frameworks, don't reinvent

Before inventing a new macro-liquidity signal or tweaking the ADR-044 blend, check it
against two established practitioner frameworks rather than re-deriving from scratch:
- **Lyn Alden's "Fed net liquidity"** = `Fed balance sheet − TGA − Reverse Repo`. This
  repo's `data/macro/FED_NET_LIQUIDITY.csv` already matches this exactly — no change
  needed, just know that it's grounded in a named, citable framework, not an invented
  proxy.
- **Michael Howell / CrossBorder Capital's Global Liquidity Index** is a much richer
  ~80-economy index (official + financial-sector + private-sector liquidity, including
  repo/collateral/shadow-banking, not just central-bank M2). It's a **paid product** —
  don't try to scrape or replicate it in full. But know where this repo's free 5-country
  `GLOBAL_M2.csv` sum diverges from it (coverage, and raw-FX-level conversion vs.
  Howell's richer weighting) before treating "Global M2" as equivalent to "Howell's GLI"
  in a writeup — they are related but not the same thing.

Full framework notes and a concrete, untested improvement idea (reduce FX-conversion
noise in the Global M2 series) are in **[references/methodology.md § Macro-Liquidity
Frameworks](references/methodology.md)**.

## Formalizing correlation claims: the IC framework

This repo already computes Spearman rank correlations with block-bootstrap CIs (see
`backtest/results/study_summary/01_data_and_methods.md`) — that machinery does not need
to be rebuilt. But factor investing has a standard, slightly more formal vocabulary for
the same idea (Information Coefficient, IC time series, ICIR, IC t-stat, IC-decay curves
across forecast horizons) that would make existing lead-lag and rule-breadth findings
(studies 03 and 06) more legible and add a formal significance test on top of the
already-computed correlations, **without requiring any new data**. See
**[references/methodology.md § IC Framework](references/methodology.md)** before adding
a new correlation claim to a study, and consider reframing an existing lead-lag result
as an IC-decay curve rather than inventing a new statistic.

## Before writing up the study

- [ ] Label any in-sample-only number as in-sample; never call it "the edge."
- [ ] If N ≥ ~5 variants were scanned, ran the data-mining gut check (SR₀ vs. observed).
- [ ] Included the standing baselines from `backtest/AGENTS.md` if this is a
      portfolio-level study (real buy-and-hold DCA, 4 classic portfolios, 3 trim configs).
- [ ] Checked whether a Sharpe-only edge also shows up in CAGR (vol-targeting caveat).
- [ ] If a new macro signal was built, checked it against Alden's net-liquidity
      definition and noted (don't overclaim) how it relates to Howell's richer GLI.
- [ ] Was honest in the verdict — this program's house style rejects ideas that don't
      pan out (see studies 09/12/13) rather than overselling weak results; match that.
- [ ] Added the study to `backtest/results/study_summary/README.md` per its own stated
      convention (Files-table row + Executive-summary point) — don't skip this step.

## Resources

- **`references/methodology.md`** — the deeper technical material this file points to:
  exact Deflated Sharpe Ratio formula + worked repo example, the full PBO/combinatorial
  cross-validation recipe, a concrete vol-targeted DCA design, Howell/Alden framework
  detail, and the IC/ICIR/IC-decay formalization. Read the specific section linked above
  rather than the whole file top-to-bottom unless doing a first-time deep read.
