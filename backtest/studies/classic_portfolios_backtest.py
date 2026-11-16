#!/usr/bin/env python3
"""
Study 16 -- how do well-known, published "textbook" portfolios perform under the
exact same DCA/cost/PLN conventions used throughout this folder, as a reference
point/contrast to the user's own aggressive crypto-heavy book (studies 07/14/15)?

Four classic, canonically-weighted portfolios (NOT the user's own strategy -- these
are real named portfolios, weights taken as published, not re-derived):
  1. 60/40                  -- 60% US stock (SPY), 40% bonds (AGG)
  2. Permanent Portfolio     -- Harry Browne 1981: 25% stock (SPY), 25% LT treasuries
                                (TLT), 25% cash (flat 0% nominal PLN), 25% gold (GLD)
  3. Golden Butterfly        -- Portfolio Charts: 20% total US stock (SPY), 20%
                                small-cap value (IWN), 20% LT treasuries (TLT), 20%
                                short-term treasuries (SHY), 20% gold (GLD)
  4. Ray Dalio All Weather   -- Bridgewater / Tony Robbins book version: 30% stock
                                (SPY), 40% LT treasuries (TLT), 15% intermediate
                                treasuries (IEF), 7.5% gold (GLD), 7.5% broad
                                commodities (DBC)

Conventions reused unchanged from portfolio_macro_blend_backtest.py /
trim_frequency_backtest.py: buy-only DCA 1000 PLN initial + 1000 PLN/month, 15bps
trading cost, prices converted to PLN via USDPLN_X, no-lookahead, the same 5
overlapping start-date windows (1/3/5/8/13y back from the latest common available
month, END = usdpln.index.max()).

KEY METHODOLOGICAL DIFFERENCE from studies 07/14/15: these are classic textbook
portfolios, so this script tests standard ANNUAL REBALANCING BACK TO TARGET WEIGHTS
instead of the 2x-overshoot-trim-to-gold rule (that rule is specific to the user's
own aggressive crypto strategy and does not apply to a generic named portfolio).
Monthly contributions are allocated pro-rata at that month's target weights (same
DCA convention as the existing scripts' `w = tt/sum()` pattern). Once every 12
months -- on the anniversary of that window's own start month, chosen for
reproducibility across all 5 overlapping windows without an arbitrary "January"
cutoff -- the portfolio is FULLY rebalanced back to the exact target weights.
Rebalancing turnover is costed the same way the existing scripts cost trim-rule
turnover: one-way turnover (sum of |actual-target| / 2) is charged as 2x that
amount in trading volume (sell side + buy side), at the same 15bps rate, added to
the monthly contribution's own cost.

THREE VARIANTS per portfolio, per three explicit mid-task user requests ("all
those portfolios should also be sliding risk on/off adjusted", then "of course
all portfolios should always be compared to simple buy and hold (DCA of
course)", then a correction: "buy and hold DCA" must use the SAME contribution
algorithm as the real production strategy, not a naive fixed-percentage split) --
three points on the same spectrum, from no rebalancing discipline at all to a
moving annually-rebalanced target:
  1. BUYHOLD -- monthly DCA contributions routed via the REAL production
               deficit-proportional "catch-up" mechanic (see backtest/AGENTS.md
               "Rebalancing rule" / `MainRebalancingStrategy.java`, ADR-040/045):
               O_total = current_total + this month's contribution A; P_i =
               target_weight_i * O_total (what position i SHOULD be worth after
               the deposit); Q_i = P_i - current_value_i (deficit if positive);
               Q_pos = sum of positive Q_i; buy_i = A * Q_i / Q_pos for every
               Q_i > 0, ZERO for any position already at/above target. The whole
               contribution chases whatever is most underweight -- NOT a fixed
               percentage split (this script's original, since-corrected
               approximation; studies 07/14/15 still use the fixed-split
               approximation for the user's own portfolio, per an explicit user
               decision not to touch those figures right now). NO REBALANCING/
               SELLING of any kind, ever -- existing holdings just drift with
               price returns; a position that's run ahead of target simply stops
               receiving new money until the rest of the portfolio catches up.
  2. FIXED   -- target weight is the named canonical table, constant every month,
               contributions allocated at fixed target-weight proportions, WITH
               the annual full rebalance-to-target described above (the pure
               textbook "rebalance with selling" portfolio, as originally
               specified -- deliberately NOT changed by the BUYHOLD correction,
               since it's a distinct, real textbook technique in its own right).
  3. BLEND   -- same ADR-044 sliding mechanism reused unchanged from
               portfolio_macro_blend_backtest.py (net-liq + Global-M2 YoY-
               acceleration composite z-score, 3mo smoothing, blend = clip(0.5 -
               smoothed/2, 0, 1), expanding no-lookahead z-scores): each month's
               target = RISK_ON*(1-blend) + RISK_OFF*blend, where RISK_ON/RISK_OFF
               are a symmetric tilt built from the named table (see TILT_FRACTION
               below) -- MORE stock/commodity weight in risk-on, LESS in risk-off,
               mirroring the semantics of every other RISK_ON/RISK_OFF table in
               this folder (risk-on = lean into risk assets, risk-off = lean
               defensive). Same annual full-rebalance-to-(that month's)-target
               mechanic as FIXED (also not touched by the BUYHOLD correction),
               just with a moving target instead of a fixed one.
  4. TRIM    -- new standing baseline (backtest/AGENTS.md "Standing baselines"
               point 3, added mid-task 2026-07-09): BUYHOLD's real catch-up DCA
               mechanic PLUS a monthly threshold-triggered PARTIAL SELL layered on
               top -- any position whose actual weight reaches `trigger x its own
               current target weight` gets trimmed back to `trim_to x target`;
               freed value is redistributed pro-rata across every OTHER present
               position in proportion to THAT position's own target weight
               (general/portfolio-agnostic -- NOT dumped into one fixed "safe
               haven" asset like gold, which is the user's own personal choice
               for their own crypto book, not a general rule). Tested at three
               (trigger, trim_to) pairs on every portfolio: (1.5, 1.0) -- the
               user's own live rule (ADR-048, 2026-07-10); (2.0, 1.0); (2.0, 1.5)
               -- the prior rule, pre-ADR-048, kept as a reference point only,
               NOT current. This is "BUYHOLD +
               threshold trim," NOT FIXED's annual-calendar rebalance -- the only
               correction mechanism is the threshold check itself, evaluated
               monthly (same cadence convention as study #15). Fire-frequency
               (which asset fires, how often) is reported per portfolio per
               config over a single continuous non-overlapping pass, same method
               as study #15.

Parallel worth noting: for the user's own aggressive book, an equivalent buy-and-
hold reference already exists -- study 07's no-trim/pure-buy variant (13y
46%/-80%, 8y 32%/-73%) is the same style of construct (never sells/rebalances)
applied to that portfolio, BUT still uses the older fixed-percentage-split
approximation for its contribution step, not the real catch-up mechanic used by
this script's BUYHOLD -- not apples-to-apples with the corrected numbers here,
per explicit user decision to fix study 16 only for now. Not recomputed here,
cited in the write-up with that caveat attached.

RISK_ON/RISK_OFF construction (judgment call, calibrated to an explicit user
example: for 60/40, risk-on=80/20 stock/bond, risk-off=40/60 stock/bond -- a
symmetric +-1/3 tilt of the named "risk" (stock+commodity) weight around its own
value, TILT_FRACTION=1/3 below): every asset is classified RISK (equities SPY/IWN,
commodities DBC -- the two the-user-said-lean-into-in-good-times legs) or DEFENSE
(bonds TLT/IEF/SHY/AGG, gold GLD, cash CASH). RISK_ON multiplies the total RISK-
bucket weight by (1+TILT_FRACTION), RISK_OFF by (1-TILT_FRACTION); the freed/needed
weight is added to/taken from the DEFENSE bucket, split proportionally to each
defense asset's own existing named weight (and within the RISK bucket, proportional
to each risk asset's own named weight, for portfolios with >1 risk asset). Applied
identically (same TILT_FRACTION) to all 4 portfolios for consistency; not
independently re-optimized per portfolio.

Does not modify any existing study script.

REFRESHED 2026-08-06 (task 28, portfolio-spec refresh): blend divisor/orientation and
the TRIM_CONFIGS trigger/trim-to labels now import from `portfolio_spec.py` (this
script's own named-classic-portfolio RISK_ON/RISK_OFF tables, built from TILT_FRACTION,
are unaffected -- they have nothing to do with MAIN's own target table). Was mislabeled
same as every other trim-rule script in this folder: (2.0x, 1.5x) called "the user's own
live rule" when ADR-048 already made (1.5x, 1.0x) live on 2026-07-10.
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
TILT_FRACTION = 1.0 / 3.0     # RISK bucket: RISK_ON = named*(1+f), RISK_OFF = named*(1-f)

# ---------------------------------------------------------------------------
# Four classic portfolios, canonical published weights (percent, sum to 100).
PORTFOLIOS = {
    "60/40": {"SPY": 60, "AGG": 40},
    "Permanent Portfolio (Harry Browne)": {"SPY": 25, "TLT": 25, "CASH": 25, "GLD": 25},
    "Golden Butterfly (Portfolio Charts)": {"SPY": 20, "IWN": 20, "TLT": 20, "SHY": 20, "GLD": 20},
    "Ray Dalio All Weather": {"SPY": 30, "TLT": 40, "IEF": 15, "GLD": 7.5, "DBC": 7.5},
}
for name, w in PORTFOLIOS.items():
    assert abs(sum(w.values()) - 100) < 1e-9, name

RISK_BUCKET = {"SPY", "IWN", "DBC"}         # equities + commodities: "lean in" assets
DEFENSE_BUCKET = {"TLT", "IEF", "SHY", "AGG", "GLD", "CASH"}   # bonds/gold/cash: "lean out" assets

ALL_ASSETS = sorted({a for w in PORTFOLIOS.values() for a in w if a != "CASH"})


def build_risk_tables(named):
    """From a named (fixed) weight table, build symmetric RISK_ON / RISK_OFF
    tables per the TILT_FRACTION rule described in the module docstring. Returns
    (risk_on, risk_off) dicts, each summing to 100."""
    risk_assets = {a: w for a, w in named.items() if a in RISK_BUCKET}
    def_assets = {a: w for a, w in named.items() if a not in RISK_BUCKET}
    risk_total = sum(risk_assets.values())
    def_total = sum(def_assets.values())

    def scale(risk_mult):
        new_risk_total = risk_total * risk_mult
        new_def_total = 100.0 - new_risk_total
        out = {}
        for a, w in risk_assets.items():
            out[a] = new_risk_total * (w / risk_total) if risk_total else 0.0
        for a, w in def_assets.items():
            out[a] = new_def_total * (w / def_total) if def_total else 0.0
        return out

    risk_on = scale(1 + TILT_FRACTION)
    risk_off = scale(1 - TILT_FRACTION)
    return risk_on, risk_off


RISK_ON_TABLES = {}
RISK_OFF_TABLES = {}
for name, w in PORTFOLIOS.items():
    RISK_ON_TABLES[name], RISK_OFF_TABLES[name] = build_risk_tables(w)
    assert abs(sum(RISK_ON_TABLES[name].values()) - 100) < 1e-6, name
    assert abs(sum(RISK_OFF_TABLES[name].values()) - 100) < 1e-6, name


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
for a in ALL_ASSETS:
    RET[a] = pln_ret(a)

END = usdpln.index.max()
STARTS = {"13y": 13, "8y": 8, "5y": 5, "3y": 3, "1y": 1}
starts = {k: END - v * 12 for k, v in STARTS.items()}

# Longest continuous history available per portfolio (all of its own assets have a
# valid monthly return), for the trim-rule fire-frequency pass (study 15's method).
FULL_START = {name: max(RET[a].dropna().index.min() for a in w if a != "CASH")
              for name, w in PORTFOLIOS.items()}

# Threshold-triggered trim-rule configs -- standing baseline, backtest/AGENTS.md
# "Standing baselines" point 3: (trigger, trim_to) pairs, tested on every portfolio.
# REFRESHED 2026-08-06 (task 28): was mislabeled -- (2.0, 1.5) is NOT the user's own live
# rule; ADR-048 made (1.5, 1.0) live on 2026-07-10. See portfolio_spec.py.
TRIM_CONFIGS = [(spec.TRIM_CONFIGS[0][0], *spec.TRIM_CURRENT),
                ("2.0 / 1.0 (AGENTS.md standing 3rd reference variant)", *spec.TRIM_REFERENCE),
                (spec.TRIM_CONFIGS[1][0], *spec.TRIM_PRIOR)]

# ---------------------------------------------------------------------------
# Inception-date confirmation: first available monthly-return observation per
# asset, vs. the earliest window start (13y). Every fetched ticker's true Yahoo
# inception (confirmed separately, see study write-up) predates 2007-01 (the
# common data-file start used here, matching fetch_long_history.py's convention),
# which in turn predates every window's start by 6+ years -- so the 13y-history
# requirement holds for all assets used in every portfolio above.
inception = {a: usd_close(a).index.min() for a in ALL_ASSETS}

# ---------------------------------------------------------------------------
# ADR-044 macro blend -- reused UNCHANGED from portfolio_macro_blend_backtest.py /
# trim_frequency_backtest.py (net-liq + Global-M2 YoY acceleration, expanding
# no-lookahead z-scores, 0.7/0.3 composite, 3mo smoothing).

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
blend = spec.blend_live(smoothed_z)     # LIVE divisor 0.5 (ADR-049) + orientation (ADR-071); NaN while insufficient history -> collapses to risk-on (b=1.0) in sim()

# ---------------------------------------------------------------------------


def sim(start, mode, named, risk_on=None, risk_off=None, trigger=None, trim_to=None, log=None):
    """mode: 'buyhold' (contributions routed via the REAL production deficit-
    proportional catch-up mechanic -- see backtest/AGENTS.md "Rebalancing rule" /
    MainRebalancingStrategy.java -- and NEVER rebalanced/sold) | 'fixed'
    (contributions allocated at fixed target-weight proportions, WITH annual full
    rebalance) | 'blend' (monthly target interpolated between risk_on/risk_off by
    the ADR-044 blend, WITH annual full rebalance) | 'trim' (BUYHOLD's real
    catch-up DCA mechanic PLUS a monthly threshold-triggered partial-trim check:
    any position at/above `trigger * its own target weight` gets trimmed back to
    `trim_to * target`, proceeds redistributed pro-rata across every OTHER
    present position in proportion to that position's own target weight --
    general/portfolio-agnostic, NOT dumped into one fixed "safe haven" asset; see
    backtest/AGENTS.md "Standing baselines" point 3). Annual full rebalance
    happens on the 12-month anniversary of `start` (idx % 12 == 0, idx > 0) for
    'fixed'/'blend' only; 'buyhold'/'trim' never do a calendar rebalance -- 'trim'
    only ever corrects via its own threshold check. `log`, if a list, gets one
    dict appended per trim-fire event (month/asset/target/actual/ratio/trimmed).
    Returns (xirr, maxdd, navs).

    BUYHOLD/TRIM's contribution step is the real 12-step production algorithm,
    not a naive fixed-percentage split: O_total = N_total + A; P_i = target_w_i *
    O_total (what position i SHOULD be worth after the deposit); Q_i = P_i - N_i
    (deficit if positive); Q_pos = sum of positive Q_i; buy_i = A * Q_i / Q_pos
    for every Q_i > 0, ZERO for any position at/above target. The whole
    contribution chases whatever is most underweight -- never a fixed split, and
    never a sell."""
    months = pd.period_range(start, END, freq="M"); n = len(months)
    assets = list(named.keys())
    hold = {a: 0.0 for a in assets}
    nav = 1.0; navs = []
    for idx, t in enumerate(months):
        present = [a for a in assets if a == "CASH" or (RET[a] is not None and t in RET[a].index and not np.isnan(RET[a].get(t, np.nan)))]
        if mode in ("fixed", "buyhold", "trim"):
            tt = named
        else:
            bv = blend.get(t, np.nan)
            b = 1.0 if pd.isna(bv) else float(bv)    # insufficient history -> collapse to risk-on (b=1.0, LIVE orientation)
            tt = {a: spec.effective_target(risk_on[a], risk_off[a], b) for a in assets}
        w = pd.Series({a: tt[a] for a in present}); w = w / w.sum()
        Vstart = sum(hold.values()) + CONTRIB
        if mode in ("buyhold", "trim"):
            N_total = sum(hold[a] for a in present)
            O_total = N_total + CONTRIB
            Q = {a: w[a] * O_total - hold[a] for a in present}
            Q_pos = sum(q for q in Q.values() if q > 0)
            if Q_pos > 1e-9:
                for a in present:
                    if Q[a] > 0:
                        hold[a] += CONTRIB * Q[a] / Q_pos
            else:                          # degenerate guard; shouldn't trigger (Q_pos >= A always when A>0)
                for a in present:
                    hold[a] += CONTRIB * w[a]
        else:
            for a in present:
                hold[a] += CONTRIB * w[a]
        Vn = sum(hold.values())
        turnover = 0.0
        if mode in ("fixed", "blend") and idx > 0 and idx % 12 == 0:   # annual full rebalance-to-target
            target_val = {a: w[a] * Vn for a in present}
            turnover = sum(abs(hold[a] - target_val[a]) for a in present) / 2.0
            for a in present:
                hold[a] = target_val[a]
        elif mode == "trim" and trigger is not None:
            ctw = {a: named[a] / 100.0 for a in present}   # each position's own fixed target weight (fraction)
            firing = []
            freed = 0.0
            for a in present:
                if ctw[a] <= 0:
                    continue
                ww = hold[a] / Vn
                if ww >= trigger * ctw[a]:
                    keep = trim_to * ctw[a] * Vn
                    trimmed = hold[a] - keep
                    freed += trimmed
                    hold[a] = keep
                    firing.append(a)
                    if log is not None:
                        log.append({"month": str(t), "asset": a, "target_w": ctw[a],
                                     "actual_w": ww, "ratio": ww / ctw[a], "trimmed": trimmed})
            if freed > 1e-12:
                others = [a for a in present if a not in firing]
                tot_w_others = sum(ctw[a] for a in others)
                if tot_w_others > 0:
                    for a in others:
                        hold[a] += freed * (ctw[a] / tot_w_others)
                else:                      # degenerate guard: every position fired simultaneously
                    for a in firing:
                        hold[a] += freed / len(firing)
            turnover = freed          # one-way traded value; costed as 2x below, same convention as FIXED/BLEND
        Vc = sum(hold.values()); f = 1 - (CONTRIB + 2 * turnover) * COST / Vc
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


# ---------------------------------------------------------------------------
out = []
def emit(s=""):
    out.append(s); print(s)


emit("# Study 16 -- classic textbook portfolios (reference point vs. the user's own book)\n")
emit(f"Buy-only DCA 1000+1000/mo PLN, 15bps. Four variant families per portfolio, same DCA/cost "
     f"conventions throughout, differing only in rebalancing discipline: BUYHOLD (contributions "
     f"routed via the REAL production deficit-proportional catch-up mechanic -- O_total = "
     f"current+A, P_i = target_w_i*O_total, Q_i = P_i-current_i, buy_i = A*Q_i/sum(positive Q) -- "
     f"never rebalanced/sold, see backtest/AGENTS.md 'Rebalancing rule'), FIXED (contributions at "
     f"fixed target-weight proportions + annual full rebalance-to-target -- see script header for "
     f"the anniversary-month convention), BLEND (ADR-044 sliding risk-on/off target, reused "
     f"unchanged from study 14, + annual full rebalance to that month's moving target), and TRIM "
     f"(BUYHOLD's real catch-up DCA + a monthly threshold-trigger partial-sell check, general/"
     f"portfolio-agnostic redistribution -- backtest/AGENTS.md 'Standing baselines' point 3 -- "
     f"tested at (2.0,1.5)/(2.0,1.0)/(1.5,1.0)). Latest common month: {END}. "
     f"Cell = XIRR%/yr (maxDD%).\n")

emit("## Portfolio weight tables (FIXED / named, and the BLEND variant's RISK_ON / RISK_OFF endpoints)\n")
for name, w in PORTFOLIOS.items():
    emit(f"### {name}\n")
    emit("| asset | named (FIXED) % | RISK_ON % (BLEND, blend=0) | RISK_OFF % (BLEND, blend=1) |")
    emit("|---|--:|--:|--:|")
    for a, pct in w.items():
        emit(f"| {a} | {pct:.2f} | {RISK_ON_TABLES[name][a]:.2f} | {RISK_OFF_TABLES[name][a]:.2f} |")
    emit("")

emit("Risk-on/risk-off construction: RISK bucket (SPY equities, IWN small-cap value, DBC "
     f"commodities) scaled to (1{'+'}{TILT_FRACTION:.3f}) of its named weight in RISK_ON and "
     f"(1-{TILT_FRACTION:.3f}) in RISK_OFF; freed/needed weight moves to/from the DEFENSE "
     "bucket (bonds/gold/cash) proportionally to each asset's own named weight. Calibrated "
     "to an explicit worked example (60/40: risk-on 80/20 stock/bond, risk-off 40/60) and "
     "applied identically (same tilt fraction) to all 4 portfolios -- a judgment call, not "
     "independently re-optimized per portfolio (same caveat style as study 14's RISK_OFF "
     "table for the user's own book).\n")

emit("## Fetched-ticker inception confirmation (13y-history requirement)\n")
emit(f"Window start dates: " + ", ".join(f"{k}={starts[k]}" for k in STARTS) + " (13y window "
     f"starts {starts['13y']}). All assets below must clear that date by a wide margin.\n")
emit("| ticker | role | first available (this data file) | true Yahoo inception (confirmed) | "
     "years before 13y-window start |")
emit("|---|---|---|---|--:|")
TRUE_INCEPTION = {  # confirmed separately via a 1990-2008 probe fetch, see study write-up
    "SPY": "1993-01-29", "TLT": "2002-07-30", "GLD": "2004-11-18", "IWM": "2000-05-26",
    "AGG": "2003-09-29", "IEF": "2002-07-30", "SHY": "2002-07-30", "IWN": "2000-07-28",
    "DBC": "2006-02-06",
}
ROLE = {
    "SPY": "US stock (60/40, Permanent, Golden Butterfly, All Weather)",
    "TLT": "LT treasuries (Permanent, Golden Butterfly, All Weather)",
    "GLD": "gold (Permanent, Golden Butterfly, All Weather)",
    "IWM": "(fetched, not used -- see AGENTS note in write-up: superseded by IWN for the value leg)",
    "AGG": "US aggregate bonds (60/40)",
    "IEF": "intermediate treasuries (All Weather)",
    "SHY": "short-term treasuries (Golden Butterfly)",
    "IWN": "small-cap value (Golden Butterfly)",
    "DBC": "broad commodities (All Weather)",
}
for a in ["SPY", "TLT", "GLD", "AGG", "IEF", "SHY", "IWN", "DBC"]:
    first_in_file = inception.get(a, usd_close(a).index.min())
    true_incep = pd.Period(TRUE_INCEPTION[a], freq="M")
    years_margin = (starts["13y"] - true_incep).n / 12.0
    emit(f"| {a} | {ROLE[a]} | {first_in_file} | {TRUE_INCEPTION[a]} | {years_margin:.1f}y |")
emit("")

emit("## Headline results\n")
emit("| portfolio | variant | " + " | ".join(STARTS) + " |")
emit("|---|---|" + "--:|" * len(STARTS))
results = {}
for name, w in PORTFOLIOS.items():
    for label, mode in [("BUYHOLD", "buyhold"), ("FIXED", "fixed"), ("BLEND", "blend")]:
        cells = []
        for k in STARTS:
            xirr, maxdd, navs = sim(starts[k], mode, w, RISK_ON_TABLES[name], RISK_OFF_TABLES[name])
            results[(name, mode, k)] = (xirr, maxdd, navs)
            cells.append(f"{xirr*100:.0f} ({maxdd*100:.0f})")
        emit(f"| {name} | {label} | " + " | ".join(cells) + " |")
    for cfg_label, trg, tto in TRIM_CONFIGS:
        cells = []
        for k in STARTS:
            xirr, maxdd, navs = sim(starts[k], "trim", w, trigger=trg, trim_to=tto)
            results[(name, ("trim", trg, tto), k)] = (xirr, maxdd, navs)
            cells.append(f"{xirr*100:.0f} ({maxdd*100:.0f})")
        emit(f"| {name} | TRIM {cfg_label} | " + " | ".join(cells) + " |")

emit(f"\nStart months: " + ", ".join(f"{k}={starts[k]}" for k in STARTS) + ". Cell = XIRR%/yr (maxDD%). "
     f"TRIM variants use BUYHOLD's real catch-up DCA plus a monthly threshold-trigger check "
     f"(see script header) -- not FIXED's annual-calendar rebalance.")

# ---------------------------------------------------------------------------
# Trim-rule fire-frequency, single continuous pass per portfolio (study 15's method).
emit("\n## Trim-rule fire-frequency (single continuous pass per portfolio, study #15's method)\n")
emit("For each portfolio, one non-overlapping pass from the earliest month all of its own "
     "assets have valid returns through the latest common month -- gives a clean fire count "
     "per (trigger, trim_to) config, independent of the 5 overlapping DCA windows above.\n")
fire_logs = {}
for name, w in PORTFOLIOS.items():
    fstart = FULL_START[name]
    n_years = (END - fstart).n / 12.0
    emit(f"### {name} ({fstart}..{END}, {n_years:.1f}y)\n")
    emit("| config | total fires | fired assets (count each) |")
    emit("|---|--:|---|")
    for cfg_label, trg, tto in TRIM_CONFIGS:
        log = []
        sim(fstart, "trim", w, trigger=trg, trim_to=tto, log=log)
        fire_logs[(name, trg, tto)] = log
        if log:
            df = pd.DataFrame(log)
            per_asset = df.groupby("asset").size().sort_values(ascending=False)
            breakdown = ", ".join(f"{a} ({c})" for a, c in per_asset.items())
        else:
            breakdown = "(no fires)"
        emit(f"| {cfg_label} | {len(log)} | {breakdown} |")
    emit("")

emit("\n## Reference row: the user's own portfolio (NOT recomputed in this script)\n")
emit("Cited from study 14's CURRENT-mode row (`portfolio_macro_blend_backtest.py`, same "
     f"END={END}, same DCA/cost/PLN conventions) -- a different rebalancing mechanic "
     "(buy-only + 2x-overshoot-trim-to-gold + ADR-044 macro blend, NOT annual rebalance-to-"
     "target) and a different, much more concentrated asset roster (50% crypto / 30% gold "
     "tilt). Not literally rerun here; cited as-published for comparison. NOTE: that row still "
     "uses the OLDER fixed-percentage-split DCA approximation, not the real catch-up mechanic "
     "this script's BUYHOLD now uses (studies 07/14/15 not corrected, per explicit user "
     "decision to fix study 16 only for now) -- not apples-to-apples with the BUYHOLD rows "
     "above.\n")
emit("| portfolio | 13y | 8y | 5y | 3y | 1y |")
emit("|---|--:|--:|--:|--:|--:|")
emit("| User's own book (study 14, CURRENT/ADR-044, older fixed-split DCA approx.) | 34 (-24) | 33 (-20) | 21 (-28) | 24 (-15) | -3 (-11) |")

emit("\nCaveats: overlapping windows share one underlying price/macro history (not "
     "independent draws); 1y/3y windows are single-episode reads; SPY/TLT/GLD/IWN/SHY/IEF/"
     "AGG/DBC data files start 2007-01 by convention (matching fetch_long_history.py) even "
     "though several have earlier true Yahoo inception -- irrelevant here since 2007-01 "
     "already predates the earliest (13y) window by 6+ years; IWM (blend small-cap, already "
     "in data/) was fetched previously but NOT used in Golden Butterfly here -- IWN (value) "
     "is used instead per the task's more-faithful ticker mapping; DBC has the shortest "
     "runway of the 5 newly-fetched tickers (inception 2006-02) but still clears the 13y "
     "window by ~7.3 years; the user's reference row uses a different mechanism and a "
     "different (far more concentrated) asset roster, so the comparison is return/drawdown "
     "profile vs. profile, not apples-to-apples on holdings; the BLEND variant's RISK_ON/"
     "RISK_OFF tables are a judgment-call tilt (TILT_FRACTION=1/3, uniform across all 4 "
     "portfolios), not independently calibrated per portfolio or re-optimized against this "
     "backtest's own results; BUYHOLD now uses the real deficit-proportional catch-up "
     "mechanic (corrected mid-task -- the original version used a naive fixed-percentage "
     "split, which materially overstated BUYHOLD's edge over FIXED; see study write-up); "
     "studies 07/14/15's own-portfolio figures still use that older fixed-split approximation "
     "and were NOT corrected (explicit user decision, study 16 only for now); TRIM's "
     "redistribution-to-others rule is a judgment-call generalization of the BLEND overlay's "
     "own DEFENSE-bucket redistribution logic, applied portfolio-agnostically (every OTHER "
     "position gets a share proportional to its own target weight) per backtest/AGENTS.md's "
     "explicit instruction not to route proceeds to one fixed safe-haven asset the way the "
     "user's own crypto book does; TRIM's fire-frequency pass uses a per-portfolio FULL_START "
     "(earliest month all of that portfolio's own assets have a valid return), so the four "
     "portfolios' continuous-pass windows are not identical in length to each other.")

with open(os.path.join(RESULTS, "classic_portfolios_backtest.md"), "w") as fh:
    fh.write("\n".join(out) + "\n")
print("\nWrote results/classic_portfolios_backtest.md")
