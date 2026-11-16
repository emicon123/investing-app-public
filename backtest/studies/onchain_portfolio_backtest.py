#!/usr/bin/env python3
"""
Portfolio-level test: does adding an on-chain trigger (LTH-SOPR extreme, file 10's
most defensible reversal signal) to the shipped macro dynamic-target de-risk mechanism
improve real-algo portfolio outcomes? Same real algo as files 07/09 (AGENTS.md:
deficit-proportional catch-up BUY-ONLY DCA + 2x-trim-to-1.5x-target, cut=0.33 aggressive)
and the SAME target/ticker roster as portfolio_strategy_backtest.py, for apples-to-apples
comparison — only the risk-off TRIGGER definition varies.

On-chain data only goes back to 2022-07 (bitcoin-data.com), so this can only test the
1y and 3y start dates (AGENTS.md's 5/8/13y windows require history we don't have for
this specific signal) -- flagged prominently, not hidden.

LTH-SOPR threshold: 75th percentile of the full 2022-2026 sample (~1.86) -- calibrated
in-sample given the tiny history; a real limitation, noted in the write-up.

CORRECTED 2026-07-13 (study #26): the monthly contribution-allocation step now uses the
REAL production deficit-proportional "catch-up" mechanic (ADR-040/045,
MainRebalancingStrategy.java -- see backtest/AGENTS.md "Rebalancing rule"), not the
naive fixed-percentage split (`hold[a] += CONTRIB * w[a]`) this script used from its
original 2026-07-03 authoring through 2026-07-12 -- it predated the 2026-07-09
mechanic-fix pass across studies 07/14/15/16/17/20 and was never folded in. O_total =
current_total + this month's contribution A; P_i = (present-renormalized) target
weight_i * O_total (using the SAME risk-off-adjusted `tt` dict already computed this
month by `cur_target()` for the trim-trigger check below, so the deficit calc and the
trim check always agree on "what's the target this month"); Q_i = P_i - current_value_i
(deficit if positive, surplus if negative); Q_pos = sum of positive Q_i; every
currently-underweight position gets A * Q_i / Q_pos of the new contribution --
positions already at/above target get zero new money that month. See
results/study_summary/26_onchain_trigger_dca_mechanic_fix.md for how the numbers moved.

REFRESHED 2026-08-06 (task 28, portfolio-spec refresh): TARGET now uses the LIVE
risk-on weights from `portfolio_spec.py` (was the pre-ADR-047 demo proxy). TRIGGER/
TRIM_TO now default to the LIVE ADR-048 rule (1.5x/1.0x) -- was hardcoded 2.0x/1.5x,
which this script's own header used to describe as "the real algo" without flagging
that ADR-048 had already tightened it three weeks before this refresh.
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
ONCHAIN = os.path.join(BASE, "data", "onchain")
CONTRIB = spec.CONTRIB_PLN
COST = spec.COST_BPS
TRIGGER, TRIM_TO = spec.TRIM_CURRENT     # live ADR-048 rule: 1.5x / 1.0x
CUT = 0.33
MINERS = spec.MINERS_BASKET
TARGET = dict(spec.SIMPLIFIED_RISK_ON)
CRYPTO = spec.SIMPLIFIED_CRYPTO
TICKER = dict(spec.SIMPLIFIED_TICKER_FILE)
LTH_SOPR_THRESHOLD = 1.86   # 75th percentile of the full 2022-2026 sample


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


def native_ret(t):                   # already PLN-denominated (Warsaw/.WA listings) -> no FX leg
    return usd_close(t).dropna().pct_change()


RET = {}
for a in TARGET:
    if a == "BOND_PL":
        RET[a] = native_ret(TICKER[a])       # ETFBCASH.PL is PLN-native (Warsaw listing)
    elif a == "BOND_DE":
        RET[a] = pln_ret(TICKER[a])          # ZPR1.DE is USD-denominated despite the .DE suffix
    else:
        RET[a] = pd.DataFrame({m: pln_ret(m) for m in MINERS}).mean(axis=1) if a == "miners" else pln_ret(TICKER[a])


def macro_m(f, c, weekly=False):
    s = pd.read_csv(os.path.join(MACRO, f), parse_dates=["date"]).set_index("date")[c].astype(float)
    if weekly:
        s = s.resample("ME").last()
    s.index = s.index.to_period("M"); return s.groupby(level=0).last()

nl_acc = (macro_m("FED_NET_LIQUIDITY.csv", "net_liquidity_bn", weekly=True).pct_change(12).diff(3)).shift(1)
gm_acc = (macro_m("GLOBAL_M2.csv", "global_m2_bn").pct_change(12).diff(3)).shift(2)
MACRO_RISK_OFF = ((nl_acc < 0) & (gm_acc < 0))

lth_sopr = pd.read_csv(os.path.join(ONCHAIN, "BTC_LTH_SOPR.csv"), parse_dates=["date"]).set_index("date")["lth_sopr"].astype(float)
lth_sopr_m = lth_sopr.resample("ME").last(); lth_sopr_m.index = lth_sopr_m.index.to_period("M")
ONCHAIN_RISK_OFF = (lth_sopr_m >= LTH_SOPR_THRESHOLD)

END = usdpln.index.max()
STARTS = {"3y": 3, "1y": 1}   # on-chain data only goes back to 2022-07 -- 5/8/13y not testable


def cur_target(ro, cut):
    if not ro or cut >= 1.0:
        return dict(TARGET)
    tt = dict(TARGET); freed = 0.0
    for a in CRYPTO:
        freed += TARGET[a] * (1 - cut); tt[a] = TARGET[a] * cut
    tt["GLD"] = TARGET["GLD"] + freed
    return tt


def sim(start, risk_off_series):
    months = pd.period_range(start, END, freq="M"); n = len(months)
    hold = {a: 0.0 for a in TARGET}; nav = 1.0; navs = []
    for t in months:
        present = [a for a in TARGET if a == "CASH" or (RET[a] is not None and t in RET[a].index and not np.isnan(RET[a].get(t, np.nan)))]
        ro = bool(risk_off_series.get(t, False))
        tt = cur_target(ro, CUT)
        ctw = {a: tt[a] / 100.0 for a in tt}
        w = pd.Series({a: tt[a] for a in present}); w = w / w.sum()
        Vstart = sum(hold.values()) + CONTRIB
        # Real production deficit-proportional "catch-up" DCA mechanic (ADR-040/045),
        # NOT a naive fixed-percentage split -- see backtest/AGENTS.md "Rebalancing
        # rule". O_total = current total + this month's contribution; P_i =
        # (present-renormalized) target weight_i * O_total (same risk-off-adjusted `tt`
        # the trim-trigger check below uses); Q_i = P_i - current_i (deficit if
        # positive); the whole contribution is split across every currently-underweight
        # position proportional to the size of its own deficit -- positions already
        # at/above target get zero new money this month.
        N_total = sum(hold[a] for a in present)
        O_total = N_total + CONTRIB
        Q = {a: w[a] * O_total - hold[a] for a in present}
        Q_pos = sum(q for q in Q.values() if q > 0)
        if Q_pos > 1e-9:
            for a in present:
                if Q[a] > 0:
                    hold[a] += CONTRIB * Q[a] / Q_pos
        else:                          # degenerate guard; shouldn't trigger (Q_pos >= CONTRIB always)
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


starts = {k: END - v * 12 for k, v in STARTS.items()}
out = []
def emit(s=""):
    out.append(s); print(s)

emit("# On-chain trigger portfolio test — LTH-SOPR vs macro vs composite\n")
emit("Real algo (buy-only Q_pos catch-up DCA + CURRENT 1.5x/1.0x-trim-to-target, ADR-048 "
     "live rule, cut=0.33). Only the risk-off TRIGGER "
     "definition varies. **On-chain data only exists from 2022-07 -- 5/8/13y start dates "
     "from files 07/09 are NOT testable here; only 3y and 1y.** Each cell = XIRR%/yr (maxDD%).\n")

COMPOSITE_OR = MACRO_RISK_OFF.combine(ONCHAIN_RISK_OFF, lambda a, b: bool(a) or bool(b))
COMPOSITE_AND = MACRO_RISK_OFF.combine(ONCHAIN_RISK_OFF, lambda a, b: bool(a) and bool(b))

configs = [
    ("NO de-risk (baseline, always risk-on)", pd.Series(False, index=MACRO_RISK_OFF.index)),
    ("Macro only (shipped, ADR-042)", MACRO_RISK_OFF),
    ("On-chain only (LTH-SOPR>=1.86)", ONCHAIN_RISK_OFF),
    ("Composite OR (macro or on-chain)", COMPOSITE_OR),
    ("Composite AND (macro and on-chain)", COMPOSITE_AND),
]
emit("| config | " + " | ".join(STARTS) + " |")
emit("|---|" + "--:|" * len(STARTS))
for label, series in configs:
    cells = [f"{(lambda r: '%.0f (%.0f)' % (r[0]*100, r[1]*100))(sim(starts[k], series))}" for k in STARTS]
    emit(f"| {label} | " + " | ".join(cells) + " |")

emit(f"\nStart months: " + ", ".join(f"{k}={starts[k]}" for k in STARTS) + ". Cell = XIRR%/yr (maxDD%).")

# how often each trigger fires + overlap, over the testable window
common = MACRO_RISK_OFF.index.intersection(ONCHAIN_RISK_OFF.index)
mro, oro = MACRO_RISK_OFF.reindex(common).fillna(False), ONCHAIN_RISK_OFF.reindex(common).fillna(False)
emit(f"\nTrigger frequency ({len(common)} months, {common.min()}..{common.max()}): "
     f"macro risk-off {mro.sum()} months, on-chain risk-off {oro.sum()} months, "
     f"both {(mro & oro).sum()} months, either {(mro | oro).sum()} months.")

emit("\n## Caveats")
emit("Only 2 start dates testable (on-chain history is ~4 years); LTH-SOPR threshold "
     "(75th percentile, 1.86) calibrated on the SAME full sample being backtested -- "
     "real in-sample bias given how little history exists; see file 10 for the signal-level "
     "caveats (native-2022+ IC is strong but doesn't replicate over 2010+ history).")

with open(os.path.join(BASE, "results", "onchain_portfolio_backtest.md"), "w") as fh:
    fh.write("\n".join(out) + "\n")
print("\nWrote results/onchain_portfolio_backtest.md")
