#!/usr/bin/env python3
"""
Regression-channel mean-reversion timing backtest (per-asset, standalone active timing).

NOT the legacy DCA-weight regression tilt (dca_regression_backtest.py /
example_portfolio_backtest.py, REJECTED per ADR-040 — that scaled monthly DCA
purchase *sizes*, buy-only, inside the whole-portfolio proportional allocator).
This is a different, new question: for ONE asset at a time (no portfolio
weighting), does an active buy-low/sell-high rule against a rolling log-linear
regression channel beat simple buy-and-hold?

Method (reuses the log-linear OLS fit convention from dca_regression_backtest.py
/ the legacy `Regresja-liniowa` spreadsheet sheet, same "nearest SD band" framing,
but a new trading rule — see task brief):
  1. Resample daily closes -> weekly (Friday) closes.
  2. At each week t, using ONLY the trailing N years of weekly closes ending at
     t (no lookahead), fit ln(close_k) = a + b*k (k = 1-indexed week counter
     over the trailing window). Compute residual sample stddev (ddof=1,
     matches Apache Commons DescriptiveStatistics, same as the legacy sheet).
  3. z_t = (ln(close_t) - trendline_t) / residual_stddev_t.
  4. State machine per asset, starts FLAT:
       FLAT  and z_t <= -1.5  -> BUY  (100% long)
       LONG  and z_t >= +1.5  -> SELL (100% cash)
       else: hold current state.
     Idle cash while FLAT earns 0% (explicit simplifying assumption).
  5. Benchmark = buy-and-hold the same asset over the exact same eligible
     window (lump sum at the first computable date, held to the end).

Two independent studies, same code, parametrized by window_years:
  - 10y study: only tickers with ~19y of history (the *_full.csv files).
  - 5y study: the 10y universe (now with a 5y window) + shorter-history
    tickers that clear a 5y minimum (scanned programmatically — see
    UNIVERSE_5Y_EXTRA below and the exclusions noted there).

Outputs (results/):
  regression_channel_backtest_10y.csv   per-asset metrics, 10y window
  regression_channel_backtest_5y.csv    per-asset metrics, 5y window
  regression_channel_backtest_trades.csv  every round-trip trade, both windows
  regression_channel_backtest.json      full nested detail (incl. per-trade)
  regression_channel_backtest.md        raw results tables
"""

import os
import json
import numpy as np
import pandas as pd

BASE = os.path.join(os.path.dirname(__file__), "..")
DATA_DIR = os.path.join(BASE, "data", "prices")
RESULTS_DIR = os.path.join(BASE, "results")
os.makedirs(RESULTS_DIR, exist_ok=True)

ENTRY_Z = -1.5
EXIT_Z = 1.5
MIN_WEEK_COVERAGE = 0.5   # safety floor: require >= 50% of the nominal window's weeks present

# ── Asset universe ───────────────────────────────────────────────────────────
# (file_key, display_name, category)
# 10y study: the ~19y-history *_full.csv files (per task brief). BTC_USD_full
# included; ETH_USD_full/SOL_USD_full deliberately EXCLUDED here (only ~10.9y /
# ~6.2y total history -> a true 10y trailing window leaves an almost-nonexistent
# evaluation tail, borderline/impossible per task brief) but they DO qualify for
# the 5y study below.
UNIVERSE_10Y = [
    ("AAPL_full",  "AAPL",  "mega_cap_equity"),
    ("AMZN_full",  "AMZN",  "mega_cap_equity"),
    ("GOOGL_full", "GOOGL", "mega_cap_equity"),
    ("MSFT_full",  "MSFT",  "mega_cap_equity"),
    ("NVDA_full",  "NVDA",  "mega_cap_equity"),
    ("JPM_full",   "JPM",   "single_stock_equity"),
    ("TSLA_full",  "TSLA",  "mega_cap_equity"),
    ("META_full",  "META",  "mega_cap_equity"),
    ("SPY_full",   "SPY",   "broad_index_etf"),
    ("QQQ_full",   "QQQ",   "broad_index_etf"),
    ("DIA_full",   "DIA",   "broad_index_etf"),
    ("IWM_full",   "IWM",   "broad_index_etf"),
    ("EEM_full",   "EEM",   "broad_index_etf"),
    ("FXI_full",   "FXI",   "broad_index_etf"),
    ("XLE_full",   "XLE",   "sector_etf"),
    ("XLF_full",   "XLF",   "sector_etf"),
    ("GLD_full",   "GLD",   "commodity"),
    ("SLV_full",   "SLV",   "commodity"),
    ("TLT_full",   "TLT",   "bond_credit_usd_etf"),
    ("HYG_full",   "HYG",   "bond_credit_usd_etf"),
    ("LQD_full",   "LQD",   "bond_credit_usd_etf"),
    ("UUP_full",   "UUP",   "bond_credit_usd_etf"),
    ("BTC_USD_full", "BTC", "crypto"),
]

# 5y study extras: scanned data/prices/*.csv programmatically (see analysis in
# task notes) for actual first/last date -> total span >= 5y, EXCLUDING:
#   - FX conversion pairs (CHFPLN_X/EURPLN_X/USDPLN_X) -- not investable assets here
#   - aggregate files (_summary.csv, _portfolio_summary.csv)
#   - GOOG (dup share class of GOOGL, already in the base universe via GOOGL_full)
#   - WULF -- DATA QUALITY: this ticker's pre-~2021 history is a flat/illiquid
#     shell-company price series (constant ~$3.67 for years, then a hard jump to
#     ~$24 around the actual TeraWulf reverse-merger in late 2021) -- 26y of file
#     history but the first ~21y are not the real economic asset; would corrupt
#     any trailing-window OLS fit that still spans the flat era. Excluded.
#   - IREN, FLIP_USD, CRCL, SUI20947_USD, TAO22974_USD, OLAS_USD, LOFI34187_USD,
#     XPL_USD, ETCGLDRMAU_WA, QUTM_DE, ZPR1_DE -- all < 5.0y of actual history
#     (checked precisely, see task notes; several are 2024/2025 listings).
# NOTE: CIFR's first ~11 months (2020-10 -> 2021-09) are pre-merger SPAC-shell
# trading (flat ~$9.90-$10.78 NAV) before the Cipher Mining reverse merger --
# smaller/self-resolving contamination vs WULF (fraction of one 5y window, not
# decades) but flagged explicitly in the summary as a caveat, not excluded.
UNIVERSE_5Y_EXTRA = [
    ("ETH_USD_full", "ETH",   "crypto"),
    ("SOL_USD_full", "SOL",   "crypto"),
    ("ADA_USD",      "ADA",   "crypto"),
    ("ALGO_USD",     "ALGO",  "crypto"),
    ("ASML",         "ASML",  "single_stock_equity"),
    ("AVGO",         "AVGO",  "single_stock_equity"),
    ("BABA",         "BABA",  "single_stock_equity"),
    ("BNB_USD",      "BNB",   "crypto"),
    ("CIFR",         "CIFR",  "crypto_miner_equity"),
    ("COIN",         "COIN",  "crypto_exchange_equity"),
    ("DOGE_USD",     "DOGE",  "crypto"),
    ("ETFBCASH_WA",  "ETFBCASH.WA", "bond_credit_usd_etf"),
    ("FET_USD",      "FET",   "crypto"),
    ("FISV",         "FISV",  "single_stock_equity"),
    ("GC_F",         "GC=F",  "commodity"),
    ("GME",          "GME",   "single_stock_equity"),
    ("HBAR_USD",     "HBAR",  "crypto"),
    ("HUT",          "HUT",   "crypto_miner_equity"),
    ("ICGA_DE",      "ICGA.DE", "broad_index_etf"),
    ("JNJ",          "JNJ",   "single_stock_equity"),
    ("LIN",          "LIN",   "single_stock_equity"),
    ("LINK_USD",     "LINK",  "crypto"),
    ("MARA",         "MARA",  "crypto_miner_equity"),
    ("MSTR",         "MSTR",  "crypto_proxy_equity"),
    ("NQSE_DE",      "NQSE.DE", "broad_index_etf"),
    ("NVO",          "NVO",   "single_stock_equity"),
    ("PA_F",         "PA=F",  "commodity"),
    ("SI_F",         "SI=F",  "commodity"),
    ("TRAC_USD",     "TRAC",  "crypto"),
    ("TRX_USD",      "TRX",   "crypto"),
    ("TSM",          "TSM",   "single_stock_equity"),
    ("WMT",          "WMT",   "single_stock_equity"),
    ("XRP_USD",      "XRP",   "crypto"),
    ("ZANO_USD",     "ZANO",  "crypto"),
    ("ZEC_USD",      "ZEC",   "crypto"),
]

UNIVERSE_5Y = UNIVERSE_10Y + UNIVERSE_5Y_EXTRA


# ── Data loading (handles both csv formats present in data/prices) ─────────

def load_close(file_key: str) -> pd.Series:
    path = os.path.join(DATA_DIR, f"{file_key}.csv")
    if not os.path.exists(path):
        return pd.Series(dtype=float)
    with open(path) as fh:
        first_line = fh.readline()
    if first_line.startswith("date,close"):
        # fetch_long_history.py / fetch_crypto_history.py format: date,close
        df = pd.read_csv(path)
        df["date"] = pd.to_datetime(df["date"], format="%Y-%m-%d", errors="coerce")
        df = df.dropna(subset=["date"]).set_index("date")
        s = pd.to_numeric(df["close"], errors="coerce")
    else:
        # yfinance 3-row-header format: Price/Ticker/Date rows, then OHLC data
        df = pd.read_csv(path, skiprows=3, header=None,
                          names=["Date", "Open", "High", "Low", "Close"])
        df["Date"] = pd.to_datetime(df["Date"], format="%Y-%m-%d", errors="coerce")
        df = df.dropna(subset=["Date"]).set_index("Date")
        s = pd.to_numeric(df["Close"], errors="coerce")
    s = s.dropna()
    s.index = s.index.normalize()
    s = s.sort_index()
    return s[~s.index.duplicated(keep="last")]


# ── No-lookahead rolling regression-channel z-score ─────────────────────────

def rolling_regression_z(weekly: pd.Series, window_years: float) -> pd.Series:
    """z_t = (ln(close_t) - trend_t) / resid_stddev_t, fit on trailing
    window_years of weekly closes ending at t (inclusive), no lookahead.
    Two-pointer sliding window for O(n) total cost."""
    idx = weekly.index
    vals = weekly.to_numpy(dtype=float)
    n = len(idx)
    if n == 0:
        return pd.Series(dtype=float)
    offset = pd.DateOffset(years=window_years)
    first_date = idx[0]
    min_points = int(52 * window_years * MIN_WEEK_COVERAGE)

    out_idx, out_val = [], []
    start = 0
    for i in range(n):
        t = idx[i]
        if t < first_date + offset:
            continue  # not yet N full trailing years of history
        cutoff = t - offset
        while idx[start] < cutoff:
            start += 1
        prices = vals[start:i + 1]
        m = len(prices)
        if m < max(10, min_points) or (prices <= 0).any():
            continue
        k = np.arange(1, m + 1, dtype=float)
        y = np.log(prices)
        kbar, ybar = k.mean(), y.mean()
        Sxx = float(((k - kbar) ** 2).sum())
        if Sxx <= 0:
            continue
        b = float(((k - kbar) * (y - ybar)).sum() / Sxx)
        a = float(ybar - b * kbar)
        resid = y - (a + b * k)
        sd = float(np.sqrt((resid ** 2).sum() / (m - 1)))  # sample stddev, ddof=1
        if not np.isfinite(sd) or sd <= 0:
            continue
        trend_ln = a + b * m
        z = (y[-1] - trend_ln) / sd
        out_idx.append(t)
        out_val.append(z)
    return pd.Series(out_val, index=pd.DatetimeIndex(out_idx, name="date"))


def max_drawdown(values) -> float:
    values = np.asarray(values, dtype=float)
    if values.size == 0:
        return 0.0
    peak = np.maximum.accumulate(values)
    dd = np.where(peak > 0, (values - peak) / peak, 0.0)
    return float(dd.min())


# ── Per-asset backtest ───────────────────────────────────────────────────────

def backtest_asset(file_key: str, name: str, category: str, window_years: float,
                    entry_z: float = ENTRY_Z, exit_z: float = EXIT_Z) -> dict:
    px = load_close(file_key)
    base = {"file": file_key, "name": name, "category": category,
            "window_years": window_years}
    if px.empty:
        return {**base, "eligible": False, "reason": "no price data"}

    weekly = px.resample("W-FRI").last().dropna()
    z = rolling_regression_z(weekly, window_years)
    if z.empty:
        return {**base, "eligible": False,
                "reason": f"insufficient history for a {window_years:g}y trailing window"}

    dates = z.index
    zvals = z.to_numpy(dtype=float)
    prices = weekly.loc[dates].to_numpy(dtype=float)
    n = len(dates)

    state = "FLAT"
    nav = 1.0
    navs = [nav]
    in_market_periods = 0
    trades = []
    entry_price = None
    entry_date = None

    for i in range(1, n):
        if state == "LONG":
            nav *= prices[i] / prices[i - 1]
            in_market_periods += 1
        navs.append(nav)
        zt = zvals[i]
        t = dates[i]
        if state == "FLAT" and zt <= entry_z:
            state = "LONG"
            entry_price = prices[i]
            entry_date = t
        elif state == "LONG" and zt >= exit_z:
            state = "FLAT"
            trades.append({
                "entry_date": str(entry_date.date()),
                "exit_date": str(t.date()),
                "entry_price": round(float(entry_price), 4),
                "exit_price": round(float(prices[i]), 4),
                "return_pct": round(float(prices[i] / entry_price - 1) * 100, 2),
            })
            entry_price, entry_date = None, None

    open_position = state == "LONG"
    n_periods = n - 1
    pct_in_market = (in_market_periods / n_periods * 100) if n_periods > 0 else 0.0

    navs = np.array(navs, dtype=float)
    years_span = (dates[-1] - dates[0]).days / 365.25
    strat_total_return = navs[-1] - 1.0
    strat_cagr = (navs[-1] ** (1 / years_span) - 1) if years_span > 0 and navs[-1] > 0 else None
    strat_mdd = max_drawdown(navs)

    bh_total_return = prices[-1] / prices[0] - 1.0
    bh_cagr = ((prices[-1] / prices[0]) ** (1 / years_span) - 1) if years_span > 0 else None
    bh_mdd = max_drawdown(prices)

    n_round_trips = len(trades)
    wins = sum(1 for tr in trades if tr["return_pct"] > 0)
    win_rate = (wins / n_round_trips * 100) if n_round_trips > 0 else None

    return {
        **base, "eligible": True,
        "start_date": str(dates[0].date()), "end_date": str(dates[-1].date()),
        "years_span": round(years_span, 2), "n_weeks_eligible": n,
        "strategy": {
            "total_return_pct": round(strat_total_return * 100, 1),
            "cagr_pct": round(strat_cagr * 100, 1) if strat_cagr is not None else None,
            "max_drawdown_pct": round(strat_mdd * 100, 1),
            "n_round_trips": n_round_trips,
            "win_rate_pct": round(win_rate, 1) if win_rate is not None else None,
            "pct_time_in_market": round(pct_in_market, 1),
            "open_position_at_end": open_position,
        },
        "benchmark_buy_hold": {
            "total_return_pct": round(bh_total_return * 100, 1),
            "cagr_pct": round(bh_cagr * 100, 1) if bh_cagr is not None else None,
            "max_drawdown_pct": round(bh_mdd * 100, 1),
        },
        "trades": trades,
    }


# ── Study runner + reporting ────────────────────────────────────────────────

def run_study(window_years: float, universe: list, tag: str) -> list:
    results = []
    for file_key, name, category in universe:
        r = backtest_asset(file_key, name, category, window_years)
        results.append(r)
        if r.get("eligible"):
            s, b = r["strategy"], r["benchmark_buy_hold"]
            print(f"  [{tag}] {name:14s} {r['start_date']} -> {r['end_date']}  "
                  f"strat CAGR {s['cagr_pct']:>6} vs B&H {b['cagr_pct']:>6}  "
                  f"trades={s['n_round_trips']:<3} winrate={s['win_rate_pct']}")
        else:
            print(f"  [{tag}] {name:14s} SKIPPED: {r['reason']}")
    return results


def to_dataframe(results: list) -> pd.DataFrame:
    rows = []
    for r in results:
        if not r.get("eligible"):
            rows.append({"name": r["name"], "category": r["category"], "eligible": False,
                         "reason": r.get("reason")})
            continue
        s, b = r["strategy"], r["benchmark_buy_hold"]
        rows.append({
            "name": r["name"], "category": r["category"], "eligible": True,
            "start_date": r["start_date"], "end_date": r["end_date"],
            "years_span": r["years_span"],
            "strat_total_return_pct": s["total_return_pct"], "strat_cagr_pct": s["cagr_pct"],
            "strat_max_dd_pct": s["max_drawdown_pct"], "n_round_trips": s["n_round_trips"],
            "win_rate_pct": s["win_rate_pct"], "pct_time_in_market": s["pct_time_in_market"],
            "open_position_at_end": s["open_position_at_end"],
            "bh_total_return_pct": b["total_return_pct"], "bh_cagr_pct": b["cagr_pct"],
            "bh_max_dd_pct": b["max_drawdown_pct"],
            "cagr_edge_pp": (round(s["cagr_pct"] - b["cagr_pct"], 1)
                              if s["cagr_pct"] is not None and b["cagr_pct"] is not None else None),
        })
    return pd.DataFrame(rows)


def write_md_table(df: pd.DataFrame, title: str) -> str:
    lines = [f"### {title}", ""]
    elig = df[df["eligible"] == True].sort_values("cagr_edge_pp", ascending=False)
    skipped = df[df["eligible"] == False]
    cols = ["name", "category", "start_date", "end_date", "years_span",
            "strat_cagr_pct", "bh_cagr_pct", "cagr_edge_pp", "strat_max_dd_pct", "bh_max_dd_pct",
            "n_round_trips", "win_rate_pct", "pct_time_in_market", "open_position_at_end"]
    header = ("| asset | category | start | end | yrs | strat CAGR% | B&H CAGR% | edge(pp) | "
              "strat maxDD% | B&H maxDD% | trades | win% | %in-mkt | open? |")
    sep = "|" + "---|" * 14
    lines.append(header)
    lines.append(sep)
    for _, r in elig.iterrows():
        lines.append("| " + " | ".join(str(r[c]) for c in cols) + " |")
    if not skipped.empty:
        lines.append("")
        lines.append("Skipped (insufficient history):")
        for _, r in skipped.iterrows():
            lines.append(f"- {r['name']} ({r['category']}): {r['reason']}")
    lines.append("")
    return "\n".join(lines)


def main():
    print("=" * 100)
    print("REGRESSION-CHANNEL MEAN-REVERSION TIMING BACKTEST")
    print("=" * 100)

    print("\n--- 10y window study ---")
    res_10y = run_study(10.0, UNIVERSE_10Y, "10y")
    print("\n--- 5y window study ---")
    res_5y = run_study(5.0, UNIVERSE_5Y, "5y")

    df_10y = to_dataframe(res_10y)
    df_5y = to_dataframe(res_5y)
    df_10y.to_csv(os.path.join(RESULTS_DIR, "regression_channel_backtest_10y.csv"), index=False)
    df_5y.to_csv(os.path.join(RESULTS_DIR, "regression_channel_backtest_5y.csv"), index=False)

    # all trades, both windows, one CSV
    trade_rows = []
    for tag, res in (("10y", res_10y), ("5y", res_5y)):
        for r in res:
            if not r.get("eligible"):
                continue
            for tr in r["trades"]:
                trade_rows.append({"window": tag, "asset": r["name"], "category": r["category"], **tr})
    pd.DataFrame(trade_rows).to_csv(
        os.path.join(RESULTS_DIR, "regression_channel_backtest_trades.csv"), index=False)

    with open(os.path.join(RESULTS_DIR, "regression_channel_backtest.json"), "w") as f:
        json.dump({"entry_z": ENTRY_Z, "exit_z": EXIT_Z,
                    "results_10y": res_10y, "results_5y": res_5y}, f, indent=2)

    # ── aggregate stats ──
    def aggregate(df: pd.DataFrame) -> dict:
        elig = df[df["eligible"] == True]
        n = len(elig)
        if n == 0:
            return {}
        beat_cagr = int((elig["strat_cagr_pct"] > elig["bh_cagr_pct"]).sum())
        beat_total = int((elig["strat_total_return_pct"] > elig["bh_total_return_pct"]).sum())
        zero_trades = int((elig["n_round_trips"] == 0).sum())
        return {
            "n_assets": n,
            "n_beat_cagr": beat_cagr,
            "n_beat_total_return": beat_total,
            "n_zero_trades": zero_trades,
            "median_cagr_edge_pp": round(float(elig["cagr_edge_pp"].median()), 1),
            "mean_cagr_edge_pp": round(float(elig["cagr_edge_pp"].mean()), 1),
            "median_strat_maxdd_pct": round(float(elig["strat_max_dd_pct"].median()), 1),
            "median_bh_maxdd_pct": round(float(elig["bh_max_dd_pct"].median()), 1),
            "median_win_rate_pct": (round(float(elig.loc[elig["n_round_trips"] > 0, "win_rate_pct"].median()), 1)
                                      if (elig["n_round_trips"] > 0).any() else None),
            "median_pct_time_in_market": round(float(elig["pct_time_in_market"].median()), 1),
            "total_round_trips": int(elig["n_round_trips"].sum()),
        }

    agg_10y, agg_5y = aggregate(df_10y), aggregate(df_5y)
    print("\n--- AGGREGATE: 10y window ---")
    print(json.dumps(agg_10y, indent=2))
    print("\n--- AGGREGATE: 5y window ---")
    print(json.dumps(agg_5y, indent=2))

    with open(os.path.join(RESULTS_DIR, "regression_channel_backtest.md"), "w") as f:
        f.write("# Regression-channel mean-reversion timing backtest -- raw results\n\n")
        f.write(f"Rule: FLAT->LONG at z<={ENTRY_Z}, LONG->FLAT at z>={EXIT_Z}. "
                "z = trailing no-lookahead log-linear OLS regression z-score on weekly closes.\n\n")
        f.write("## Aggregate -- 10y window\n\n```json\n" + json.dumps(agg_10y, indent=2) + "\n```\n\n")
        f.write("## Aggregate -- 5y window\n\n```json\n" + json.dumps(agg_5y, indent=2) + "\n```\n\n")
        f.write(write_md_table(df_10y, "Per-asset results -- 10y window") + "\n")
        f.write(write_md_table(df_5y, "Per-asset results -- 5y window") + "\n")

    print("\nWritten:")
    for fn in ["regression_channel_backtest_10y.csv", "regression_channel_backtest_5y.csv",
               "regression_channel_backtest_trades.csv", "regression_channel_backtest.json",
               "regression_channel_backtest.md"]:
        print(f"  results/{fn}")


if __name__ == "__main__":
    main()
