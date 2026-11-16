#!/usr/bin/env python3
"""
PASSIVE portfolio DCA backtest (per AGENTS.md: 1000 PLN start + 1000 PLN every month).
Holds the target allocation; monthly contributions buy toward target (no selling, no
macro timing). Everything in PLN (USD assets converted via USDPLN).

Targets (% of total, LIVE risk-on weights, portfolio_spec.py -- see that module for full
derivation/citation; REFRESHED 2026-08-06, task 28, was the pre-ADR-047 demo proxy Gold
30/CASH 10/QQQ 5/FXI 2.5/MSTR 2.5; RE-REFRESHED 2026-09-25, task 28 second pass, live
ADR-074/087 weights): Gold(GLD proxy) 20, US100(QQQ proxy for Nasdaq-100) 22.5,
China(FXI proxy for ICGA.DE) 2.5, Bonds (ETFBCASH.PL/ZPR1.DE file proxies for the live
ETFPZUCASH.PL/ZPR1.DE, risk-off-only, 0 risk-on),
and Crypto 48.5 = BTC-spot 20 + miners 5 + ETH 10 + SOL 5 + COIN 3 + SUI 3 + DOGE 1 +
LINK 1.5 (SIMPLIFIED sums to 93.5 risk-on, not 100 -- the Experiments sleeve (flat 2.5%
both regimes) plus ONDO-USD 1.5 and India/QDV5.DE 2.5 (no price files) are omitted for
tractability; the existing renorm() present-based renormalization below
scales the remainder up automatically). No CASH bucket at all (removed entirely by
ADR-066).
Assets phase in at listing (target renormalised over what exists). Reports money-weighted
return (XIRR), time-weighted CAGR, max drawdown, and final allocation drift.
"""
import os
import sys
import numpy as np
import pandas as pd
sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "lib"))
import portfolio_spec as spec  # noqa: E402 -- backtest/lib, added to path above (task 159)

BASE = os.path.join(os.path.dirname(__file__), "..")
PRICES = os.path.join(BASE, "data", "prices")
START = pd.Period("2018-01", "M")
CONTRIB = spec.CONTRIB_PLN
MINERS = spec.MINERS_BASKET
TARGET = dict(spec.SIMPLIFIED_RISK_ON)


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


TICKER = dict(spec.SIMPLIFIED_TICKER_FILE)

usdpln = usd_close("USDPLN_X")               # PLN per USD
def pln_ret(t):
    px = (usd_close(t) * usdpln).dropna()
    return px.pct_change()
def native_ret(t):                            # already PLN-denominated (Warsaw/.WA listings)
    return usd_close(t).dropna().pct_change()

RET = {}
for a in TARGET:
    if a == "BOND_PL":
        RET[a] = native_ret(TICKER[a])       # ETFBCASH.PL is PLN-native (Warsaw listing)
    elif a == "BOND_DE":
        RET[a] = pln_ret(TICKER[a])          # ZPR1.DE is USD-denominated despite the .DE suffix
    else:
        RET[a] = (pd.DataFrame({m: pln_ret(m) for m in MINERS}).mean(axis=1) if a == "miners" else pln_ret(TICKER[a]))

months = pd.period_range(START, usdpln.index.max(), freq="M")
# Truncate the ragged data end: past the last month-end bar of every price file no
# TARGET asset has a return (present would be empty). Ending the window there keeps
# the XIRR contribution schedule exact -- see the 2026-09-25 correction note.
LAST_DATA = max(RET[a].dropna().index.max() for a in TARGET if RET[a] is not None)
months = months[months <= LAST_DATA]


def renorm(present):
    w = pd.Series({a: TARGET[a] for a in present}); return w / w.sum()


N = len(months)


def run(rebalance):
    hold = {a: 0.0 for a in TARGET}
    nav = 1.0; navs = []
    for t in months:
        present = [a for a in TARGET if RET[a] is not None and t in RET[a].index and not np.isnan(RET[a].get(t, np.nan))]
        tw = renorm(present)
        Vstart = sum(hold.values()) + CONTRIB
        if rebalance:                                   # sell+buy to target each month
            for a in TARGET:
                hold[a] = tw[a] * Vstart if a in present else 0.0
        else:                                           # buy toward target, never sell
            short = {a: max(0.0, tw[a] * Vstart - hold[a]) for a in present}
            ssum = sum(short.values())
            for a in present:
                hold[a] += CONTRIB * (short[a] / ssum if ssum > 0 else tw[a])
        for a in TARGET:                                # market move (month t)
            if hold[a] == 0:
                continue
            r = RET[a].get(t, np.nan)
            hold[a] *= (1 + (0.0 if np.isnan(r) else r))
        Vend = sum(hold.values())
        nav *= Vend / Vstart; navs.append(nav)
    V = sum(hold.values())
    navs = pd.Series(navs)
    return V, navs.iloc[-1] ** (12 / N) - 1, (navs / navs.cummax() - 1).min(), hold


def xirr(V):
    lo, hi = -0.95, 1.0
    for _ in range(200):
        mid = (lo + hi) / 2
        npv = sum(-CONTRIB / (1 + mid) ** i for i in range(N)) + V / (1 + mid) ** N
        lo, hi = (mid, hi) if npv > 0 else (lo, mid)
    return (1 + mid) ** 12 - 1


contributed = CONTRIB * N
Vp, cagrp, ddp, holdp = run(False)     # passive drift (no selling)
Vr, cagrr, ddr, holdr = run(True)      # rebalanced to target monthly

out = []
def emit(s=""):
    out.append(s); print(s)

emit("# Passive portfolio DCA backtest (1000 PLN start + 1000 PLN/month)\n")
emit(f"Window {months.min()} → {months.max()} ({N} months), PLN (USD via USDPLN). "
     f"Total contributed {contributed:,.0f} PLN. No macro timing.\n")
emit("| metric | PASSIVE (no selling, drift) | REBALANCED (monthly→target) |")
emit("|---|--:|--:|")
emit(f"| Final value | {Vp:,.0f} PLN | {Vr:,.0f} PLN |")
emit(f"| Profit | {Vp-contributed:,.0f} ({Vp/contributed-1:+.0%}) | {Vr-contributed:,.0f} ({Vr/contributed-1:+.0%}) |")
emit(f"| **XIRR (money-weighted)** | **{xirr(Vp)*100:.1f}%/yr** | **{xirr(Vr)*100:.1f}%/yr** |")
emit(f"| Time-weighted CAGR | {cagrp*100:.1f}%/yr | {cagrr*100:.1f}%/yr |")
emit(f"| Max drawdown | {ddp*100:.0f}% | {ddr*100:.0f}% |")

emit("\n**Final allocation vs target (shows the drift):**")
emit("| asset | target% | PASSIVE% | REBAL% |")
emit("|---|--:|--:|--:|")
for a in TARGET:
    emit(f"| {a} | {TARGET[a]:.1f} | {holdp[a]/Vp*100:.1f} | {holdr[a]/Vr*100:.1f} |")

emit("\nNote: miners = equal-weight monthly basket of high-vol names → return likely overstated "
     "(rebalancing/volatility-pumping artifact); treat the miners line as an upper bound.")

with open(os.path.join(BASE, "results", "portfolio_dca_passive_backtest.md"), "w") as fh:
    fh.write("\n".join(out) + "\n")
print("\nWrote results/portfolio_dca_passive_backtest.md")
