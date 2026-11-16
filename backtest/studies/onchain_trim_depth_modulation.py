#!/usr/bin/env python3
"""
Study #12 — on-chain-modulated TRIM DEPTH for the shipped +100% rule.

Context (follow-on to study #11 / file 10): using on-chain euphoria (SOPR/NUPL) as
an INDEPENDENT sell/de-risk trigger, alone or OR/AND-fused with the macro gate, was
already tested (file 10's `onchain_portfolio_backtest.py`) and lost outright to the
macro-only gate — largely because on-chain-euphoria and macro-risk-off are mostly
independent axes (2/48 co-occurring months there). This script tests a different,
narrower idea instead: don't use on-chain data as a standalone trigger. Use it only
to modulate the DEPTH of the trim the shipped, purely price-based +100% rule ALREADY
fires (AGENTS.md: asset >= 2x its target -> trim to the midpoint between target and
actual, e.g. 10%->20% trims to 15%, proceeds -> GOLD). This sidesteps the
coincident-vs-leading problem: it's not trying to call a top in advance, it's
reacting to an already-realized overshoot and asking "given we're trimming anyway,
should we trim harder (or softer) this time".

Method:
  1. Reuse the exact multi-asset portfolio simulation from `portfolio_strategy_backtest.py`
     (same TARGET dict, same RET construction, same +100% trigger condition), with the
     macro dynamic-target cut DISABLED (cut=1.0) so the only sell mechanism active is
     the pure price-driven +100% rule — isolates the trim-depth question from the
     already-tested macro overlay.
  2. Primary euphoria signal: derived NUPL (Coinmetrics-sourced, BTC 2010-07+, ETH
     2015-08+, same pipeline as file 08/10) -> no-lookahead EXPANDING percentile rank
     vs its own history to date (12mo burn-in, same convention as onchain_mvrv_study.py).
     "Euphoric" = percentile >= 0.75 at the trigger month. Only BTC_spot and ETH
     positions are conditioned this way (they're the only sleeve members with a
     free on-chain read); every other asset (miners, SOL, COIN, SUI, DOGE, MSTR, ...)
     always uses the standard fixed trim.
  3. Secondary/corroborating check: native NUPL (bitcoin-data.com, 2022-07+, short
     window) cross-checked against the derived-percentile euphoria flag in the overlap.
  4. Enumerate EVERY historical +100% trigger-fire event for BTC_spot/ETH since
     inception (one long baseline run from 2010-07), tagging each with its concurrent
     euphoria state — the actual n we have to work with.
  5. Portfolio-level test across trim-depth "modes" (baseline fixed 1.5x; deeper-only
     on euphoric fires; shallower/skip-only on NON-euphoric fires; both) over the
     standard 1/3/5/8/13y start dates (AGENTS.md), XIRR/maxDD.
  6. Proxy/robustness check (per the user's fallback request): conditional on being
     IN 2x-trigger territory generally (not just the discrete fire event), does
     forward BTC drawdown differ between high-NUPL and normal-NUPL months?
"""
import os
import numpy as np
import pandas as pd

BASE = os.path.join(os.path.dirname(__file__), "..")
PRICES = os.path.join(BASE, "data", "prices")
ONCHAIN = os.path.join(BASE, "data", "onchain")
MACRO = os.path.join(BASE, "data", "macro")
CONTRIB = 1000.0
COST = 15 / 1e4
TRIGGER = 2.0
MINERS = ["MARA", "HUT", "CIFR", "IREN", "WULF"]
TARGET = {"GLD": 30, "CASH": 10, "QQQ": 5, "FXI": 2.5, "MSTR": 2.5,
          "BTC_spot": 17.5, "miners": 7.5, "ETH": 13, "SOL": 5, "COIN": 4, "SUI": 2, "DOGE": 1}
CRYPTO = ["BTC_spot", "miners", "ETH", "SOL", "COIN", "SUI", "DOGE"]
TICKER = {"BTC_spot": "BTC_USD", "ETH": "ETH_USD", "SOL": "SOL_USD", "DOGE": "DOGE_USD",
          "SUI": "SUI20947_USD", "COIN": "COIN", "GLD": "GLD", "QQQ": "QQQ", "FXI": "FXI", "MSTR": "MSTR"}
ONCHAIN_ASSETS = ("BTC_spot", "ETH")   # the only sleeve members with a free on-chain reading


def usd_close(t):
    full = os.path.join(PRICES, f"{t}_full.csv")
    if os.path.exists(full):
        s = pd.read_csv(full, parse_dates=["date"]).set_index("date")["close"]
    else:
        df = pd.read_csv(os.path.join(PRICES, f"{t}.csv"), skiprows=[1, 2])
        df = df.rename(columns={df.columns[0]: "date"}); df["date"] = pd.to_datetime(df["date"], errors="coerce")
        df = df.dropna(subset=["date"]).set_index("date"); c = "Close" if "Close" in df.columns else df.columns[-1]
        s = pd.to_numeric(df[c], errors="coerce")
    m = s.dropna().resample("ME").last(); m.index = m.index.to_period("M"); return m


usdpln = usd_close("USDPLN_X")
def pln_ret(t):
    return (usd_close(t) * usdpln).dropna().pct_change()


RET = {"CASH": None}
for a in TARGET:
    if a != "CASH":
        RET[a] = pd.DataFrame({m: pln_ret(m) for m in MINERS}).mean(axis=1) if a == "miners" else pln_ret(TICKER[a])

END = usdpln.index.max()
STARTS = {"13y": 13, "8y": 8, "5y": 5, "3y": 3, "1y": 1}
starts = {k: END - v * 12 for k, v in STARTS.items()}

# ── on-chain euphoria signal (no-lookahead expanding percentile, 12mo burn-in) ────
def expanding_pct_rank(s, min_periods=12):
    vals = s.to_numpy()
    out = np.full(len(vals), np.nan)
    for i in range(len(vals)):
        if i + 1 < min_periods:
            continue
        window = vals[: i + 1]
        out[i] = (window <= vals[i]).mean()
    return pd.Series(out, index=s.index)


def load_derived_nupl(asset):
    f = os.path.join(ONCHAIN, f"{asset}_NUPL_MVRVZ_derived.csv")
    s = pd.read_csv(f, parse_dates=["date"]).set_index("date")["nupl"].astype(float)
    m = s.resample("ME").last(); m.index = m.index.to_period("M")
    return m


EUPHORIA_THRESH = 0.75
burn = {"BTC_spot": ("BTC", "2011-07"), "ETH": ("ETH", "2016-08")}
NUPL_PCTL = {}
NUPL_LEVEL = {}
for sleeve, (asset, burn_start) in burn.items():
    nupl = load_derived_nupl(asset)
    pctl = expanding_pct_rank(nupl, min_periods=12)
    pctl = pctl[pctl.index >= pd.Period(burn_start, "M")]
    NUPL_PCTL[sleeve] = pctl
    NUPL_LEVEL[sleeve] = nupl

EUPHORIC = {sleeve: (pctl >= EUPHORIA_THRESH) for sleeve, pctl in NUPL_PCTL.items()}


def is_euphoric(sleeve, month):
    e = EUPHORIC.get(sleeve)
    if e is None or month not in e.index:
        return None   # no on-chain reading available at this month (pre burn-in / n/a)
    val = e.get(month)
    return bool(val) if not pd.isna(val) else None


def trim_to_for(asset, month, mode):
    """Return the trim_to multiple (of current target) for this asset/month/mode,
    or None to mean 'skip the trim entirely this event'."""
    DEFAULT = 1.5
    if asset not in ONCHAIN_ASSETS or mode == "baseline":
        return DEFAULT
    euphoric = is_euphoric(asset, month)
    if euphoric is None:
        return DEFAULT
    if mode == "deeper_1.0":
        return 1.0 if euphoric else DEFAULT
    if mode == "deeper_0.75":
        return 0.75 if euphoric else DEFAULT
    if mode == "shallower_1.75":
        return DEFAULT if euphoric else 1.75
    if mode == "skip_normal":
        return DEFAULT if euphoric else None
    if mode == "both_1.0_1.75":
        return 1.0 if euphoric else 1.75
    raise ValueError(mode)


def sim(start, mode, log_events=None):
    months = pd.period_range(start, END, freq="M"); n = len(months)
    hold = {a: 0.0 for a in TARGET}; nav = 1.0; navs = []
    ctw = {a: TARGET[a] / 100.0 for a in TARGET}
    for t in months:
        present = [a for a in TARGET if a == "CASH" or (RET[a] is not None and t in RET[a].index and not np.isnan(RET[a].get(t, np.nan)))]
        w = pd.Series({a: TARGET[a] for a in present}); w = w / w.sum()
        Vstart = sum(hold.values()) + CONTRIB
        for a in present:
            hold[a] += CONTRIB * w[a]                      # buy-only, proportional to target
        sold = 0.0
        Vn = sum(hold.values())
        for a in present:                                  # +100% rule -> gold
            if a in ("GLD", "CASH"):
                continue
            ww = hold[a] / Vn
            if ctw[a] > 0 and ww >= TRIGGER * ctw[a]:
                if log_events is not None:
                    log_events.append({"month": t, "asset": a, "ratio": ww / ctw[a],
                                        "euphoric": is_euphoric(a, t) if a in ONCHAIN_ASSETS else None,
                                        "nupl_pctl": NUPL_PCTL.get(a, pd.Series(dtype=float)).get(t, np.nan)})
                trim_to = trim_to_for(a, t, mode)
                if trim_to is None:
                    continue                                # skip: no trim this event
                keep = trim_to * ctw[a] * Vn
                if keep < hold[a]:
                    sold += hold[a] - keep; hold["GLD"] += hold[a] - keep; hold[a] = keep
        Vc = sum(hold.values()); f = 1 - (CONTRIB + 2 * sold) * COST / Vc
        for a in hold:
            hold[a] *= f
        for a in TARGET:
            if a == "CASH" or hold[a] == 0:
                continue
            r = RET[a].get(t, np.nan); hold[a] *= (1 + (0.0 if np.isnan(r) else r))
        Vend = sum(hold.values()); nav *= Vend / Vstart; navs.append(nav)
    navs = pd.Series(navs); V = sum(hold.values())
    lo, hi = -0.95, 1.0
    for _ in range(200):
        mid = (lo + hi) / 2
        npv = sum(-CONTRIB / (1 + mid) ** i for i in range(n)) + V / (1 + mid) ** n
        lo, hi = (mid, hi) if npv > 0 else (lo, mid)
    return (1 + mid) ** 12 - 1, (navs / navs.cummax() - 1).min()


out = []
def emit(s=""):
    out.append(s); print(s)


emit("# 12 — On-chain-modulated trim depth for the shipped +100% rule\n")
emit("Script: `onchain_trim_depth_modulation.py`. Follow-on to study #11 / file 10. "
     "Does NOT use on-chain data as a standalone sell trigger (already tested, lost "
     "to macro-only in file 10). Tests instead whether on-chain euphoria (derived "
     "NUPL percentile, no-lookahead) should modulate how DEEP the existing purely "
     "price-based +100% trim goes, only for BTC_spot/ETH (the only sleeve members "
     "with a free on-chain read). Macro dynamic-target cut is DISABLED (cut=1.0) "
     "throughout to isolate the trim-depth question from the already-tested macro "
     "overlay.\n")

# ═══════════════════════════════════════════════════════════════════════════════
emit("## 1. Inventory — every historical +100% trigger-fire event, BTC_spot & ETH "
     "(2010-07 -> present, one continuous run, baseline trim_to=1.5x throughout so "
     "the event list itself isn't shaped by the modulation being tested)\n")

events = []
sim(pd.Period("2010-07", "M"), "baseline", log_events=events)
ev = pd.DataFrame(events)
ev_oc = ev[ev["asset"].isin(ONCHAIN_ASSETS)].copy()

emit(f"Total +100% trigger-fire events, all assets, 2010-07..{END}: {len(ev)}")
emit(f"Of which BTC_spot/ETH (on-chain-conditionable): {len(ev_oc)}")
emit("\n**By asset:**")
emit("| asset | fire events |")
emit("|---|--:|")
for a, cnt in ev["asset"].value_counts().items():
    emit(f"| {a} | {cnt} |")

emit("\n**BTC_spot/ETH fire events in detail** (month, weight/target ratio at fire, "
     "euphoric = derived-NUPL percentile >= 0.75 at that month, `n/a` = pre-burn-in, "
     "no on-chain reading yet):\n")
emit("| month | asset | ratio (actual/target) | NUPL percentile | euphoric? |")
emit("|---|---|--:|--:|---|")
for _, r in ev_oc.sort_values("month").iterrows():
    pctl = r["nupl_pctl"]
    pctl_s = f"{pctl:.2f}" if not pd.isna(pctl) else "n/a"
    eu = "n/a" if r["euphoric"] is None else ("YES" if r["euphoric"] else "no")
    emit(f"| {r['month']} | {r['asset']} | {r['ratio']:.2f}x | {pctl_s} | {eu} |")

n_euphoric = sum(1 for e in ev_oc["euphoric"] if e is True)
n_normal = sum(1 for e in ev_oc["euphoric"] if e is False)
n_na = sum(1 for e in ev_oc["euphoric"] if e is None)
emit(f"\n**Split**: {n_euphoric} euphoric-state fires, {n_normal} normal-state fires, "
     f"{n_na} pre-burn-in/n-a fires (out of {len(ev_oc)} total BTC/ETH fires).\n")

# ═══════════════════════════════════════════════════════════════════════════════
emit("## 2. Secondary/corroborating check: native NUPL (bitcoin-data.com, 2022-07+) "
     "vs the derived-percentile euphoria flag, in the overlap window\n")
native_nupl = pd.read_csv(os.path.join(ONCHAIN, "BTC_NUPL.csv"), parse_dates=["date"]).set_index("date")["nupl"].astype(float)
native_nupl_m = native_nupl.resample("ME").last(); native_nupl_m.index = native_nupl_m.index.to_period("M")
common_nat = native_nupl_m.index.intersection(NUPL_PCTL["BTC_spot"].dropna().index)
native_euphoric = (native_nupl_m.reindex(common_nat) >= 0.5)   # fixed-level "euphoria/greed" band, short window -> no percentile
derived_euphoric = EUPHORIC["BTC_spot"].reindex(common_nat)
agree = (native_euphoric.astype(bool) == derived_euphoric.astype(bool)).mean() if len(common_nat) else float("nan")
emit(f"Overlap {common_nat.min() if len(common_nat) else 'n/a'}..{common_nat.max() if len(common_nat) else 'n/a'} "
     f"({len(common_nat)} months). Native NUPL>=0.5 vs derived-percentile>=0.75 agree "
     f"in {agree*100:.0f}% of months (both are reading the same underlying quantity, "
     "different absolute-vs-relative cutoffs — rough agreement expected, not identity).\n")
any_fires_native_window = ev_oc[ev_oc["month"] >= pd.Period("2022-07", "M")]
emit(f"BTC_spot/ETH +100% fires inside the native-SOPR/NUPL-covered window "
     f"(2022-07+): {len(any_fires_native_window)} — {'none' if len(any_fires_native_window)==0 else 'see table above'}. "
     "This is the corroborating check the user asked for; the derived series (§1) is "
     "the primary test given it has multi-cycle coverage.\n")

# ═══════════════════════════════════════════════════════════════════════════════
emit("## 3. Portfolio-level test — does modulating trim depth change XIRR/drawdown?\n")
emit("Same real-algo mechanic as files 07/09/11's portfolio tests (buy-only + "
     "+100%-rule, macro cut DISABLED here to isolate this question), 1/3/5/8/13y "
     "starts. Each cell = XIRR%/yr (maxDD%).\n")

MODES = [
    ("baseline (fixed 1.5x, shipped)", "baseline"),
    ("deeper on euphoric fires only (euphoric->1.0x, normal->1.5x)", "deeper_1.0"),
    ("deeper still on euphoric (euphoric->0.75x, i.e. BELOW target, normal->1.5x)", "deeper_0.75"),
    ("shallower on normal fires only (euphoric->1.5x, normal->1.75x)", "shallower_1.75"),
    ("skip normal fires entirely (euphoric->1.5x, normal->no trim)", "skip_normal"),
    ("both (euphoric->1.0x, normal->1.75x)", "both_1.0_1.75"),
]

emit("| mode | " + " | ".join(STARTS) + " |")
emit("|---|" + "--:|" * len(STARTS))
results_tbl = {}
for label, mode in MODES:
    cells = []
    for k in STARTS:
        xirr, dd = sim(starts[k], mode)
        results_tbl[(mode, k)] = (xirr, dd)
        cells.append(f"{xirr*100:.0f} ({dd*100:.0f})")
    emit(f"| {label} | " + " | ".join(cells) + " |")

emit(f"\nStart months: " + ", ".join(f"{k}={starts[k]}" for k in STARTS) + ".\n")

emit("**Count of BTC_spot/ETH fires actually affected by the modulation, per "
     "start-date window** (euphoric fires / normal fires, out of on-chain-eligible "
     "fires in that window — this is the real n behind the table above):\n")
emit("| window | euphoric fires | normal fires | n/a (pre burn-in) |")
emit("|---|--:|--:|--:|")
for k in STARTS:
    sub = ev_oc[ev_oc["month"] >= starts[k]]
    ne = sum(1 for e in sub["euphoric"] if e is True)
    nn = sum(1 for e in sub["euphoric"] if e is False)
    na = sum(1 for e in sub["euphoric"] if e is None)
    emit(f"| {k} | {ne} | {nn} | {na} |")
emit("")

# ═══════════════════════════════════════════════════════════════════════════════
emit("## 4. Proxy check — forward BTC drawdown conditional on NUPL state while "
     "\"in 2x-trigger territory\" generally (larger sample than discrete fire events)\n")
emit("Broader, less strict version of the same question: instead of only the exact "
     "months the rule fires, take every month where BTC's *simulated portfolio "
     "weight* is already at/above the 2x-target threshold (i.e. sustained overshoot, "
     "not just the instant it first crosses) and compare subsequent BTC drawdown "
     "over the following 3/6/12 months, split by NUPL-percentile state.\n")

# rebuild BTC weight path under baseline sim, single continuous run, to find the
# "in territory" months (>= 2x target), independent of when trims fire
def btc_weight_path(mode="baseline"):
    hold = {a: 0.0 for a in TARGET}
    ctw = {a: TARGET[a] / 100.0 for a in TARGET}
    months = pd.period_range(pd.Period("2010-07", "M"), END, freq="M")
    ratios = {}
    for t in months:
        present = [a for a in TARGET if a == "CASH" or (RET[a] is not None and t in RET[a].index and not np.isnan(RET[a].get(t, np.nan)))]
        w = pd.Series({a: TARGET[a] for a in present}); w = w / w.sum()
        for a in present:
            hold[a] += CONTRIB * w[a]
        Vn = sum(hold.values())
        sold = 0.0
        for a in present:
            if a in ("GLD", "CASH"):
                continue
            ww = hold[a] / Vn
            if a == "BTC_spot":
                ratios[t] = ww / ctw[a]
            if ctw[a] > 0 and ww >= TRIGGER * ctw[a]:
                trim_to = trim_to_for(a, t, mode)
                if trim_to is None:
                    continue
                keep = trim_to * ctw[a] * Vn
                if keep < hold[a]:
                    sold += hold[a] - keep; hold["GLD"] += hold[a] - keep; hold[a] = keep
        Vc = sum(hold.values()); f = 1 - (CONTRIB + 2 * sold) * COST / Vc
        for a in hold:
            hold[a] *= f
        for a in TARGET:
            if a == "CASH" or hold[a] == 0:
                continue
            r = RET[a].get(t, np.nan); hold[a] *= (1 + (0.0 if np.isnan(r) else r))
    return pd.Series(ratios)


btc_ratio = btc_weight_path("baseline")
in_territory = btc_ratio[btc_ratio >= 1.9]     # near/at-or-above the 2x threshold (loosened slightly to widen n)
px_btc = usd_close("BTC_USD")


def fwd_dd(px, t, h):
    """Max drawdown from level at t over the following h months (no-lookahead: uses
    only t..t+h, the realized path after the observation month)."""
    fut = px[(px.index > t) & (px.index <= t + h)]
    if len(fut) == 0:
        return float("nan")
    path = pd.concat([px[[t]], fut]) if t in px.index else fut
    return (path / path.cummax() - 1).min()


rows = []
for t in in_territory.index:
    pctl = NUPL_PCTL["BTC_spot"].get(t, np.nan)
    for h in (3, 6, 12):
        rows.append({"month": t, "h": h, "pctl": pctl, "fwd_dd": fwd_dd(px_btc, t, h)})
dd_df = pd.DataFrame(rows)

emit(f"Months with BTC portfolio-weight-ratio >= 1.9x target: {len(in_territory)} "
     f"({in_territory.index.min() if len(in_territory) else 'n/a'}..{in_territory.index.max() if len(in_territory) else 'n/a'}).\n")
emit("| horizon | euphoric (pctl>=0.75) n | mean fwd maxDD | normal n | mean fwd maxDD |")
emit("|---|--:|--:|--:|--:|")
for h in (3, 6, 12):
    sub = dd_df[dd_df["h"] == h].dropna(subset=["pctl", "fwd_dd"])
    eu = sub[sub["pctl"] >= EUPHORIA_THRESH]["fwd_dd"]
    no = sub[sub["pctl"] < EUPHORIA_THRESH]["fwd_dd"]
    emit(f"| {h}m | {len(eu)} | {eu.mean()*100 if len(eu) else float('nan'):+.1f}% "
         f"| {len(no)} | {no.mean()*100 if len(no) else float('nan'):+.1f}% |")
emit("\nNote: consecutive in-territory months are highly overlapping/autocorrelated "
     "(the same overshoot episode spans several months in a row) — this inflates the "
     "apparent n, it is NOT n independent observations. Read directionally only.\n")

emit("## Verdict\n")
emit("See discussion in the write-up (`results/study_summary/12_onchain_trim_depth_"
     "modulation.md`) for the honest read on sample size and whether any mode "
     "actually helps.")

with open(os.path.join(BASE, "results", "onchain_trim_depth_modulation.md"), "w") as fh:
    fh.write("\n".join(out) + "\n")
print("\nWrote results/onchain_trim_depth_modulation.md")
