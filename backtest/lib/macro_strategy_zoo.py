#!/usr/bin/env python3
"""
Study 06 (RECONSTRUCTED 2026-07-12) -- macro-strategy zoo: 11 fixed, a-priori macro
risk-on/off rules x 28 assets, judged by BREADTH (how many assets a rule helps), plus a
Part 3 walk-forward re-pick on BTC/ETH/SOL.

*** RECONSTRUCTION NOTICE ***
This script did not survive in git history (confirmed during Phase-2/#19 scoping, same
as `liquidity_timing_backtest.py` / study 04 -- see that file's docstring and
`.claude/tasks/20-vol-targeting-then-19-dsr-pbo.task.md` Phase 2). Rebuilt from
`backtest/results/study_summary/06_strategy_zoo.md`, its raw table
`backtest/results/macro_strategy_zoo.md`, and `01_data_and_methods.md`'s asset inventory
/ transform / lag conventions. See `results/study19_first_pass_diff.md` for the fidelity
comparison this reconstruction was validated against.

**KNOWN, DELIBERATE ASSET-ROSTER DIVERGENCE FROM THE ORIGINAL STUDY -- read before
comparing numbers.** The task file's Phase 2 step 3 spec (already reviewed/tightened by a
data-analyst pass, see the task file's "Reviewer's verdict") specifies this exact
28-asset list, reconstructed from `01_data_and_methods.md`'s general asset inventory:
19 traditional (SPY/QQQ/IWM/EEM/TLT/HYG/LQD/UUP/GLD/SLV/NVDA/AAPL/MSFT/TSLA/AMZN/GOOGL/
JPM/XLF/XLE) + 6 crypto majors (BTC/ETH/SOL/ADA/LINK/DOGE) + 3 crypto-equities
(MSTR/COIN/MARA). Cross-checking this against the SURVIVING raw table
(`results/macro_strategy_zoo.md`) shows the ORIGINAL study actually used a slightly
DIFFERENT 28-asset roster: it included BNB and XRP (both present as CSVs in
`data/prices/`) but did NOT include JPM or HYG at all. This reconstruction follows the
task file's explicit spec (JPM/HYG in, BNB/XRP out) rather than silently reverting to the
original roster, per this run's instruction to implement the already-reviewed plan as
written, not re-derive it -- but this means the aggregate breadth metrics (median
DeltaSharpe, %beat) are being computed over a MODESTLY DIFFERENT 28-asset universe than
the one that produced the published 20/28 -- a 2-asset swap out of 28, not a bug. The six
named individual assets the task's own guardrail checks (COIN/SOL/MARA/TSLA/ADA/MSTR
positive; UUP/GLD negative) are present in BOTH rosters, so that specific check is
unaffected.

Rule constructions (all reuse this repo's ALREADY-ESTABLISHED acceleration/lag
conventions -- `classic_portfolios_backtest.py`'s `nl_accel`/`gm_accel`, and
`01_data_and_methods.md`'s stated publication lags: ISM/net-liq ~+1mo, M2 ~+2mo -- not
invented fresh):
   1. NetLiq accel>0      : netliq_monthly.pct_change(12).diff(3).shift(1) > 0
   2. GlobalM2 YoY>0       : global_m2.pct_change(12).shift(2) > 0
   3. NetLiq YoY>0         : netliq_monthly.pct_change(12).shift(1) > 0
   4. GlobalM2 YoY accel>0 : global_m2.pct_change(12).diff(3).shift(2) > 0
   5. USM2 YoY accel>0     : us_m2.pct_change(12).diff(3).shift(2) > 0
   6. NetLiq rising(3m)    : netliq_monthly.pct_change(3).shift(1) > 0   (LEVEL 3mo change,
      distinct from rule 3's YoY -- judgment call, see below)
   7. Gold YoY>0 (risk proxy): GLD_full.csv monthly close .pct_change(12).shift(1) > 0
   8. Combo M2accel & NetLiqYoY: rule 4 AND rule 3 (both already independently lagged)
   9. ISM rising           : ism.diff(1).shift(1) > 0
  10. ISM>50                : ism.shift(1) > 50
  11. ISM<50 & rising (early cycle): (ism.shift(1) < 50) AND (ism.diff(1).shift(1) > 0)

Judgment calls NOT pinned down by any surviving artifact (fixed BEFORE the first run,
not adjusted afterward, per the task's anti-tuning rule):
  - "NetLiq rising(3m)" (rule 6) is read as a LEVEL 3-month % change (distinct from rule
    3's YoY and rule 1's YoY-acceleration) -- the name literally says "rising," not
    "accelerating" or "YoY," and it would otherwise duplicate an already-listed rule.
  - "Gold YoY>0 (risk proxy)" (rule 7) is read as a plain, uniformly-applied ON/OFF rule
    like every other row (hold asset when Gold YoY>0, else cash) -- i.e., the "(risk
    proxy)" annotation describes what the underlying series represents, it does not flip
    the ON/OFF direction relative to every other rule in this table.
  - "Combo M2accel & NetLiqYoY" (rule 8) is read as rule 4 AND rule 3 specifically (the
    two components literally named in its own label), not some other pair.
  - MARA's price history (`MARA.csv`) predates its 2020-2021 bitcoin-mining pivot (it was
    "Marathon Patent Group," a patent-licensing shell, before that). This reconstruction
    uses MARA's FULL raw history, unmodified, to match what the ORIGINAL study most
    likely did (there is no evidence in either .md writeup that MARA was truncated, and
    truncating it now -- discovered only during this review -- would be an untested,
    unreviewed methodology CHANGE relative to the original, which the anti-tuning rule
    says not to introduce pre-emptively). Flagged here as an inherited data-quality
    caveat, not fixed.
  - Freshness: ADA/LINK/DOGE/MSTR/COIN/MARA (no maintained fetcher, see
    `01_data_and_methods.md`'s "kept" pipeline list) are ~3-4 weeks stale (to
    2026-06-12/16) relative to the other 22 assets (to 2026-07-06 / 2026-06-18, refreshed
    2026-07-10). Documented here, not patched with a new fetcher in this run (out of
    scope for Phase 2 steps 1-4 -- see the task file's step 10 for where this belongs).
  - T-bill cash leg: same DGS3MO-fetch-or-0%-fallback policy as `liquidity_timing_backtest.py`
    (study 04) -- see that file for the fetch implementation, duplicated here so this
    script stays independently runnable.
  - Part 3 walk-forward: window grid {1, 3, 6} months and a 6-month OOS block are stated
    directly in the raw table's own header ("pick GlobalM2 accel window {1,3,6}m on the
    past, trade next 6m OOS") -- not inferred. The INITIAL TRAINING WINDOW (61 months) IS
    a reverse-engineered, pinned-down detail: the published "OOS from" dates (BTC
    2015-08, ETH 2020-09) are each EXACTLY 61 monthly periods after that asset's own
    first available monthly close (BTC 2010-07, ETH 2015-08) -- an exact match for both
    assets, treated as confirmed, not a free parameter.

Outputs (this run, Phase 2 steps 1-4 of the task file):
  - `results/study19_variant_returns_06.csv` -- T x 308 matrix (11 rules x 28 assets) of
    monthly strategy returns, one column per (rule, asset) pair.
  - `results/study19_variant_returns_06_part3_walkforward.csv` -- bonus artifact (not one
    of the two/one CSVs formally required by the task's step 8 list, but cheap to produce
    now and needed again in the next checkpoint) -- Part 3's chained walk-forward OOS
    monthly return series for BTC/ETH/SOL.
"""

import os
import io
import urllib.request

import numpy as np
import pandas as pd

# NOTE (task 28, 2026-09-25): this module moved from backtest/ to backtest/lib/ (task
# 159 packaging) but data/ still lives in backtest/data/, so resolve one level above
# this file's own dir (was BACKTEST_DIR=dirname(__file__) -> backtest/lib/data/... which
# does not exist; broke macro_monthly()/load_close() for globalm2_leg and ic_framework).
BACKTEST_DIR = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
PRICES = os.path.join(BACKTEST_DIR, "data", "prices")
MACRO = os.path.join(BACKTEST_DIR, "data", "macro")
RESULTS = os.path.join(BACKTEST_DIR, "results")

COST_BPS = 0.0015   # 15bps/switch, per 06_strategy_zoo.md

# --- 28-asset roster, per the task file's step-3 spec (see docstring for the
#     documented divergence from the original study's actual roster) ---
TRADITIONAL_19 = ["SPY", "QQQ", "IWM", "EEM", "TLT", "HYG", "LQD", "UUP", "GLD", "SLV",
                  "NVDA", "AAPL", "MSFT", "TSLA", "AMZN", "GOOGL", "JPM", "XLF", "XLE"]
CRYPTO_MAJORS_6 = ["BTC", "ETH", "SOL", "ADA", "LINK", "DOGE"]
CRYPTO_EQUITY_3 = ["MSTR", "COIN", "MARA"]
ASSETS_28 = TRADITIONAL_19 + CRYPTO_MAJORS_6 + CRYPTO_EQUITY_3
assert len(ASSETS_28) == 28, len(ASSETS_28)

# file-stub per ticker (the string used to build the CSV filename)
FILE_STUB = {a: a for a in TRADITIONAL_19}
FILE_STUB.update({"BTC": "BTC_USD", "ETH": "ETH_USD", "SOL": "SOL_USD",
                   "ADA": "ADA_USD", "LINK": "LINK_USD", "DOGE": "DOGE_USD",
                   "MSTR": "MSTR", "COIN": "COIN", "MARA": "MARA"})

ORPHAN_TICKERS = {"ADA", "LINK", "DOGE", "MSTR", "COIN", "MARA"}  # no maintained fetcher


# ---------------------------------------------------------------------------
# Schema-aware price loader (mirrors classic_portfolios_backtest.py's usd_close())
# ---------------------------------------------------------------------------

def load_close(stub: str):
    """Returns (close_series, schema_str). Detects and normalizes both CSV schemas
    present in data/prices/: the clean `{stub}_full.csv` (date,close) and the raw
    multi-index yfinance export (`{stub}.csv`, 3 junk header rows, needs the Close
    column, index 4)."""
    full = os.path.join(PRICES, f"{stub}_full.csv")
    if os.path.exists(full):
        s = pd.read_csv(full, parse_dates=["date"]).set_index("date")["close"].sort_index()
        return s.astype(float), "full"
    raw = os.path.join(PRICES, f"{stub}.csv")
    df = pd.read_csv(raw, skiprows=[1, 2])
    df = df.rename(columns={df.columns[0]: "date"})
    df["date"] = pd.to_datetime(df["date"], errors="coerce")
    df = df.dropna(subset=["date"]).set_index("date")
    col = "Close" if "Close" in df.columns else df.columns[4]
    s = pd.to_numeric(df[col], errors="coerce").sort_index()
    return s, "raw-multiindex"


def sanity_check_orphans():
    print("\n[sanity check] the 6 orphan tickers (no `_full.csv`, raw multi-index schema):")
    for a in sorted(ORPHAN_TICKERS):
        s, schema = load_close(FILE_STUB[a])
        s = s.dropna()
        print(f"  {a} ({FILE_STUB[a]}.csv, schema={schema}): "
              f"first={s.index.min().date()} {s.iloc[0]:.4f}  "
              f"last={s.index.max().date()} {s.iloc[-1]:.4f}  n={len(s)}")


def fred_dgs3mo(timeout=12):
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
    except Exception as e:  # noqa: BLE001
        msg = (f"DGS3MO fetch FAILED ({type(e).__name__}: {e}) -- falling back to 0% cash "
               f"leg, matching both 04_timing_backtest.md's and 06_strategy_zoo.md's "
               f"documented 'cash leg = 0% when FRED unreachable' convention.")
        return None, False, msg


def cash_monthly_return(monthly_index):
    dgs, ok, msg = fred_dgs3mo()
    print(f"[cash-leg] {msg}")
    if not ok:
        return pd.Series(0.0, index=monthly_index), False, msg
    m = dgs.resample("ME").mean()
    m.index = m.index.to_period("M")
    aligned = m.reindex(monthly_index).ffill()
    monthly_ret = (1.0 + aligned / 100.0) ** (1.0 / 12.0) - 1.0
    return monthly_ret.fillna(0.0), True, msg


# ---------------------------------------------------------------------------
# Metrics (mirrors liquidity_timing_backtest.py's, annualized by sqrt(12) for monthly)
# ---------------------------------------------------------------------------

def calc_sharpe(returns, periods_per_year=12):
    r = returns.dropna()
    sd = r.std()
    if not sd or sd <= 0 or np.isnan(sd):
        return float("nan")
    return float(r.mean() / sd * np.sqrt(periods_per_year))


def calc_cagr(returns, periods_per_year=12):
    r = returns.dropna()
    if r.empty:
        return float("nan")
    nav = (1.0 + r).cumprod()
    years = len(r) / periods_per_year
    if years <= 0:
        return float("nan")
    return float(nav.iloc[-1] ** (1.0 / years) - 1.0)


def calc_maxdd(returns):
    r = returns.dropna()
    if r.empty:
        return float("nan")
    nav = (1.0 + r).cumprod()
    return float((nav / nav.cummax() - 1.0).min())


# ---------------------------------------------------------------------------
# Macro monthly series & the 11 rules
# ---------------------------------------------------------------------------

def macro_monthly(fname, col, weekly=False):
    s = pd.read_csv(os.path.join(MACRO, fname), parse_dates=["date"]).set_index("date")[col].astype(float)
    if weekly:
        s = s.resample("ME").last()
    s.index = s.index.to_period("M")
    return s.groupby(level=0).last()


def build_rules(netliq_m, gm_m, usm2_m, ism_m, gld_m):
    nl_yoy = netliq_m.pct_change(12)
    nl_accel = nl_yoy.diff(3).shift(1)
    nl_yoy_lagged = nl_yoy.shift(1)
    nl_rising3m = netliq_m.pct_change(3).shift(1)

    gm_yoy = gm_m.pct_change(12)
    gm_yoy_lagged = gm_yoy.shift(2)
    gm_accel = gm_yoy.diff(3).shift(2)

    usm2_yoy = usm2_m.pct_change(12)
    usm2_accel = usm2_yoy.diff(3).shift(2)

    gold_yoy = gld_m.pct_change(12).shift(1)

    ism_lagged = ism_m.shift(1)
    ism_diff_lagged = ism_m.diff(1).shift(1)

    rules = {
        "NetLiqAccel": nl_accel > 0,
        "GlobalM2YoY": gm_yoy_lagged > 0,
        "NetLiqYoY": nl_yoy_lagged > 0,
        "GlobalM2Accel": gm_accel > 0,
        "USM2Accel": usm2_accel > 0,
        "NetLiqRising3m": nl_rising3m > 0,
        "GoldYoY": gold_yoy > 0,
        "ComboM2AccelNetLiqYoY": (gm_accel > 0) & (nl_yoy_lagged > 0),
        "ISMRising": ism_diff_lagged > 0,
        "ISMGt50": ism_lagged > 50,
        "ISMLt50Rising": (ism_lagged < 50) & (ism_diff_lagged > 0),
    }
    # preserve NaN where inputs are NaN (comparisons above turn NaN into False -- fix that)
    masks = {
        "NetLiqAccel": nl_accel, "GlobalM2YoY": gm_yoy_lagged, "NetLiqYoY": nl_yoy_lagged,
        "GlobalM2Accel": gm_accel, "USM2Accel": usm2_accel, "NetLiqRising3m": nl_rising3m,
        "GoldYoY": gold_yoy, "ComboM2AccelNetLiqYoY": gm_accel.combine(nl_yoy_lagged, lambda a, b: np.nan if (pd.isna(a) or pd.isna(b)) else 0.0),
        "ISMRising": ism_diff_lagged, "ISMGt50": ism_lagged, "ISMLt50Rising": ism_lagged.combine(ism_diff_lagged, lambda a, b: np.nan if (pd.isna(a) or pd.isna(b)) else 0.0),
    }
    signals = {}
    for name, boolean in rules.items():
        sig = boolean.astype(float)
        sig[masks[name].isna()] = np.nan
        signals[name] = sig  # 1.0 = ON (hold asset), 0.0 = OFF (cash), NaN = undefined
    return signals


RULE_NAMES = ["NetLiqAccel", "GlobalM2YoY", "NetLiqYoY", "GlobalM2Accel", "USM2Accel",
              "NetLiqRising3m", "GoldYoY", "ComboM2AccelNetLiqYoY", "ISMRising",
              "ISMGt50", "ISMLt50Rising"]
RULE_LABEL = {
    "NetLiqAccel": "NetLiq accel>0", "GlobalM2YoY": "GlobalM2 YoY>0", "NetLiqYoY": "NetLiq YoY>0",
    "GlobalM2Accel": "GlobalM2 YoY accel>0", "USM2Accel": "USM2 YoY accel>0",
    "NetLiqRising3m": "NetLiq rising(3m)", "GoldYoY": "Gold YoY>0 (risk proxy)",
    "ComboM2AccelNetLiqYoY": "Combo M2accel & NetLiqYoY", "ISMRising": "ISM rising",
    "ISMGt50": "ISM>50", "ISMLt50Rising": "ISM<50 & rising (early cycle)",
}


def strategy_returns_with_cost(position, asset_ret, cash_ret, cost_bps=COST_BPS):
    idx = position.index
    ret = pd.Series(np.nan, index=idx, dtype=float)
    prev_pos = None
    n_trades = 0
    for t in idx:
        p = position.loc[t]
        a = asset_ret.loc[t] if t in asset_ret.index else np.nan
        if pd.isna(p) or pd.isna(a):
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
    sanity_check_orphans()

    netliq_m = macro_monthly("FED_NET_LIQUIDITY.csv", "net_liquidity_bn", weekly=True)
    gm_m = macro_monthly("GLOBAL_M2.csv", "global_m2_bn")
    usm2_m = macro_monthly("US_M2.csv", "m2_usd_bn")
    ism_m = macro_monthly("ISM_PMI.csv", "ism_pmi")

    gld_close, _ = load_close("GLD")
    gld_m = gld_close.resample("ME").last()
    gld_m.index = gld_m.index.to_period("M")

    signals = build_rules(netliq_m, gm_m, usm2_m, ism_m, gld_m)

    monthly_index = netliq_m.index  # macro monthly grid (2008-01 -> latest)
    cash_ret, cash_ok, cash_msg = cash_monthly_return(monthly_index)

    asset_close_m = {}
    asset_ret_m = {}
    schema_used = {}
    print("\n[data] per-asset monthly close coverage (28 assets):")
    for a in ASSETS_28:
        close, schema = load_close(FILE_STUB[a])
        schema_used[a] = schema
        m = close.resample("ME").last()
        m.index = m.index.to_period("M")
        m = m.groupby(level=0).last()
        asset_close_m[a] = m
        r = m.pct_change()
        asset_ret_m[a] = r
        valid = r.dropna()
        stale_flag = " [STALE, orphan ticker]" if a in ORPHAN_TICKERS else ""
        if len(valid):
            print(f"  {a} ({schema}): {valid.index.min()} -> {valid.index.max()} n={len(valid)}{stale_flag}")
        else:
            print(f"  {a} ({schema}): NO VALID RETURNS{stale_flag}")

    # --- Part 1 + Part 2 raw data: T x 308 matrix of (rule, asset) monthly returns ---
    part_returns = {}
    part_trades = {}
    for rule in RULE_NAMES:
        sig = signals[rule].reindex(monthly_index)
        for a in ASSETS_28:
            pos = sig.reindex(asset_ret_m[a].index)
            ret, n_trades = strategy_returns_with_cost(pos, asset_ret_m[a], cash_ret)
            part_returns[(rule, a)] = ret
            part_trades[(rule, a)] = n_trades

    zoo_df = pd.DataFrame({f"{rule}__{a}": part_returns[(rule, a)]
                            for rule in RULE_NAMES for a in ASSETS_28})
    zoo_path = os.path.join(RESULTS, "study19_variant_returns_06.csv")
    zoo_df.to_csv(zoo_path)
    print(f"\n[output] wrote {zoo_path} ({zoo_df.shape[0]} rows x {zoo_df.shape[1]} cols)")

    # --- Part 1 breadth table ---
    print("\n## Part 1 -- breadth across 28 assets (median delta-Sharpe / %beat B&H)\n")
    print("| rule | median dSharpe | %beat B&H | median dCAGR% | median dmaxDD% |")
    print("|---|--:|--:|--:|--:|")
    bh_sharpe_cache = {}
    bh_cagr_cache = {}
    bh_maxdd_cache = {}
    for a in ASSETS_28:
        r = asset_ret_m[a].dropna()
        bh_sharpe_cache[a] = calc_sharpe(r)
        bh_cagr_cache[a] = calc_cagr(r)
        bh_maxdd_cache[a] = calc_maxdd(r)

    part1_table = {}
    for rule in RULE_NAMES:
        dsharpes, dcagrs, dmaxdds, beat = [], [], [], 0
        per_asset_dsharpe = {}
        for a in ASSETS_28:
            r = part_returns[(rule, a)].dropna()
            if len(r) < 12:  # need at least ~1yr of valid overlap to be meaningful
                continue
            s = calc_sharpe(r)
            c = calc_cagr(r)
            d = calc_maxdd(r)
            if np.isnan(s) or np.isnan(bh_sharpe_cache[a]):
                continue
            ds = s - bh_sharpe_cache[a]
            dsharpes.append(ds)
            dcagrs.append(c - bh_cagr_cache[a])
            dmaxdds.append((d - bh_maxdd_cache[a]) * 100)
            per_asset_dsharpe[a] = ds
            if s > bh_sharpe_cache[a]:
                beat += 1
        med_ds = float(np.median(dsharpes)) if dsharpes else float("nan")
        med_dc = float(np.median(dcagrs)) * 100 if dcagrs else float("nan")
        med_dd = float(np.median(dmaxdds)) if dmaxdds else float("nan")
        n_assets = len(dsharpes)
        part1_table[rule] = dict(median_dsharpe=med_ds, beat=beat, n_assets=n_assets,
                                  median_dcagr=med_dc, median_dmaxdd=med_dd,
                                  per_asset_dsharpe=per_asset_dsharpe)
        print(f"| {RULE_LABEL[rule]} | {med_ds:+.2f} | {beat}/{n_assets} | {med_dc:+.0f} | {med_dd:+.0f} |")

    # rank check (must remain #1 by median dSharpe)
    ranked = sorted(part1_table.items(), key=lambda kv: kv[1]["median_dsharpe"], reverse=True)
    print("\n[rank check] rules ranked by median dSharpe (descending):")
    for i, (rule, stats) in enumerate(ranked, start=1):
        print(f"  {i}. {RULE_LABEL[rule]}: {stats['median_dsharpe']:+.3f} ({stats['beat']}/{stats['n_assets']})")

    # --- Part 2: NetLiq accel>0 per-asset breakdown (for the named-asset sign check) ---
    print("\n## Part 2 -- NetLiq accel>0 per asset (dSharpe vs B&H)\n")
    named_check_assets = ["COIN", "SOL", "MARA", "TSLA", "ADA", "MSTR", "UUP", "GLD"]
    for a in named_check_assets:
        ds = part1_table["NetLiqAccel"]["per_asset_dsharpe"].get(a, float("nan"))
        print(f"  {a}: dSharpe = {ds:+.2f}")

    # --- Part 3: Global-M2-accel walk-forward window-repick on BTC/ETH/SOL ---
    print("\n## Part 3 -- walk-forward: pick GlobalM2 accel window {1,3,6}m on the past, "
          "trade next 6m OOS\n")
    gm_yoy_raw = gm_m.pct_change(12)
    part3_windows = [1, 3, 6]
    part3_positions = {}
    for w in part3_windows:
        accel_w = gm_yoy_raw.diff(w).shift(2)
        sig = (accel_w > 0).astype(float)
        sig[accel_w.isna()] = np.nan
        part3_positions[w] = sig

    INITIAL_TRAIN_MONTHS = 61  # pinned down from the raw table -- see docstring
    OOS_BLOCK_MONTHS = 6
    part3_series = {}
    part3_summary = {}
    for a in ["BTC", "ETH", "SOL"]:
        valid_idx = asset_ret_m[a].dropna().index
        n = len(valid_idx)
        if n <= INITIAL_TRAIN_MONTHS + OOS_BLOCK_MONTHS:
            print(f"{a}: insufficient history for Part 3 walk-forward ({n} valid months) -- "
                  f"n/a (short OOS), matches the published SOL caveat.")
            continue

        # precompute each candidate window's own full-history return series (needed for
        # expanding in-sample Sharpe evaluation at each repick point)
        per_w_returns = {}
        for w in part3_windows:
            pos = part3_positions[w].reindex(asset_ret_m[a].index)
            ret, _ = strategy_returns_with_cost(pos, asset_ret_m[a], cash_ret)
            per_w_returns[w] = ret

        chain_ret = pd.Series(np.nan, index=valid_idx, dtype=float)
        chain_prev_pos = None
        block_start = INITIAL_TRAIN_MONTHS
        chosen_log = []
        while block_start < n:
            block_end = min(block_start + OOS_BLOCK_MONTHS, n)
            train_months = valid_idx[:block_start]
            best_w, best_sharpe = None, -np.inf
            for w in part3_windows:
                r = per_w_returns[w].reindex(train_months).dropna()
                if len(r) < 12:
                    continue
                s = calc_sharpe(r)
                if np.isnan(s):
                    continue
                if s > best_sharpe or (s == best_sharpe and (best_w is None or w < best_w)):
                    best_sharpe, best_w = s, w
            if best_w is None:
                best_w = part3_windows[0]
            block_months = valid_idx[block_start:block_end]
            for t in block_months:
                p = part3_positions[best_w].reindex(asset_ret_m[a].index).get(t, np.nan)
                if pd.isna(p):
                    continue
                ar = asset_ret_m[a].get(t, np.nan)
                cr = cash_ret.get(t, 0.0)
                if pd.isna(ar):
                    continue
                gross = p * ar + (1.0 - p) * cr
                cost = COST_BPS if (chain_prev_pos is not None and p != chain_prev_pos) else 0.0
                chain_ret.loc[t] = gross - cost
                chain_prev_pos = p
            chosen_log.append((str(block_months[0]) if len(block_months) else None, best_w))
            block_start = block_end

        part3_series[a] = chain_ret.dropna()
        r = part3_series[a]
        oos_from = r.index.min()
        oos_sharpe = calc_sharpe(r)
        oos_cagr = calc_cagr(r)
        oos_maxdd = calc_maxdd(r)
        bh_oos = asset_ret_m[a].reindex(r.index).dropna()
        part3_summary[a] = dict(oos_sharpe=oos_sharpe, oos_cagr=oos_cagr, oos_maxdd=oos_maxdd,
                                 bh_sharpe=calc_sharpe(bh_oos), bh_cagr=calc_cagr(bh_oos),
                                 bh_maxdd=calc_maxdd(bh_oos), oos_from=oos_from,
                                 n_oos_months=len(r))

    print("| asset | OOS CAGR% | OOS Sharpe | OOS maxDD% | B&H CAGR% | B&H Sharpe | B&H maxDD% | OOS from |")
    print("|---|--:|--:|--:|--:|--:|--:|--|")
    for a in ["BTC", "ETH", "SOL"]:
        if a not in part3_summary:
            print(f"| {a} | n/a (short OOS) | | | | | | |")
            continue
        s = part3_summary[a]
        print(f"| {a} | {s['oos_cagr']*100:.0f} | {s['oos_sharpe']:.2f} | {s['oos_maxdd']*100:.0f} | "
              f"{s['bh_cagr']*100:.0f} | {s['bh_sharpe']:.2f} | {s['bh_maxdd']*100:.0f} | {s['oos_from']} |")

    part3_df = pd.DataFrame({a: part3_series[a] for a in part3_series})
    part3_path = os.path.join(RESULTS, "study19_variant_returns_06_part3_walkforward.csv")
    part3_df.to_csv(part3_path)
    print(f"\n[output] wrote {part3_path} ({part3_df.shape[0]} rows x {part3_df.shape[1]} cols)")

    print("\n[cash-leg summary] " + cash_msg)

    return dict(part1_table=part1_table, part3_summary=part3_summary, schema_used=schema_used,
                cash_ok=cash_ok, cash_msg=cash_msg)


if __name__ == "__main__":
    main()
