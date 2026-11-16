#!/usr/bin/env python3
"""
Study 04 (RECONSTRUCTED 2026-07-12) -- Fed net-liquidity timing on BTC/ETH/SOL: is the
lead tradeable?

*** RECONSTRUCTION NOTICE ***
This script did not survive in git history (confirmed during Phase-2/#19 scoping --
`git log --all --follow -- backtest/liquidity_timing_backtest.py` returns nothing). It is
rebuilt from scratch from `backtest/results/study_summary/04_timing_backtest.md` and the
raw table it was built from, `backtest/results/liquidity_timing_backtest.md` (which
*did* survive -- the distilled writeup's source table, not the code). See
`.claude/tasks/20-vol-targeting-then-19-dsr-pbo.task.md` Phase 2 for the reconstruction
spec and fidelity guardrail; see `results/study19_first_pass_diff.md` for the fidelity
comparison this reconstruction was validated against on its first (and, per the task's
anti-tuning rule, only unconditionally-sanctioned) run.

Key methodology decisions PINNED DOWN FROM THE SURVIVING RAW TABLE (not guessed), because
`results/liquidity_timing_backtest.md` reports enough precision to reverse-engineer them
exactly:
  - Weekly grid = Fed net-liquidity's own weekly (Wednesday) cadence. Each asset's daily
    close is aligned onto that Wednesday index via an "as-of" backward join (last close
    on/before each Wednesday) -- confirmed because the first valid week for each asset,
    under this join, lands exactly on the first Wednesday on/after that asset's own
    first `_full.csv` price date (BTC 2010-07-18 -> 2010-07-21; ETH 2015-08-08 ->
    2015-08-12; SOL 2020-04-10 -> 2020-04-15), and:
  - Walk-forward INITIAL TRAINING WINDOW = exactly 157 weeks. Verified by reverse-
    engineering the published "OOS from" dates in the raw table (BTC 2013-07-24, ETH
    2018-08-15, SOL 2023-04-19): each is EXACTLY 157 weekly (Wednesday) periods after
    that asset's own first valid aligned week -- an exact match across all three assets,
    not a coincidence, so this is treated as a confirmed reconstruction detail rather
    than a free parameter.
  - OOS block length = 13 weeks and the L-grid = {4, 8, 13, 26, 52} weeks -- both stated
    directly in prose in `04_timing_backtest.md` / the raw table header, not inferred.

Judgment calls NOT pinned down by any surviving artifact (documented here, flagged again
in the task's completion report, per the anti-tuning rule -- these were fixed BEFORE the
first run and were not adjusted afterward):
  - Sharpe convention: annualized mean/std of weekly returns (x sqrt(52)), NO risk-free
    subtraction. (`vol_targeted_dca_backtest.py`'s docstring already flagged this exact
    convention question as unresolved for study 04; this reconstruction is what pins it
    down going forward.)
  - Switch-cost application: 10bps subtracted from the return of any week in which the
    position (asset vs. cash) differs from the previous week's position. The very first
    week a variant becomes valid is NOT charged a cost (establishing an initial stance,
    not "switching" from a prior one).
  - Walk-forward repick tie-break: if two+ candidate windows L tie exactly on in-sample
    Sharpe at a repick point, the smallest L wins (deterministic, essentially never
    binds in practice with continuous Sharpe values).
  - T-bill cash leg: DGS3MO fetched live from FRED (same `fredgraph.csv` endpoint as
    `fetch_macro_data.py`); if unreachable, cash leg = 0% for every week, per both
    `04_timing_backtest.md`'s and `06_strategy_zoo.md`'s explicit "cash leg = 0% when
    FRED unreachable" caveat. The fetch attempt and its outcome (success + sample rates,
    or failure) are logged to stdout every run, never silently assumed.

Outputs (this run, Phase 2 steps 1-4 of the task file):
  - `results/study19_variant_returns_04_insample.csv` -- Part 1's T x 15 matrix (15
    columns = 5 windows x 3 assets, one shared weekly index, NaN before each asset's own
    price history starts).
  - `results/study19_variant_returns_04_walkforward.csv` -- Part 2's chained walk-forward
    OOS return series, T x 3 (one column per asset, each asset's own OOS length, NaN
    elsewhere on the shared index). THIS series' own length is what must be fed as `T`
    into `deflated_sharpe_ratio()` in the next checkpoint -- never the longer in-sample
    grid's length.
"""

import os
import io
import urllib.request

import numpy as np
import pandas as pd

BACKTEST_DIR = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..")  # backtest/ root (task 159)
PRICES = os.path.join(BACKTEST_DIR, "data", "prices")
MACRO = os.path.join(BACKTEST_DIR, "data", "macro")
RESULTS = os.path.join(BACKTEST_DIR, "results")

ASSETS = ["BTC", "ETH", "SOL"]
TICKER_STUB = {"BTC": "BTC_USD", "ETH": "ETH_USD", "SOL": "SOL_USD"}
WINDOWS = [4, 8, 13, 26, 52]          # weeks -- fixed count, decided in the task spec
COST_BPS = 0.0010                     # 10bps/switch, per 04_timing_backtest.md
INITIAL_TRAIN_WEEKS = 157             # pinned down from the raw table -- see docstring
OOS_BLOCK_WEEKS = 13


# ---------------------------------------------------------------------------
# Data loading
# ---------------------------------------------------------------------------

def load_full_close(stub: str) -> pd.Series:
    """Load a clean `{stub}_full.csv` (date,close) -- per the task spec, ONLY the
    `_full.csv` schema is used for BTC/ETH/SOL, never the messier non-`_full` variant."""
    path = os.path.join(PRICES, f"{stub}_full.csv")
    s = pd.read_csv(path, parse_dates=["date"]).set_index("date")["close"].sort_index()
    return s.astype(float)


def fred_dgs3mo(timeout=12):
    """Attempt a live FRED fetch of DGS3MO (3-month T-bill, annualized %), same endpoint
    convention as `fetch_macro_data.py`'s `fred()`. Returns (series_or_None, success_bool,
    message_str) -- never raises; the caller decides the 0%-fallback policy."""
    url = "https://fred.stlouisfed.org/graph/fredgraph.csv?id=DGS3MO"
    try:
        req = urllib.request.Request(url, headers={"User-Agent": "Mozilla/5.0 (study19-reconstruction)"})
        with urllib.request.urlopen(req, timeout=timeout) as r:
            raw = r.read()
        df = pd.read_csv(io.BytesIO(raw), na_values=[".", ""])
        df.columns = ["date", "value"]
        df["date"] = pd.to_datetime(df["date"])
        s = df.set_index("date")["value"].astype(float).dropna()
        msg = (f"DGS3MO fetch SUCCEEDED: {s.index.min().date()} -> {s.index.max().date()}, "
               f"n={len(s)}; sample rates: {s.tail(3).to_dict()}")
        return s, True, msg
    except Exception as e:  # noqa: BLE001 -- log and fall back, never crash the study
        msg = (f"DGS3MO fetch FAILED ({type(e).__name__}: {e}) -- falling back to 0% cash "
               f"leg, matching both 04_timing_backtest.md's and 06_strategy_zoo.md's "
               f"documented 'cash leg = 0% when FRED unreachable' convention.")
        return None, False, msg


def cash_weekly_return(weekly_index: pd.DatetimeIndex):
    """Weekly T-bill cash return aligned to `weekly_index`. Returns (series, success, msg)."""
    dgs, ok, msg = fred_dgs3mo()
    print(f"[cash-leg] {msg}")
    if not ok:
        return pd.Series(0.0, index=weekly_index), False, msg
    # as-of align the (irregular daily/weekly, sometimes missing on holidays) DGS3MO
    # series onto our Wednesday index, then convert the annualized % rate to a simple
    # weekly return: (1 + rate/100) ** (1/52) - 1.
    left = pd.DataFrame({"date": weekly_index})
    right = dgs.rename("rate").rename_axis("date").reset_index()
    aligned = pd.merge_asof(left, right, on="date", direction="backward").set_index("date")["rate"]
    weekly_ret = (1.0 + aligned / 100.0) ** (1.0 / 52.0) - 1.0
    return weekly_ret.fillna(0.0), True, msg


# ---------------------------------------------------------------------------
# Metrics
# ---------------------------------------------------------------------------

def calc_sharpe(returns: pd.Series, periods_per_year=52) -> float:
    r = returns.dropna()
    sd = r.std()
    if not sd or sd <= 0 or np.isnan(sd):
        return float("nan")
    return float(r.mean() / sd * np.sqrt(periods_per_year))


def calc_cagr(returns: pd.Series) -> float:
    r = returns.dropna()
    if r.empty:
        return float("nan")
    nav = (1.0 + r).cumprod()
    years = (r.index.max() - r.index.min()).days / 365.25
    if years <= 0:
        return float("nan")
    return float(nav.iloc[-1] ** (1.0 / years) - 1.0)


def calc_maxdd(returns: pd.Series) -> float:
    r = returns.dropna()
    if r.empty:
        return float("nan")
    nav = (1.0 + r).cumprod()
    return float((nav / nav.cummax() - 1.0).min())


# ---------------------------------------------------------------------------
# Signal & strategy construction
# ---------------------------------------------------------------------------

def weekly_align(daily: pd.Series, weekly_index: pd.DatetimeIndex) -> pd.Series:
    """As-of backward join: value at each weekly_index date = last daily close on/before
    that date (NaN if the daily series hasn't started yet)."""
    left = pd.DataFrame({"date": weekly_index})
    right = daily.rename("close").rename_axis("date").reset_index()
    out = pd.merge_asof(left, right, on="date", direction="backward").set_index("date")["close"]
    return out


def position_from_signal(signal: pd.Series) -> pd.Series:
    """1.0 = long asset, 0.0 = cash, NaN = not yet defined."""
    pos = pd.Series(np.nan, index=signal.index)
    pos[signal > 0] = 1.0
    pos[signal <= 0] = 0.0
    return pos


def strategy_returns_with_cost(position: pd.Series, asset_ret: pd.Series, cash_ret: pd.Series,
                                cost_bps: float = COST_BPS) -> tuple:
    """Walk position/asset_ret/cash_ret sequentially, charge `cost_bps` whenever the
    position changes from the previous *valid* week (first valid week is a free initial
    stance, not a 'switch'). Returns (return_series, n_trades)."""
    idx = position.index
    ret = pd.Series(np.nan, index=idx)
    prev_pos = None
    n_trades = 0
    for t in idx:
        p = position.loc[t]
        a = asset_ret.loc[t] if t in asset_ret.index else np.nan
        if np.isnan(p) or np.isnan(a):
            prev_pos = None
            continue
        c = cash_ret.loc[t] if t in cash_ret.index else 0.0
        gross = p * a + (1.0 - p) * c
        cost = 0.0
        if prev_pos is not None and p != prev_pos:
            cost = cost_bps
            n_trades += 1
        ret.loc[t] = gross - cost
        prev_pos = p
    return ret, n_trades


def main():
    os.makedirs(RESULTS, exist_ok=True)
    netliq = pd.read_csv(os.path.join(MACRO, "FED_NET_LIQUIDITY.csv"),
                          parse_dates=["date"]).set_index("date")["net_liquidity_bn"].astype(float)
    weekly_index = netliq.index

    cash_ret, cash_ok, cash_msg = cash_weekly_return(weekly_index)

    asset_close = {a: load_full_close(TICKER_STUB[a]) for a in ASSETS}
    asset_weekly_close = {a: weekly_align(asset_close[a], weekly_index) for a in ASSETS}
    asset_ret = {a: asset_weekly_close[a].pct_change() for a in ASSETS}

    print("\n[data] asset weekly-aligned coverage (as-of backward join onto net-liq Wednesdays):")
    for a in ASSETS:
        valid = asset_ret[a].dropna()
        print(f"  {a}: first_valid_week={valid.index.min().date()} last={valid.index.max().date()} "
              f"n_returns={len(valid)}")

    # --- signals per window L ---
    signals = {L: netliq.pct_change(L).shift(1) for L in WINDOWS}  # 1-week publication lag
    positions = {(a, L): position_from_signal(signals[L].reindex(weekly_index))
                 for a in ASSETS for L in WINDOWS}
    # zero out positions where the asset itself has no valid return yet
    for a in ASSETS:
        for L in WINDOWS:
            mask_no_asset = asset_ret[a].isna()
            positions[(a, L)] = positions[(a, L)].mask(mask_no_asset, np.nan)

    # --- Part 1: in-sample full-history returns per (asset, L) ---
    part1_returns = {}
    part1_trades = {}
    for a in ASSETS:
        for L in WINDOWS:
            ret, n_trades = strategy_returns_with_cost(positions[(a, L)], asset_ret[a], cash_ret)
            part1_returns[(a, L)] = ret
            part1_trades[(a, L)] = n_trades

    insample_df = pd.DataFrame({f"{a}_L{L}": part1_returns[(a, L)] for a in ASSETS for L in WINDOWS})
    insample_path = os.path.join(RESULTS, "study19_variant_returns_04_insample.csv")
    insample_df.to_csv(insample_path)
    print(f"\n[output] wrote {insample_path} ({insample_df.shape[0]} rows x {insample_df.shape[1]} cols)")

    print("\n## Part 1 -- full-sample window scan (in-sample, descriptive)\n")
    bh_stats = {}
    for a in ASSETS:
        bh_ret = asset_ret[a].dropna()
        bh_sharpe = calc_sharpe(bh_ret)
        bh_cagr = calc_cagr(bh_ret)
        bh_maxdd = calc_maxdd(bh_ret)
        bh_stats[a] = (bh_sharpe, bh_cagr, bh_maxdd, len(bh_ret))
        print(f"**{a}** -- buy&hold: CAGR {bh_cagr*100:.0f}% * Sharpe {bh_sharpe:.2f} * "
              f"maxDD {bh_maxdd*100:.0f}% * n={len(bh_ret)}w")
        print("| net-liq L | CAGR% | Sharpe | maxDD% | time-in% | trades |")
        print("|--:|--:|--:|--:|--:|--:|")
        for L in WINDOWS:
            r = part1_returns[(a, L)].dropna()
            sharpe = calc_sharpe(r)
            cagr = calc_cagr(r)
            maxdd = calc_maxdd(r)
            time_in = positions[(a, L)].dropna().mean() * 100
            beat = "*" if sharpe > bh_sharpe else ""
            print(f"| {L}w | {cagr*100:.0f} | {sharpe:.2f} {beat} | {maxdd*100:.0f} | "
                  f"{time_in:.0f} | {part1_trades[(a, L)]} |")
        print()

    # --- Part 2: walk-forward OOS ---
    print("\n## Part 2 -- walk-forward OOS (expanding window, re-pick best-Sharpe L, "
          f"trade next {OOS_BLOCK_WEEKS}w, chain the blocks)\n")

    wf_series = {}
    wf_summary = {}
    for a in ASSETS:
        valid_idx = asset_ret[a].dropna().index  # this asset's own usable weekly timeline
        n = len(valid_idx)
        if n <= INITIAL_TRAIN_WEEKS + OOS_BLOCK_WEEKS:
            print(f"{a}: insufficient history for walk-forward ({n} valid weeks) -- skipped.")
            continue

        chain_ret = pd.Series(np.nan, index=valid_idx)
        chain_prev_pos = None
        chosen_L_log = []
        block_start = INITIAL_TRAIN_WEEKS
        while block_start < n:
            block_end = min(block_start + OOS_BLOCK_WEEKS, n)
            train_weeks = valid_idx[:block_start]  # expanding window, up to (not incl.) this block

            # pick best in-sample Sharpe among the L grid, using ONLY train_weeks
            best_L, best_sharpe = None, -np.inf
            for L in WINDOWS:
                r = part1_returns[(a, L)].reindex(train_weeks).dropna()
                if len(r) < 8:  # too little data for any meaningful Sharpe -- skip candidate
                    continue
                s = calc_sharpe(r)
                if np.isnan(s):
                    continue
                if s > best_sharpe or (s == best_sharpe and (best_L is None or L < best_L)):
                    best_sharpe, best_L = s, L
            if best_L is None:
                best_L = WINDOWS[0]  # degenerate fallback, shouldn't trigger given 157wk train

            block_weeks = valid_idx[block_start:block_end]
            for t in block_weeks:
                p = positions[(a, best_L)].get(t, np.nan)
                if np.isnan(p):
                    continue
                ar = asset_ret[a].get(t, np.nan)
                cr = cash_ret.get(t, 0.0)
                if np.isnan(ar):
                    continue
                gross = p * ar + (1.0 - p) * cr
                cost = COST_BPS if (chain_prev_pos is not None and p != chain_prev_pos) else 0.0
                chain_ret.loc[t] = gross - cost
                chain_prev_pos = p
            chosen_L_log.append((block_weeks[0].date() if len(block_weeks) else None, best_L))
            block_start = block_end

        wf_series[a] = chain_ret.dropna()
        r = wf_series[a]
        oos_from = r.index.min()
        oos_sharpe = calc_sharpe(r)
        oos_cagr = calc_cagr(r)
        oos_maxdd = calc_maxdd(r)
        bh_oos = asset_ret[a].reindex(r.index).dropna()
        bh_oos_sharpe = calc_sharpe(bh_oos)
        bh_oos_cagr = calc_cagr(bh_oos)
        bh_oos_maxdd = calc_maxdd(bh_oos)
        wf_summary[a] = dict(oos_sharpe=oos_sharpe, oos_cagr=oos_cagr, oos_maxdd=oos_maxdd,
                              bh_sharpe=bh_oos_sharpe, bh_cagr=bh_oos_cagr, bh_maxdd=bh_oos_maxdd,
                              oos_from=oos_from, n_oos_weeks=len(r),
                              n_blocks=len(chosen_L_log), L_history=chosen_L_log)

    print("| asset | OOS CAGR% | OOS Sharpe | OOS maxDD% | B&H CAGR% | B&H Sharpe | B&H maxDD% | OOS from | n_weeks |")
    print("|---|--:|--:|--:|--:|--:|--:|--|--:|")
    for a in ASSETS:
        if a not in wf_summary:
            continue
        s = wf_summary[a]
        print(f"| {a} | {s['oos_cagr']*100:.0f} | {s['oos_sharpe']:.2f} | {s['oos_maxdd']*100:.0f} | "
              f"{s['bh_cagr']*100:.0f} | {s['bh_sharpe']:.2f} | {s['bh_maxdd']*100:.0f} | "
              f"{s['oos_from'].date()} | {s['n_oos_weeks']} |")

    # --- write the required walk-forward output CSV (T x 3, shared index, NaN outside
    #     each asset's own OOS window) ---
    wf_df = pd.DataFrame({a: wf_series[a] for a in wf_series})
    wf_path = os.path.join(RESULTS, "study19_variant_returns_04_walkforward.csv")
    wf_df.to_csv(wf_path)
    print(f"\n[output] wrote {wf_path} ({wf_df.shape[0]} rows x {wf_df.shape[1]} cols)")

    print("\n[cash-leg summary] " + cash_msg)

    return dict(bh_stats=bh_stats, part1_returns=part1_returns, part1_trades=part1_trades,
                positions=positions, wf_summary=wf_summary, cash_ok=cash_ok, cash_msg=cash_msg)


if __name__ == "__main__":
    main()
