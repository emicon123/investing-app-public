# 13 — Per-asset regression-channel mean-reversion timing (buy low / sell high vs. a rolling trend)

Script: `regression_channel_backtest.py` → `results/regression_channel_backtest_10y.csv`,
`_5y.csv`, `_trades.csv`, `.json`, `.md`. **Not** a re-test of the rejected DCA-weight
regression tilt (`dca_regression_backtest.py` / `example_portfolio_backtest.py`, ADR-040 —
that scaled monthly *purchase sizes* inside the whole-portfolio buy-only allocator). This is
a standalone, per-asset **active timing** question, asked one asset at a time with no
portfolio weighting: does going 100% long/100% cash against a rolling log-linear regression
channel beat simple buy-and-hold?

**Method.** Daily closes → weekly (Friday) closes. At each week `t`, fit
`ln(close_k) = a + b·k` by OLS on *only* the trailing N years of weekly closes ending at `t`
(no lookahead, same log-linear/residual-stddev convention as `dca_regression_backtest.py` and
the legacy `Regresja-liniowa` sheet). `z_t = (ln(close_t) − trend_t) / residual_stddev_t`.
State machine per asset, starts FLAT: **FLAT→LONG at z≤−1.5**, **LONG→FLAT at z≥+1.5**,
otherwise hold. Idle cash while FLAT earns **0%** (explicit simplifying assumption — no
risk-free rate modeled). Benchmark = lump-sum buy-and-hold over the exact same eligible
window (first date with a full trailing window → last available date). Two independent
studies, same code: **10y window** (23 assets with ~19y history — mega-cap tech, broad
indices, sector ETFs, bonds, commodities, BTC) and **5y window** (58 assets — the 10y
universe re-run with a 5y window, plus altcoins, miners, and shorter-history single stocks,
scanned programmatically for ≥5y of real history).

## Headline: the rule loses to buy-and-hold on both windows, badly so for trending growth assets; its only real edge is on genuinely range-bound assets (bonds, a couple of sideways equity indices)

| | n assets | median CAGR edge (pp) | mean CAGR edge (pp) | n beat CAGR | n beat total return | total round trips | median win rate | median strat maxDD | median B&H maxDD | median %time in market |
|---|--:|--:|--:|--:|--:|--:|--:|--:|--:|--:|
| **10y window** | 23 | **−4.8** | −8.0 | 4/23 | 4/23 | 23 | 100% | −31.8% | −39.3% | 49.1% |
| **5y window** | 58 | **−5.6** | −9.9 | 13/58 | 13/58 | 82 | 100% | −31.0% | −47.5% | 41.5% |

Both windows tell the same story: the strategy underperforms buy-and-hold on CAGR for the
large majority of assets (19/23 in the 10y study, 45/58 in the 5y study), and the average
underperformance (mean edge) is much worse than the median — a fat left tail driven by a
handful of catastrophic misses on strongly trending assets, exactly the failure mode the task
brief anticipated.

### Per-round-trip quality is good — the problem is opportunity cost while FLAT, not bad trades

Median round-trip **win rate is 100%** in both windows (when a completed round trip happens,
it is very likely to be profitable — the channel does correctly flag genuine local extremes).
The strategy also **does** meaningfully cut drawdown in aggregate (median strat maxDD
−31.8%/−31.0% vs. B&H −39.3%/−47.5%). But CAGR still loses because trending assets spend long
stretches never touching ±1.5σ, so the strategy sits in 0%-yielding cash while the asset
keeps compounding — for a buy-and-hold-oriented long-term investor, the CAGR give-up
dominates the drawdown benefit.

### The two "zero-trade" failure modes

| window | assets that never once entered (0% time in market) | median edge for that group |
|---|---|---|
| 10y | TSLA, GLD, SLV (3/23) | −15.2 pp |
| 5y | SOL, ALGO, CIFR, COIN, DOGE, ETFBCASH.WA, HBAR, HUT, ICGA.DE, NQSE.DE, TRAC, TRX, XRP, ZANO (14/58) | −11.25 pp |

Exactly the risk flagged in the task brief: for assets in a persistent uptrend, the ±1.5σ zone
can simply never fire. **TSLA, GLD and SLV never touched z≤−1.5 across the entire 10y
window** — 0 trades, 0% return, while the assets themselves returned +30.9%/yr, +14.1%/yr and
+15.2%/yr respectively (edges of −30.9, −14.1, −15.2 pp). **NVDA** fired once (1 trade, 100%
win rate) but then spent the rest of the decade "too expensive" to re-enter — only 20.9% of
weeks in-market, CAGR 24.0% vs. B&H 59.6% (−35.6 pp, the single worst outcome in the 10y
study). BTC entered once in 2020 and simply never exited (0 completed round trips, 67.9%
time in market) yet still trailed B&H by 16.7 pp/yr — a late, one-shot entry into a
persistent bull run underperforms just buying it from day one.

### Where the "wins" actually come from — genuine timing edge vs. lucky non-participation

Of the 13 "beat CAGR" assets in the 5y study, **8 never completed a single trade** — they are
mostly short-lived, high-volatility altcoins/miners (COIN, SOL, ZANO, ALGO, DOGE, HBAR, TRAC,
XRP) whose short eligible window (often <2 years, one case only 0.17 years) happened to be a
crash, so sitting in cash at 0% trivially beat a falling asset. This is **not** evidence of
timing skill — it is largely a function of short/thin eligible windows, and is called out
explicitly as an artifact, not a finding.

The remaining 5 "genuine" winners (completed ≥1 profitable round trip *and* beat CAGR) are
**FXI, TLT, LQD/HYG-adjacent bonds, IWM** (10y) and **FXI, TLT, BTC (+0.1 pp, essentially a
tie), LINK, MSTR** (5y) — assets that are structurally range-bound or trade sideways/downward
rather than persistently trending: **bonds are the one category with a consistently positive
median edge** (10y bond_credit_usd_etf median +0.85 pp), because their price action genuinely
oscillates around a mean the way the strategy assumes, unlike trending equities/crypto.

### Category breakdown (median CAGR edge, pp)

| category | 10y median edge | 5y median edge |
|---|--:|--:|
| bond_credit_usd_etf (TLT/LQD/HYG/UUP) | **+0.85** | −1.0 |
| broad_index_etf (SPY/QQQ/DIA/IWM/EEM/FXI) | −0.65 | −3.2 |
| sector_etf (XLE/XLF) | −3.2 | −5.8 |
| single_stock_equity | −4.8 | −14.7 |
| commodity (GLD/SLV/GC=F/SI=F/PA=F) | −14.65 | −6.1 |
| mega_cap_equity (AAPL/AMZN/MSFT/NVDA/TSLA/META/GOOGL) | **−16.8** | −13.0 |
| crypto | −16.7 | +0.1 (mean −4.0, mixed) |
| crypto_miner_equity (MARA/HUT/CIFR) | — | **−44.1** (mean −69.5, worst) |

Mega-cap equities and crypto miners are the worst category matches for this rule — both are
persistently-trending growth assets where "wait for −1.5σ" means missing most of the
compounding, and where re-entry after a sell rarely captures the next leg up in time.

## 10y vs. 5y window comparison

Shortening the window does **not** rescue the strategy — it produces essentially the same
qualitative outcome (median edge −4.8 pp vs. −5.6 pp) and a slightly worse *mean* edge (−8.0
vs. −9.9 pp), because the 5y universe adds a set of highly volatile, thinly-traded
miners/altcoins (HUT −146.7 pp, TRX −62.1 pp, ZEC −86.0 pp, AVGO −52.5 pp) that whipsaw
badly against a short trailing channel. The 5y window does fire somewhat more often (1.41
round trips/asset vs. 1.00) and delivers a larger absolute drawdown reduction (B&H median
maxDD −47.5% vs. strat −31.0%, a bigger gap than the 10y study's −39.3%/−31.8%) — but that is
mostly because the 5y universe includes assets with catastrophic B&H drawdowns (many crypto
alts down 65–95%), where any reduced participation mechanically caps the worst case, not
because the shorter window times entries/exits better. **Neither window is a "fix" for the
other — both fail on trending assets and both do modestly better on bonds/range-bound
assets.**

## Verdict

- **The strategy loses to buy-and-hold on both windows, for most assets, and badly so on
  average.** Median CAGR edge is negative (−4.8 pp 10y, −5.6 pp 5y); only 4/23 (10y) and
  13/58 (5y) assets beat buy-and-hold CAGR, and roughly half of the 5y "winners" never took a
  single trade — an artifact of short eligible windows on crashing altcoins, not skill.
- **Confirmed exactly the failure mode flagged in the task brief:** strongly trending assets
  (TSLA, GLD, SLV, NVDA in the 10y study; HUT, TRX, ZEC, AVGO, MARA in the 5y study) rarely or
  never touch the ±1.5σ zone, or fire once and then miss years of subsequent trend —
  producing 0% or badly lagging returns against double-digit-CAGR benchmarks.
- **The one place the rule shows a real, if modest, edge is genuinely range-bound assets** —
  bonds (TLT/LQD, positive median edge in the 10y study) and a couple of sideways/declining
  equity indices (FXI, IWM) — where round-trip win rate is 100% and the mean-reversion
  assumption actually matches the asset's statistical behavior.
- **Drawdown is meaningfully reduced in aggregate** (median strat maxDD roughly 8–17 pp
  shallower than B&H in both windows) — a genuine risk-management side-benefit, but not
  enough to offset the CAGR give-up for a long-term compounding-focused investor, and not
  something this backtest was asked to optimize for (Sharpe/risk-adjusted return not
  computed).
- **Net recommendation: do not adopt this rule as a general per-asset timing overlay**,
  especially not for the mega-cap-equity and crypto/miner sleeves of the portfolio, where it
  would have cost 13–36 pp/yr of CAGR historically. It may be worth a narrower follow-up
  restricted to the bond sleeve only, where it showed a small, consistent edge — but that is
  a much smaller and more speculative claim than the general rule tested here.

## Caveats

- Idle cash while FLAT is modeled at a hard **0%** — a real implementation would earn some
  risk-free rate, which would shrink (not eliminate) the negative edge somewhat, since the
  competing benchmark also has no cash drag by construction (100% invested from day one).
- Round-trip win rate of 100% at the median is real but based on **small n** per asset
  (median ~1 trade in the 10y study, so "100% win rate" for many assets means literally "the
  one trade it took was a winner" — not a statistically robust hit rate).
- The 5y study's "8 zero-trade winners" all have very short eligible windows (some <1 year,
  one only 0.17 years) — those data points are noise, not a meaningful sample, and are
  explicitly excluded from the "genuine edge" reading above.
- CIFR's pre-merger SPAC-shell history and WULF's decades-long flat pre-merger history (WULF
  excluded entirely) are pre-existing data-quality notes carried over from the universe
  construction in `regression_channel_backtest.py`; they do not materially change the
  category-level conclusions here.
- No transaction costs, slippage, taxes, or spread are modeled (matches the buy-and-hold
  benchmark's own simplifications, so the *edge* figures are comparable to each other, but
  neither strategy's absolute return is investable exactly as shown).
