#!/usr/bin/env python3
"""
Study 15 -- trim-rule (the "+100% rule") frequency & depth audit, plus a tighter
1.5x-trigger/1.0x-trim-to alternative. Direct sequel to study 14
(portfolio_macro_blend_backtest.py) -- reuses the exact same data loading, RISK_ON /
RISK_OFF tables, and ADR-044 blend computation so results stay apples-to-apples
comparable to study 14's published numbers. Does NOT modify study 14's script.

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
applied to study 16 (classic_portfolios_backtest.py); the fixed-split
approximation this replaces materially understated real-world drift, which in
turn understated how often the trim rule fires -- see the correction note at the
top of results/study_summary/15_trim_frequency_and_depth.md for how the numbers
moved.

Two headline questions (per task brief):
  1. How often does the 2x-trigger/1.5x-trim-to rule actually fire, historically, per
     asset -- is waiting for a 2x overshoot rare or common?
  2. What if we loosen it to fire at 1.5x and trim all the way back to 1.0x (exact
     target)? Compare against the current rule on both return/drawdown (5-window
     XIRR/maxDD, same format as study 14) and fire-frequency.

Plus (additive, per orchestrator's "squeeze the lemon" follow-up):
  3. Overshoot-at-fire distribution (monthly granularity can overshoot the trigger).
  4. Regime clustering -- do CURRENT-mode fires cluster inside the sustained
     MID_OFF/RISK_OFF episodes already identified in study 14, vs. firing in ordinary
     months from pure price growth (quantifies how much of CURRENT's extra firing over
     BASELINE is target-contraction-driven vs. price-driven)?
  5. Whipsaw/thrashing check -- does the tighter 1.5/1.0 rule cause assets to re-fire in
     quick succession more than the current 2.0/1.5 rule (cost-side consideration)?
  6. A light 3-point sensitivity grid (2.0/1.5, 1.75/1.25, 1.5/1.0) for a fire-frequency
     vs. return/drawdown tradeoff curve.

Two passes per (TRIGGER, TRIM_TO) config:
  A. A SINGLE CONTINUOUS PASS over the longest available history (not the 5 overlapping
     DCA windows) for BASELINE and CURRENT modes -- gives a clean, non-overlapping fire
     count for frequency analysis.
  B. The standard 5 start-date windows (1/3/5/8/13y) for XIRR%/yr and maxDD%, matching
     study 14's table format, for the rule endpoints tested.

REFRESHED 2026-08-06 (task 28, portfolio-spec refresh): imports RISK_ON/RISK_OFF, the
blend formula (divisor + orientation), and the trim trigger/trim-to pair from the new
`portfolio_spec.py` (single source of truth) instead of hardcoding its own stale copies.
RE-REFRESHED 2026-09-25 (task 28, second pass): the shared tables moved again (live
ADR-074/087 weights: QQQ 22.5/27.5, FXI 2.5/7.5, ETH 10.0/1.0, LINK-USD 1.5/0.15 added;
see portfolio_spec.py) -- this script imports them, so it picks the move up with no
further code change. Three things changed vs. this script's prior form -- see
`results/study_summary/15_trim_frequency_and_depth.md`'s 2026-08-06 correction note for
the full before/after: (1) blend divisor 2.0 -> 0.5 (ADR-049) and orientation flipped to
the live 0=risk-off/1=risk-on convention (ADR-071); (2) target table updated from the
pre-ADR-047 demo proxy (Gold flat 30/30, CASH residual sink, MSTR a major line item) to
the live ADR-067 weights (Gold 20/30, no CASH bucket at all, Bonds a new risk-off-only
sink, Equity absorbing what CASH used to); (3) **the headline trim-trigger mislabeling
this task exists to fix**: this script's own CONFIGS list and every "CURRENT rule" header
below used to call (2.0x, 1.5x) "current" -- ADR-048 made (1.5x, 1.0x) the live rule on
2026-07-10, three weeks before this refresh. (2.0x, 1.5x) is now correctly labeled PRIOR.
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
MINERS = spec.MINERS_BASKET

# ---------------------------------------------------------------------------
# LIVE target tables (portfolio_spec.py SIMPLIFIED_* -- see that module for the full
# derivation/citation). GLD/QQQ/FXI are long-history proxies for the real Gold/
# NQSE.DE/ICGA.DE positions (AGENTS.md's standing convention); BOND_PL/BOND_DE are the
# real ETFBCASH.PL/ZPR1.DE tickers (risk-off-only, new since ADR-050).
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
def pln_ret(t):                      # USD-denominated asset -> PLN return
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
# Macro blend (ADR-044) -- identical to study 14.

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
# LIVE divisor (0.5, ADR-049) + LIVE orientation (0=risk-off, 1=risk-on, ADR-071) --
# was `(0.5 - smoothed_z/2.0).clip(0,1)` (divisor 2.0, 0=risk-on/1=risk-off) before this
# refresh. See portfolio_spec.py's orientation note for the blend_live=1-blend_old identity.
blend = spec.blend_live(smoothed_z)

ZONE_BINS = spec.ZONE_BINS
ZONE_LABELS = spec.ZONE_LABELS_LIVE     # live (ADR-071): 0=RISK_OFF .. 1=RISK_ON


def zone_of(b):
    return pd.cut(b, bins=ZONE_BINS, labels=ZONE_LABELS)


# Sustained MID_OFF/RISK_OFF episodes (blend<=0.4 in the LIVE orientation -- equivalent
# to the old blend_old>=0.6 threshold, since blend_live = 1-blend_old; same episode set,
# just expressed on the new axis), 3+ consecutive months -- recomputed with the exact
# same logic study 14 used, so it reproduces that study's episode table.
full_blend = blend.dropna()
off_mask = full_blend <= 0.4
EPISODES = []
cur_start = None
for i, (t, v) in enumerate(off_mask.items()):
    if v and cur_start is None:
        cur_start = t
    if not v and cur_start is not None:
        EPISODES.append((cur_start, full_blend.index[i - 1])); cur_start = None
if cur_start is not None:
    EPISODES.append((cur_start, full_blend.index[-1]))
EPISODES = [(s, e) for s, e in EPISODES if (e - s).n >= 2]


def in_episode(t):
    return any(s <= t <= e for s, e in EPISODES)


# ---------------------------------------------------------------------------
# Longest available continuous history: earliest month GLD (the one asset present in
# both RISK_ON/RISK_OFF at full weight throughout) has a valid monthly return.
FULL_START = RET["GLD"].dropna().index.min()


def sim(start, mode, trigger, trim_to, log=None):
    """mode: 'baseline' | 'current'. If log is a list, trim events are appended to it."""
    months = pd.period_range(start, END, freq="M"); n = len(months)
    hold = {a: 0.0 for a in RISK_ON}; nav = 1.0; navs = []
    for t in months:
        present = [a for a in RISK_ON if RET[a] is not None and t in RET[a].index and not np.isnan(RET[a].get(t, np.nan))]
        if mode == "baseline":
            tt = dict(RISK_ON)                 # fixed target, no macro overlay at all
            b = 1.0                            # for logging only (1.0 = pure risk-on, live orientation)
        else:
            bv = blend.get(t, np.nan)
            # insufficient blend history -> collapse to risk-on (b=1.0 in the LIVE
            # orientation; was b=0.0 under the old 0=risk-on convention -- same target,
            # different numeral because the axis flipped, see portfolio_spec.py).
            b = 1.0 if pd.isna(bv) else float(bv)
            tt = {a: spec.effective_target(RISK_ON[a], RISK_OFF.get(a, RISK_ON[a]), b) for a in RISK_ON}
        ctw = {a: tt[a] / 100.0 for a in tt}
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
        for a in present:
            if a == "GLD":
                continue
            ww = hold[a] / Vn
            if ctw[a] > 0 and ww >= trigger * ctw[a]:
                keep = trim_to * ctw[a] * Vn
                trimmed = hold[a] - keep
                sold += trimmed
                if log is not None:
                    log.append({
                        "month": str(t), "mode": mode, "trigger": trigger, "trim_to": trim_to,
                        "asset": a, "target_w": ctw[a], "actual_w": ww, "ratio": ww / ctw[a],
                        "trimmed_frac_of_position": trimmed / hold[a] if hold[a] else np.nan,
                        "sold_frac_of_portfolio": trimmed / Vn,
                        "blend": b, "in_sustained_episode": in_episode(t),
                    })
                hold["GLD"] += trimmed; hold[a] = keep
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
    return xirr, maxdd, navs


# ---------------------------------------------------------------------------
out = []
def emit(s=""):
    out.append(s); print(s)


MODES = [("BASELINE (fixed target, no macro)", "baseline"),
         ("CURRENT (ADR-044 blend, live mechanism)", "current")]
# CURRENT/PRIOR/REFERENCE per portfolio_spec.py (ADR-048 made 1.5x/1.0x live 2026-07-10;
# 2.0x/1.5x is now the PRIOR reference, NOT current -- this was this script's own
# mislabeling before the 2026-08-06 refresh, see the header note and the correction note
# in results/study_summary/15_trim_frequency_and_depth.md). 1.75/1.25 kept as a bonus
# sensitivity midpoint between the two, same as before this refresh.
TRG_CUR, TTO_CUR = spec.TRIM_CURRENT
TRG_PRI, TTO_PRI = spec.TRIM_PRIOR
CONFIGS = [(spec.TRIM_CONFIGS[0][0], TRG_CUR, TTO_CUR),
           ("1.75 / 1.25 (sensitivity midpoint, between CURRENT and PRIOR)", 1.75, 1.25),
           (spec.TRIM_CONFIGS[1][0], TRG_PRI, TTO_PRI)]

n_full_years = (END - FULL_START).n / 12.0

emit("# Study 15 -- trim-rule (+100%) frequency & depth audit\n")
emit(f"Single continuous pass {FULL_START}..{END} ({n_full_years:.1f}y, no overlapping windows) "
     f"for the fire-count/frequency analysis; the standard 5 start-date windows "
     f"(1/3/5/8/13y, matching study 14) for XIRR/maxDD comparability. Buy-only DCA "
     f"1000+1000/mo PLN, 15bps.\n")

# All logs, all configs, both modes, single continuous pass.
all_logs = {}
for cfg_label, trg, tto in CONFIGS:
    for mode_label, mode in MODES:
        log = []
        sim(FULL_START, mode, trg, tto, log)
        all_logs[(trg, tto, mode)] = pd.DataFrame(log)

# Dump raw event log (all configs) to csv.
combined = pd.concat([df for df in all_logs.values() if not df.empty], ignore_index=True) if any(len(v) for v in all_logs.values()) else pd.DataFrame()
combined.to_csv(os.path.join(RESULTS, "trim_events.csv"), index=False)
print(f"Wrote results/trim_events.csv ({len(combined)} rows)")

# ---------------------------------------------------------------------------
emit(f"## 1. How often does the CURRENT rule ({TRG_CUR:.1f}x trigger / trim to {TTO_CUR:.1f}x, "
     "ADR-048 live rule) actually fire?\n")
emit(f"Continuous pass, {FULL_START}..{END} ({n_full_years:.1f} years), non-overlapping. "
     f"PRIOR ({TRG_PRI:.1f}x/{TTO_PRI:.1f}x, pre-ADR-048) shown alongside for comparison -- "
     "it is NOT the live rule, kept as a reference point only.\n")

for cfg_label, trg, tto in [(spec.TRIM_CONFIGS[0][0], TRG_CUR, TTO_CUR),
                             (spec.TRIM_CONFIGS[1][0], TRG_PRI, TTO_PRI)]:
    emit(f"### {cfg_label}\n")
    for mode_label, mode in MODES:
        df = all_logs[(trg, tto, mode)]
        emit(f"#### {mode_label}: {len(df)} total fires\n")
        if df.empty:
            emit("(no fires)\n"); continue
        rows = []
        for a, g in df.groupby("asset"):
            months_present = RET[a].dropna().index
            months_present = months_present[(months_present >= FULL_START) & (months_present <= END)]
            years_present = max(len(months_present) / 12.0, 1e-9)
            fire_months = sorted(pd.Period(m) for m in g["month"])
            if len(fire_months) >= 2:
                gaps = [(fire_months[i + 1] - fire_months[i]).n for i in range(len(fire_months) - 1)]
                avg_gap = np.mean(gaps)
            else:
                avg_gap = np.nan
            rows.append({
                "asset": a, "fires": len(g), "pct_of_total": len(g) / len(df) * 100,
                "avg_months_between_fires": avg_gap,
                "fires_per_year_present": len(g) / years_present,
                "years_present": years_present,
            })
        tbl = pd.DataFrame(rows).sort_values("fires", ascending=False)
        emit("| asset | fires | % of total | avg months between fires | implied fires/yr (while held) | years present |")
        emit("|---|--:|--:|--:|--:|--:|")
        for _, r in tbl.iterrows():
            gap = "n/a (fired once)" if np.isnan(r["avg_months_between_fires"]) else f"{r['avg_months_between_fires']:.0f}"
            emit(f"| {r['asset']} | {r['fires']:.0f} | {r['pct_of_total']:.0f}% | {gap} | "
                 f"{r['fires_per_year_present']:.2f} | {r['years_present']:.1f} |")
        emit("")

base_df = all_logs[(TRG_CUR, TTO_CUR, "baseline")]
cur_df = all_logs[(TRG_CUR, TTO_CUR, "current")]
base_df_prior = all_logs[(TRG_PRI, TTO_PRI, "baseline")]
cur_df_prior = all_logs[(TRG_PRI, TTO_PRI, "current")]
emit(f"**BASELINE vs CURRENT, at the CURRENT rule ({TRG_CUR:.1f}/{TTO_CUR:.1f}):** "
     f"{len(base_df)} fires vs {len(cur_df)} fires over the same {n_full_years:.1f}-year "
     f"continuous window ({len(cur_df) - len(base_df):+d} fires under CURRENT-mechanism).\n")
emit(f"**Same comparison at the PRIOR rule ({TRG_PRI:.1f}/{TTO_PRI:.1f}), for reference:** "
     f"{len(base_df_prior)} fires vs {len(cur_df_prior)} fires "
     f"({len(cur_df_prior) - len(base_df_prior):+d}).\n")

# ---------------------------------------------------------------------------
emit("## 2. Overshoot-at-fire distribution (actual/target ratio at the moment of firing)\n")
emit(f"Monthly granularity means a position can already be well past the trigger by the "
     f"time it's checked (it isn't checked continuously). Ratio = actual weight / that "
     f"month's target weight, at the fire event, CURRENT rule = {TRG_CUR:.1f}x trigger:\n")
OVERSHOOT_MULT = 1.25   # same relative severity bar as the pre-refresh script's fixed "ratio>=2.5" at a 2.0x trigger (2.5/2.0=1.25x)
emit(f"| mode | n fires | min | p25 | median | p75 | max | % fires with ratio >= {TRG_CUR * OVERSHOOT_MULT:.2f}x |")
emit("|---|--:|--:|--:|--:|--:|--:|--:|")
for mode_label, mode in MODES:
    df = all_logs[(TRG_CUR, TTO_CUR, mode)]
    if df.empty:
        emit(f"| {mode_label} | 0 | - | - | - | - | - | - |"); continue
    r = df["ratio"]
    emit(f"| {mode_label} | {len(df)} | {r.min():.2f} | {r.quantile(.25):.2f} | {r.median():.2f} | "
         f"{r.quantile(.75):.2f} | {r.max():.2f} | {(r >= TRG_CUR * OVERSHOOT_MULT).mean() * 100:.0f}% |")
worst = cur_df.loc[cur_df["ratio"].idxmax()] if not cur_df.empty else None
if worst is not None:
    emit(f"\nLargest single overshoot (CURRENT mode, {TRG_CUR:.1f}x rule): **{worst['asset']}**, "
         f"{worst['month']}, ratio {worst['ratio']:.2f}x (target {worst['target_w']*100:.2f}%, "
         f"actual {worst['actual_w']*100:.1f}%) -- trimmed "
         f"{worst['trimmed_frac_of_position']*100:.0f}% of the position that month.")
emit("")

# ---------------------------------------------------------------------------
emit("## 3. Regime clustering -- how much of CURRENT's extra firing is target-contraction-driven?\n")
emit("Sustained MID_OFF/RISK_OFF episodes (blend<=0.4 in the live 0=risk-off/1=risk-on "
     "orientation -- ADR-071; the same episode set study 14 identified under the old "
     "0=risk-on/1=risk-off convention as blend_old>=0.6), 3+ consecutive months:\n")
emit("| start | end | months |")
emit("|---|---|--:|")
for s, e in EPISODES:
    emit(f"| {s} | {e} | {(e - s).n + 1} |")
episode_months = sum((e - s).n + 1 for s, e in EPISODES)
total_months = (END - FULL_START).n + 1
emit(f"\nThese episodes cover {episode_months} of {total_months} months in the full window "
     f"({episode_months / total_months * 100:.0f}% of history).\n")

for label, df in [("CURRENT", cur_df), ("BASELINE", base_df)]:
    if df.empty:
        emit(f"{label}: no fires.\n"); continue
    in_ep = df["in_sustained_episode"].sum() if "in_sustained_episode" in df else 0
    emit(f"**{label}**: {in_ep}/{len(df)} fires ({in_ep / len(df) * 100:.0f}%) fell inside a "
         f"sustained MID_OFF/RISK_OFF episode; {len(df) - in_ep} ({(len(df) - in_ep) / len(df) * 100:.0f}%) "
         f"fired in ordinary RISK_ON/MID_ON/NEUTRAL months.")
emit("")

# ---------------------------------------------------------------------------
emit(f"## 4. Whipsaw check -- does the CURRENT {TRG_CUR:.1f}x/{TTO_CUR:.1f}x rule cause more "
     "quick re-fires than the PRIOR rule?\n")
emit("A \"quick re-fire\" = the same asset fires again within <=6 months of its previous fire "
     "(each re-fire costs another round of 15bps trading cost on top of the return effect).\n")
emit("| config | mode | total fires | consecutive same-asset gaps | quick re-fires (<=6mo) | quick re-fire rate |")
emit("|---|---|--:|--:|--:|--:|")
for cfg_label, trg, tto in [(spec.TRIM_CONFIGS[1][0], TRG_PRI, TTO_PRI), (spec.TRIM_CONFIGS[0][0], TRG_CUR, TTO_CUR)]:
    for mode_label, mode in MODES:
        df = all_logs[(trg, tto, mode)]
        gaps_all = []
        for a, g in df.groupby("asset"):
            fm = sorted(pd.Period(m) for m in g["month"])
            gaps_all += [(fm[i + 1] - fm[i]).n for i in range(len(fm) - 1)]
        quick = sum(1 for g in gaps_all if g <= 6)
        rate = quick / len(gaps_all) * 100 if gaps_all else float("nan")
        rate_s = f"{rate:.0f}%" if gaps_all else "n/a"
        emit(f"| {cfg_label} | {mode.upper()} | {len(df)} | {len(gaps_all)} | {quick} | {rate_s} |")
emit("")

# ---------------------------------------------------------------------------
emit("## 5. Sensitivity grid -- fire-frequency vs. return/drawdown tradeoff\n")
emit("Continuous-pass fire counts (CURRENT mode, the live macro-blend mechanism -- not to be "
     "confused with the CURRENT trim-rule config, i.e. the live 1.5x/1.0x pair) and 5-window "
     "headline results (both modes) at three trim-rule configs spanning CURRENT (1.5x/1.0x, "
     "live) through PRIOR (2.0x/1.5x, pre-ADR-048).\n")
emit("### Fire counts, continuous pass, CURRENT mode\n")
emit("| config | total fires | fires/yr |")
emit("|---|--:|--:|")
for cfg_label, trg, tto in CONFIGS:
    df = all_logs[(trg, tto, "current")]
    emit(f"| {cfg_label} | {len(df)} | {len(df) / n_full_years:.2f} |")
emit("")

emit("### 5-window XIRR%/yr (maxDD%) by config\n")
window_results = {}
for cfg_label, trg, tto in CONFIGS:
    for mode_label, mode in MODES:
        for k in STARTS:
            xirr, maxdd, navs = sim(starts[k], mode, trg, tto)
            window_results[(trg, tto, mode, k)] = (xirr, maxdd)
emit("| config | mode | 13y | 8y | 5y | 3y | 1y |")
emit("|---|---|--:|--:|--:|--:|--:|")
for cfg_label, trg, tto in CONFIGS:
    for mode_label, mode in MODES:
        cells = [f"{window_results[(trg, tto, mode, k)][0]*100:.0f} ({window_results[(trg, tto, mode, k)][1]*100:.0f})" for k in STARTS]
        emit(f"| {cfg_label} | {mode.upper()} | " + " | ".join(cells) + " |")
emit(f"\nStart months: " + ", ".join(f"{k}={starts[k]}" for k in STARTS) + ". Cell = XIRR%/yr (maxDD%).")

emit("\nCaveats: miners basket return overstated (survivorship -- MARA/CIFR/IREN/WULF/HUT "
     "each began trading at different points); gold rallied through most of this sample "
     "(flatters any variant that trims into gold regardless of trigger quality); newer assets "
     "(SUI since 2023-05, COIN since 2021-04, SOL since 2020-04) have thin history so their "
     "per-asset fire-frequency stats above are based on a much shorter window than GLD/QQQ/BTC "
     "-- their 'years present' column should be read alongside the fire count, not in "
     "isolation; overlapping windows / one shared underlying price/macro history in section 5's "
     "5-window table; GLD/QQQ/FXI are long-history proxies for the real Gold/Nasdaq-100/China "
     "positions (portfolio_spec.py's SIMPLIFIED_* tables, live ADR-074/087 weights), not the "
     "literal tickers -- see AGENTS.md's standing FXI-for-ICGA.DE convention; the Experiments "
     "sleeve (2.5 flat, unaffected by blend/trim either way) plus ONDO-USD and India (no price "
     "files) are omitted and their weight implicitly redistributed pro-rata by the existing "
     "present-renormalization step.")

with open(os.path.join(RESULTS, "trim_frequency_backtest.md"), "w") as fh:
    fh.write("\n".join(out) + "\n")
print("\nWrote results/trim_frequency_backtest.md")
