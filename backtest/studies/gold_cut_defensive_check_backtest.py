#!/usr/bin/env python3
"""
Study 22 -- ADR-047 gold-cut / equity-destination portfolio-level A/B (proxy-based).

Per `.claude/tasks/adr-047-048-walkforward-validation.task.md` (data-analyst/data-scientist
design review, 2026-07-12). ADR-047 (2026-07-07) cut Gold's risk-on target 30%->10% (risk-off
unchanged at 30%) and cut Bonds to 0%/0% (was 10/10 each, 20% combined risk-off), routing the
freed weight into NQSE.DE (Nasdaq-100 UCITS) and ICGA.DE (MSCI China UCITS) rather than bonds
or cash -- see `docs/architecture/05-rebalancing-algorithm.md` Sec 7.3.1 for the exact tables.
Never backtested anywhere in `backtest/` before this task.

**Proxy-vs-literal split (reviewer addition, resolves "test the real tickers" vs. "cover all
5 windows")**: NQSE.DE (2018-09->) and ICGA.DE (2019-07->) are too short for the 8y (2018-06)
and 13y (2013-06) windows. This script runs the FULL 5-window portfolio-level A/B using
QQQ/FXI as proxies for NQSE.DE/ICGA.DE -- exactly study 14's own scaffold/analogy convention
(`portfolio_macro_blend_backtest.py`'s demo portfolio already uses QQQ/FXI as the Nasdaq-100/
China-UCITS stand-ins). This tests the SHAPE/MAGNITUDE of the reallocation (cut gold 30->10,
route ~20pp into a Nasdaq-100-like/China-like equity sleeve) under long, clean history -- NOT
a claim that the literal NQSE.DE/ICGA.DE tickers themselves are proven defensive (see the
companion script `nqse_icga_defensiveness_check.py` for that separate, literal-ticker
question). Both facts are stated plainly in the write-up per the task's explicit instruction
not to let one stand in for the other.

**Table-change matrix -- explicit 2x2 (N=4), matching study #20's own controlled-A/B
precedent**: {OLD table (Gold 30/30, no equity absorption -- literally the pre-ADR-047
demo state, now imported from `portfolio_spec.py`'s LEGACY_DEMO_* reference)} x {NEW table
(live weights via `portfolio_spec.py` SIMPLIFIED_* -- QQQ 22.5/27.5, FXI 2.5/7.5,
ETH 10.0/1.0, LINK 1.5/0.15, Bonds risk-off-only, no CASH: this is the cumulative
post-ADR-047 evolution through ADR-074/087, NOT just ADR-047's own application -- the
"table effect" delta below therefore measures pre-ADR-047 vs. live, and the study_summary
correction note says so plainly)} x {old divisor 2.0, live divisor 0.5 (ADR-049 --
"new divisor" is no longer study 21's walk-forward pick but the actual live value)}.

REFRESHED 2026-09-25 (task 28, second pass): OLD/NEW tables, blend orientation and the
trim pair now come from `portfolio_spec.py`; the held-fixed trim pair is the live
CURRENT (1.5x, 1.0x, ADR-048), and "new divisor" 0.5 is the ADR-049
live value. See the 2026-09-25 correction note in the study_summary for the framing
change (NEW is no longer "ADR-047 applied" but "live as of ADR-074/087").

HISTORICAL NOTE (how NEW was derived before the 2026-09-25 refresh -- kept for the
record, no longer the construction): demo-portfolio-scale application of ADR-047's real
production point changes -- the demo portfolio's OLD QQQ/FXI targets (5.0/15.0 and
2.5/7.5) were numerically identical to real production's OLD NQSE.DE/ICGA.DE targets, so
ADR-047's real NEW absolute values (NQSE.DE 18.5/28.5, ICGA.DE 9.0/14.0) were applied to
QQQ/FXI directly, unscaled, with CASH absorbing the residual. Superseded: NEW is now the
live SIMPLIFIED_* tables (see above), and the OLD-vs-NEW delta measures pre-ADR-047 vs.
live, not ADR-047's own scope (every later bucket change -- Bonds sink, ETH cut,
LINK/ONDO promotion, India split -- rides along in NEW).

Standing baseline (`backtest/AGENTS.md`): includes the 60/40 classic-portfolio cross-check
(SPY/AGG, BUYHOLD only), per study #20's precedent that one diversified comparison point is
sufficient when the study isn't re-auditing the classics themselves -- required here (unlike
study 21) because a ~20pp gold-to-equity reallocation is a general portfolio-construction
question, not a blend-internal tuning constant.

Retained per this task's explicit instruction ("keep whatever script(s) this task produces").
"""
import os
import sys
import numpy as np
import pandas as pd
sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "lib"))
import portfolio_spec as spec  # noqa: E402 -- backtest/lib, added to path above (task 159)

BASE = os.path.join(os.path.dirname(__file__), "..")
PRICES = os.path.join(BASE, "data", "prices")
MACRO = os.path.join(BASE, "data", "macro")
RESULTS = os.path.join(BASE, "results")
CONTRIB = spec.CONTRIB_PLN
COST = spec.COST_BPS
TRIGGER, TRIM_TO = spec.TRIM_CURRENT   # LIVE (ADR-048): 1.5x trigger / 1.0x trim-to
MINERS = spec.MINERS_BASKET

# ---------------------------------------------------------------------------
# OLD table -- the pre-ADR-047 demo state, kept as the historical before-leg of this
# A/B exactly as `portfolio_spec.py` preserves it (LEGACY_DEMO_* reference -- Gold flat
# 30/30, CASH residual sink, MSTR a major line item). Unchanged by the refresh.
OLD_RISK_ON = dict(spec.LEGACY_DEMO_RISK_ON)
OLD_RISK_OFF = dict(spec.LEGACY_DEMO_RISK_OFF)

# NEW table -- the LIVE tables (portfolio_spec.py SIMPLIFIED_*). Was ADR-047's own
# application (Gold 10/30, QQQ 18.5/28.5, FXI 9.0/14.0) before the 2026-09-25 refresh;
# now the cumulative live state through ADR-074 (LINK/ONDO promotion, ETH 13->10) and
# ADR-087 (India split: QQQ 22.5/27.5, FXI 2.5/7.5) -- no CASH bucket at all.
NEW_RISK_ON = dict(spec.SIMPLIFIED_RISK_ON)
NEW_RISK_OFF = dict(spec.SIMPLIFIED_RISK_OFF)

TICKER = dict(spec.SIMPLIFIED_TICKER_FILE)
TICKER["MSTR"] = "MSTR"   # OLD-leg only (MSTR is a 0.1 Experiments position live, omitted
                          # from SIMPLIFIED_*); MSTR_full.csv/MSTR.csv is on disk.


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


ALL_ASSETS = set(OLD_RISK_ON) | set(NEW_RISK_ON)
RET = {"CASH": None}   # CASH exists only in the OLD leg (pre-ADR-047 world had a cash
                       # bucket); the NEW/live leg has no CASH row at all.
for a in ALL_ASSETS:
    if a != "CASH":
        RET[a] = pd.DataFrame({m: pln_ret(m) for m in MINERS}).mean(axis=1) if a == "miners" else pln_ret(TICKER[a])

END = usdpln.index.max()
STARTS = spec.STANDARD_WINDOWS
starts = {k: END - v * 12 for k, v in STARTS.items()}

# ---------------------------------------------------------------------------
# ADR-044 blend (unchanged construction) -- steepness divisor is a parameter, per study 21.
# LIVE orientation throughout (0=risk-off, 1=risk-on, ADR-071) via spec.blend_live.

def macro_m(f, c, weekly=False):
    s = pd.read_csv(os.path.join(MACRO, f), parse_dates=["date"]).set_index("date")[c].astype(float)
    if weekly:
        s = s.resample("ME").last()
    s.index = s.index.to_period("M"); return s.groupby(level=0).last()


netliq = macro_m("FED_NET_LIQUIDITY.csv", "net_liquidity_bn", weekly=True)
gm = macro_m("GLOBAL_M2.csv", "global_m2_bn")
nl_accel = netliq.pct_change(12).diff(3).shift(1)
gm_accel = gm.pct_change(12).diff(3).shift(2)
Z_MIN_PERIODS = 24


def expanding_z(s):
    m = s.expanding(min_periods=Z_MIN_PERIODS).mean()
    sd = s.expanding(min_periods=Z_MIN_PERIODS).std()
    return (s - m) / sd


z_nl = expanding_z(nl_accel)
z_gm = expanding_z(gm_accel)
full_idx = pd.period_range(min(z_nl.index.min(), z_gm.index.min()), END, freq="M")
composite_z = 0.7 * z_nl.reindex(full_idx) + 0.3 * z_gm.reindex(full_idx)
smoothed_z = composite_z.rolling(3, min_periods=3).mean()
BLENDS = {d: spec.blend_live(smoothed_z, d) for d in (2.0, 0.5)}

# ---------------------------------------------------------------------------

def sim(start, risk_on, risk_off, divisor):
    """Blend-driven target interpolation (ADR-044, fixed divisor) + the live CURRENT
    1.5x-trigger/1.0x-trim-to-gold rule (ADR-048), real deficit-proportional catch-up
    DCA -- identical mechanics to study 14/21, parametrized on (risk_on, risk_off,
    divisor). CASH plumbing is kept for the OLD leg only (its world had a cash
    bucket); the NEW/live leg never contains CASH."""
    months = pd.period_range(start, END, freq="M"); n = len(months)
    assets = list(risk_on.keys())
    hold = {a: 0.0 for a in assets}; nav = 1.0; navs = []
    bl = BLENDS[divisor]
    for t in months:
        present = [a for a in assets if a == "CASH" or (RET[a] is not None and t in RET[a].index and not np.isnan(RET[a].get(t, np.nan)))]
        bv = bl.get(t, np.nan)
        # insufficient history -> collapse to risk-on (b=1.0 in the LIVE orientation).
        b = 1.0 if pd.isna(bv) else float(bv)
        tt = {a: spec.effective_target(risk_on[a], risk_off.get(a, risk_on[a]), b) for a in assets}
        ctw = {a: tt[a] / 100.0 for a in tt}
        w = pd.Series({a: tt[a] for a in present}); w = w / w.sum()
        Vstart = sum(hold.values()) + CONTRIB
        N_total = sum(hold[a] for a in present)
        O_total = N_total + CONTRIB
        Q = {a: w[a] * O_total - hold[a] for a in present}
        Q_pos = sum(q for q in Q.values() if q > 0)
        if Q_pos > 1e-9:
            for a in present:
                if Q[a] > 0:
                    hold[a] += CONTRIB * Q[a] / Q_pos
        else:
            for a in present:
                hold[a] += CONTRIB * w[a]
        sold = 0.0
        Vn = sum(hold.values())
        for a in present:
            if a in ("GLD", "CASH"):
                continue
            ww = hold[a] / Vn
            if ctw[a] > 0 and ww >= TRIGGER * ctw[a]:
                keep = TRIM_TO * ctw[a] * Vn
                sold += hold[a] - keep; hold["GLD"] += hold[a] - keep; hold[a] = keep
        Vc = sum(hold.values()); f = 1 - (CONTRIB + 2 * sold) * COST / Vc
        for a in hold:
            hold[a] *= f
        for a in assets:
            if a == "CASH" or hold[a] == 0:
                continue
            r = RET[a].get(t, np.nan); hold[a] *= (1 + (0.0 if np.isnan(r) else r))
        Vend = sum(hold.values()); nav *= Vend / Vstart; navs.append(nav)
    navs = pd.Series(navs, index=months); V = sum(hold.values())
    lo, hi = -0.95, 1.0
    for _ in range(200):
        mid = (lo + hi) / 2
        npv = sum(-CONTRIB / (1 + mid) ** i for i in range(n)) + V / (1 + mid) ** n
        lo, hi = (mid, hi) if npv > 0 else (lo, mid)
    xirr = (1 + mid) ** 12 - 1
    maxdd = (navs / navs.cummax() - 1).min()
    return xirr, maxdd, navs


# 60/40 classic-portfolio cross-check (self-contained, per study 20's precedent -- not an
# import of classic_portfolios_backtest.py, a small function reusing its named weights).
def sim_6040(start):
    tickers = {"SPY": 60.0, "AGG": 40.0}
    ret = {t: pln_ret(t) for t in tickers}
    months = pd.period_range(start, END, freq="M"); n = len(months)
    hold = {a: 0.0 for a in tickers}; nav = 1.0; navs = []
    w = pd.Series({a: tickers[a] / 100.0 for a in tickers})
    for t in months:
        Vstart = sum(hold.values()) + CONTRIB
        N_total = sum(hold.values()); O_total = N_total + CONTRIB
        Q = {a: w[a] * O_total - hold[a] for a in tickers}
        Q_pos = sum(q for q in Q.values() if q > 0)
        if Q_pos > 1e-9:
            for a in tickers:
                if Q[a] > 0:
                    hold[a] += CONTRIB * Q[a] / Q_pos
        else:
            for a in tickers:
                hold[a] += CONTRIB * w[a]
        Vc = sum(hold.values()); f = 1 - CONTRIB * COST / Vc
        for a in hold:
            hold[a] *= f
        for a in tickers:
            r = ret[a].get(t, np.nan); hold[a] *= (1 + (0.0 if np.isnan(r) else r))
        Vend = sum(hold.values()); nav *= Vend / Vstart; navs.append(nav)
    navs = pd.Series(navs, index=months); V = sum(hold.values())
    lo, hi = -0.95, 1.0
    for _ in range(200):
        mid = (lo + hi) / 2
        npv = sum(-CONTRIB / (1 + mid) ** i for i in range(n)) + V / (1 + mid) ** n
        lo, hi = (mid, hi) if npv > 0 else (lo, mid)
    xirr = (1 + mid) ** 12 - 1
    maxdd = (navs / navs.cummax() - 1).min()
    return xirr, maxdd, navs


# ---------------------------------------------------------------------------
out = []
def emit(s=""):
    out.append(s); print(s)


emit("# Study 22 -- gold-cut/equity-destination A/B, OLD (pre-ADR-047) vs LIVE (proxy-based, full 5-window)\n")
emit(f"Buy-only DCA 1000+1000/mo PLN, 15bps, CURRENT 1.5x/1.0x-trim-to-gold rule fixed "
     f"throughout (ADR-048 live rule; was 2.0x/1.5x PRIOR before the 2026-09-25 refresh). "
     f"Latest month: {END}. QQQ/FXI stand in for Nasdaq-100/China (proxy-vs-"
     f"literal split -- see script header and the companion `nqse_icga_defensiveness_check.py` "
     f"for the literal-ticker question this script does NOT answer). NEW is the live "
     f"table through ADR-074/087, not just ADR-047's own application -- see header.\n")

emit("## OLD table (pre-ADR-047) vs NEW table (live through ADR-074/087), demo-portfolio scale\n")
emit("| asset | OLD risk-on | OLD risk-off | NEW risk-on | NEW risk-off |")
emit("|---|--:|--:|--:|--:|")
for a in list(OLD_RISK_ON) + [k for k in NEW_RISK_ON if k not in OLD_RISK_ON]:
    o = OLD_RISK_ON.get(a, float("nan")); oo = OLD_RISK_OFF.get(a, float("nan"))
    n = NEW_RISK_ON.get(a, float("nan")); nn = NEW_RISK_OFF.get(a, float("nan"))
    emit(f"| {a} | {o:.3f} | {oo:.3f} | {n:.3f} | {nn:.3f} |")
emit("")

emit("## 2x2 table-change x divisor matrix (N=4, decided up front)\n")
emit("Divisor 0.5 is the ADR-049 LIVE value (study 21's walk-forward chain also selects "
     "0.5 at its most recent checkpoint, so the matrix keeps study 21's own {2.0, 0.5} "
     "pair -- now reading 0.5 as live, not as the walk-forward-honest alternative to a "
     "1.0 live value).\n")
emit("| table | divisor | " + " | ".join(STARTS) + " |")
emit("|---|--:|" + "--:|" * len(STARTS))
matrix_navs = {}
for table_label, ro, rf in [("OLD (pre-ADR-047)", OLD_RISK_ON, OLD_RISK_OFF),
                            ("NEW (live through ADR-074/087)", NEW_RISK_ON, NEW_RISK_OFF)]:
    for d in (2.0, 0.5):
        cells = []
        for k in STARTS:
            xirr, maxdd, navs = sim(starts[k], ro, rf, d)
            matrix_navs[(table_label, d, k)] = navs
            cells.append(f"{xirr*100:.0f} ({maxdd*100:.0f})")
        emit(f"| {table_label} | {d} | " + " | ".join(cells) + " |")
emit(f"\nStart months: " + ", ".join(f"{k}={starts[k]}" for k in STARTS) + ". Cell = XIRR%/yr (maxDD%).")

emit("\n## Isolating the table effect vs. the divisor effect (13y/8y windows, pp deltas)\n")
emit("| comparison | 13y XIRR delta | 13y maxDD delta | 8y XIRR delta | 8y maxDD delta |")
emit("|---|--:|--:|--:|--:|")
def cell_xirr_dd(table, d, k):
    xirr, maxdd, _ = sim(starts[k], OLD_RISK_ON if table == "OLD" else NEW_RISK_ON,
                          OLD_RISK_OFF if table == "OLD" else NEW_RISK_OFF, d)
    return xirr, maxdd
r_old20_13, dd_old20_13 = cell_xirr_dd("OLD", 2.0, "13y")
r_new20_13, dd_new20_13 = cell_xirr_dd("NEW", 2.0, "13y")
r_old05_13, dd_old05_13 = cell_xirr_dd("OLD", 0.5, "13y")
r_new05_13, dd_new05_13 = cell_xirr_dd("NEW", 0.5, "13y")
r_old20_8, dd_old20_8 = cell_xirr_dd("OLD", 2.0, "8y")
r_new20_8, dd_new20_8 = cell_xirr_dd("NEW", 2.0, "8y")
emit(f"| Table effect (NEW-OLD) @ divisor=2.0 | {100*(r_new20_13-r_old20_13):+.1f}pp | "
     f"{100*(dd_new20_13-dd_old20_13):+.1f}pp | {100*(r_new20_8-r_old20_8):+.1f}pp | "
     f"{100*(dd_new20_8-dd_old20_8):+.1f}pp |")
emit(f"| Table effect (NEW-OLD) @ divisor=0.5 | {100*(r_new05_13-r_old05_13):+.1f}pp | "
     f"{100*(dd_new05_13-dd_old05_13):+.1f}pp | -- | -- |")
emit(f"| Divisor effect (0.5-2.0) @ OLD table | {100*(r_old05_13-r_old20_13):+.1f}pp | "
     f"{100*(dd_old05_13-dd_old20_13):+.1f}pp | -- | -- |")
emit(f"| Divisor effect (0.5-2.0) @ NEW table | {100*(r_new05_13-r_new20_13):+.1f}pp | "
     f"{100*(dd_new05_13-dd_new20_13):+.1f}pp | -- | -- |")

emit("\n## Standing baseline: 60/40 classic-portfolio cross-check (SPY/AGG, BUYHOLD real "
     "catch-up DCA)\n")
emit("| window | XIRR%/yr | maxDD% |")
emit("|---|--:|--:|")
for k in STARTS:
    xirr, maxdd, _ = sim_6040(starts[k])
    emit(f"| {k} | {xirr*100:.0f} | {maxdd*100:.0f} |")

emit("\n## Caveats\n")
emit("- **This is the proxy-based portfolio-level A/B ONLY** -- QQQ/FXI stand in for "
     "NQSE.DE/ICGA.DE, exactly matching study 14's own scaffold/analogy convention ('a "
     "reasonable-but-unverified analogy, not a re-derivation from scratch'). This answers "
     "'is a ~20pp gold-to-equity reallocation of this rough shape defensible', NOT 'do the "
     "two actual ETFs behave defensively' -- see the companion literal-ticker script for "
     "that separate question. Do not read a passing result here as proof the real "
     "NQSE.DE/ICGA.DE tickers are defensive, or vice versa.\n"
     "- **New-table derivation is itself a judgment-call analogy** (live production "
     "targets reused for the long-history proxies QQQ/FXI/LINK/BONDs since the demo "
     "portfolio's OLD values already numerically matched real production's OLD values, "
     "via portfolio_spec.py) -- not independently re-optimized for this demo portfolio's "
     "own weight structure. NEW is the cumulative live state through ADR-074/087, not "
     "just ADR-047's own application -- see the header.\n"
     "- **Divisor 0.5 is the ADR-049 live value** -- this matrix therefore does not "
     "include a literal '1.0' cell at all, by design (study 21's own walk-forward chain "
     "likewise never selects 1.0 at any checkpoint).\n"
     "- Same caveats as study 14/21 apply unchanged: miners-basket survivorship, gold's "
     "multi-year rally flatters any gold-trim-destination variant, overlapping windows "
     "share one underlying price/macro history, 1y/3y windows are single-episode reads.")

with open(os.path.join(RESULTS, "gold_cut_defensive_check_backtest.md"), "w") as fh:
    fh.write("\n".join(out) + "\n")
print("\nWrote results/gold_cut_defensive_check_backtest.md")
