#!/usr/bin/env python3
"""
On-chain MVRV top-timing study — does BTC/ETH MVRV (market-cap / realized-cap) sharpen
the "start selling" trigger beyond the macro de-risk gate already shipped (net-liq accel
& Global-M2 accel, see study_summary/07)?

Data: Coinmetrics community API `CapMVRVCur` (free tier), daily since each asset's
approx inception (BTC 2010-07-18, ETH 2015-08-08) -> data/onchain/{BTC,ETH}_MVRV.csv
(fetched via inline urllib call, community tier — no paid RealizedCap component needed
since MVRV is served pre-computed).

Method: no-lookahead EXPANDING percentile rank of monthly MVRV (min 12 months history).
Raw MVRV thresholds decay across cycles (2013 top ~5.9, 2017 ~4.7, 2021 ~4.0 — BTC market
matures, realized cost basis rises faster than fresh euphoria can re-inflate MVRV), so an
absolute cutoff isn't stable across a 15y study; percentile-of-own-history-to-date is.
Burn-in: BTC eval starts 2011-07 (drop the noisy first ~12mo post-inception where realized
cap is tiny and MVRV swings 10-146x on nothing), ETH starts once 12mo of its own history
exist (~2016-08).

PART A: Spearman IC of monthly MVRV-percentile(t) vs FORWARD return(t, t+h) for
h=1,2,3,6,12 months, BTC and ETH — is it actually predictive of subsequent drawdowns?
PART B: same IC for the existing macro net-liq-accel / Global-M2-accel signals, computed
over the IDENTICAL sample window as each asset's on-chain series, for a fair side-by-side.
PART C: composite trigger (macro risk-off OR mvrv extreme, e.g. pctl>=0.85) vs each alone
— bucketed mean/median forward return in triggered vs not-triggered months.
"""
import os
import numpy as np
import pandas as pd

BASE = os.path.join(os.path.dirname(__file__), "..")
PRICES = os.path.join(BASE, "data", "prices")
MACRO = os.path.join(BASE, "data", "macro")
ONCHAIN = os.path.join(BASE, "data", "onchain")
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
    full = os.path.join(PRICES, f"{t}_full.csv")
    s = pd.read_csv(full, parse_dates=["date"]).set_index("date")["close"]
    m = s.dropna().resample("ME").last()
    m.index = m.index.to_period("M")
    return m


def macro_m(f, c, weekly=False):
    s = pd.read_csv(os.path.join(MACRO, f), parse_dates=["date"]).set_index("date")[c].astype(float)
    if weekly:
        s = s.resample("ME").last()
    s.index = s.index.to_period("M")
    return s.groupby(level=0).last()


# same formulas/lags as portfolio_strategy_backtest.py's shipped macro de-risk gate
nl_acc = (macro_m("FED_NET_LIQUIDITY.csv", "net_liquidity_bn", weekly=True).pct_change(12).diff(3)).shift(1)
gm_acc = (macro_m("GLOBAL_M2.csv", "global_m2_bn").pct_change(12).diff(3)).shift(2)
MACRO_RISK_OFF = ((nl_acc < 0) & (gm_acc < 0))


def load_mvrv(asset):
    s = pd.read_csv(os.path.join(ONCHAIN, f"{asset}_MVRV.csv"), parse_dates=["date"]).set_index("date")["mvrv"]
    m = s.resample("ME").last()
    m.index = m.index.to_period("M")
    return m


def expanding_pct_rank(s, min_periods=12):
    vals = s.to_numpy()
    out = np.full(len(vals), np.nan)
    for i in range(len(vals)):
        if i + 1 < min_periods:
            continue
        window = vals[: i + 1]
        out[i] = (window <= vals[i]).mean()
    return pd.Series(out, index=s.index)


def fwd_ret(s, h):
    return s.shift(-h) / s - 1


out = []
def emit(s=""):
    out.append(s); print(s)


emit("# On-chain MVRV study — top-timing signal vs the shipped macro de-risk gate\n")
emit("MVRV percentile = no-lookahead expanding rank of monthly MVRV vs its own history to "
     "date (min 12mo burn-in). Cells = Spearman IC (n obs), t-stat in parens.\n")

results = {}
for asset, burn_start in [("BTC", "2011-07"), ("ETH", "2016-08")]:
    px = me_close(f"{asset}_USD")
    mvrv = load_mvrv(asset)
    pctl = expanding_pct_rank(mvrv, min_periods=12)
    pctl = pctl[pctl.index >= pd.Period(burn_start, "M")]

    common = px.index.intersection(pctl.index).intersection(nl_acc.index).intersection(gm_acc.index)
    common = common.sort_values()
    px_c, pctl_c = px.reindex(common), pctl.reindex(common)
    nla_c, gma_c = nl_acc.reindex(common), gm_acc.reindex(common)
    ro_c = MACRO_RISK_OFF.reindex(common).astype(float)

    emit(f"## {asset} — sample {common.min()} .. {common.max()} ({len(common)} months)\n")
    emit("| signal | " + " | ".join(f"fwd {h}m" for h in HORIZONS) + " |")
    emit("|---|" + "--:|" * len(HORIZONS))

    row_sig = {"MVRV percentile": pctl_c, "net-liq accel": nla_c, "Global-M2 accel": gma_c,
               "macro risk-off (0/1)": ro_c}
    ics = {name: {} for name in row_sig}
    for name, sig in row_sig.items():
        cells = []
        for h in HORIZONS:
            fr = fwd_ret(px_c, h)
            r, n = spearman(sig.to_numpy(), fr.to_numpy())
            ics[name][h] = r
            t = tstat(r, n)
            flag = " o" if not np.isnan(r) and abs(t) >= 1.96 else ""
            cells.append(f"{r:+.2f} (n={n},t={t:.1f}){flag}")
        emit(f"| {name} | " + " | ".join(cells) + " |")
    emit("")
    results[asset] = dict(px=px_c, pctl=pctl_c, ro=ro_c, common=common)

emit("`o` = |t| >= 1.96 (~95% two-sided). Negative IC = signal high -> forward return low "
     "(the desired direction for a top/de-risk trigger).\n")

emit("## Bucketed forward returns — triggered vs not (3-month forward)\n")
for asset in results:
    px_c, pctl_c, ro_c = results[asset]["px"], results[asset]["pctl"], results[asset]["ro"]
    fr3 = fwd_ret(px_c, 3)
    mvrv_trig = (pctl_c >= 0.85).astype(float)
    macro_trig = ro_c
    composite = ((mvrv_trig == 1) | (macro_trig == 1)).astype(float)
    both = ((mvrv_trig == 1) & (macro_trig == 1)).astype(float)

    emit(f"### {asset}")
    emit("| trigger | months on | mean fwd 3m | median fwd 3m | months off | mean fwd 3m (off) |")
    emit("|---|--:|--:|--:|--:|--:|")
    for label, trig in [("MVRV pctl>=0.85", mvrv_trig), ("macro risk-off", macro_trig),
                         ("composite (OR)", composite), ("both (AND)", both)]:
        on = fr3[trig == 1].dropna(); off = fr3[trig == 0].dropna()
        emit(f"| {label} | {len(on)} | {on.mean()*100:+.1f}% | {on.median()*100:+.1f}% "
             f"| {len(off)} | {off.mean()*100:+.1f}% |")
    emit("")

emit("## Read")
emit("MVRV-percentile IC vs macro-signal IC head-to-head over the identical window per "
     "asset; composite bucket table shows whether OR-ing on-chain into the macro trigger "
     "changes the forward-return profile of triggered months (more/less negative than "
     "macro alone), i.e. whether on-chain adds anything beyond what net-liq/Global-M2 "
     "already catch.")

with open(os.path.join(BASE, "results", "onchain_mvrv_study.md"), "w") as fh:
    fh.write("\n".join(out) + "\n")
print("\nWrote results/onchain_mvrv_study.md")
