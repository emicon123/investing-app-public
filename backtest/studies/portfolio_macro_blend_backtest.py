#!/usr/bin/env python3
"""
Study 14 — sequel to 07: same demo portfolio, but the macro overlay is modeled the way
production ACTUALLY works today (ADR-044 sliding blend), not the old binary
cut+redistribute mechanism study 07 used.

Per user correction mid-session: the 2x-trim-to-1.5x-target->gold rule from study 07 IS
kept in every variant here (it is what the user still does manually per CLAUDE.md's
"reduce 10%->20%+ back to 15%, swap to gold" strategy note, even though ADR-045 removed
the AUTOMATIC version of it from the purchase-planning software itself). What actually
varies between the three variants below is only the MONTHLY TARGET WEIGHTS the DCA
contribution (and the trim trigger) use -- fixed vs. blend-interpolated vs.
blend-reversed.

Three variants, same portfolio (GLD/CASH/QQQ/FXI/MSTR/BTC/miners/ETH/SOL/COIN/SUI/DOGE):
  1. BASELINE  -- fixed risk-on target table, no macro overlay at all. Pure control.
  2. CURRENT   -- ADR-044 blend interpolates target between RISK_ON and RISK_OFF tables
                  each month: blend = clip(0.5 - smoothed_z/2, 0, 1), smoothed_z = 3mo
                  rolling avg of composite_z = 0.7*z(netliq YoY accel) + 0.3*z(globalM2
                  YoY accel), z-scored EXPANDING (no lookahead) vs each series' own
                  history to date.
  3. REVERSED  -- identical signal & tables, blend_reversed = 1 - blend (contrarian /
                  "buy risk when liquidity decelerates = cheap" hypothesis).

DCA 1000 PLN start + 1000/month, PLN, 15bps cost, 5 start dates (1/3/5/8/13y).

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
results/study_summary/14_macro_blend_momentum_vs_contrarian.md for how the numbers
moved. The blend series itself (composite_z/smoothed_z/blend/blend_reversed) is
untouched -- only how a given month's contribution gets split across assets given
that month's target weights changes.
"""
import os
import numpy as np
import pandas as pd

BASE = os.path.join(os.path.dirname(__file__), "..")
PRICES = os.path.join(BASE, "data", "prices")
MACRO = os.path.join(BASE, "data", "macro")
RESULTS = os.path.join(BASE, "results")
CONTRIB = 1000.0
COST = 15 / 1e4
TRIGGER = 2.0       # +100% rule: fire when actual >= 2x current (effective) target
TRIM_TO = 1.5        # trim back to 1.5x current target (study-07 "current" setting)
MINERS = ["MARA", "HUT", "CIFR", "IREN", "WULF"]

# ---------------------------------------------------------------------------
# Risk-on table: identical demo portfolio used throughout this folder (study 07).
RISK_ON = {"GLD": 30, "CASH": 10, "QQQ": 5, "FXI": 2.5, "MSTR": 2.5,
           "BTC_spot": 17.5, "miners": 7.5, "ETH": 13, "SOL": 5, "COIN": 4, "SUI": 2, "DOGE": 1}
CRYPTO = ["BTC_spot", "miners", "ETH", "SOL", "COIN", "SUI", "DOGE"]

# ---------------------------------------------------------------------------
# Risk-off table: adapted from the REAL production ADR-044 tables (V37 migration:
# backend/src/main/resources/db/migration/V37__sliding_macro_regime_targets.sql), applying
# each ticker/bucket's real risk-on->risk-off RETENTION RATIO to this demo portfolio's
# equivalent bucket. Real production ratios used:
#   BTC-USD        20.00 -> 11.00   (retain 55%)
#   miners (each)   1.00 ->  0.30   (retain 30%)
#   ETH-USD        13.00 ->  1.30   (retain 10%)
#   SOL-USD         5.00 ->  0.50   (retain 10%)
#   COIN            4.00 ->  0.40   (retain 10%)
#   SUI-USDT        2.00 ->  0.20   (retain 10%)
#   DOGE-USD        1.00 ->  0.10   (retain 10%)
#   NQSE.DE (Nasdaq100 UCITS, direct analog of QQQ)      5.00 -> 15.00  (3x)
#   ICGA.DE (MSCI China UCITS, direct analog of FXI)     2.50 ->  7.50  (3x)
#   ETFBCASH.PL / ZPR1.DE (new bond/defensive sink)      0    -> 10 each (20% combined, new)
#   MSTR reclassified EXPERIMENTS bucket -> flat (no regime change)
#   Gold / PLN-cash / CHF-cash -> flat (unchanged both regimes)
# Applied here: same retention ratios for BTC/miners/ETH/SOL/COIN/SUI/DOGE; QQQ and FXI
# get the same 3x bump as their real NQSE.DE/ICGA.DE analogs; MSTR flat (as in prod);
# gold flat. This demo portfolio has no separate bond-ETF ticker, so the freed weight
# that in prod goes to ETFBCASH.PL/ZPR1.DE (~20%) is routed to CASH instead -- CASH is
# the closest available zero-vol defensive analog in this asset list. Both tables sum to
# exactly 100 (required for the interpolation to be sum-conserving, ADR-044 §7.3).
RISK_OFF = {
    "GLD": 30.000,
    "QQQ": 15.000,        # 3x, mirrors NQSE.DE 5->15
    "FXI": 7.500,          # 3x, mirrors ICGA.DE 2.5->7.5
    "MSTR": 2.500,         # flat, mirrors MSTR -> EXPERIMENTS (no regime move)
    "BTC_spot": 9.625,     # 17.5 * 0.55
    "miners": 2.250,       # 7.5 * 0.30
    "ETH": 1.300,          # 13 * 0.10
    "SOL": 0.500,          # 5 * 0.10
    "COIN": 0.400,         # 4 * 0.10
    "SUI": 0.200,          # 2 * 0.10
    "DOGE": 0.100,         # 1 * 0.10
}
RISK_OFF["CASH"] = 100.0 - sum(v for k, v in RISK_OFF.items())  # = 30.625, absorbs freed weight
assert abs(sum(RISK_ON.values()) - 100) < 1e-9
assert abs(sum(RISK_OFF.values()) - 100) < 1e-9

TICKER = {"BTC_spot": "BTC_USD", "ETH": "ETH_USD", "SOL": "SOL_USD", "DOGE": "DOGE_USD",
          "SUI": "SUI20947_USD", "COIN": "COIN", "GLD": "GLD", "QQQ": "QQQ", "FXI": "FXI", "MSTR": "MSTR"}


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
for a in RISK_ON:
    if a != "CASH":
        RET[a] = pd.DataFrame({m: pln_ret(m) for m in MINERS}).mean(axis=1) if a == "miners" else pln_ret(TICKER[a])

END = usdpln.index.max()
STARTS = {"13y": 13, "8y": 8, "5y": 5, "3y": 3, "1y": 1}
starts = {k: END - v * 12 for k, v in STARTS.items()}

# ---------------------------------------------------------------------------
# Macro blend (ADR-044): net-liq + Global-M2 YoY-acceleration, expanding no-lookahead
# z-scores, 0.7/0.3 composite, 3mo smoothing, blend = clip(0.5 - smoothed/2, 0, 1).

def macro_m(f, c, weekly=False):
    s = pd.read_csv(os.path.join(MACRO, f), parse_dates=["date"]).set_index("date")[c].astype(float)
    if weekly:
        s = s.resample("ME").last()
    s.index = s.index.to_period("M"); return s.groupby(level=0).last()


netliq = macro_m("FED_NET_LIQUIDITY.csv", "net_liquidity_bn", weekly=True)
gm = macro_m("GLOBAL_M2.csv", "global_m2_bn")

# YoY -> 3mo acceleration, with a publish-lag shift (same convention as study 07):
# net-liq (weekly, FRED) shift 1mo; Global M2 (monthly, slowest legs China/Japan) shift 2mo.
nl_accel = netliq.pct_change(12).diff(3).shift(1)
gm_accel = gm.pct_change(12).diff(3).shift(2)

Z_MIN_PERIODS = 24  # 2 years of accel history before trusting the expanding z-score


def expanding_z(s):
    m = s.expanding(min_periods=Z_MIN_PERIODS).mean()
    sd = s.expanding(min_periods=Z_MIN_PERIODS).std()
    return (s - m) / sd


z_nl = expanding_z(nl_accel)
z_gm = expanding_z(gm_accel)
full_idx = pd.period_range(min(z_nl.index.min(), z_gm.index.min()), END, freq="M")
composite_z = 0.7 * z_nl.reindex(full_idx) + 0.3 * z_gm.reindex(full_idx)
smoothed_z = composite_z.rolling(3, min_periods=3).mean()
blend = (0.5 - smoothed_z / 2.0).clip(0, 1)          # NaN while insufficient history
blend_reversed = 1 - blend

ZONE_BINS = [-0.01, 0.2, 0.4, 0.6, 0.8, 1.01]
ZONE_LABELS = ["RISK_ON", "MID_ON", "NEUTRAL", "MID_OFF", "RISK_OFF"]


def zone_of(b):
    return pd.cut(b, bins=ZONE_BINS, labels=ZONE_LABELS)


# ---------------------------------------------------------------------------

def sim(start, mode):
    """mode: 'baseline' | 'current' | 'reversed'"""
    months = pd.period_range(start, END, freq="M"); n = len(months)
    hold = {a: 0.0 for a in RISK_ON}; nav = 1.0; navs = []
    for t in months:
        present = [a for a in RISK_ON if a == "CASH" or (RET[a] is not None and t in RET[a].index and not np.isnan(RET[a].get(t, np.nan)))]
        if mode == "baseline":
            b = 0.0
        else:
            b = blend.get(t, np.nan)
            b = 0.0 if pd.isna(b) else float(b)   # insufficient history -> collapse to risk-on, per ADR-044/06-data-flow.md
            if mode == "reversed":
                b = 1 - b
        tt = {a: RISK_ON[a] * (1 - b) + RISK_OFF.get(a, RISK_ON[a]) * b for a in RISK_ON}
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
        for a in present:                          # 2x-trim rule on the CURRENT (effective) target -> gold
            if a in ("GLD", "CASH"):
                continue
            ww = hold[a] / Vn
            if ctw[a] > 0 and ww >= TRIGGER * ctw[a]:
                keep = TRIM_TO * ctw[a] * Vn
                sold += hold[a] - keep; hold["GLD"] += hold[a] - keep; hold[a] = keep
        Vc = sum(hold.values()); f = 1 - (CONTRIB + 2 * sold) * COST / Vc
        for a in hold:
            hold[a] *= f
        for a in RISK_ON:
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


emit("# Study 14 — macro-blend portfolio backtest: BASELINE vs CURRENT (ADR-044) vs REVERSED\n")
emit(f"Buy-only DCA 1000+1000/mo PLN, 15bps, 2x-trim-to-1.5x-target->gold kept in all three "
     f"variants (per user: still applies -- see header note in the script). Latest month: {END}. "
     f"Each cell = XIRR%/yr (maxDD%).\n")

emit("## Risk-on / risk-off target tables\n")
emit("| asset | risk-on % | risk-off % | retention |")
emit("|---|--:|--:|--:|")
for a in RISK_ON:
    on, off = RISK_ON[a], RISK_OFF.get(a, RISK_ON[a])
    emit(f"| {a} | {on:.3f} | {off:.3f} | {(off/on*100 if on else float('nan')):.0f}% |")
emit("")

MODES = [("BASELINE (fixed target, no macro)", "baseline"),
         ("CURRENT (ADR-044 blend, momentum framing)", "current"),
         ("REVERSED (1-blend, contrarian framing)", "reversed")]

emit("## Headline results\n")
emit("| variant | " + " | ".join(STARTS) + " |")
emit("|---|" + "--:|" * len(STARTS))
results = {}
for label, mode in MODES:
    cells = []
    for k in STARTS:
        xirr, maxdd, navs = sim(starts[k], mode)
        results[(mode, k)] = (xirr, maxdd, navs)
        cells.append(f"{xirr*100:.0f} ({maxdd*100:.0f})")
    emit(f"| {label} | " + " | ".join(cells) + " |")

emit(f"\nStart months: " + ", ".join(f"{k}={starts[k]}" for k in STARTS) + ". Cell = XIRR%/yr (maxDD%).")

# ---------------------------------------------------------------------------
# Blend regime occupancy, full history and per start-date window.
emit("\n## Blend regime occupancy (CURRENT variant's blend; REVERSED = mirror-image zones)\n")
full_blend = blend.dropna()
emit("Share of months in each zone, full available history "
     f"({full_blend.index.min()}..{full_blend.index.max()}, n={len(full_blend)}):\n")
zc = zone_of(full_blend).value_counts(normalize=True).reindex(ZONE_LABELS).fillna(0) * 100
emit("| " + " | ".join(ZONE_LABELS) + " |")
emit("|" + "--:|" * len(ZONE_LABELS))
emit("| " + " | ".join(f"{zc[z]:.0f}%" for z in ZONE_LABELS) + " |")

emit("\nPer start-date window (CURRENT variant zone occupancy, % of months):\n")
emit("| window | " + " | ".join(ZONE_LABELS) + " |")
emit("|---|" + "--:|" * len(ZONE_LABELS))
for k in STARTS:
    b = blend.loc[starts[k]:END].dropna()
    zc = zone_of(b).value_counts(normalize=True).reindex(ZONE_LABELS).fillna(0) * 100
    emit(f"| {k} | " + " | ".join(f"{zc[z]:.0f}%" for z in ZONE_LABELS) + " |")

# ---------------------------------------------------------------------------
# Contiguous regime episodes (RISK_OFF / MID_OFF stretches of 3+ months), full history.
emit("\n## Notable sustained RISK_OFF/MID_OFF episodes (blend>=0.6, 3+ consecutive months)\n")
z = zone_of(full_blend)
off_mask = full_blend >= 0.6
episodes = []
cur_start = None
for i, (t, v) in enumerate(off_mask.items()):
    if v and cur_start is None:
        cur_start = t
    if not v and cur_start is not None:
        prev_t = full_blend.index[i - 1]
        episodes.append((cur_start, prev_t))
        cur_start = None
if cur_start is not None:
    episodes.append((cur_start, full_blend.index[-1]))
episodes = [(s, e) for s, e in episodes if (e - s).n >= 2]  # 3+ months
if episodes:
    emit("| start | end | months | avg blend |")
    emit("|---|---|--:|--:|")
    for s, e in episodes:
        seg = full_blend.loc[s:e]
        emit(f"| {s} | {e} | {(e-s).n+1} | {seg.mean():.2f} |")
else:
    emit("(none of at least 3 consecutive months)")

emit("\nCaveats: miners basket return overstated (survivorship — MARA/CIFR/IREN/WULF/HUT "
     "each began trading at different points, average silently reweights among the living "
     "subset); gold rallied through most of this sample (flatters any de-risk-into-gold "
     "variant regardless of trigger quality); overlapping windows and one shared underlying "
     "price/macro history across all three variants (not independent draws); 1y/3y windows "
     "are noisy single-episode reads, not statistically robust; the risk-off table is a "
     "judgment-call adaptation of the real production V37 ratios to a simpler asset list, "
     "not itself independently re-optimized/back-tested.")

with open(os.path.join(RESULTS, "portfolio_macro_blend_backtest.md"), "w") as fh:
    fh.write("\n".join(out) + "\n")
print("\nWrote results/portfolio_macro_blend_backtest.md")

# Also dump the monthly blend series (raw) for reference/inspection.
blend_df = pd.DataFrame({"composite_z": composite_z, "smoothed_z": smoothed_z,
                          "blend": blend, "blend_reversed": blend_reversed})
blend_df.index = blend_df.index.astype(str)
blend_df.to_csv(os.path.join(RESULTS, "macro_blend_series.csv"), index_label="month")
print("Wrote results/macro_blend_series.csv")
