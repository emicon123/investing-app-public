#!/usr/bin/env python3
"""
Study 27 -- FXI-extended-history China-market-proxy defensiveness check.

Extension of study 22's literal-ticker companion script (`nqse_icga_defensiveness_check.py`),
scoped ONLY to the China/ICGA leg -- NQSE.DE is not touched anywhere in this file, and
`nqse_icga_defensiveness_check.py` itself is not modified (this is a new, separate script,
per this program's convention of never mutating a script that already fed a published
finding). Re-implements the original's exact 4-part methodology (HAC/Newey-West liquidity-
beta regression, risk-off-conditional split, episode-level table, risk-off-months
correlation-to-GLD), substituting `FXI_full.csv` for `ICGA_DE.csv` as the China-market
series, plus a 5th section (new here) that directly checks FXI against ICGA.DE's own real
price history over the window where both exist.

IMPORTANT SCOPE NOTE (same distinction the original companion script and study 22's own
Part A/Part B split already draw): this is a CHINA-MARKET-PROXY check -- FXI stands in for
ICGA.DE, the exact same convention study 22 Part A already used for its portfolio-level A/B
-- NOT a literal test of the ICGA.DE fund itself. FXI (iShares China Large-Cap ETF) tracks
the FTSE China 25 index; ICGA.DE (Amundi/Lyxor MSCI China UCITS) tracks the MSCI China index.
Close, overlapping China-market exposure, but not an identical index -- a proxy, not a
substitute.

Data-availability facts (spot-checked directly against `backtest/data/prices/`, not
assumed): FXI_full.csv 2004-10-08->2026-06-17 (~21.7y, USD-denominated, US-listed, already-
proven ICGA.DE stand-in per study 22 Part A) vs. ICGA_DE.csv 2019-07-02->2026-07-02 (~7.0y,
EUR-denominated, Xetra-listed, raw 3-junk-header yfinance schema, real fund but far shorter
history). FXI's extra ~14.7y of history is the entire point of this script: it reaches all 5
of study 14's named risk-off episodes, including the two earliest (2012-06->08, 2014-11->
2015-03) that ICGA.DE's real price history could never reach at any point. GLD_full.csv
(2007-01-03->2026-07-06, ~19.5y) is the Gold comparator throughout (study 14's own
convention) -- checked directly: GLD's data starts ~5.4y before the earliest (2012-06)
episode, so that episode is comfortably inside GLD's coverage, not at its data edge.

Currency-conversion methodology (flagged explicitly by this task, a real difference from the
original script): FXI is a USD-denominated, US-listed ETF, unlike ICGA.DE (EUR-denominated,
Xetra-listed). Parts 2-4's PLN conversion therefore uses USDPLN_X for FXI, not EURPLN_X --
matching each ticker's own native quote currency, the same convention the original script
used (GLD via USDPLN_X, NQSE.DE/ICGA.DE via EURPLN_X). This means FXI and GLD's PLN legs now
share the same FX pair (both USD-native) -- expected and fine, not a bug; it removes one
currency-confound layer (EUR/PLN) that the original ICGA.DE-based analysis carried. Part 1
(the liquidity-beta regression itself) uses FXI's own native USD return, unconverted, matching
the original script's "native quote currency, no secondary PLN conversion" convention.

Blend classification: identical FIXED input to the original -- `results/macro_blend_series.
csv`'s original /2.0-divisor `blend` column, loaded exactly as the original script does, not
re-derived from any of study 21's divisor variants.

Metric package (5 parts -- the original's 4, plus a new internal-consistency check):
  1. Liquidity-beta HAC/Newey-West regression (native USD, extends study 05's methodology,
     same L=floor(4*(T/100)^(2/9)) Newey-West rule of thumb as the original script).
  2. Risk-off-conditional (blend>=0.6) vs. all-other-months split of beta + corr-to-GLD.
  3. Episode-level table -- now all 5 of study 14's named episodes (FXI's 21.7y reaches all
     5; the original ICGA.DE-based table only reached 2 of them).
  4. Correlation of FXI's own PLN return to GLD's PLN return, risk-off months only.
  5. NEW -- FXI-vs-real-ICGA.DE agreement check: recomputes ICGA.DE's own episode-level
     stats (EUR->PLN via EURPLN_X, exactly reproducing the original script's methodology) for
     the 2 episodes both series cover (2021-05->09, 2021-12->2023-01), plus the direct
     month-to-month correlation between FXI's and ICGA.DE's own PLN return series over their
     entire common history and over risk-off months only -- does the FXI proxy actually track
     the real ICGA.DE fund where both exist, or diverge? Reading ICGA_DE.csv here is in-scope
     (this is a China/ICGA-only extension); NQSE.DE is never read or referenced.

Run inside a local venv with statsmodels installed (same requirement as the original script:
`python3 -m venv ...; pip install scipy statsmodels`).
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
    """Native-quote-currency monthly close (unconverted) -- USD for GLD/FXI/USDPLN_X, EUR for
    ICGA.DE/EURPLN_X. Identical to the original script's helper (raw-3-junk-header-schema
    fallback proven in portfolio_macro_blend_backtest.py's usd_close())."""
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


FXI = native_close("FXI")          # USD, 2004-10 .. (ICGA.DE proxy, per study 22 Part A)
ICGA = native_close("ICGA_DE")     # EUR, 2019-07 .. (real fund, for the overlap check only)
GLD_native = native_close("GLD")   # USD

netliq = macro_m("FED_NET_LIQUIDITY.csv", "net_liquidity_bn", weekly=True)
gm = macro_m("GLOBAL_M2.csv", "global_m2_bn")
netliq_yoy = netliq.pct_change(12)
gm_yoy = gm.pct_change(12)

# PLN conversion -- USDPLN_X for GLD AND FXI (both USD-native), EURPLN_X for ICGA.DE (EUR-
# native, overlap check only).
usdpln = native_close("USDPLN_X")
eurpln = native_close("EURPLN_X")
GLD_pln_ret = (GLD_native * usdpln).dropna().pct_change()
FXI_pln_ret = (FXI * usdpln).dropna().pct_change()
ICGA_pln_ret = (ICGA * eurpln).dropna().pct_change()

# Original /2.0-divisor blend classification, FIXED input (study 14's own series, loaded
# exactly as the original companion script does).
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


emit("# Study 27 -- FXI-extended-history China-market-proxy defensiveness check\n")
emit("Extension of study 22's literal-ticker companion (`nqse_icga_defensiveness_check.py`), "
     "China/ICGA leg only -- NQSE.DE is not touched anywhere in this script or its output. "
     "Substitutes FXI (iShares China Large-Cap, USD, 21.7y history) for ICGA.DE (real fund, "
     "EUR, only 7.0y history) to reach all 5 of study 14's named risk-off episodes instead of "
     "just 2. **This is a China-market-proxy check (FXI stands in for ICGA.DE, same "
     "convention as study 22 Part A's portfolio-level A/B), not a literal test of the "
     "ICGA.DE fund itself** -- FXI tracks FTSE China 25, ICGA.DE tracks MSCI China, a close "
     "but not identical index. "
     f"FXI: {FXI.index.min()}..{FXI.index.max()} ({len(FXI)}mo). "
     f"ICGA.DE (real fund, overlap check only): {ICGA.index.min()}..{ICGA.index.max()} "
     f"({len(ICGA)}mo). GLD: {GLD_native.index.min()}..{GLD_native.index.max()} "
     f"({len(GLD_native)}mo) -- starts "
     f"{(pd.Period('2012-06', 'M') - GLD_native.index.min()).n / 12:.1f}y before the earliest "
     "(2012-06) named episode, comfortably inside GLD's coverage, not at its data edge.\n")

# ===========================================================================
# Part 1 -- liquidity-beta HAC/Newey-West regression
# ===========================================================================
emit("## Part 1 -- liquidity-beta regression (extends study #05's methodology; FXI, as an "
     "ICGA.DE China-market proxy, is not previously in `results/liquidity_beta_ranking.md`)\n")
emit("`asset YoY return = a + beta*(driver YoY)`, HAC/Newey-West standard errors "
     "(L = floor(4*(T/100)^(2/9)), Newey & West 1994's standard rule of thumb -- same "
     "convention as the original companion script), min 24 months. Native quote currency "
     "(USD for FXI/GLD, matching study 05's own no-secondary-PLN-conversion convention).\n")
emit("| ticker | driver | beta | t | R2 | n | lag |")
emit("|---|---|--:|--:|--:|--:|--:|")
part1_rows = []
for tkr, series in [("FXI (ICGA.DE proxy)", FXI)]:
    yoy = series.pct_change(12)
    for driver_label, driver in [("net-liq YoY", netliq_yoy), ("Global-M2 YoY", gm_yoy)]:
        r = hac_beta(yoy, driver, tkr, driver_label)
        part1_rows.append(r)
        if r["note"]:
            emit(f"| {tkr} | {driver_label} | -- | -- | -- | {r['n']} | -- ({r['note']}) |")
        else:
            emit(f"| {tkr} | {driver_label} | {r['beta']:.2f} | {r['t']:.2f} | {r['r2']:.2f} | {r['n']} | {r['lag']} |")
emit("\nFor scale, study 05's own category aggregates (mean net-liq beta): Crypto ~24, "
     "Crypto-equity ~6, **Equity ~1**, FX ~0.2, **Metal (incl. gold) ~0.1**. FXI is a "
     "China-equity-index ETF, so the equity aggregate (~1, not the metals aggregate ~0.1) is "
     "the directly relevant prior for what beta 'should' look like if it behaves like a "
     "typical equity index rather than like gold.")

# ===========================================================================
# Part 2 -- risk-off-conditional split
# ===========================================================================
emit("\n## Part 2 -- risk-off-conditional (blend>=0.6) vs. all-other-months split\n")
emit("Splits FXI's own monthly YoY-return-vs-net-liq-YoY regression into risk-off months "
     "(blend>=0.6, original /2.0-divisor classification, study 14's own "
     "`macro_blend_series.csv`) vs. everything else. FXI's much longer history spreads "
     "risk-off months across all 5 named episodes (not just the single 2021-12..2023-01 "
     "episode that dominated the original ICGA.DE-based split) -- see Part 3 for the "
     "per-episode breakdown.\n")
emit("| ticker | subsample | beta (net-liq YoY, simple OLS) | n | corr(ret, GLD ret) | n(corr) |")
emit("|---|---|--:|--:|--:|--:|")
for tkr, series, pln_ret in [("FXI (ICGA.DE proxy)", FXI, FXI_pln_ret)]:
    yoy = series.pct_change(12)
    df = pd.concat([yoy.rename("y"), netliq_yoy.rename("x")], axis=1).dropna()
    is_off = df.index.isin(risk_off_months)
    for label, mask in [("risk-off (blend>=0.6)", is_off), ("all other months", ~is_off)]:
        sub = df[mask]
        if len(sub) >= 3:
            b = np.polyfit(sub["x"], sub["y"], 1)[0]
        else:
            b = np.nan
        ret_df = pd.concat([pln_ret.rename("a"), GLD_pln_ret.rename("g")], axis=1).dropna()
        ret_mask = ret_df.index.isin(risk_off_months) if "risk-off" in label else ~ret_df.index.isin(risk_off_months)
        ret_sub = ret_df[ret_mask]
        corr = ret_sub["a"].corr(ret_sub["g"]) if len(ret_sub) >= 3 else np.nan
        emit(f"| {tkr} | {label} | {b:.2f} | {len(sub)} | {corr:.2f} | {len(ret_sub)} |")

# ===========================================================================
# Part 3 -- episode-level table (all 5 named episodes -- FXI's history reaches all of them)
# ===========================================================================
emit("\n## Part 3 -- episode-level cumulative return & maxDD (GLD vs FXI-as-ICGA-proxy, PLN)\n")
emit("Study 14's own 5 named sustained-risk-off episodes -- FXI's 21.7y history reaches ALL "
     "5 (the original ICGA.DE-based table reached only 2: 2021-05..09 and 2021-12..2023-01). "
     "GLD covers all 5 too (checked above -- 2012-06 is not at GLD's data edge).\n")
EPISODES = [("2012-06", "2012-08"), ("2014-11", "2015-03"), ("2018-08", "2019-01"),
            ("2021-05", "2021-09"), ("2021-12", "2023-01")]


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
episode_results = {}
for e_start, e_end in EPISODES:
    for label, ret_series in [("GLD", GLD_pln_ret), ("FXI (ICGA.DE proxy)", FXI_pln_ret)]:
        r = episode_stats(ret_series, e_start, e_end)
        episode_results[(e_start, e_end, label)] = r
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
emit("\n## Part 4 -- correlation of FXI (ICGA.DE proxy) monthly return to GLD's own, "
     "risk-off-labeled months only (PLN)\n")
emit("The most directly interpretable 'does it move like gold when it's supposed to' "
     "number. Positive/near-1 correlation = moves with gold (defensive co-movement); "
     "negative/near-0 = does not. FXI's longer history means this n now spans all 5 named "
     "episodes' risk-off months, not just the 2 the original ICGA.DE-based check could "
     "reach.\n")
emit("| ticker | corr(ret, GLD ret), risk-off months only | n |")
emit("|---|--:|--:|")
part4_results = {}
for tkr, pln_ret in [("FXI (ICGA.DE proxy)", FXI_pln_ret)]:
    ret_df = pd.concat([pln_ret.rename("a"), GLD_pln_ret.rename("g")], axis=1).dropna()
    ret_off = ret_df[ret_df.index.isin(risk_off_months)]
    corr = ret_off["a"].corr(ret_off["g"]) if len(ret_off) >= 3 else np.nan
    part4_results[tkr] = (corr, len(ret_off))
    emit(f"| {tkr} | {corr:.2f} | {len(ret_off)} |")

# ===========================================================================
# Part 5 -- FXI-vs-real-ICGA.DE agreement check (NEW; not in the original script)
# ===========================================================================
emit("\n## Part 5 -- FXI-vs-real-ICGA.DE agreement check (internal consistency, NEW section)\n")
emit("Does the FXI proxy actually track the real ICGA.DE fund where both have data? "
     "Recomputes real ICGA.DE's own episode-level stats (EUR->PLN via EURPLN_X, exactly "
     "reproducing the original companion script's methodology and figures) for the 2 "
     "episodes both series cover, side by side with FXI's Part-3 numbers above.\n")
OVERLAP_EPISODES = [("2021-05", "2021-09"), ("2021-12", "2023-01")]
emit("| episode | metric | GLD | FXI (proxy) | ICGA.DE (real) | FXI - ICGA.DE delta |")
emit("|---|---|--:|--:|--:|--:|")
for e_start, e_end in OVERLAP_EPISODES:
    gld_r = episode_stats(GLD_pln_ret, e_start, e_end)
    fxi_r = episode_results[(e_start, e_end, "FXI (ICGA.DE proxy)")]
    icga_r = episode_stats(ICGA_pln_ret, e_start, e_end)
    if fxi_r and icga_r:
        d_ret = (fxi_r[0] - icga_r[0]) * 100
        d_dd = (fxi_r[1] - icga_r[1]) * 100
    else:
        d_ret = d_dd = np.nan
    emit(f"| {e_start}..{e_end} | cum. return | {gld_r[0]*100:+.1f}% | {fxi_r[0]*100:+.1f}% | "
         f"{icga_r[0]*100:+.1f}% | {d_ret:+.1f}pp |")
    emit(f"| {e_start}..{e_end} | maxDD | {gld_r[1]*100:.1f}% | {fxi_r[1]*100:.1f}% | "
         f"{icga_r[1]*100:.1f}% | {d_dd:+.1f}pp |")

# Direct month-to-month correlation between the two series' own PLN returns, both over their
# entire common history and restricted to risk-off months.
common = pd.concat([FXI_pln_ret.rename("fxi"), ICGA_pln_ret.rename("icga")], axis=1).dropna()
corr_common_all = common["fxi"].corr(common["icga"])
common_off = common[common.index.isin(risk_off_months)]
corr_common_off = common_off["fxi"].corr(common_off["icga"]) if len(common_off) >= 3 else np.nan
emit(f"\nDirect month-to-month correlation of FXI's own PLN return to real ICGA.DE's own PLN "
     f"return, over their entire common history ({common.index.min()}..{common.index.max()}, "
     f"n={len(common)}): **{corr_common_all:.2f}**. Restricted to risk-off months only "
     f"(n={len(common_off)}): **{corr_common_off:.2f}**.")

emit("\n## Caveats\n")
emit("- **This is a China-market-proxy check, not a literal test of the ICGA.DE fund.** FXI "
     "(FTSE China 25) and ICGA.DE (MSCI China) are close but not identical indices -- Part 5 "
     "above is the direct evidence for how closely they actually track each other in this "
     "data, not an assumption.\n"
     "- **FXI is USD-native; ICGA.DE is EUR-native.** Parts 2-5's PLN conversion uses "
     "USDPLN_X for FXI/GLD and EURPLN_X for ICGA.DE -- FXI and GLD now share one FX pair "
     "(both USD), which removes the EUR/PLN confound layer the original ICGA.DE-based check "
     "carried, but also means Part 5's FXI-vs-ICGA.DE comparison is not a pure like-for-like "
     "return comparison -- part of any divergence between the two columns could reflect "
     "USD/PLN vs. EUR/PLN currency moves, not just the underlying China-equity indices "
     "diverging.\n"
     "- **The 3 newly-reached episodes (2012-06..08, 2014-11..2015-03, 2018-08..2019-01) have "
     "no real ICGA.DE data at all to cross-check against** (ICGA.DE's real history starts "
     "2019-07) -- confidence in FXI as a proxy for those 3 episodes rests entirely on Part 5's "
     "overlap-window agreement check (2021-05..09, 2021-12..2023-01), not on direct evidence "
     "for the 3 pre-2019 episodes themselves.\n"
     "- **Episode coverage is far broader here than the original script (5 vs. 2), but the "
     "2021-12..2023-01 episode is still, by a wide margin, the largest and longest (14mo vs. "
     "3-6mo for the other 4)** -- study 14's own n~1-episode caution still applies, just less "
     "severely than in the original ICGA.DE-only check.\n"
     "- **Newey-West lag (L=floor(4*(T/100)^(2/9)))** is a standard rule-of-thumb default, "
     "same convention as the original script, not a re-derivation of study 05's own "
     "(unrecoverable) exact choice.\n"
     "- The risk-off classification (blend>=0.6) uses the FIXED, original /2.0-divisor "
     "series from study 14 -- deliberately not re-derived from any of study 21's divisor "
     "variants, to avoid mixing two different zone-boundary definitions into one episode "
     "list.\n"
     "- **Scope**: this script covers the China/ICGA leg only. NQSE.DE and the original "
     "`nqse_icga_defensiveness_check.py` script are untouched.")

with open(os.path.join(RESULTS, "icga_fxi_extended_defensiveness_check.md"), "w") as fh:
    fh.write("\n".join(out) + "\n")
print("\nWrote results/icga_fxi_extended_defensiveness_check.md")
