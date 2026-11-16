# Virtune GPW crypto-ETN tracking-quality check (2026-08-21)

**Question**: four Virtune crypto ETNs (physically-backed, GPW-listed, PLN-denominated)
started trading 2026-02-25 — Bitcoin Prime (`ETNVIRBTCP`), Staked Ethereum (`ETNVIRETH`),
Staked Solana (`ETNVIRSOL`), XRP (`ETNVIRXRP`). Not related to the macro/on-chain thread —
a one-off instrument-quality check, prompted by the user asking how well these track their
underlying coin, for anyone considering them as a GPW-accessible (incl. IKE/IKZE) route into
crypto exposure instead of Binance/XTB.

**Method**: not a backtest — there's only one window (the ETN's entire ~6-month trading
life so far; will lengthen over time, no multiple start-dates possible yet). Fetched full
BiznesRadar historical quotes (PLN) for all four tickers, converted to USD-equivalent via
daily USDPLN (Yahoo), and compared against BTC/ETH/SOL/XRP USD spot (Yahoo) over the same
123 GPW trading days (2026-02-25 → 2026-08-20). Computed daily-return correlation (+ a
lag ±2d check to rule out GPW-close-vs-crypto-close session-timing artifacts — peak sits at
lag 0 for all four, so the correlation reading is real, not a data-alignment bug), annualised
tracking error, and total-return tracking gap vs. each ETN's own published TER (+ staking
passthrough for ETH/SOL). Scripts: `fetch_virtune_etn_data.py`, `virtune_etn_tracking_check.py`.
Full numbers: `../virtune_etn_tracking_check.md`.

## Findings

| ETN | Corr (daily ret) | TrackErr (ann.) | Tracking gap, USD terms | Expected from TER alone |
|---|---|---|---|---|
| ETNVIRBTCP | 0.51 | 40.4% | −1.43% | −0.12% (TER 0.25%) |
| ETNVIRETH | 0.59 | 51.9% | −0.79% | −0.14% (TER 1.40%, staking ~1.12%) |
| ETNVIRSOL | 0.61 | 48.9% | +3.95% | +0.98% (TER 0.95%, staking ~3.0%) |
| ETNVIRXRP | 0.72 | 38.8% | −5.06% | −0.72% (TER 1.49%) |

1. **Directionally correct, magnitude noisy.** All four move the right way vs. their coin
   (positive correlation, right-signed tracking gap for 3/4), but daily-return correlation
   (0.51–0.72) is far below what a genuinely 1:1 physically-backed product would show if it
   traded like a liquid ETF — a liquid single-stock/ETF tracker vs. its own price is
   essentially r≈0.99+. This isn't a session-timing artifact (lag-0 peak confirmed for all
   four) — it's real day-to-day noise, consistent with a brand-new, thinly-traded GPW listing
   (some early days show volume in the hundreds of units) where the quoted close can drift
   from NAV between market-maker updates.
2. **The total-return tracking gap is bigger than the published TER alone explains, both
   directions.** BTC/ETH are close-ish to the TER-only expectation (−1.43% vs −0.12% expected;
   −0.79% vs −0.14%) — a bit worse than fees alone, plausibly bid/ask + a modest listing-period
   settling effect. SOL notably *out*-performed its already-positive expected gap (+3.95% vs
   +0.98% expected) and XRP notably *under*-performed its expected drag (−5.06% vs −0.72%
   expected) — in both cases roughly 3-4x the fee-only prediction, most likely a
   premium/discount-to-NAV effect common in newly-listed, low-AUM ETPs rather than a
   structural flaw in the physical-backing mechanism.
3. **A PLN investor's actual experienced gap is bigger still** because the FX leg (USDPLN)
   compounds independently on each side (see "as-traded" PLN return vs. "coin-in-PLN" columns
   in the full report) — not a tracking-quality issue per se, just the reminder that these are
   PLN instruments and USDPLN moves add their own variance on top of any coin/ETN tracking gap.

## Addendum (2026-08-21): tracking during a live burst

BTC/ETH/SOL/XRP all ripped +20-38% over 2026-08-17→21 (a genuine "burst," not noise).
Re-ran the check session-aligned to GPW's ~17:00 Warsaw close instead of Yahoo's ~00:00 UTC
crypto-bar close (the two are ~9h apart in summer, which matters when the underlying is
moving fast intraday) — script: `virtune_burst_window_check.py`, full table:
`../virtune_burst_window_check.md`. Clear pattern, consistent across all four ETNs:

| ETN | Day the burst hit hardest (08-19) | Catch-up day (08-20, 10-40x volume) | Still live (08-21 intraday) |
|---|---|---|---|
| BTCP | ETN lagged spot by **-3.8pp** cum. | narrowed to -1.0pp | flipped to **+2.1pp** premium |
| ETH | lagged by **-6.7pp** | narrowed to -1.9pp | **+3.0pp** premium |
| SOL | lagged by **-4.4pp** | narrowed to -0.4pp | **+1.9pp** premium |
| XRP | lagged by **-3.4pp** | *widened* to -4.2pp | **+3.4pp** premium |

**Mechanism, not noise**: on the day the move accelerated (08-19), all four ETNs badly
under-reacted vs. spot on very thin volume (a few hundred to a few thousand units traded).
The next session (08-20) volume jumped 10-40x and the ETNs caught up — and by today's
intraday session all four have flipped from a tracking *deficit* to a tracking *premium*
(+1.9 to +3.4pp cumulative since 08-17), i.e. they're now running slightly hotter than spot,
not just catching up. This is consistent with a thinly-traded, newly-listed product where
the market-maker/AP mechanism can't fully arbitrage the ETN back to NAV in real time during
a fast move — it lags on the way up, then (once volume returns) can overshoot rather than
settle cleanly back to fair value. Reinforces the main finding above (moderate r, gap bigger
than TER alone explains) with a mechanism: **during calm periods tracking is close; during a
fast burst, expect a session or two of real slippage in either direction before it
re-converges** — worth knowing if timing an entry/exit around a specific day's move.

## Bottom line

Over the only 6 months these products have existed, they track the right coin in the right
direction but with real day-to-day noise (r≈0.5-0.7, not r≈0.99) and a total-return slippage
that runs 3-4x the published TER for two of the four — likely NAV premium/discount rather
than a mechanical tracking failure, but not yet distinguishable with 6 months of one listing
event. **Report-only, no ADR/portfolio-allocation change implied** — worth re-checking again
once the products have a longer, less newly-listed history (per house convention, this file
records a finding, it doesn't recommend adding these to any portfolio target).
