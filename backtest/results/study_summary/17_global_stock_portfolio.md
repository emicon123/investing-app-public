# 17 — "Global 20": a 100%-individual-stock, no-ETF, no-bonds/gold global equity portfolio

Script: `global_stocks_backtest.py` → `results/global_stocks_backtest.md`,
`results/global_stocks_trim_events.csv`. Fetch: `fetch_global_stocks_data.py`
(new/refreshed `_full.csv` tickers). Companion to study #16, reusing the same
simulation engine (BUYHOLD real catch-up DCA, FIXED annual rebalance, BLEND
macro overlay, TRIM threshold variants, per `backtest/AGENTS.md` "Standing
baselines") but on a completely different kind of portfolio: 20 individual
stocks, equal-weight (5% each), no ETFs, no bonds/gold/cash at all.

## The portfolio — 20 companies, equal weight

| region | tickers |
|---|---|
| **USA (12, 60%)** | AAPL, MSFT, AMZN, GOOGL, NVDA, JPM, JNJ, WMT, TSLA, AVGO, KO, V |
| **Europe (4, 20%)** | ASML (Netherlands), NVO (Novo Nordisk, Denmark), SAP (Germany, US ADR), UL (Unilever, UK/NL, US ADR) |
| **Rest of World (4, 20%)** | TSM (Taiwan Semi, US ADR), TM (Toyota, Japan, US ADR), BABA (Alibaba, China, US ADR), SAMSUNG (Samsung Electronics, Korea) |

Equal-weight (5% each) is a simplifying convention (standard for a demo
portfolio like this), **not** market-cap-weighted — a cap-weighted version would
look very different (dominated by AAPL/MSFT/NVDA/AMZN/GOOGL).

## Data and the Samsung problem

Reused unchanged (already `_full.csv`): AAPL, MSFT, AMZN, GOOGL, NVDA, JPM,
TSLA. Fetched fresh `_full.csv` (same `fetch_long_history.py` convention,
`start=2007-01-01`) for everything else, including tickers that already had a
short-format plain `.csv` (deliberately not parsed/mixed in, per task brief):
JNJ, WMT, AVGO, KO, V, ASML, NVO, SAP, UL, TSM, TM, BABA.

**Samsung has no direct USD ADR.** Tried its primary Korea Exchange listing,
`005930.KS` (KRW-denominated), via yfinance first — it came back clean, with
history to 2007-01 in this dataset (Yahoo's practical data-start for this
ticker) and no notable gaps. **Used directly, no ADR-proxy fallback needed.**
Chained KRW → USD → PLN: fetched `USDKRW=X` (KRW per 1 USD) over the same date
range, then `price_USD = price_KRW / USDKRW`, then the usual `× USDPLN` hop —
one extra FX leg vs. every other ticker in this study.

## 13-year-history requirement — only Alibaba fails, and only partially

| ticker | true first-available date | clears 2013-06? | margin |
|---|---|---|--:|
| AAPL / MSFT / JPM / JNJ / WMT / KO / NVO / UL / TM | 1990-01-02 | YES | 23.4y |
| AMZN | 1997-05-15 | YES | 16.1y |
| TSM | 1997-10-09 | YES | 15.7y |
| NVDA | 1999-01-22 | YES | 14.4y |
| SAMSUNG | 2000-01-04 | YES | 13.4y |
| ASML | 1995-03-15 | YES | 18.2y |
| SAP | 1995-09-18 | YES | 17.8y |
| GOOGL | 2004-08-19 | YES | 8.8y |
| V | 2008-03-19 | YES | 5.2y |
| AVGO | 2009-08-06 | YES | 3.8y |
| TSLA | 2010-06-29 | YES | **3.0y** (smallest margin of any always-present ticker) |
| BABA | 2014-09-19 | **NO** | missing until 2014-09-19, ~1.2y after the window start |

**Only BABA fails.** It doesn't exist at all until September 2014, so it's
naturally absent from 2013-06 through 2014-09 inside the 13y window (its 5%
target share redistributes pro-rata among whichever of the other 19 stocks are
present that month — the same `present`-list mechanism the existing scripts
already use for not-yet-existing assets); it's present for the **entire**
8y/5y/3y/1y windows (all start after its IPO). **Samsung clears 13y comfortably,
contrary to the a-priori concern flagged in the task brief** — its data starts
2007-01 here, well before 2013-06. TSLA has the tightest margin of any
always-present ticker (3.0y) but still clears cleanly.

## Headline results — cell = XIRR%/yr (maxDD%)

| variant | 13y | 8y | 5y | 3y | 1y |
|---|--:|--:|--:|--:|--:|
| BUYHOLD (real catch-up DCA, never sells) | 32 (**−34**) | 27 (−24) | 26 (−15) | 28 (−12) | 30 (−4) |
| FIXED (annual full rebalance) | 27 (−17) | 27 (**−17**) | 26 (−16) | 30 (−13) | 35 (−5) |
| TRIM 2.0/1.5 (user's own live rule) | 27 (−18) | 26 (−17) | 25 (−15) | 27 (−12) | 30 (−4) |
| TRIM 2.0/1.0 | 26 (−18) | 26 (−17) | 24 (−15) | 27 (−12) | 30 (−4) |
| TRIM 1.5/1.0 | 26 (−16) | 25 (−15) | 24 (−15) | 26 (−12) | 30 (−4) |

Start months: 13y=2013-06, 8y=2018-06, 5y=2021-06, 3y=2023-06, 1y=2025-06.

## BLEND: not implemented as an active overlay — a design call, explained

This is a 100%-equity, no-bonds/gold/cash book. The ADR-044 risk-on/off
mechanism works by shifting weight from a RISK bucket into a DEFENSE bucket
that genuinely exists in the portfolio — every study #16 portfolio held at
least one real defensive asset-class leg (bonds, gold, or cash) to rotate into.
Here, all 20 positions are equally "risk" — there is nothing defensive to
rotate into without inventing an arbitrary split (e.g., picking 4 "safer"
stocks as a fake DEFENSE bucket), which would misrepresent both the portfolio
(equal-weight individual stocks, not asset classes) and the overlay (designed
for asset-class buckets). **Chose not to force a contrived RISK/DEFENSE split.**
Instead, the underlying blend regime's month occupancy is reported per window,
purely as context — **not applied to any weight**:

| window | RISK_ON | MID_ON | NEUTRAL | MID_OFF | RISK_OFF |
|---|--:|--:|--:|--:|--:|
| 13y | 7% | 38% | 31% | 15% | 8% |
| 8y | 10% | 34% | 23% | 20% | 12% |
| 5y | 5% | 33% | 22% | 23% | 17% |
| 3y | 3% | 53% | 28% | 17% | 0% |
| 1y | 8% | 58% | 17% | 17% | 0% |

## Trim-rule fire-frequency (single continuous pass, study #15's method)

One non-overlapping pass, **2010-07..2026-06 (15.9y)** — shorter than study
#16's 19.3y passes because TSLA (the latest-inception always-present stock,
2010-06-29) sets the common start; BABA is still allowed to phase in/out
within this window via the `present`-list mechanism.

| config | total fires | fired stocks (count each) |
|---|--:|---|
| 2.0/1.5 (user's own live rule) | **20** | NVDA (8), AVGO (5), TSLA (5), AMZN (1), TSM (1) |
| 2.0/1.0 | 14 | NVDA (5), AVGO (3), TSLA (3), AMZN (1), SAMSUNG (1), TSM (1) |
| 1.5/1.0 | 33 | NVDA (8), AVGO (6), TSLA (6), AMZN (2), ASML (2), NVO (2), TSM (2), AAPL (1), GOOGL (1), MSFT (1), SAMSUNG (1), WMT (1) |

**This is the opposite result from study #16, exactly as expected going in
(and now confirmed rather than assumed):** the trim rule fires **often** here —
20 times at the user's own live setting (2.0/1.5) over 15.9 years, essentially
identical in scale to the user's own crypto book's 21 fires (2.0/1.5, 19.3y,
study #15) — because individual stocks, unlike diversified asset-class
buckets, regularly have idiosyncratic runs large enough to double relative to
a fixed 5% target. **NVDA, AVGO, and TSLA do almost all the firing** (8+5+5 =
18 of 20 fires at 2.0/1.5, 90%) — the AI/semiconductor supercycle and Tesla's
multi-year run are the dominant drivers, structurally similar to how BTC does
most of the firing in the user's own crypto book (study #15).

## Verdict: for individual stocks, rebalancing/trim actually earns its keep — the opposite of study #16's finding

- **BUYHOLD vs FIXED diverge sharply here, unlike study #16.** At 13y, BUYHOLD's
  drawdown (−34%) is roughly double FIXED's (−17%) for only 5pp less return
  (32% vs 27%); at 8y the gap is similar (−24% vs −17%, same 27% return both).
  Study #16 found BUYHOLD and FIXED converge to within 1pp of each other for
  diversified asset-class portfolios — **that finding does not generalize to
  individual stocks.** The reason is the same mechanism in reverse: the real
  catch-up DCA mechanic (BUYHOLD) corrects drift using only new cash flow, and
  cash flow is a shrinking fraction of AUM as a position compounds — a single
  stock like NVDA can run to many multiples of its 5% target far faster than
  monthly contributions alone can catch up the rest of the portfolio, something
  that essentially never happens to a diversified asset-class bucket in study
  #16. **For concentrated single-name risk, periodic hard rebalancing (or
  threshold trim) meaningfully reduces drawdown; for diversified buckets, it's
  close to redundant.**
- **TRIM meaningfully cuts drawdown vs. BUYHOLD at the long windows** (13y:
  −34%→−18%/−16%; 8y: −24%→−17%/−15%), for a modest return cost (32%→26-27%,
  8y 27%→25-26%) — a real, usable risk-control lever here, unlike study #16
  where it was a near-total no-op. At 5y/3y/1y the gap narrows to near-nothing
  (consistent with fewer of NVDA/AVGO/TSLA's fires falling inside those shorter
  windows).
- **FIXED (annual full rebalance) is at least as effective as TRIM at long
  windows and clearly better at 1y** (35% vs 30%, same or better drawdown) —
  the disciplined annual reset outperforms the always-on threshold check here,
  likely because a full reset every 12 months catches drift across ALL 20
  positions at once (not just whichever single position happens to be furthest
  above 1.5-2x that particular month), and at 1y a single well-timed annual
  rebalance event can capture a large realized gain.
- **Three-way comparison — Global 20 sits between the classics and the user's
  own book** on both return and drawdown, closer to the user's own book:

| portfolio | 13y | 8y | 5y | 3y | 1y |
|---|--:|--:|--:|--:|--:|
| 60/40 (study 16, FIXED) | 10 (−12) | 9 (−12) | 8 (−12) | 10 (−12) | 15 (−3) |
| Golden Butterfly (study 16, FIXED) | 8 (−15) | 8 (−15) | 8 (−15) | 11 (−9) | 13 (−1) |
| **Global 20 (this study, FIXED)** | **27 (−17)** | **27 (−17)** | **26 (−16)** | **30 (−13)** | **35 (−5)** |
| **Global 20 (this study, BUYHOLD)** | **32 (−34)** | **27 (−24)** | **26 (−15)** | **28 (−12)** | **30 (−4)** |
| User's own book (study 14, CURRENT/ADR-044, cited, older DCA approx.) | 34 (−24) | 33 (−20) | 21 (−28) | 24 (−15) | −3 (−11) |

  Global 20's FIXED variant achieves **higher return than every classic
  portfolio in study #16 at every window** (27-35% vs 5-13%) while running a
  drawdown profile closer to (though generally milder than) the user's own
  crypto book's, and BUYHOLD's uncontrolled 13y drawdown (−34%) is actually
  **worse** than the user's own book's 13y drawdown (−24%) — a 20-stock
  equal-weight book with no risk control at all can, in the single-asset
  blow-off tail, draw down harder than a purpose-built crypto/gold portfolio
  with an active de-risking mechanism. This is a "concentrated growth-stock"
  risk/return profile, not a "textbook diversified" one — much closer in
  character to the user's own aggressive book than to 60/40 or Permanent
  Portfolio, even though every position here is a large, liquid, single-name
  equity rather than crypto.

## Caveats
- **Overlapping windows share one underlying price history** (not independent
  draws); **1y/3y windows are single-episode reads** — same caveats as every
  prior study in this folder.
- **Equal-weight is a simplifying convention**, not how any real index or
  actual investor would size 20 individual stocks — a market-cap-weighted
  version would look very different (far more concentrated in mega-cap tech
  from the start, likely reducing but not eliminating the BUYHOLD-vs-FIXED gap
  since the concentration would already be "baked in" rather than accumulating
  via drift).
- **BABA's absence from part of the 13y window** (2013-06 to 2014-09) means the
  13y column isn't a clean, constant-20-stock read for its first ~1.2 years —
  flagged, not hidden.
- **Samsung's two-hop FX chain** (KRW→USD→PLN) is unique to this ticker in this
  study — a small additional source of FX noise/slippage vs. every other
  ticker's single USD→PLN hop.
- **BLEND is not an active variant here** (equity-only book has no internal
  defensive bucket — see rationale above); its regime-occupancy table is
  context, not a tested overlay, and should not be read as "BLEND was tested
  and did nothing" — it was deliberately not implemented as a weight-altering
  mechanism at all.
- **TRIM's redistribution-to-others rule is the same general/portfolio-agnostic
  design as study #16** (every other present position gets a share of freed
  value proportional to its own target weight — all equal 5% here, so
  effectively an equal split among the other present stocks at the moment of
  each fire).
- **The classic-portfolio and user's-own-book reference rows are cited from
  studies 16/14, not recomputed in this script** — study 14's row still uses
  the older fixed-percentage-split DCA approximation, not the real catch-up
  mechanic used throughout this study (see `backtest/AGENTS.md` "Rebalancing
  rule"), so it is not perfectly apples-to-apples on methodology, only on
  DCA/cost/PLN/window conventions.
- **NVDA/AVGO/TSLA dominating the fire count is itself a single-cycle
   finding** — the 2010s-2020s AI/semiconductor supercycle and Tesla's
   multi-year run are real, large, historically unusual single-name events; a
   different 15.9-year sample would very plausibly show different stocks (or
   far fewer fires) doing the same concentrating.

## 2026-09-25 correction note (task 28, second pass — label-only fix, sims untouched)
No sim number changed: this book is its own universe (no shared weights to refresh).
Two label fixes via `portfolio_spec.py`: the trim configs now carry correct
currency annotations (CURRENT on 1.5x/1.0x per ADR-048; 2.0x/1.5x demoted to PRIOR —
it was labeled "user's own live rule" before), and the context-only regime-occupancy
table uses the live blend divisor/orientation/labels. Fire counts: CURRENT 34,
PRIOR 21, REFERENCE 15 over the 2010-07..2026-09 pass.
