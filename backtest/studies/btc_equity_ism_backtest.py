#!/usr/bin/env python3
"""
Does conditioning the "BTC-equity" sleeve (BTC miners + MSTR) on ISM PMI add value?

Context
-------
Proposed rule: within total BTC exposure (BTC coin + miners + MSTR), the equity
sleeve (miners + MSTR) should be at most ~25% of BTC exposure when ISM PMI is very
high and ~10% when ISM is very low (PRO-CYCLICAL: more levered beta in expansion).
Candidate new miners: IREN, WULF (+ existing MARA, HUT, CIFR; MSTR as BTC treasury).

This script answers "how do we know if it's OK?" the same way ADR-040 killed the
regression-band weight: measure the signal, then the portfolio impact.

  PART A — Signal / Information Coefficient
    Is the miners+MSTR-minus-BTC monthly return spread predictable from ISM?
    No-lookahead: signal at the start of month t = ISM most recently PUBLISHED
    (ISM for month t-1, released ~1st business day of t). Target = realised spread
    over month t. Report Spearman & Pearson IC (level and dISM), t-stat, and the
    conditional mean spread in high- vs low-ISM regimes. Also the sleeve's realised
    beta/correlation to BTC (is it really "levered BTC"?).

  PART B — DCA sleeve backtest
    Hold the BTC-exposure budget fixed; vary only the BTC-vs-equity split.
    Monthly buy-only DCA (mirrors the production pure-proportional DCA). Compare
    static equity fractions {0,10,17,25,40%} vs ISM pro-cyclical vs ISM contrarian.
    If the ISM-conditioned schemes don't beat the best static, ISM adds nothing
    (=> use a fixed cap, or none). Metrics: final / ROI / money-weighted IRR /
    max drawdown / return-per-unit-drawdown.

Everything is in USD: BTC and the equity sleeve are both USD, so the within-sleeve
comparison is FX-invariant; dropping the USDPLN leg removes noise.
"""

import numpy as np, pandas as pd, os, json
import dca_regression_backtest as bt   # reuse load_close, monthly_irr, max_drawdown

RESULTS_DIR = os.path.join(os.path.join(os.path.dirname(__file__), ".."), "results")

# ── ISM Manufacturing PMI, monthly (period = the month the value describes) ──────
# 2022-04 .. 2026-06: exact, YCharts (ycharts.com/indicators/us_pmi) + TradingEconomics.
# 2020-01 .. 2022-03: published ISM Manufacturing PMI composite prints (widely reported).
ISM = {
    "2020-01": 50.9, "2020-02": 50.1, "2020-03": 49.1, "2020-04": 41.5, "2020-05": 43.1,
    "2020-06": 52.6, "2020-07": 54.2, "2020-08": 56.0, "2020-09": 55.4, "2020-10": 59.3,
    "2020-11": 57.5, "2020-12": 60.7,
    "2021-01": 58.7, "2021-02": 60.8, "2021-03": 64.7, "2021-04": 60.7, "2021-05": 61.2,
    "2021-06": 60.6, "2021-07": 59.5, "2021-08": 59.9, "2021-09": 61.1, "2021-10": 60.8,
    "2021-11": 61.1, "2021-12": 58.7,
    "2022-01": 57.6, "2022-02": 58.6, "2022-03": 57.1, "2022-04": 55.4, "2022-05": 56.1,
    "2022-06": 53.0, "2022-07": 52.8, "2022-08": 52.8, "2022-09": 50.9, "2022-10": 50.2,
    "2022-11": 49.0, "2022-12": 48.4,
    "2023-01": 47.4, "2023-02": 47.7, "2023-03": 46.3, "2023-04": 47.1, "2023-05": 46.9,
    "2023-06": 46.0, "2023-07": 46.4, "2023-08": 47.6, "2023-09": 49.0, "2023-10": 46.7,
    "2023-11": 46.7, "2023-12": 47.1,
    "2024-01": 49.1, "2024-02": 47.8, "2024-03": 50.3, "2024-04": 49.2, "2024-05": 48.7,
    "2024-06": 48.5, "2024-07": 46.8, "2024-08": 47.2, "2024-09": 47.2, "2024-10": 46.5,
    "2024-11": 48.4, "2024-12": 49.2,
    "2025-01": 50.9, "2025-02": 50.3, "2025-03": 49.0, "2025-04": 48.7, "2025-05": 48.5,
    "2025-06": 49.0, "2025-07": 48.0, "2025-08": 48.7, "2025-09": 49.1, "2025-10": 48.7,
    "2025-11": 48.2, "2025-12": 47.9,
    "2026-01": 52.6, "2026-02": 52.4, "2026-03": 52.7, "2026-04": 52.7, "2026-05": 54.0,
    "2026-06": 54.0,
}

# BTC-equity sleeve = miners + MSTR. Each enters only once it IS a BTC vehicle
# (predecessor-company history on the same ticker is excluded). COIN is a crypto
# *exchange*, not a miner/MSTR, so it is NOT in this sleeve.
SLEEVE_STARTS = {
    "MSTR": "2020-08-11",   # first BTC treasury purchase announced
    "MARA": "2021-01-01",   # Marathon Patent -> Marathon Digital (BTC miner) rebrand
    "HUT":  "2018-03-08",   # Hut 8 — BTC miner since listing
    "CIFR": "2021-08-01",   # Cipher Mining SPAC completion (data is SPAC shell before)
    "IREN": "2021-11-17",   # Iris Energy / IREN IPO (data start)
    "WULF": "2021-12-01",   # TeraWulf listing (data before is IKONICS predecessor)
}

def me(s):  # month-end series
    return s.resample("ME").last()

def spearman(x, y):
    rx = pd.Series(x).rank().to_numpy(); ry = pd.Series(y).rank().to_numpy()
    return float(np.corrcoef(rx, ry)[0, 1])

def pearson(x, y):
    return float(np.corrcoef(np.asarray(x, float), np.asarray(y, float))[0, 1])

def tstat(r, n):
    return float(r * np.sqrt((n - 2) / (1 - r * r))) if (n > 2 and abs(r) < 1) else float("nan")

def clip(v, lo, hi):
    return max(lo, min(hi, v))

# ── load month-end prices and monthly returns ────────────────────────────────────
btc_me = me(bt.load_close("BTC_USD"))
btc_ret = btc_me.pct_change()

sleeve_me, sleeve_ret = {}, {}
for name, start in SLEEVE_STARTS.items():
    s = bt.load_close(name)
    s = s[s.index >= pd.Timestamp(start)]
    m = me(s)
    sleeve_me[name] = m
    sleeve_ret[name] = m.pct_change()

# common monthly grid
grid = pd.period_range("2020-08", "2026-06", freq="M").to_timestamp("M").normalize()

def basket_ret_at(ts):
    """Equal-weight mean monthly return across sleeve names available this month."""
    rs = []
    for name in SLEEVE_STARTS:
        r = sleeve_ret[name]
        idx = r.index[(r.index.year == ts.year) & (r.index.month == ts.month)]
        if len(idx) and not np.isnan(r.loc[idx[-1]]):
            rs.append(float(r.loc[idx[-1]]))
    return float(np.mean(rs)) if rs else np.nan, len(rs)

def btc_ret_at(ts):
    idx = btc_ret.index[(btc_ret.index.year == ts.year) & (btc_ret.index.month == ts.month)]
    return float(btc_ret.loc[idx[-1]]) if len(idx) and not np.isnan(btc_ret.loc[idx[-1]]) else np.nan

def ism_key(ts):           # ISM published just before month ts = ISM of previous month
    p = (ts.to_period("M") - 1)
    return ISM.get(str(p))

rows = []
for ts in grid:
    br, n = basket_ret_at(ts)
    bt_r = btc_ret_at(ts)
    ism_prev = ism_key(ts)
    if np.isnan(br) or np.isnan(bt_r) or ism_prev is None:
        continue
    rows.append({"month": ts.strftime("%Y-%m"), "n_names": n, "ism_prev": ism_prev,
                 "basket_ret": br, "btc_ret": bt_r, "spread": br - bt_r})
df = pd.DataFrame(rows)
df["d_ism"] = df["ism_prev"].diff()

# ── PART A: signal / IC ──────────────────────────────────────────────────────────
def ic_block(sub, label):
    n = len(sub)
    out = {"label": label, "n": n}
    if n >= 6:
        for sig in ["ism_prev", "d_ism"]:
            s = sub.dropna(subset=[sig])
            sp = spearman(s[sig], s["spread"]); pe = pearson(s[sig], s["spread"])
            out[sig] = {"spearman_IC": round(sp, 3), "pearson": round(pe, 3),
                        "t_stat": round(tstat(sp, len(s)), 2), "n": len(s)}
        out["beta_to_btc"] = round(float(np.cov(sub["basket_ret"], sub["btc_ret"])[0, 1]
                                         / np.var(sub["btc_ret"])), 2)
        out["corr_to_btc"] = round(pearson(sub["basket_ret"], sub["btc_ret"]), 2)
        out["mean_spread_pct"] = round(float(sub["spread"].mean()) * 100, 2)
        # regime conditional mean spread (using signal known at month start)
        hi = sub[sub["ism_prev"] >= 55]; lo = sub[sub["ism_prev"] < 50]
        out["spread_high_ism_pct"] = round(float(hi["spread"].mean()) * 100, 2) if len(hi) else None
        out["spread_low_ism_pct"]  = round(float(lo["spread"].mean()) * 100, 2) if len(lo) else None
        out["n_high_ism"] = len(hi); out["n_low_ism"] = len(lo)
    return out

ic_full = ic_block(df[df["month"] >= "2021-12"].copy(), "2021-12+ (IREN/WULF live)")
ic_long = ic_block(df.copy(), "2020-09+ (MSTR/MARA/HUT, CIFR from 21-08)")

# ── PART B: DCA sleeve backtest ───────────────────────────────────────────────────
# basket total-return index (equal-weight, monthly rebalanced) and BTC price index.
def build_indices(start_month):
    sub = df[df["month"] >= start_month].reset_index(drop=True)
    btc_idx = [100.0]; bas_idx = [100.0]
    for _, r in sub.iterrows():
        btc_idx.append(btc_idx[-1] * (1 + r["btc_ret"]))
        bas_idx.append(bas_idx[-1] * (1 + r["basket_ret"]))
    return sub, np.array(btc_idx[1:]), np.array(bas_idx[1:])

def frac_static(v):     return lambda ism: v
def frac_ism_pro(ism, lo_i=48, hi_i=59, lo_f=0.10, hi_f=0.25):
    return clip(lo_f + (ism - lo_i) / (hi_i - lo_i) * (hi_f - lo_f), lo_f, hi_f)
def frac_ism_contra(ism, lo_i=48, hi_i=59, lo_f=0.10, hi_f=0.25):
    return clip(hi_f - (ism - lo_i) / (hi_i - lo_i) * (hi_f - lo_f), lo_f, hi_f)

SCHEMES = {
    "btc_only": frac_static(0.0), "eq_10": frac_static(0.10), "eq_17": frac_static(0.17),
    "eq_25": frac_static(0.25), "eq_40": frac_static(0.40),
    "ism_pro": frac_ism_pro, "ism_contra": frac_ism_contra,
}
CONTRIB = 100.0

def run_window(start_month, label):
    sub, btc_idx, bas_idx = build_indices(start_month)
    n = len(sub)
    res = {}
    for name, ffn in SCHEMES.items():
        u_btc = u_bas = 0.0; eq = []
        for i in range(n):
            f = ffn(sub.loc[i, "ism_prev"])
            u_btc += CONTRIB * (1 - f) / btc_idx[i]
            u_bas += CONTRIB * f / bas_idx[i]
            eq.append(u_btc * btc_idx[i] + u_bas * bas_idx[i])
        final = eq[-1]; invested = CONTRIB * n
        roi = final / invested - 1
        irr_m = bt.monthly_irr([-CONTRIB] * n + [final])
        irr = ((1 + irr_m) ** 12 - 1) if irr_m is not None else None
        mdd = bt.max_drawdown(np.array(eq))
        res[name] = {"final": round(final), "roi_pct": round(roi * 100, 1),
                     "irr_pct": round(irr * 100, 1) if irr is not None else None,
                     "maxdd_pct": round(mdd * 100, 1),
                     "ret_per_dd": round(roi / abs(mdd), 2) if mdd < 0 else None}
    return {"label": label, "start": start_month, "months": n, "schemes": res}

windows = [run_window("2021-12", "2021-12+ (IREN/WULF live)"),
           run_window("2020-09", "2020-09+ (longer, fewer names)")]

# ── report ────────────────────────────────────────────────────────────────────────
print("=" * 90)
print("PART A — SIGNAL: does ISM predict the (miners+MSTR  -  BTC) monthly return spread?")
print("=" * 90)
for ic in (ic_full, ic_long):
    print(f"\n[{ic['label']}]  n={ic['n']} months")
    if "ism_prev" in ic:
        print(f"  sleeve beta to BTC = {ic['beta_to_btc']}   corr = {ic['corr_to_btc']}   "
              f"mean monthly spread = {ic['mean_spread_pct']}%")
        print(f"  IC(ISM level -> next-month spread): Spearman {ic['ism_prev']['spearman_IC']:+.3f} "
              f"(t={ic['ism_prev']['t_stat']:+.2f}, n={ic['ism_prev']['n']})   Pearson {ic['ism_prev']['pearson']:+.3f}")
        print(f"  IC(dISM        -> next-month spread): Spearman {ic['d_ism']['spearman_IC']:+.3f} "
              f"(t={ic['d_ism']['t_stat']:+.2f}, n={ic['d_ism']['n']})   Pearson {ic['d_ism']['pearson']:+.3f}")
        print(f"  mean spread when ISM>=55 (high): {ic['spread_high_ism_pct']}%  (n={ic['n_high_ism']})   "
              f"vs ISM<50 (low): {ic['spread_low_ism_pct']}%  (n={ic['n_low_ism']})")
        print(f"    -> pro-cyclical thesis needs HIGH-ISM spread > LOW-ISM spread")

print("\n" + "=" * 90)
print("PART B — DCA sleeve backtest: $100/mo into BTC-exposure, split BTC vs miners+MSTR")
print("=" * 90)
for w in windows:
    print(f"\n[{w['label']}]  start {w['start']}  ({w['months']} months, ${w['months']*CONTRIB:,.0f} invested)")
    print(f"  {'scheme':<12}{'final$':>10}{'ROI%':>8}{'IRR/yr%':>9}{'maxDD%':>9}{'ROI/|DD|':>10}")
    for name, o in w["schemes"].items():
        irr = o["irr_pct"] if o["irr_pct"] is not None else float("nan")
        rpd = o["ret_per_dd"] if o["ret_per_dd"] is not None else float("nan")
        tag = "  <- pro-cyclical (proposed)" if name == "ism_pro" else (
              "  <- contrarian" if name == "ism_contra" else "")
        print(f"  {name:<12}{o['final']:>10,.0f}{o['roi_pct']:>8.1f}{irr:>9.1f}"
              f"{o['maxdd_pct']:>9.1f}{rpd:>10.2f}{tag}")

out = {"signal": [ic_full, ic_long], "dca": windows,
       "notes": "USD; ISM_prev = no-lookahead signal; sleeve=miners+MSTR equal-weight"}
with open(os.path.join(RESULTS_DIR, "btc_equity_ism_summary.json"), "w") as f:
    json.dump(out, f, indent=2)
print("\nWritten: results/btc_equity_ism_summary.json")
print(f"Monthly observations used: {len(df)}  ({df['month'].iloc[0]} .. {df['month'].iloc[-1]})")
