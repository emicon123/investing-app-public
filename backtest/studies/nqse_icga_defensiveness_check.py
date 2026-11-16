#!/usr/bin/env python3
"""
Study 22 (companion) -- literal NQSE.DE/ICGA.DE defensiveness check.

Per `.claude/tasks/adr-047-048-walkforward-validation.task.md` (data-analyst/data-scientist
design review, 2026-07-12). Tests the data-scientist's flagged, untested, contrary
hypothesis: does NQSE.DE (Nasdaq-100 UCITS) / ICGA.DE (MSCI China UCITS) actually behave
DEFENSIVELY (lower drawdown, lower/negative correlation to the risk-off trigger) relative to
Gold during real historical risk-off episodes, or is increasing their target weight in
risk-off (ADR-047) actually PROCYCLICAL -- the opposite of what the freed gold weight is
supposed to buy?

This is the literal-ticker half of Study 22's proxy-vs-literal split (see the companion
`gold_cut_defensive_check_backtest.py` for the full 5-window portfolio-level QQQ/FXI-proxy
A/B -- that script answers "is a ~20pp gold-to-equity reallocation of this rough shape
defensible"; THIS script answers "do the two actual ETFs behave defensively" -- two
different claims, two different data sources, not interchangeable).

Data-availability facts, spot-checked directly against `backtest/data/prices/` (not
assumed): NQSE_DE.csv 2018-09-10->2026-07-02 (~7.8y), ICGA_DE.csv 2019-07-02->2026-07-02
(~7.0y), both raw 3-junk-header yfinance schema, EUR-denominated (Xetra listings), no
`_full.csv` variant -- reuses `usd_close()`'s existing `skiprows=[1,2]`+`Close`-column
fallback (already proven for exactly this schema in `portfolio_macro_blend_backtest.py`).
GLD_full.csv (2007-01->2026-07, ~19.5y) is the Gold comparator throughout, per study 14's
own convention -- the real production Gold ticker (`ETCGLDRMAU_WA.csv`) has only ~1.5y of
history and is unusable here.

Risk-off episode coverage (checked directly against study 14's 5 named episodes,
`14_macro_blend_momentum_vs_contrarian.md`, using the ORIGINAL /2.0-divisor
`results/macro_blend_series.csv` `blend` column as a FIXED input -- not re-derived from any
of study 21's divisor variants, to avoid mixing two different zone-boundary definitions into
one episode list): only 2021-05->09 and 2021-12->2023-01 are covered by BOTH tickers;
2018-08->2019-01 is covered by NQSE.DE only (truncated to its actual 2018-09-10 start, so the
first month of that episode is missing even for NQSE.DE) and not at all by ICGA.DE; the two
earliest episodes (2012, 2014-15) predate both tickers entirely. The largest covered episode
by far (14 months, 2021-12->2023-01) is the SAME single episode study 14 already flagged as
driving essentially its entire headline result -- every conditional statistic below inherits
that "n~1" caution and is reported as descriptive, not statistically decisive.

Metric package (4 parts, per the task's tightened spec):
  1. Liquidity-beta HAC/Newey-West regression (extends study #05's own methodology --
     confirmed NQSE.DE/ICGA.DE are NOT in study 05's existing output; genuinely new
     computation), asset YoY return = a + beta*(driver YoY), vs both Fed net liquidity and
     Global M2, min 24 months, reported against study 05's own category aggregates
     (metals ~0.1, equities ~1) for scale.
  2. Conditional (risk-off-only, blend>=0.6) vs. all-other-months version of the same
     regression + correlation-to-GLD in each subsample.
  3. Episode-level table: cumulative return & maxDD of GLD vs NQSE.DE vs ICGA.DE over each
     covered episode's exact date range.
  4. Correlation of each of NQSE.DE/ICGA.DE's own return series to GLD's, restricted to
     risk-off-labeled months.

All price series are converted to PLN (GLD via USDPLN_X, NQSE.DE/ICGA.DE via EURPLN_X) for
parts 2-4 (episode table, correlations) -- consistent with every other portfolio-level
metric in this repo, avoiding an uncontrolled EUR/USD-vs-GLD's-USD currency confound. Part 1
(the liquidity-beta regression itself) uses each ticker's own NATIVE quote currency
(unconverted EUR for NQSE.DE/ICGA.DE), matching study 05's own convention -- its published
ranking table includes raw FX pairs (USDPLN_X/EURPLN_X/CHFPLN_X) as first-class rows, meaning
its per-asset return series were not secondarily PLN-normalized on top of each ticker's own
native quote.

Newey-West lag: L = floor(4*(T/100)^(2/9)) (Newey & West 1994's standard rule of thumb) --
study 05's own script no longer exists (deleted once distilled, `01_data_and_methods.md`), so
its exact lag choice cannot be confirmed; this is a named, standard convention, not a
re-derivation of study 05's own (unrecoverable) choice.

Run inside a local venv with statsmodels installed (same venv as `blend_steepness_
walkforward_backtest.py` -- `python3 -m venv ...; pip install scipy statsmodels`).
Retained per this task's explicit instruction ("keep whatever script(s) this task produces").
"""
import os
import numpy as np
import pandas as pd
import statsmodels.api as sm

BASE = os.path.join(os.path.dirname(__file__), "..")
PRICES = os.path.join(BASE, "data", "prices")
MACRO = os.path.join(BASE, "data", "macro")
RESULTS = os.path.join(BASE, "results")


def native_close(t):
    """Native-quote-currency monthly close (unconverted) -- USD for GLD, EUR for NQSE.DE/
    ICGA.DE. Reuses the same raw-3-junk-header-schema fallback proven in
    portfolio_macro_blend_backtest.py's usd_close()."""
    full = os.path.join(PRICES, f"{t}_full.csv")
    if os.path.exists(full):
        s = pd.read_csv(full, parse_dates=["date"]).set_index("date")["close"]
    else:
        df = pd.read_csv(os.path.join(PRICES, f"{t}.csv"), skiprows=[1, 2])
        df = df.rename(columns={df.columns[0]: "date"}); df["date"] = pd.to_datetime(df["date"], errors="coerce")
        df = df.dropna(subset=["date"]).set_index("date"); c = "Close" if "Close" in df.columns else df.columns[-1]
        s = pd.to_numeric(df[c], errors="coerce")
    m = s.dropna().resample("ME").last(); m.index = m.index.to_period("M"); return m


def macro_m(f, c, weekly=False):
    s = pd.read_csv(os.path.join(MACRO, f), parse_dates=["date"]).set_index("date")[c].astype(float)
    if weekly:
        s = s.resample("ME").last()
    s.index = s.index.to_period("M"); return s.groupby(level=0).last()


NQSE = native_close("NQSE_DE")     # EUR, 2018-09 ..
ICGA = native_close("ICGA_DE")     # EUR, 2019-07 ..
GLD_native = native_close("GLD")   # USD

netliq = macro_m("FED_NET_LIQUIDITY.csv", "net_liquidity_bn", weekly=True)
gm = macro_m("GLOBAL_M2.csv", "global_m2_bn")
netliq_yoy = netliq.pct_change(12)
gm_yoy = gm.pct_change(12)

# PLN conversion (parts 2-4) -- USDPLN_X for GLD, EURPLN_X for NQSE.DE/ICGA.DE.
usdpln = native_close("USDPLN_X")
eurpln = native_close("EURPLN_X")
GLD_pln_ret = (GLD_native * usdpln).dropna().pct_change()
NQSE_pln_ret = (NQSE * eurpln).dropna().pct_change()
ICGA_pln_ret = (ICGA * eurpln).dropna().pct_change()

# Original /2.0-divisor blend classification, FIXED input (study 14's own series, not
# re-derived from any of study 21's divisor variants).
blend_series = pd.read_csv(os.path.join(RESULTS, "macro_blend_series.csv"))
blend_series["month"] = pd.PeriodIndex(blend_series["month"], freq="M")
blend_series = blend_series.set_index("month")
blend = blend_series["blend"]
risk_off_months = set(blend[blend >= 0.6].dropna().index)

MIN_MONTHS = 24


def hac_beta(asset_yoy, driver_yoy, label_asset, label_driver):
    df = pd.concat([asset_yoy.rename("y"), driver_yoy.rename("x")], axis=1).dropna()
    n = len(df)
    if n < MIN_MONTHS:
        return {"asset": label_asset, "driver": label_driver, "beta": np.nan, "t": np.nan,
                "r2": np.nan, "n": n, "note": f"n={n} < {MIN_MONTHS}mo minimum -- not computed"}
    X = sm.add_constant(df["x"].values)
    L = int(np.floor(4 * (n / 100) ** (2 / 9)))
    model = sm.OLS(df["y"].values, X).fit(cov_type="HAC", cov_kwds={"maxlags": max(L, 1)})
    return {"asset": label_asset, "driver": label_driver, "beta": model.params[1],
            "t": model.tvalues[1], "r2": model.rsquared, "n": n, "lag": max(L, 1), "note": ""}


# ---------------------------------------------------------------------------
out = []
def emit(s=""):
    out.append(s); print(s)


emit("# Study 22 (companion) -- literal NQSE.DE / ICGA.DE defensiveness check\n")
emit("Tests whether the two actual ETFs ADR-047 routes risk-off weight into behave "
     "defensively (vs. Gold) during real historical risk-off episodes, using their own "
     "price history (not the QQQ/FXI proxies used in the companion portfolio-level A/B). "
     f"NQSE.DE: {NQSE.index.min()}..{NQSE.index.max()} ({len(NQSE)}mo). "
     f"ICGA.DE: {ICGA.index.min()}..{ICGA.index.max()} ({len(ICGA)}mo).\n")

# ===========================================================================
# Part 1 -- liquidity-beta HAC/Newey-West regression
# ===========================================================================
emit("## Part 1 -- liquidity-beta regression (extends study #05's methodology; NQSE.DE/"
     "ICGA.DE are new, not previously in `results/liquidity_beta_ranking.md`)\n")
emit("`asset YoY return = a + beta*(driver YoY)`, HAC/Newey-West standard errors "
     "(L = floor(4*(T/100)^(2/9)), Newey & West 1994's standard rule of thumb -- study 05's "
     "own script/lag choice no longer exists to confirm against), min 24 months. Native "
     "quote currency (EUR for NQSE.DE/ICGA.DE, matching study 05's own no-secondary-PLN-"
     "conversion convention).\n")
emit("| ticker | driver | beta | t | R2 | n | lag |")
emit("|---|---|--:|--:|--:|--:|--:|")
part1_rows = []
for tkr, series in [("NQSE.DE", NQSE), ("ICGA.DE", ICGA)]:
    yoy = series.pct_change(12)
    for driver_label, driver in [("net-liq YoY", netliq_yoy), ("Global-M2 YoY", gm_yoy)]:
        r = hac_beta(yoy, driver, tkr, driver_label)
        part1_rows.append(r)
        if r["note"]:
            emit(f"| {tkr} | {driver_label} | -- | -- | -- | {r['n']} | -- ({r['note']}) |")
        else:
            emit(f"| {tkr} | {driver_label} | {r['beta']:.2f} | {r['t']:.2f} | {r['r2']:.2f} | {r['n']} | {r['lag']} |")
emit("\nFor scale, study 05's own category aggregates (mean net-liq beta): Crypto ~24, "
     "Crypto-equity ~6, **Equity ~1**, FX ~0.2, **Metal (incl. gold) ~0.1**. NQSE.DE/ICGA.DE "
     "are equity-index ETFs, so the equity aggregate (~1, not the metals aggregate ~0.1) is "
     "the directly relevant prior for what beta 'should' look like if they behave like "
     "typical equities rather than like gold.")

# ===========================================================================
# Part 2 -- risk-off-conditional split
# ===========================================================================
emit("\n## Part 2 -- risk-off-conditional (blend>=0.6) vs. all-other-months split\n")
emit("Splits each ticker's own monthly YoY-return-vs-net-liq-YoY regression into risk-off "
     "months (blend>=0.6, original /2.0-divisor classification, study 14's own "
     "`macro_blend_series.csv`) vs. everything else. **Small-sample caveat (per the "
     "episode-coverage note above): treat as descriptive, single-episode-dominated, not a "
     "statistically decisive test** -- most risk-off months in either ticker's history "
     "belong to the single 2021-12..2023-01 episode.\n")
emit("| ticker | subsample | beta (net-liq YoY, simple OLS) | n | corr(ret, GLD ret) | n(corr) |")
emit("|---|---|--:|--:|--:|--:|")
for tkr, series, pln_ret in [("NQSE.DE", NQSE, NQSE_pln_ret), ("ICGA.DE", ICGA, ICGA_pln_ret)]:
    yoy = series.pct_change(12)
    df = pd.concat([yoy.rename("y"), netliq_yoy.rename("x")], axis=1).dropna()
    is_off = df.index.isin(risk_off_months)
    for label, mask in [("risk-off (blend>=0.6)", is_off), ("all other months", ~is_off)]:
        sub = df[mask]
        if len(sub) >= 3:
            b = np.polyfit(sub["x"], sub["y"], 1)[0]
        else:
            b = np.nan
        # correlation of PLN monthly return to GLD's PLN monthly return, same subsample mask
        ret_df = pd.concat([pln_ret.rename("a"), GLD_pln_ret.rename("g")], axis=1).dropna()
        ret_mask = ret_df.index.isin(risk_off_months) if "risk-off" in label else ~ret_df.index.isin(risk_off_months)
        ret_sub = ret_df[ret_mask]
        corr = ret_sub["a"].corr(ret_sub["g"]) if len(ret_sub) >= 3 else np.nan
        emit(f"| {tkr} | {label} | {b:.2f} | {len(sub)} | {corr:.2f} | {len(ret_sub)} |")

# ===========================================================================
# Part 3 -- episode-level table
# ===========================================================================
emit("\n## Part 3 -- episode-level cumulative return & maxDD (GLD vs NQSE.DE vs ICGA.DE, PLN)\n")
emit("Study 14's own 5 named sustained-risk-off episodes -- coverage per the header note "
     "above; 2012-06..08 and 2014-11..2015-03 predate both tickers (excluded, no data).\n")
EPISODES = [("2018-08", "2019-01"), ("2021-05", "2021-09"), ("2021-12", "2023-01")]


def episode_stats(ret_series, start, end):
    seg = ret_series.loc[pd.Period(start, "M"):pd.Period(end, "M")].dropna()
    if len(seg) == 0:
        return None
    nav = (1 + seg).cumprod()
    cum_ret = nav.iloc[-1] - 1
    maxdd = (nav / nav.cummax() - 1).min()
    return cum_ret, maxdd, len(seg), seg.index.min(), seg.index.max()


emit("| episode | ticker | actual covered range | cum. return | maxDD | n(mo) |")
emit("|---|---|---|--:|--:|--:|")
for e_start, e_end in EPISODES:
    for label, ret_series in [("GLD", GLD_pln_ret), ("NQSE.DE", NQSE_pln_ret), ("ICGA.DE", ICGA_pln_ret)]:
        r = episode_stats(ret_series, e_start, e_end)
        if r is None:
            emit(f"| {e_start}..{e_end} | {label} | (no data) | -- | -- | 0 |")
        else:
            cum_ret, maxdd, n, actual_start, actual_end = r
            note = "" if str(actual_start) == e_start else f" (truncated, actual start {actual_start})"
            emit(f"| {e_start}..{e_end} | {label} | {actual_start}..{actual_end}{note} | "
                 f"{cum_ret*100:+.1f}% | {maxdd*100:.1f}% | {n} |")

# ===========================================================================
# Part 4 -- correlation to GLD, risk-off months only
# ===========================================================================
emit("\n## Part 4 -- correlation of NQSE.DE/ICGA.DE monthly return to GLD's own, "
     "risk-off-labeled months only (PLN)\n")
emit("The most directly interpretable 'does it move like gold when it's supposed to' "
     "number. Positive/near-1 correlation = moves with gold (defensive co-movement); "
     "negative/near-0 = does not.\n")
emit("| ticker | corr(ret, GLD ret), risk-off months only | n |")
emit("|---|--:|--:|")
for tkr, pln_ret in [("NQSE.DE", NQSE_pln_ret), ("ICGA.DE", ICGA_pln_ret)]:
    ret_df = pd.concat([pln_ret.rename("a"), GLD_pln_ret.rename("g")], axis=1).dropna()
    ret_off = ret_df[ret_df.index.isin(risk_off_months)]
    corr = ret_off["a"].corr(ret_off["g"]) if len(ret_off) >= 3 else np.nan
    emit(f"| {tkr} | {corr:.2f} | {len(ret_off)} |")

emit("\n## Caveats\n")
emit("- **Episode coverage is genuinely thin**: at most 2-3 episodes, and the largest by "
     "far (14mo, 2021-12..2023-01) is the SAME single episode study 14 already flagged as "
     "driving essentially all of its own headline result -- every conditional statistic "
     "above rests on this same n~1 episode more than it rests on any general pattern. "
     "Report as descriptive, not as a statistically decisive test.\n"
     "- **NQSE.DE/ICGA.DE are EUR-denominated; the liquidity-beta regression (Part 1) uses "
     "native EUR return, unconverted** (matching study 05's own convention) -- a portion of "
     "any beta reflects EUR-specific dynamics, not purely equity-index dynamics, though this "
     "is the same convention study 05 itself used for every other non-USD-native ticker "
     "consideration.\n"
     "- **Parts 2-4 use PLN-converted returns** (GLD via USDPLN_X, NQSE.DE/ICGA.DE via "
     "EURPLN_X) for apples-to-apples comparability, consistent with every other "
     "portfolio-level metric in this repo -- a design choice, not a re-derivation of study "
     "05's own currency convention.\n"
     "- **Newey-West lag (L=floor(4*(T/100)^(2/9)))** is a standard rule-of-thumb default, "
     "not a confirmed match to study 05's own (now-unrecoverable) exact choice.\n"
     "- The risk-off classification (blend>=0.6) uses the FIXED, original /2.0-divisor "
     "series from study 14 -- deliberately not re-derived from any of study 21's divisor "
     "variants, to avoid mixing two different zone-boundary definitions into one episode "
     "list.")

with open(os.path.join(RESULTS, "nqse_icga_defensiveness_check.md"), "w") as fh:
    fh.write("\n".join(out) + "\n")
print("\nWrote results/nqse_icga_defensiveness_check.md")
