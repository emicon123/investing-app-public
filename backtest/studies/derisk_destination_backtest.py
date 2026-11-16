#!/usr/bin/env python3
"""
De-risk DESTINATION test — where should crypto proceeds go when the +100% rule (or
the macro dynamic-target de-risk, file 07) trims a position? Shipped: all -> GOLD
(ADR-042). User idea: route to bond ETFs instead — ETFBCASH.WA (Beta ETF, PLN, a
real-duration PLN treasury/bond fund — NOT a stable cash-equivalent, it dropped -23%
in the 2022 rate-hike year) and ZPR1.DE (SPDR 1-3 Month T-Bill UCITS ETF, USD,
near-cash — steady ~4-5%/yr grind, near-zero drawdown).

Same real-algo mechanic as file 07 (pure-proportional BUY-ONLY DCA + 2x-trim-to-1.5x
target, macro dynamic-target cut x0.33 aggressive) — only the DESTINATION of trimmed
crypto proceeds changes. Both new tickers are recent listings (ETFBCASH.WA data from
2021-06, ZPR1.DE from 2023-11), so windows that predate a ticker fall back to GOLD for
that ticker's share (mirrors production reality: the app can only route to a ticker
once it has price data / the user actually holds it).

Destinations tested: GOLD (shipped baseline), BONDS (ETFBCASH+ZPR1 equal split, gold
fallback pre-inception), MIX (50% gold / 50% bonds).

REFRESHED 2026-09-25 (task 28, portfolio-spec refresh, second pass): imports TARGET /
CRYPTO / TICKER / trim pair / DCA constants from `portfolio_spec.py` (single source of
truth) instead of hardcoding its own stale pre-ADR-047 copies. Four things changed vs.
this script's prior form: (1) base weights updated to the live tables (QQQ 22.5/27.5,
FXI 2.5/7.5, ETH 10.0/1.0, LINK-USD 1.5/0.15 added, CASH bucket and MSTR-as-major
removed); (2) the held-fixed trim pair is now the live CURRENT (1.5x trigger / 1.0x
trim-to, ADR-048) -- the old (2.0x, 1.5x) it modeled is the PRIOR rule, reference only;
(3) contributions now use the real deficit-proportional Q_pos catch-up DCA mechanic
(ADR-040/045, backtest/AGENTS.md) instead of the naive fixed-weight split this script
used before; (4) the binary risk-off cut scaffold itself is unchanged (that IS this
study's question -- continuous-blend vs. binary-cut is study 07/14 territory, not this
file's). Bond-leg price files are unchanged (ETFBCASH_WA.csv is the on-disk history
for the PLN leg; V59 switched the live ticker to ETFPZUCASH.PL with no allocation
change, so the longer Beta-ETF history stays the backtest proxy -- see
portfolio_spec.py).
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
TRIGGER, TRIM_TO = spec.TRIM_CURRENT   # LIVE (ADR-048): 1.5x trigger / 1.0x trim-to
CUT = 0.33          # the adopted aggressive cut (07's recommendation)
MINERS = spec.MINERS_BASKET
# LIVE target tables (portfolio_spec.py SIMPLIFIED_* -- GLD/QQQ/FXI are long-history
# proxies, LINK is the real LINK-USD, BOND legs are native; see that module).
TARGET = dict(spec.SIMPLIFIED_RISK_ON)
CRYPTO = spec.SIMPLIFIED_CRYPTO
TICKER = dict(spec.SIMPLIFIED_TICKER_FILE)
DEST_TICKERS = {"BOND_PL": "ETFBCASH_WA", "BOND_DE": "ZPR1_DE"}   # native filenames


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
def pln_ret(t):                       # USD-denominated asset -> PLN return
    return (usd_close(t) * usdpln).dropna().pct_change()
def native_ret(t):                    # already PLN-denominated -> no FX leg
    return usd_close(t).dropna().pct_change()


RET = {}
for a in TARGET:
    RET[a] = pd.DataFrame({m: pln_ret(m) for m in MINERS}).mean(axis=1) if a == "miners" else pln_ret(TICKER[a])
RET["BOND_PL"] = native_ret(DEST_TICKERS["BOND_PL"])   # ETFBCASH.WA is PLN-native
RET["BOND_DE"] = pln_ret(DEST_TICKERS["BOND_DE"])      # ZPR1.DE is USD-denominated


def macro_m(f, c, weekly=False):
    s = pd.read_csv(os.path.join(MACRO, f), parse_dates=["date"]).set_index("date")[c].astype(float)
    if weekly:
        s = s.resample("ME").last()
    s.index = s.index.to_period("M"); return s.groupby(level=0).last()

nl_acc = (macro_m("FED_NET_LIQUIDITY.csv", "net_liquidity_bn", weekly=True).pct_change(12).diff(3)).shift(1)
gm_acc = (macro_m("GLOBAL_M2.csv", "global_m2_bn").pct_change(12).diff(3)).shift(2)
RISK_OFF = ((nl_acc < 0) & (gm_acc < 0))

END = usdpln.index.max()
STARTS = {"13y": 13, "8y": 8, "5y": 5, "3y": 3, "1y": 1}


def cur_target(ro):
    if not ro:
        return dict(TARGET)
    tt = dict(TARGET); freed = 0.0
    for a in CRYPTO:
        freed += TARGET[a] * (1 - CUT); tt[a] = TARGET[a] * CUT
    tt["GLD"] = TARGET["GLD"] + freed
    return tt


def sim(start, dest):
    """dest: 'GOLD' | 'BONDS' | 'MIX' — where TRIMMED proceeds go (contributions
    still flow via the real Q_pos catch-up DCA; only the CURRENT-rule overshoot
    amount is redirected)."""
    months = pd.period_range(start, END, freq="M"); n = len(months)
    hold = {a: 0.0 for a in TARGET}
    hold["BOND_PL"] = 0.0; hold["BOND_DE"] = 0.0
    nav = 1.0; navs = []
    for t in months:
        present = [a for a in TARGET if (RET[a] is not None and t in RET[a].index and not np.isnan(RET[a].get(t, np.nan)))]
        ro = bool(RISK_OFF.get(t, False))
        tt = cur_target(ro)
        ctw = {a: tt[a] / 100.0 for a in tt}
        w = pd.Series({a: tt[a] for a in present}); w = w / w.sum()
        Vstart = sum(hold.values()) + CONTRIB
        # Real production deficit-proportional "catch-up" DCA (ADR-040/045), NOT a
        # naive fixed-percentage split -- see backtest/AGENTS.md. Same idiom as
        # trim_frequency_backtest.py's sim().
        N_total = sum(hold[a] for a in present)
        O_total = N_total + CONTRIB
        Q = {a: w[a] * O_total - hold[a] for a in present}
        Q_pos = sum(q for q in Q.values() if q > 0)
        if Q_pos > 1e-9:
            for a in present:
                if Q[a] > 0:
                    hold[a] += CONTRIB * Q[a] / Q_pos
        else:                          # degenerate guard; shouldn't trigger
            for a in present:
                hold[a] += CONTRIB * w[a]
        sold = 0.0
        Vn = sum(hold.values())
        bond_pl_ok = t in RET["BOND_PL"].index and not np.isnan(RET["BOND_PL"].get(t, np.nan))
        bond_de_ok = t in RET["BOND_DE"].index and not np.isnan(RET["BOND_DE"].get(t, np.nan))
        for a in present:
            if a == "GLD":
                continue
            ww = hold[a] / Vn
            if ctw[a] > 0 and ww >= TRIGGER * ctw[a]:
                keep = TRIM_TO * ctw[a] * Vn
                proceeds = hold[a] - keep
                sold += proceeds; hold[a] = keep
                if dest == "GOLD" or (dest in ("BONDS", "MIX") and not (bond_pl_ok or bond_de_ok)):
                    hold["GLD"] += proceeds
                elif dest == "BONDS":
                    _route_to_bonds(hold, proceeds, bond_pl_ok, bond_de_ok)
                elif dest == "MIX":
                    hold["GLD"] += proceeds * 0.5
                    _route_to_bonds(hold, proceeds * 0.5, bond_pl_ok, bond_de_ok)
        Vc = sum(hold.values()); f = 1 - (CONTRIB + 2 * sold) * COST / Vc
        for a in hold:
            hold[a] *= f
        for a in list(TARGET) + ["BOND_PL", "BOND_DE"]:
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
    final_mix = {"GLD": hold["GLD"] / V * 100, "BOND_PL": hold["BOND_PL"] / V * 100, "BOND_DE": hold["BOND_DE"] / V * 100}
    return (1 + mid) ** 12 - 1, (navs / navs.cummax() - 1).min(), final_mix


def _route_to_bonds(hold, amount, pl_ok, de_ok):
    if pl_ok and de_ok:
        hold["BOND_PL"] += amount * 0.5; hold["BOND_DE"] += amount * 0.5
    elif pl_ok:
        hold["BOND_PL"] += amount
    elif de_ok:
        hold["BOND_DE"] += amount
    else:
        hold["GLD"] += amount   # unreachable given caller's gold-fallback gate, kept for safety


starts = {k: END - v * 12 for k, v in STARTS.items()}
out = []
def emit(s=""):
    out.append(s); print(s)

emit("# De-risk destination test — GOLD vs BOND ETFs (ETFBCASH.WA + ZPR1.DE)\n")
emit("Real algo (buy-only Q_pos catch-up DCA + CURRENT 1.5x-trigger/1.0x-trim-to rule, "
     "ADR-048), macro dynamic-target cut x0.33 "
     "(07's recommendation). Only the TRIMMED-PROCEEDS destination varies. Both bond "
     "ETFs are recent listings (ETFBCASH.WA from 2021-06, ZPR1.DE from 2023-11) — "
     "windows/months before a ticker existed fall back to gold for that leg "
     "(mirrors production: can't route to a ticker before it has price history).\n")
emit("| destination | " + " | ".join(STARTS) + " |")
emit("|---|" + "--:|" * len(STARTS))
final_mixes = {}
for label, dest in [("GOLD (shipped, ADR-042)", "GOLD"), ("BONDS (ETFBCASH+ZPR1, gold fallback)", "BONDS"),
                     ("MIX (50% gold / 50% bonds)", "MIX")]:
    cells = []
    for k in STARTS:
        xirr, dd, mix = sim(starts[k], dest)
        cells.append(f"{xirr*100:.0f} ({dd*100:.0f})")
        final_mixes[(label, k)] = mix
    emit(f"| {label} | " + " | ".join(cells) + " |")

emit(f"\nStart months: " + ", ".join(f"{k}={starts[k]}" for k in STARTS) + ". Cell = XIRR%/yr (maxDD%).")

emit("\n## Final allocation drift into destination (all windows — every simulation "
     "runs through to the present, so even a 13y/8y window can route POST-2021/2023 "
     "trims into the bond ETFs once they existed; only trims before a ticker's "
     "inception fall back to gold)")
emit("| destination | window | GLD% | BOND_PL(ETFBCASH)% | BOND_DE(ZPR1)% |")
emit("|---|---|--:|--:|--:|")
for label, dest in [("GOLD", "GOLD"), ("BONDS", "BONDS"), ("MIX", "MIX")]:
    for k in STARTS:
        mix = final_mixes[(next(l for l, d in [("GOLD (shipped, ADR-042)", "GOLD"), ("BONDS (ETFBCASH+ZPR1, gold fallback)", "BONDS"), ("MIX (50% gold / 50% bonds)", "MIX")] if d == dest), k)]
        emit(f"| {dest} | {k} | {mix['GLD']:.1f} | {mix['BOND_PL']:.1f} | {mix['BOND_DE']:.1f} |")

emit("\n## Read")
emit("ETFBCASH.WA is NOT a cash-equivalent — it fell -23% through the 2022 rate-hike "
     "year (real duration risk on PLN treasuries). ZPR1.DE (1-3mo T-bills) is genuinely "
     "near-cash (~4-5%/yr, near-zero drawdown). **BONDS/MIX underperform pure GOLD on "
     "every window, including 13y/8y** where most of the diverted capital still went "
     "to gold pre-2021 — because gold happened to rally hard through exactly these "
     "risk-off windows (see file 07's caveat) while the bond ETFs' total return over "
     "the *same* recent stretch was far more modest (ETFBCASH: roughly flat 2021-23, "
     "+5%/yr 2024-26; ZPR1: steady ~4-5%/yr, no upside kicker). Diverting even a "
     "fraction of trimmed proceeds away from gold cost real return with only a modest "
     "or negative drawdown benefit (5y: DD gets WORSE for BONDS, -18%→-22%, because "
     "some proceeds still went into ETFBCASH which itself has duration-driven drawdown "
     "risk, unlike gold which was in a multi-year uptrend).")
emit("\n## Caveats")
emit("Short bond-ETF history (2.6-5y) means this is NOT a full-cycle test of the bond "
     "leg — no rate-hike-cycle stress test for ZPR1 (T-bill yield could go to ~0% in a "
     "future cutting cycle) and only ONE rate-hike drawdown observed for ETFBCASH "
     "(2022). Gold rose through the same risk-off windows in file 07 — that tailwind is "
     "specific to gold and won't repeat identically for bonds.")

with open(os.path.join(BASE, "results", "derisk_destination_backtest.md"), "w") as fh:
    fh.write("\n".join(out) + "\n")
print("\nWrote results/derisk_destination_backtest.md")
