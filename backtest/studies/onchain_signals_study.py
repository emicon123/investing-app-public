#!/usr/bin/env python3
"""
On-chain signal study #2 — SOPR, LTH/STH-SOPR, MVRV Z-Score, NUPL, LTH/STH supply vs
BTC forward returns. Follow-on to file 08 (which tested raw MVRV percentile and was
rejected as a standalone trigger). Same no-lookahead Spearman-IC methodology as the
macro lead-lag study (03) and file 08.

Data: bitcoin-data.com free, unauthenticated REST API (mirrors Glassnode-style
indicators). Daily since ~2022-07 for SOPR/LTH-SOPR/STH-SOPR/MVRV-Z/NUPL/LTH-STH
supply -> data/onchain/BTC_{SOPR,LTH_SOPR,STH_SOPR,MVRV_ZSCORE,NUPL,LTH_SUPPLY_BTC,
STH_SUPPLY_BTC}.csv. Cross-checked against our own longer-history (2010+) derived
NUPL/MVRV-Z from free Coinmetrics MVRV+MarketCap (file 08's data pipeline) ->
data/onchain/BTC_NUPL_MVRVZ_derived.csv.

Signals tested per the user's list: SOPR (raw level + <1 indicator, the "important
one"), LTH-SOPR, STH-SOPR, MVRV Z-Score (native 2022+ and derived 2010+), NUPL (native
and derived), LTH/STH supply (level trend, 3-month % change). Options OI by strike and
URPD are explicitly out of scope (no free historical time series / no free source).
"""
import os
import numpy as np
import pandas as pd

BASE = os.path.join(os.path.dirname(__file__), "..")
PRICES = os.path.join(BASE, "data", "prices")
ONCHAIN = os.path.join(BASE, "data", "onchain")
MACRO = os.path.join(BASE, "data", "macro")
HORIZONS = [1, 2, 3, 6, 12]


def spearman(x, y):
    x, y = np.asarray(x, float), np.asarray(y, float)
    m = ~(np.isnan(x) | np.isnan(y))
    x, y = x[m], y[m]
    if len(x) < 8:
        return float("nan"), len(x)
    rx, ry = pd.Series(x).rank().to_numpy(), pd.Series(y).rank().to_numpy()
    return float(np.corrcoef(rx, ry)[0, 1]), len(x)


def tstat(r, n):
    return float(r * np.sqrt((n - 2) / (1 - r * r))) if (n > 2 and abs(r) < 1) else float("nan")


def me_close(t):
    s = pd.read_csv(os.path.join(PRICES, f"{t}_full.csv"), parse_dates=["date"]).set_index("date")["close"]
    m = s.dropna().resample("ME").last()
    m.index = m.index.to_period("M")
    return m


def onchain_monthly(fname, col, agg="last"):
    s = pd.read_csv(os.path.join(ONCHAIN, fname), parse_dates=["date"]).set_index("date")[col].astype(float)
    m = s.resample("ME").last() if agg == "last" else s.resample("ME").mean()
    m.index = m.index.to_period("M")
    return m


def fwd_ret(s, h):
    return s.shift(-h) / s - 1


def macro_m(f, c, weekly=False):
    s = pd.read_csv(os.path.join(MACRO, f), parse_dates=["date"]).set_index("date")[c].astype(float)
    if weekly:
        s = s.resample("ME").last()
    s.index = s.index.to_period("M")
    return s.groupby(level=0).last()


nl_acc = (macro_m("FED_NET_LIQUIDITY.csv", "net_liquidity_bn", weekly=True).pct_change(12).diff(3)).shift(1)
gm_acc = (macro_m("GLOBAL_M2.csv", "global_m2_bn").pct_change(12).diff(3)).shift(2)
MACRO_RISK_OFF = ((nl_acc < 0) & (gm_acc < 0)).astype(float)

px = me_close("BTC_USD")

# ── build the candidate signal set ────────────────────────────────────────────────
signals = {}
signals["SOPR (level)"] = onchain_monthly("BTC_SOPR.csv", "sopr")
signals["SOPR<1 (indicator)"] = (onchain_monthly("BTC_SOPR.csv", "sopr") < 1.0).astype(float)
signals["LTH-SOPR (level)"] = onchain_monthly("BTC_LTH_SOPR.csv", "lth_sopr")
signals["STH-SOPR (level)"] = onchain_monthly("BTC_STH_SOPR.csv", "sth_sopr")
signals["STH-SOPR<1 (indicator)"] = (onchain_monthly("BTC_STH_SOPR.csv", "sth_sopr") < 1.0).astype(float)
signals["MVRV Z-Score (native)"] = onchain_monthly("BTC_MVRV_ZSCORE.csv", "mvrv_zscore")
signals["NUPL (native)"] = onchain_monthly("BTC_NUPL.csv", "nupl")

derived = pd.read_csv(os.path.join(ONCHAIN, "BTC_NUPL_MVRVZ_derived.csv"), parse_dates=["date"]).set_index("date")
derived_m = derived.resample("ME").last()
derived_m.index = derived_m.index.to_period("M")
signals["MVRV Z-Score (derived, 2010+)"] = derived_m["mvrv_zscore"]
signals["NUPL (derived, 2010+)"] = derived_m["nupl"]

lth_sply = onchain_monthly("BTC_LTH_SUPPLY_BTC.csv", "lth_supply_btc")
sth_sply = onchain_monthly("BTC_STH_SUPPLY_BTC.csv", "sth_supply_btc")
signals["LTH supply 3mo %chg"] = lth_sply.pct_change(3)
signals["STH supply 3mo %chg"] = sth_sply.pct_change(3)
signals["LTH/STH supply ratio"] = lth_sply / sth_sply

out = []
def emit(s=""):
    out.append(s); print(s)


emit("# On-chain signal study #2 — SOPR / LTH-STH-SOPR / MVRV-Z / NUPL / LTH-STH supply\n")
emit("Data: bitcoin-data.com (free, no auth), daily since ~2022-07 for native series; "
     "derived NUPL/MVRV-Z (Coinmetrics-sourced, 2010+) for a longer-history cross-check. "
     "Spearman IC vs BTC forward return, no-lookahead (signal known at month t, target = "
     "return over t..t+h). Cells: IC (n, t-stat); `o` = |t|>=1.96.\n")

emit("| signal | " + " | ".join(f"fwd {h}m" for h in HORIZONS) + " |")
emit("|---|" + "--:|" * len(HORIZONS))
ics = {}
for name, sig in signals.items():
    cells = []
    for h in HORIZONS:
        common = sig.index.intersection(px.index)
        s_c, px_c = sig.reindex(common), px.reindex(common)
        fr = fwd_ret(px_c, h)
        r, n = spearman(s_c.to_numpy(), fr.to_numpy())
        ics.setdefault(name, {})[h] = r
        t = tstat(r, n)
        flag = " o" if not np.isnan(r) and abs(t) >= 1.96 else ""
        cells.append(f"{r:+.2f} (n={n},t={t:.1f}){flag}")
    emit(f"| {name} | " + " | ".join(cells) + " |")

emit("\nNegative IC = high signal -> lower forward return (the desired direction for a "
     "top/de-risk trigger). Positive IC = signal moves WITH the trend (momentum/coincident, "
     "not a reversal signal).\n")

emit("## Threshold/bucket check on the two signals the user flagged as important\n")
for name, sig, thresholds in [
    ("SOPR", signals["SOPR (level)"], [0.95, 0.98, 1.0, 1.02, 1.05]),
    ("STH-SOPR", signals["STH-SOPR (level)"], [0.95, 0.98, 1.0, 1.02, 1.05]),
]:
    common = sig.index.intersection(px.index)
    s_c, px_c = sig.reindex(common), px.reindex(common)
    fr3 = fwd_ret(px_c, 3)
    emit(f"### {name} (fwd 3m)")
    emit("| threshold | months <thr | mean fwd (below) | months >=thr | mean fwd (above) |")
    emit("|---|--:|--:|--:|--:|")
    for thr in thresholds:
        below = fr3[s_c < thr].dropna()
        above = fr3[s_c >= thr].dropna()
        emit(f"| {thr} | {len(below)} | {below.mean()*100 if len(below) else float('nan'):+.1f}% "
             f"| {len(above)} | {above.mean()*100 if len(above) else float('nan'):+.1f}% |")
    emit("")

emit("## Cross-check: native (2022+) vs derived (2010+) MVRV-Z and NUPL\n")
common22 = signals["MVRV Z-Score (native)"].dropna().index
emit(f"Overlap window: {common22.min()} .. {common22.max()} ({len(common22)} months)")
mz_native = signals["MVRV Z-Score (native)"].reindex(common22)
mz_derived = signals["MVRV Z-Score (derived, 2010+)"].reindex(common22)
corr, n = spearman(mz_native.to_numpy(), mz_derived.to_numpy())
emit(f"MVRV Z-Score native vs derived, Spearman corr = {corr:.3f} (n={n}) -- should be "
     f"near 1.0 if the derivation is sound (both track the same underlying MVRV/MarketCap).")
nu_native = signals["NUPL (native)"].reindex(common22)
nu_derived = signals["NUPL (derived, 2010+)"].reindex(common22)
corr2, n2 = spearman(nu_native.to_numpy(), nu_derived.to_numpy())
emit(f"NUPL native vs derived, Spearman corr = {corr2:.3f} (n={n2}).\n")

emit("## Comparison to the shipped macro risk-off signal (same window, 2022+)\n")
common_ro = MACRO_RISK_OFF.dropna().index.intersection(px.index)
ro_c, px_ro = MACRO_RISK_OFF.reindex(common_ro), px.reindex(common_ro)
cells = []
for h in HORIZONS:
    fr = fwd_ret(px_ro, h)
    r, n = spearman(ro_c.to_numpy(), fr.to_numpy())
    t = tstat(r, n)
    flag = " o" if not np.isnan(r) and abs(t) >= 1.96 else ""
    cells.append(f"{r:+.2f} (n={n},t={t:.1f}){flag}")
emit("| macro risk-off (0/1) | " + " | ".join(cells) + " |")

with open(os.path.join(BASE, "results", "onchain_signals_study.md"), "w") as fh:
    fh.write("\n".join(out) + "\n")
print("\nWrote results/onchain_signals_study.md")
