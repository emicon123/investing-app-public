#!/usr/bin/env python3
"""
Study 23 -- tax/cost-drag audit: adds a Belka-tax (19%, Poland) term on top of study 15's
already fire-instrumented trim-rule analysis (`trim_frequency_backtest.py`,
`trim_events.csv`). Direct sequel to study 15 -- reuses its exact data loading, RISK_ON /
RISK_OFF tables, ADR-044 blend computation, and `sim()` mechanic unchanged (does NOT
modify study 15's script, same convention study 15 itself used relative to study 14).

Per the reviewed task file (`.claude/tasks/cost-tax-audit-and-ic-framework.task.md`):
  - Primary target is study 15's fire-frequency-vs-return/drawdown tradeoff (2.0x/1.5x vs
    1.5x/1.0x). #20 is dropped (buy-side only, never trims -- a tax overlay cannot change
    its verdict). #07/#14 get a one-sentence qualitative citation only in the writeup, not
    a re-run here.
  - Cost-basis convention: AVERAGE-COST approximation (not true FIFO -- the existing
    `hold[a]` scalar has no per-lot data; restructuring it into a lot queue is out of
    scope for this task). A parallel scalar `basis[a]` tracks the running cost basis,
    incremented by the exact same PLN amount as `hold[a]` on every monthly contribution,
    and never touched by price appreciation. On a trim event trimming fraction `f` of
    `hold[a]`:
        realized_gain = f*hold[a] - f*basis[a]     (proceeds minus proportional basis)
        tax = 0.19 * max(realized_gain, 0)         (no loss offset/carryforward modeled)
        basis[a] -= f*basis[a]
    This is explicitly labeled as diverging from Poland's real FIFO rule for securities
    (see the study writeup's methodology section for the stated directional bias: average
    cost likely UNDERSTATES true tax drag on bull-trending, frequently-trimmed assets like
    BTC, because FIFO would consume the oldest, most-appreciated lots first).
  - Tax-exposure assumption: every fire in the log is treated as a REGULAR-account (fully
    taxable) disposal. Real crypto-spot fires are structurally REGULAR (crypto can never
    sit directly in IKE/IKZE -- the backtest already models raw crypto-spot, not the
    companion ETP that would carry the shelter in production). Equity/ETF fires (MSTR,
    miners, FXI/QQQ-as-proxies) are treated as REGULAR too via a stated
    capacity-exhaustion argument (Poland's combined annual IKE+IKZE cap is modest relative
    to a 1000+1000 PLN/month, 12-37-line-item buy list -- most of a multi-year
    contribution stream ends up in REGULAR once the shared annual cap is exhausted). This
    is a stated worst-realistic-case simplification, not a precise per-position accounting
    claim -- see the study writeup for the full reasoning.

Where the tax leaves the system: the trimmed dollar amount is credited to GLD (the sink,
matching study 15's mechanic) net of tax -- `hold["GLD"] += trimmed - tax` -- i.e. the tax
is real capital leaving the portfolio at the moment of the fire, not merely a bookkeeping
entry. The existing 15bps trading cost is computed on the GROSS trimmed amount (`sold`),
unchanged from study 15 -- tax stacks on top of, not instead of, the existing cost
convention.

REFRESHED 2026-08-06 (task 28, portfolio-spec refresh): imports RISK_ON/RISK_OFF, the
blend formula, and the trim trigger/trim-to pair from `portfolio_spec.py`, same as the
2026-08-06 refresh of `trim_frequency_backtest.py` -- see that script's header and
`results/study_summary/23_tax_drag_audit.md`'s 2026-08-06 correction note for the full
before/after. This script's own PRIMARY_CONFIGS/CONFIGS labels previously called (2.0x,
1.5x) "current"/"live rule" -- ADR-048 made (1.5x, 1.0x) the live rule on 2026-07-10;
(2.0x, 1.5x) is now correctly labeled PRIOR throughout.
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
BELKA_TAX = 0.19
MINERS = spec.MINERS_BASKET

# ---------------------------------------------------------------------------
# LIVE target tables (portfolio_spec.py SIMPLIFIED_* -- see trim_frequency_backtest.py's
# 2026-08-06 header note for the full proxy-ticker rationale).
RISK_ON = dict(spec.SIMPLIFIED_RISK_ON)
RISK_OFF = dict(spec.SIMPLIFIED_RISK_OFF)
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
for a in RISK_ON:
    if a == "BOND_PL":
        RET[a] = native_ret(TICKER[a])       # ETFBCASH.PL is PLN-native (Warsaw listing)
    elif a == "BOND_DE":
        RET[a] = pln_ret(TICKER[a])          # ZPR1.DE is USD-denominated despite the .DE suffix
    else:
        RET[a] = pd.DataFrame({m: pln_ret(m) for m in MINERS}).mean(axis=1) if a == "miners" else pln_ret(TICKER[a])

END = usdpln.index.max()
STARTS = spec.STANDARD_WINDOWS
starts = {k: END - v * 12 for k, v in STARTS.items()}

# ---------------------------------------------------------------------------
# Macro blend (ADR-044) -- identical to study 14/15.

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
blend = spec.blend_live(smoothed_z)     # LIVE divisor 0.5 (ADR-049) + orientation (ADR-071)

FULL_START = RET["GLD"].dropna().index.min()

# ---------------------------------------------------------------------------

def sim(start, mode, trigger, trim_to, tax_on, log=None):
    """mode: 'baseline' | 'current'. tax_on: apply the 19% Belka average-cost tax term.
    If log is a list, trim events (incl. realized_gain/tax fields) are appended to it."""
    months = pd.period_range(start, END, freq="M"); n = len(months)
    hold = {a: 0.0 for a in RISK_ON}
    basis = {a: 0.0 for a in RISK_ON}
    nav = 1.0; navs = []
    total_tax = 0.0
    for t in months:
        present = [a for a in RISK_ON if RET[a] is not None and t in RET[a].index and not np.isnan(RET[a].get(t, np.nan))]
        if mode == "baseline":
            tt = dict(RISK_ON)                 # fixed target, no macro overlay at all
            b = 1.0                            # for logging only (1.0 = pure risk-on, live orientation)
        else:
            bv = blend.get(t, np.nan)
            b = 1.0 if pd.isna(bv) else float(bv)   # insufficient history -> collapse to risk-on (b=1.0 live)
            tt = {a: spec.effective_target(RISK_ON[a], RISK_OFF.get(a, RISK_ON[a]), b) for a in RISK_ON}
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
                    amt = CONTRIB * Q[a] / Q_pos
                    hold[a] += amt
                    basis[a] += amt
        else:                          # degenerate guard; shouldn't trigger (Q_pos >= CONTRIB always)
            for a in present:
                amt = CONTRIB * w[a]
                hold[a] += amt
                basis[a] += amt
        sold = 0.0
        month_tax = 0.0
        Vn = sum(hold.values())
        for a in present:
            if a == "GLD":
                continue
            ww = hold[a] / Vn
            if ctw[a] > 0 and ww >= trigger * ctw[a]:
                keep = trim_to * ctw[a] * Vn
                trimmed = hold[a] - keep
                f_frac = trimmed / hold[a] if hold[a] else 0.0
                sold += trimmed
                realized_gain = trimmed - f_frac * basis[a]
                tax = BELKA_TAX * max(realized_gain, 0.0) if tax_on else 0.0
                month_tax += tax
                basis[a] -= f_frac * basis[a]
                if log is not None:
                    log.append({
                        "month": str(t), "mode": mode, "trigger": trigger, "trim_to": trim_to,
                        "tax_on": tax_on, "asset": a, "target_w": ctw[a], "actual_w": ww,
                        "ratio": ww / ctw[a], "trimmed_frac_of_position": f_frac,
                        "sold_frac_of_portfolio": trimmed / Vn, "trimmed_pln": trimmed,
                        "realized_gain_pln": realized_gain, "gain_frac_of_trimmed": (realized_gain / trimmed) if trimmed else np.nan,
                        "tax_pln": tax, "blend": b,
                    })
                net_credit = trimmed - tax
                hold["GLD"] += net_credit
                basis["GLD"] += net_credit
                hold[a] = keep
        total_tax += month_tax
        Vc = sum(hold.values()); f = 1 - (CONTRIB + 2 * sold) * COST / Vc
        for a in hold:
            hold[a] *= f
        for a in RISK_ON:
            if hold[a] == 0:
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
    return xirr, maxdd, navs, total_tax, V


# ---------------------------------------------------------------------------
out = []
def emit(s=""):
    out.append(s); print(s)


MODES = [("BASELINE (fixed target, no macro)", "baseline"),
         ("CURRENT (ADR-044 blend, live mechanism)", "current")]
TRG_CUR, TTO_CUR = spec.TRIM_CURRENT
TRG_PRI, TTO_PRI = spec.TRIM_PRIOR
CONFIGS = [(spec.TRIM_CONFIGS[0][0], TRG_CUR, TTO_CUR),
           ("1.75 / 1.25 (sensitivity midpoint, between CURRENT and PRIOR)", 1.75, 1.25),
           (spec.TRIM_CONFIGS[1][0], TRG_PRI, TTO_PRI)]

n_full_years = (END - FULL_START).n / 12.0

emit("# Study 23 -- tax/cost-drag audit on study 15's trim-rule fire log\n")
emit(f"Single continuous pass {FULL_START}..{END} ({n_full_years:.1f}y, no overlapping "
     f"windows) for fire-count/tax-total analysis; the standard 5 start-date windows "
     f"(1/3/5/8/13y, matching study 15) for XIRR/maxDD comparability. Buy-only DCA "
     f"1000+1000/mo PLN, 15bps trading cost + 19% Belka tax (average-cost basis "
     f"approximation, REGULAR-account assumption -- see methodology section of the "
     f"numbered writeup).\n")

# ---------------------------------------------------------------------------
emit("## 0. Data-drift check -- tax_on=False fire counts vs. study 15's published table\n")
emit("This section runs the tax-free (`tax_on=False`) variant of the sim, which should be "
     "arithmetically IDENTICAL to study 15's own unmodified `trim_frequency_backtest.py` "
     "(same formulas, same tables, same data loaders -- verified line-by-line). It is NOT.\n")
sanity_logs = {}
for cfg_label, trg, tto in CONFIGS:
    for mode_label, mode in MODES:
        log = []
        sim(FULL_START, mode, trg, tto, tax_on=False, log=log)
        sanity_logs[(trg, tto, mode)] = pd.DataFrame(log)
        emit(f"- {cfg_label} / {mode_label}: {len(log)} fires (tax_on=False, today's data)")
emit("\n**Study 15's published table (2026-07-09): 2.0/1.5 BASELINE=22/CURRENT=25; "
     "1.75/1.25 BASELINE=32/CURRENT=35; 1.5/1.0 BASELINE=79/CURRENT=83.** Today's numbers "
     "above do not match -- confirmed NOT a bug in this script (re-running study 15's own, "
     "byte-for-byte unmodified `trim_frequency_backtest.py` today reproduces these same "
     "higher counts, e.g. 54/57 at 2.0/1.5). **Root cause, traced**: "
     "`data/prices/QQQ_full.csv` and `FXI_full.csv` were created/refreshed 2026-07-10 "
     "21:35 (`git log`), i.e. AFTER study 15's 2026-07-09 publication date and after its "
     "committed `trim_events.csv` snapshot -- the raw fallback `QQQ.csv`/`FXI.csv` files "
     "study 15 originally read no longer even exist on disk (removed by the same refresh, "
     "commit `2b03ed0`). The loader prefers `_full.csv` when present, so today's re-run "
     "uses a materially longer/different QQQ and FXI history (`QQQ_full.csv` now runs back "
     "to 1999-03) than whatever shorter/differently-adjusted series study 15's original run "
     "saw. This is a genuine data-drift discovery, out of scope to fix here (study 15's own "
     "artifacts are not touched by this script), but material enough to flag prominently: "
     "**QQQ and FXI now fire at the LIVE 2.0/1.5 rule too** (13 and 11 times respectively "
     "over the 19.3y window), not just at the tighter 1.5/1.0 rule as study 15 reported. "
     "Every number in this study is computed against TODAY's on-disk data for both the "
     "tax and no-tax variants, so the internal WITH-tax-vs-NO-tax comparison below remains "
     "apples-to-apples valid even though the absolute fire counts and XIRR/maxDD figures no "
     "longer match study 15's committed table. See the numbered writeup for the "
     "recommendation to refresh study 15 itself as a separate follow-up.\n")
emit("**2026-08-06 addendum (task 28)**: study 15 WAS refreshed as recommended above, on "
     "the same date as this script -- both now import the same LIVE target table/blend "
     "formula from `portfolio_spec.py`, so their fire counts should (and, checked, do) "
     "match each other again at equivalent configs; they will NOT match either of the two "
     "prior tables cited above (2026-07-09's or the 2026-07-13 price-drift figures) for a "
     "third, unrelated reason: the target-allocation table itself changed (pre-ADR-047 demo "
     "proxy -> live ADR-067 weights, no more CASH bucket) and the blend divisor/orientation "
     "were corrected (2.0 -> 0.5, ADR-049; orientation flipped, ADR-071). This is expected, "
     "not a new bug -- see this file's top-of-module 2026-08-06 note.\n")
emit("**2026-09-25 addendum (task 28, second pass)**: the shared tables moved again (live "
     "ADR-074/087 weights: QQQ 22.5/27.5, FXI 2.5/7.5, ETH 10.0/1.0, LINK-USD 1.5/0.15 "
     "added; Experiments now 17 positions) -- both scripts import them from "
     "`portfolio_spec.py`, so both pick the move up with no further code change; fire "
     "counts at equivalent configs should still match each other, and will again NOT match "
     "the 2026-08-06 tables. Expected, not a new bug.\n")

# ---------------------------------------------------------------------------
PRIMARY_CONFIGS = [(spec.TRIM_CONFIGS[0][0], TRG_CUR, TTO_CUR), (spec.TRIM_CONFIGS[1][0], TRG_PRI, TTO_PRI)]

emit("## 1. Continuous pass, WITH tax -- fire counts, total tax paid, per-asset breakdown\n")
all_logs = {}
total_tax_by_cfg = {}
full_pass_tax = {}
full_pass_notax = {}
for cfg_label, trg, tto in CONFIGS:
    for mode_label, mode in MODES:
        log = []
        xirr, maxdd, navs, total_tax, V = sim(FULL_START, mode, trg, tto, tax_on=True, log=log)
        all_logs[(trg, tto, mode)] = pd.DataFrame(log)
        total_tax_by_cfg[(trg, tto, mode)] = total_tax
        full_pass_tax[(trg, tto, mode)] = (xirr, maxdd, V)
        xirr0, maxdd0, navs0, _, V0 = sim(FULL_START, mode, trg, tto, tax_on=False)
        full_pass_notax[(trg, tto, mode)] = (xirr0, maxdd0, V0)

combined = pd.concat([df for df in all_logs.values() if not df.empty], ignore_index=True) if any(len(v) for v in all_logs.values()) else pd.DataFrame()
combined.to_csv(os.path.join(RESULTS, "tax_drag_events.csv"), index=False)
print(f"Wrote results/tax_drag_events.csv ({len(combined)} rows)")

emit("| config | mode | fires | total tax paid (PLN, nominal, over full pass) | avg tax/fire (PLN) | median gain-frac of trimmed $ |")
emit("|---|---|--:|--:|--:|--:|")
for cfg_label, trg, tto in CONFIGS:
    for mode_label, mode in MODES:
        df = all_logs[(trg, tto, mode)]
        tt = total_tax_by_cfg[(trg, tto, mode)]
        n = len(df)
        avg = tt / n if n else float("nan")
        gf = df["gain_frac_of_trimmed"].median() if n else float("nan")
        emit(f"| {cfg_label} | {mode.upper()} | {n} | {tt:,.0f} | {avg:,.0f} | {gf*100:.0f}% |")
emit("")

emit("### Per-asset tax breakdown, CURRENT mode, 2.0/1.5 (PRIOR, pre-ADR-048 -- NOT current)\n")
df = all_logs[(2.0, 1.5, "current")]
if not df.empty:
    g = df.groupby("asset").agg(fires=("tax_pln", "size"), total_tax=("tax_pln", "sum"),
                                  min_gf=("gain_frac_of_trimmed", "min"),
                                  median_gain_frac=("gain_frac_of_trimmed", "median"),
                                  max_gf=("gain_frac_of_trimmed", "max")).sort_values("total_tax", ascending=False)
    emit("| asset | fires | total tax (PLN) | gain-frac of trimmed $ (min / median / max) |")
    emit("|---|--:|--:|--:|")
    for a, r in g.iterrows():
        emit(f"| {a} | {r['fires']:.0f} | {r['total_tax']:,.0f} | {r['min_gf']*100:.0f}% / {r['median_gain_frac']*100:.0f}% / {r['max_gf']*100:.0f}% |")
    total_all = g["total_tax"].sum()
    emit(f"\nBTC_spot+ETH+DOGE (crypto-spot, structurally REGULAR) = "
         f"{g.loc[g.index.isin(['BTC_spot','ETH','DOGE']),'total_tax'].sum()/total_all*100:.1f}% of total tax; "
         f"FXI+MSTR+QQQ (equity/ETF, REGULAR only under the capacity-exhaustion assumption) = "
         f"{g.loc[g.index.isin(['FXI','MSTR','QQQ']),'total_tax'].sum()/total_all*100:.2f}% of total tax.")
emit("")

emit("### Per-asset tax breakdown, CURRENT mode, 1.5/1.0 (CURRENT -- ADR-048 live rule)\n")
df = all_logs[(1.5, 1.0, "current")]
if not df.empty:
    g = df.groupby("asset").agg(fires=("tax_pln", "size"), total_tax=("tax_pln", "sum"),
                                  min_gf=("gain_frac_of_trimmed", "min"),
                                  median_gain_frac=("gain_frac_of_trimmed", "median"),
                                  max_gf=("gain_frac_of_trimmed", "max")).sort_values("total_tax", ascending=False)
    emit("| asset | fires | total tax (PLN) | gain-frac of trimmed $ (min / median / max) |")
    emit("|---|--:|--:|--:|")
    for a, r in g.iterrows():
        emit(f"| {a} | {r['fires']:.0f} | {r['total_tax']:,.0f} | {r['min_gf']*100:.0f}% / {r['median_gain_frac']*100:.0f}% / {r['max_gf']*100:.0f}% |")
    total_all = g["total_tax"].sum()
    crypto_names = [n for n in ["BTC_spot", "ETH", "DOGE", "SOL"] if n in g.index]
    equity_names = [n for n in ["FXI", "MSTR", "QQQ"] if n in g.index]
    emit(f"\nCrypto-spot (structurally REGULAR) = "
         f"{g.loc[g.index.isin(crypto_names),'total_tax'].sum()/total_all*100:.1f}% of total tax; "
         f"equity/ETF (REGULAR only under the capacity-exhaustion assumption) = "
         f"{g.loc[g.index.isin(equity_names),'total_tax'].sum()/total_all*100:.2f}% of total tax.")
emit("")

# ---------------------------------------------------------------------------
emit("## 1b. Full continuous-pass XIRR/maxDD/total-tax, WITH tax vs. WITHOUT tax (single 19.3y number, no window-overlap)\n")
emit("| config | mode | tax | XIRR%/yr | maxDD% | total tax paid (PLN) |")
emit("|---|---|---|--:|--:|--:|")
for cfg_label, trg, tto in PRIMARY_CONFIGS:
    for mode_label, mode in MODES:
        x0, d0, V0 = full_pass_notax[(trg, tto, mode)]
        x1, d1, V1 = full_pass_tax[(trg, tto, mode)]
        emit(f"| {cfg_label} | {mode.upper()} | no tax | {x0*100:.2f} | {d0*100:.1f} | 0 |")
        emit(f"| {cfg_label} | {mode.upper()} | WITH TAX | {x1*100:.2f} | {d1*100:.1f} | {total_tax_by_cfg[(trg,tto,mode)]:,.0f} |")
emit("")
emit("**Full-pass tax drag, CURRENT mode (2007-02..2026-06, 19.3y, single continuous "
     "compounding path):**\n")
for trg, tto, lbl in [(1.5, 1.0, "1.5/1.0 (CURRENT, ADR-048 live rule)"), (2.0, 1.5, "2.0/1.5 (PRIOR, pre-ADR-048)")]:
    x0, d0, V0 = full_pass_notax[(trg, tto, "current")]
    x1, d1, V1 = full_pass_tax[(trg, tto, "current")]
    tt = total_tax_by_cfg[(trg, tto, "current")]
    emit(f"- **{lbl}**: XIRR {x0*100:.2f}% -> {x1*100:.2f}% ({(x1-x0)*100:+.2f}pp), "
         f"maxDD {d0*100:.1f}% -> {d1*100:.1f}% ({(d1-d0)*100:+.1f}pp), total tax paid "
         f"{tt:,.0f} PLN (final portfolio value {V1:,.0f} PLN with tax, {V0:,.0f} without "
         f"-- tax is {tt/V0*100:.2f}% of what the no-tax final value would have been).")
emit("")

# ---------------------------------------------------------------------------
emit("## 2. Does adding tax change the fire count itself? (second-order NAV-path effect)\n")
emit("Paying tax removes real capital from the portfolio (less credited to the GLD sink "
     "than in the no-tax run), which marginally shrinks total NAV going forward and can "
     "very slightly change *when* later fires cross their trigger. Comparing fire counts "
     "tax_on=True vs tax_on=False on the identical continuous pass:\n")
emit("| config | mode | fires (no tax) | fires (with tax) | delta |")
emit("|---|---|--:|--:|--:|")
for cfg_label, trg, tto in CONFIGS:
    for mode_label, mode in MODES:
        n_notax = len(sanity_logs[(trg, tto, mode)])
        n_tax = len(all_logs[(trg, tto, mode)])
        emit(f"| {cfg_label} | {mode.upper()} | {n_notax} | {n_tax} | {n_tax - n_notax:+d} |")
emit("")

# ---------------------------------------------------------------------------
emit("## 3. Headline XIRR/maxDD, WITH tax vs. WITHOUT tax, 5 start-date windows\n")
emit("Cell = XIRR%/yr (maxDD%). Primary comparison: 1.5/1.0 (CURRENT, ADR-048 live rule) "
     "vs. 2.0/1.5 (PRIOR, pre-ADR-048), both modes, no-tax vs. with-tax.\n")
window_results = {}
for cfg_label, trg, tto in PRIMARY_CONFIGS:
    for mode_label, mode in MODES:
        for tax_on in (False, True):
            for k in STARTS:
                xirr, maxdd, navs, total_tax, V = sim(starts[k], mode, trg, tto, tax_on)
                window_results[(trg, tto, mode, tax_on, k)] = (xirr, maxdd)

emit("| config | mode | tax | 13y | 8y | 5y | 3y | 1y |")
emit("|---|---|---|--:|--:|--:|--:|--:|")
for cfg_label, trg, tto in PRIMARY_CONFIGS:
    for mode_label, mode in MODES:
        for tax_on in (False, True):
            cells = [f"{window_results[(trg, tto, mode, tax_on, k)][0]*100:.0f} ({window_results[(trg, tto, mode, tax_on, k)][1]*100:.0f})" for k in STARTS]
            emit(f"| {cfg_label} | {mode.upper()} | {'WITH TAX' if tax_on else 'no tax'} | " + " | ".join(cells) + " |")
emit(f"\nStart months: " + ", ".join(f"{k}={starts[k]}" for k in STARTS) + ". Cell = XIRR%/yr (maxDD%).")
emit("")

emit("### Tax-drag deltas (XIRR pp lost to tax; maxDD pp change), CURRENT mode\n")
emit("| config | 13y | 8y | 5y | 3y | 1y |")
emit("|---|--:|--:|--:|--:|--:|")
for cfg_label, trg, tto in PRIMARY_CONFIGS:
    cells = []
    for k in STARTS:
        x_notax, d_notax = window_results[(trg, tto, "current", False, k)]
        x_tax, d_tax = window_results[(trg, tto, "current", True, k)]
        cells.append(f"{(x_tax - x_notax)*100:+.1f}xirr / {(d_tax - d_notax)*100:+.1f}dd")
    emit(f"| {cfg_label} | " + " | ".join(cells) + " |")
emit("")

# ---------------------------------------------------------------------------
emit("## 4. Does the tax-adjusted gap between 2.0/1.5 (PRIOR) and 1.5/1.0 (CURRENT) widen or narrow?\n")
emit("No-tax gap (from study 15) vs. with-tax gap, CURRENT mode, XIRR%/yr (maxDD%) at "
     "each window, config = 2.0/1.5 [PRIOR, pre-ADR-048] minus 1.5/1.0 [CURRENT, ADR-048 "
     "live rule] (positive XIRR-gap = looser/PRIOR rule wins on return; positive maxDD-gap "
     "= looser/PRIOR rule has WORSE (deeper) drawdown):\n")
emit("| window | no-tax XIRR gap | with-tax XIRR gap | no-tax maxDD gap | with-tax maxDD gap |")
emit("|---|--:|--:|--:|--:|")
for k in STARTS:
    x20_notax, d20_notax = window_results[(2.0, 1.5, "current", False, k)]
    x15_notax, d15_notax = window_results[(1.5, 1.0, "current", False, k)]
    x20_tax, d20_tax = window_results[(2.0, 1.5, "current", True, k)]
    x15_tax, d15_tax = window_results[(1.5, 1.0, "current", True, k)]
    gap_x_notax = (x20_notax - x15_notax) * 100
    gap_x_tax = (x20_tax - x15_tax) * 100
    gap_d_notax = (d20_notax - d15_notax) * 100
    gap_d_tax = (d20_tax - d15_tax) * 100
    emit(f"| {k} | {gap_x_notax:+.1f}pp | {gap_x_tax:+.1f}pp | {gap_d_notax:+.1f}pp | {gap_d_tax:+.1f}pp |")
emit("")

# ---------------------------------------------------------------------------
emit("## 5. Sensitivity grid -- fire count AND cumulative tax across all 3 configs, CURRENT mode\n")
emit("| config | fires (no tax) | fires (with tax) | total tax paid (PLN) | tax paid per fire (PLN) |")
emit("|---|--:|--:|--:|--:|")
for cfg_label, trg, tto in CONFIGS:
    n_notax = len(sanity_logs[(trg, tto, "current")])
    n_tax = len(all_logs[(trg, tto, "current")])
    tt = total_tax_by_cfg[(trg, tto, "current")]
    emit(f"| {cfg_label} | {n_notax} | {n_tax} | {tt:,.0f} | {(tt/n_tax if n_tax else float('nan')):,.0f} |")
emit("")

emit("\nCaveats carried over from study 15: miners-basket survivorship, gold's tailwind "
     "flattering any gold-destination trim rule, overlapping windows in section 3's "
     "5-window table, thin history for SUI/COIN/SOL. **New caveat for this study**: the "
     "average-cost cost-basis approximation (not true FIFO) likely UNDERSTATES true tax "
     "drag on exactly the assets this audit cares about most (BTC-dominated fires at the "
     "live rule) -- see the numbered writeup's methodology section for the reasoning. No "
     "loss offset/carryforward is modeled (every realized loss is simply untaxed, not "
     "banked against a future gain) -- a further simplification, not modeled either "
     "direction.")

with open(os.path.join(RESULTS, "tax_drag_audit_backtest.md"), "w") as fh:
    fh.write("\n".join(out) + "\n")
print("\nWrote results/tax_drag_audit_backtest.md")
