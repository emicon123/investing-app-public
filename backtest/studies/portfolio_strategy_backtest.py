#!/usr/bin/env python3
"""
Main-portfolio strategy backtest — REAL algo (AGENTS.md): deficit-proportional
catch-up BUY-ONLY DCA + sell ONLY via the +100% rule (asset >= 2x its target →
trim toward target → gold).
DCA 1000 PLN start + 1000/month, PLN, 15bps cost, multiple start dates (1/3/5/8/13y).

CORRECTED 2026-07-09: the monthly contribution-allocation step now uses the REAL
production deficit-proportional "catch-up" mechanic (ADR-040/045,
MainRebalancingStrategy.java -- see backtest/AGENTS.md "Rebalancing rule"), not the
naive fixed-percentage split (`hold[a] += CONTRIB * w[a]`) used in the original
version of this script. O_total = current_total + this month's contribution A;
P_i = (present-renormalized) target_weight_i * O_total; Q_i = P_i - current_value_i
(deficit if positive, surplus if negative); Q_pos = sum of positive Q_i; every
currently-underweight position gets A * Q_i / Q_pos of the new contribution --
positions already at/above target get zero new money that month. This is NOT
winner-take-all to a single asset -- every underweight position gets a share
proportional to the size of its own deficit. Matches the same correction already
applied to studies 15/16 -- see the correction note at the top of
results/study_summary/07_portfolio_strategies.md for how the numbers moved.

Two design knobs, modelled the way the user actually operates:
  * trim_to  — where the +100% rule trims a position back TO, as a multiple of its
               (current) target: 1.5× (≈ current "halfway") or 1.0× (full reset to target).
  * cut      — the macro "start selling" signal LOWERS the crypto targets (×cut) when
               risk-off; lower targets make the existing +100% rule fire and sell the
               overshoot into gold, AND contributions then buy less crypto. cut=1.0 = no
               macro de-risk. Tested cut = 0.5 (moderate) and 0.33 (aggressive).
  risk-off = net-liq accel<0 AND Global-M2 accel<0 (lagged). The trigger & trim use the
  CURRENT (possibly lowered) target — this is the "lower target → +100% rule sells" loop.

Each cell = XIRR%/yr (maxDD%).

REFRESHED 2026-08-06 (task 28, portfolio-spec refresh): TARGET now uses the LIVE
risk-on weights from `portfolio_spec.py` (was the pre-ADR-047 demo proxy: Gold flat
30/30, CASH residual sink, MSTR a major 2.5% line item -- see that module for the
full derivation). This script's own binary cut+trim mechanism predates the ADR-044
continuous blend (superseded by study 14/15 for that reason, already noted below) --
it does NOT import portfolio_spec's blend_live()/RISK_OFF table, only its risk-on
TARGET weights and the trim trigger/trim-to pair. TRIGGER default and every config
label below previously implied (2.0x, 1.5x) is "current" -- ADR-048 made (1.5x, 1.0x)
the live rule on 2026-07-10; configs and labels below are corrected accordingly.
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
CONTRIB = spec.CONTRIB_PLN
COST = spec.COST_BPS
TRIGGER = spec.TRIM_CURRENT[0]     # +100% rule: fire when actual >= this x current target (live: 1.5x, ADR-048)
MINERS = spec.MINERS_BASKET
TARGET = dict(spec.SIMPLIFIED_RISK_ON)      # "no de-risk" baseline = live risk-on weights
CRYPTO = spec.SIMPLIFIED_CRYPTO
TICKER = dict(spec.SIMPLIFIED_TICKER_FILE)


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
RISK_OFF = ((nl_acc < 0) & (gm_acc < 0))

END = usdpln.index.max()
STARTS = spec.STANDARD_WINDOWS


def cur_target(ro, cut):
    """Current target dict (% of total): crypto ×cut in risk-off, freed weight → gold."""
    if not ro or cut >= 1.0:
        return dict(TARGET)
    tt = dict(TARGET); freed = 0.0
    for a in CRYPTO:
        freed += TARGET[a] * (1 - cut); tt[a] = TARGET[a] * cut
    tt["GLD"] = TARGET["GLD"] + freed
    return tt


def sim(start, trim_to, cut, trigger=TRIGGER):
    months = pd.period_range(start, END, freq="M"); n = len(months)
    hold = {a: 0.0 for a in TARGET}; nav = 1.0; navs = []
    for t in months:
        present = [a for a in TARGET if RET[a] is not None and t in RET[a].index and not np.isnan(RET[a].get(t, np.nan))]
        ro = bool(RISK_OFF.get(t, False))
        tt = cur_target(ro, cut)                          # current target (lowered crypto if risk-off)
        ctw = {a: tt[a] / 100.0 for a in tt}              # current target fractions
        w = pd.Series({a: tt[a] for a in present}); w = w / w.sum()
        Vstart = sum(hold.values()) + CONTRIB
        # Real production deficit-proportional "catch-up" DCA mechanic (ADR-040/045),
        # NOT a naive fixed-percentage split -- see backtest/AGENTS.md "Rebalancing
        # rule". O_total = current total + this month's contribution; P_i =
        # (present-renormalized) target weight_i * O_total; Q_i = P_i - current_i
        # (deficit if positive); the whole contribution is split across every
        # currently-underweight position proportional to the size of its own deficit
        # -- positions already at/above target get zero new money this month.
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
        for a in present:                                  # +100% rule on CURRENT target → gold
            if a == "GLD":
                continue
            ww = hold[a] / Vn
            if ctw[a] > 0 and ww >= trigger * ctw[a]:
                keep = trim_to * ctw[a] * Vn
                sold += hold[a] - keep; hold["GLD"] += hold[a] - keep; hold[a] = keep
        Vc = sum(hold.values()); f = 1 - (CONTRIB + 2 * sold) * COST / Vc
        for a in hold:
            hold[a] *= f
        for a in TARGET:
            if hold[a] == 0:
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

emit("# Strategy backtest — dynamic-target macro de-risk via the +100% rule (real algo)\n")
emit(f"Buy-only DCA 1000+1000/mo PLN, 15bps, latest {END}. Macro 'start selling' = lower crypto "
     "targets (×cut) when net-liq & Global-M2 both decelerate → the +100% rule sells the overshoot "
     f"into gold. Trigger fixed at {TRIGGER:.1f}x (ADR-048 live rule); trim_to varies "
     f"{spec.TRIM_CURRENT[1]:.1f}x (CURRENT, live) vs {spec.TRIM_PRIOR[1]:.1f}x (PRIOR, "
     "pre-ADR-048, kept as reference). Each cell = XIRR%/yr (maxDD%).\n")
configs = [
    (f"NO de-risk · trim→{spec.TRIM_CURRENT[1]:.1f}×target (CURRENT, ADR-048 live rule)", dict(cut=1.0, trim_to=spec.TRIM_CURRENT[1])),
    (f"NO de-risk · trim→{spec.TRIM_PRIOR[1]:.1f}×target (PRIOR, pre-ADR-048)", dict(cut=1.0, trim_to=spec.TRIM_PRIOR[1])),
    (f"(a) cut×0.5 · trim→{spec.TRIM_CURRENT[1]:.1f}×target (CURRENT)", dict(cut=0.5, trim_to=spec.TRIM_CURRENT[1])),
    (f"(a) cut×0.5 · trim→{spec.TRIM_PRIOR[1]:.1f}×target (PRIOR)", dict(cut=0.5, trim_to=spec.TRIM_PRIOR[1])),
    (f"(b) cut×0.33 · trim→{spec.TRIM_CURRENT[1]:.1f}×target (CURRENT)", dict(cut=0.33, trim_to=spec.TRIM_CURRENT[1])),
    (f"(b) cut×0.33 · trim→{spec.TRIM_PRIOR[1]:.1f}×target (PRIOR)", dict(cut=0.33, trim_to=spec.TRIM_PRIOR[1])),
]
emit("| config | " + " | ".join(STARTS) + " |")
emit("|---|" + "--:|" * len(STARTS))
for label, kw in configs:
    cells = [f"{(lambda r: '%.0f (%.0f)' % (r[0]*100, r[1]*100))(sim(starts[k], **kw))}" for k in STARTS]
    emit(f"| {label} | " + " | ".join(cells) + " |")

emit(f"\nStart months: " + ", ".join(f"{k}={starts[k]}" for k in STARTS) +
     ". Cell = XIRR%/yr (maxDD%). risk-off months caught by both gauges decelerating.")
emit("Caveats: miners basket return overstated; gold rose through risk-off windows (flatters "
     "de-risk); 1y/3y windows noisy; buy-only re-entry after risk-off is gradual (via DCA).")

with open(os.path.join(BASE, "results", "portfolio_strategy_backtest.md"), "w") as fh:
    fh.write("\n".join(out) + "\n")
print("\nWrote results/portfolio_strategy_backtest.md")
