#!/usr/bin/env python3
"""
Study 20 -- vol-targeted / risk-parity-weighted catch-up DCA.

Copy of study 14 (`portfolio_macro_blend_backtest.py`) -- same portfolio, same
RISK_ON/RISK_OFF tables (now imported from portfolio_spec.py -- see below), same
ADR-044 blend construction, same CURRENT 1.5x/1.0x trim-to-gold rule (ADR-048; was
the 2.0x/1.5x PRIOR rule before the 2026-09-25 task-28 refresh), same 15bps cost,
same 1000 PLN + 1000 PLN/month DCA, same 5 start-date windows (1/3/5/8/13y). Study
14's own script is UNCHANGED -- this is a separate file so study 14 stays
reproducible as-is.

The ONLY thing that changes is the monthly catch-up ALLOCATION step. Study 14
(and every other DCA study in this program, per backtest/AGENTS.md's "Rebalancing
rule") dollar-weights the catch-up across every currently-underweight position:

    buy_i = A * Q_i / Q_pos                          for every Q_i > 0, else 0

This script adds a VOL-WEIGHTED variant that risk-weights the same catch-up
instead (per `.claude/skills/quant-backtest-rigor/references/methodology.md`
"Volatility Targeting" and the task file's Phase 1 design):

    buy_i = A * (Q_i / sigma_i) / sum_j(Q_j / sigma_j)   for every Q_i > 0, else 0

sigma_i = trailing realized volatility of asset i's own monthly LOG return, a
12-month rolling window, NO LOOKAHEAD: `log_ret.rolling(12, min_periods=12).std()`,
then `.shift(1)` so the value used to size month t's contribution only uses
returns realized through month t-1 (the return that lands during month t itself
is not yet known at allocation time). Annualized by `* sqrt(12)` ONLY for the
reporting/sanity-check table below -- the ratio in the buy_i formula does not
need annualizing since the sqrt(12) factor cancels in the numerator/denominator.

FALLBACK for <12 months of trailing return history (new listing, e.g. LINK near
its own inception): that asset's catch-up dollar amount is computed via the
EXISTING dollar-weighted `Q_i/Q_pos` formula
(same Q_pos denominator as the standard mechanic, summed over every present
underweight position, not a separately-renormalized subset) -- i.e. it gets
exactly the same dollar amount the pure dollar-weighted control would have given
it. The remaining budget (A minus what the no-history assets already took) is
then split vol-weighted across the assets that DO have 12mo+ of history. This
mirrors the repo's existing "insufficient history -> collapse to the safer
fallback" precedent (Z_MIN_PERIODS / "collapse to risk-on" in this same blend).

VARIANT MATRIX -- fixed N=4, decided up front, not a search (per the skill's
"decide before looking at results" rule and the task file's Phase 1 step 5):
  {BASELINE, CURRENT (ADR-044 blend)} x {dollar-weighted catch-up (control,
  = study 14's existing mechanic), vol-weighted catch-up (new)}.
No other lookback window for sigma_i was tried -- 12 months only, as specified.
REVERSED (study 14's 3rd mode) is intentionally NOT included here; this study is
about the allocation-weighting axis, not the blend-direction axis.

Metrics per variant: XIRR, maxDD (as in every other study here), plus an
annualized monthly-NAV-return Sharpe (mean/std of the monthly nav-growth-factor
return chain, x sqrt(12), NO risk-free-rate subtraction) -- for the CAGR-vs-Sharpe
vol-targeting diagnostic (skill's "Position sizing" section). NOTE on
comparability: study 04's original Sharpe convention could not be independently
re-verified against its source code, because `liquidity_timing_backtest.py` no
longer exists on disk or in git history (confirmed during this task's own
scoping -- see the task file's "Sequencing decision" section; this is exactly
the gap Phase 2 of this task exists to fix for studies 04/06). The convention
used here (annualized mean/std of per-period returns, no risk-free subtraction)
is the standard one and is explicitly named as an assumption, not a verified
match, in the write-up.

Also runs a smaller dollar-vs-vol-weighted A/B on the 60/40 classic reference
portfolio (SPY/AGG, same named weights as `classic_portfolios_backtest.py`,
BUYHOLD mode only -- deficit-proportional catch-up, no selling, no annual
rebalance) as the cross-check that any effect isn't an artifact specific to
this one concentrated, crypto-heavy book. Does not import or modify
`classic_portfolios_backtest.py` -- reuses its ticker mapping/weights and DCA
convention only, in a small self-contained function below, so that script's own
run (and its output file) is untouched.
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
SIGMA_WINDOW = 12    # months -- fixed, not scanned (task file's Phase 1 step 5)

# ---------------------------------------------------------------------------
# LIVE target tables (portfolio_spec.py SIMPLIFIED_* -- see that module for the full
# derivation/citation). REFRESHED 2026-09-25 (task 28, second pass): was the stale
# pre-ADR-047 demo proxy (Gold flat 30/30, CASH residual sink, MSTR a major line
# item); now the live tables (QQQ 22.5/27.5, FXI 2.5/7.5, ETH 10.0/1.0, LINK-USD
# 1.5/0.15 added, no CASH bucket at all). The held-fixed trim pair is now the live
# CURRENT (1.5x, 1.0x) -- the old (2.0x, 1.5x) this script modeled is PRIOR.
RISK_ON = dict(spec.SIMPLIFIED_RISK_ON)
CRYPTO = spec.SIMPLIFIED_CRYPTO

# ---------------------------------------------------------------------------
# Risk-off table -- same source (spec.SIMPLIFIED_RISK_OFF).
RISK_OFF = dict(spec.SIMPLIFIED_RISK_OFF)

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


RET = {}
for a in RISK_ON:
    RET[a] = pd.DataFrame({m: pln_ret(m) for m in MINERS}).mean(axis=1) if a == "miners" else pln_ret(TICKER[a])

END = usdpln.index.max()
STARTS = spec.STANDARD_WINDOWS
starts = {k: END - v * 12 for k, v in STARTS.items()}

# ---------------------------------------------------------------------------
# Trailing realized vol of each asset's own monthly LOG return -- 12mo rolling
# window, shift(1) so the value used at month t's allocation step only reflects
# returns realized through month t-1 (no lookahead).
SIGMA = {}
for a in RISK_ON:
    log_ret = np.log1p(RET[a])
    SIGMA[a] = log_ret.rolling(SIGMA_WINDOW, min_periods=SIGMA_WINDOW).std().shift(1)


def sigma_at(a, t):
    s = SIGMA.get(a)
    if s is None:
        return np.nan
    return s.get(t, np.nan)


# ---------------------------------------------------------------------------
# Macro blend (ADR-044): net-liq + Global-M2 YoY-acceleration, expanding no-lookahead
# z-scores, 0.7/0.3 composite, 3mo smoothing, LIVE blend = clip(0.5 + smoothed/0.5,
# 0, 1) (divisor 0.5 ADR-049, 0=risk-off/1=risk-on ADR-071 -- was the stale
# `(0.5 - smoothed_z/2.0)` before this refresh). Same construction as study 14.

def macro_m(f, c, weekly=False):
    s = pd.read_csv(os.path.join(MACRO, f), parse_dates=["date"]).set_index("date")[c].astype(float)
    if weekly:
        s = s.resample("ME").last()
    s.index = s.index.to_period("M"); return s.groupby(level=0).last()


netliq = macro_m("FED_NET_LIQUIDITY.csv", "net_liquidity_bn", weekly=True)
gm = macro_m("GLOBAL_M2.csv", "global_m2_bn")

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
blend = spec.blend_live(smoothed_z)   # NaN while insufficient history; LIVE divisor
                                      # 0.5 (ADR-049) + LIVE orientation (ADR-071)

# ---------------------------------------------------------------------------


def calc_sharpe(navs):
    """Annualized Sharpe of the monthly nav-growth-factor return chain (mean/std
    x sqrt(12)), NO risk-free-rate subtraction -- see module docstring's note on
    why study 04's exact original convention could not be re-verified."""
    r = navs / navs.shift(1).fillna(1.0) - 1.0
    sd = r.std()
    if not sd or sd <= 0 or np.isnan(sd):
        return float("nan")
    return float(r.mean() / sd * np.sqrt(12))


def sim(start, mode, weighting):
    """mode: 'baseline' | 'current'. weighting: 'dollar' | 'vol'."""
    months = pd.period_range(start, END, freq="M"); n = len(months)
    hold = {a: 0.0 for a in RISK_ON}; nav = 1.0; navs = []
    for t in months:
        present = [a for a in RISK_ON if RET[a] is not None and t in RET[a].index and not np.isnan(RET[a].get(t, np.nan))]
        if mode == "baseline":
            b = 1.0                            # for logging only (1.0 = pure risk-on, live orientation)
        else:
            b = blend.get(t, np.nan)
            # insufficient history -> collapse to risk-on (b=1.0 in the LIVE
            # orientation; was b=0.0 under the old 0=risk-on convention).
            b = 1.0 if pd.isna(b) else float(b)   # insufficient history -> collapse to risk-on, per ADR-044/06-data-flow.md
        tt = {a: spec.effective_target(RISK_ON[a], RISK_OFF.get(a, RISK_ON[a]), b) for a in RISK_ON}
        ctw = {a: tt[a] / 100.0 for a in tt}
        w = pd.Series({a: tt[a] for a in present}); w = w / w.sum()
        Vstart = sum(hold.values()) + CONTRIB
        # Real production deficit-proportional "catch-up" DCA mechanic (ADR-040/045),
        # see backtest/AGENTS.md "Rebalancing rule". O_total = current total + this
        # month's contribution; P_i = (present-renormalized) target weight_i *
        # O_total; Q_i = P_i - current_i (deficit if positive).
        N_total = sum(hold[a] for a in present)
        O_total = N_total + CONTRIB
        Q = {a: w[a] * O_total - hold[a] for a in present}
        Q_pos = sum(q for q in Q.values() if q > 0)
        if Q_pos > 1e-9:
            if weighting == "dollar":
                for a in present:
                    if Q[a] > 0:
                        hold[a] += CONTRIB * Q[a] / Q_pos
            else:  # vol-weighted catch-up
                pos_assets = [a for a in present if Q[a] > 0]
                va, vu = [], []
                for a in pos_assets:
                    s = sigma_at(a, t)
                    (va if (not pd.isna(s) and s > 1e-9) else vu).append(a)
                # Step 1: assets with <12mo history get the SAME dollar
                # amount the pure dollar-weighted control would give them (same
                # Q_pos denominator as the standard mechanic, not a resegmented one).
                vu_total = 0.0
                for a in vu:
                    amt = CONTRIB * Q[a] / Q_pos
                    hold[a] += amt
                    vu_total += amt
                # Step 2: remaining budget split vol-weighted across the assets
                # that DO have 12mo+ of trailing history.
                remaining = CONTRIB - vu_total
                if remaining > 1e-9 and va:
                    denom = sum(Q[a] / sigma_at(a, t) for a in va)
                    if denom > 1e-9:
                        for a in va:
                            hold[a] += remaining * (Q[a] / sigma_at(a, t)) / denom
                    else:                      # degenerate guard
                        qp = sum(Q[a] for a in va)
                        for a in va:
                            hold[a] += remaining * Q[a] / qp
        else:                          # degenerate guard; shouldn't trigger (Q_pos >= CONTRIB always)
            for a in present:
                hold[a] += CONTRIB * w[a]
        sold = 0.0
        Vn = sum(hold.values())
        for a in present:                          # CURRENT 1.5x/1.0x trim rule on the CURRENT (effective) target -> gold
            if a == "GLD":
                continue
            ww = hold[a] / Vn
            if ctw[a] > 0 and ww >= TRIGGER * ctw[a]:
                keep = TRIM_TO * ctw[a] * Vn
                sold += hold[a] - keep; hold["GLD"] += hold[a] - keep; hold[a] = keep
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
# 60/40 classic-portfolio cross-check -- same named weights/tickers as
# classic_portfolios_backtest.py, BUYHOLD mode only (deficit-proportional
# catch-up, no selling, no annual rebalance). Self-contained, does not import
# or modify that script.
CLASSIC_NAMED = {"SPY": 60.0, "AGG": 40.0}
CLASSIC_RET = {a: pln_ret(a) for a in CLASSIC_NAMED}
CLASSIC_SIGMA = {a: np.log1p(CLASSIC_RET[a]).rolling(SIGMA_WINDOW, min_periods=SIGMA_WINDOW).std().shift(1)
                  for a in CLASSIC_NAMED}


def sim_classic(start, weighting):
    assets = list(CLASSIC_NAMED.keys())
    months = pd.period_range(start, END, freq="M"); n = len(months)
    hold = {a: 0.0 for a in assets}; nav = 1.0; navs = []
    for t in months:
        present = [a for a in assets if t in CLASSIC_RET[a].index and not np.isnan(CLASSIC_RET[a].get(t, np.nan))]
        if not present:
            navs.append(nav); continue
        w = pd.Series({a: CLASSIC_NAMED[a] for a in present}); w = w / w.sum()
        Vstart = sum(hold.values()) + CONTRIB
        N_total = sum(hold[a] for a in present)
        O_total = N_total + CONTRIB
        Q = {a: w[a] * O_total - hold[a] for a in present}
        Q_pos = sum(q for q in Q.values() if q > 0)
        if Q_pos > 1e-9:
            if weighting == "dollar":
                for a in present:
                    if Q[a] > 0:
                        hold[a] += CONTRIB * Q[a] / Q_pos
            else:
                pos_assets = [a for a in present if Q[a] > 0]
                va, vu = [], []
                for a in pos_assets:
                    s = CLASSIC_SIGMA[a].get(t, np.nan)
                    (va if (not pd.isna(s) and s > 1e-9) else vu).append(a)
                vu_total = 0.0
                for a in vu:
                    amt = CONTRIB * Q[a] / Q_pos
                    hold[a] += amt; vu_total += amt
                remaining = CONTRIB - vu_total
                if remaining > 1e-9 and va:
                    denom = sum(Q[a] / CLASSIC_SIGMA[a].get(t) for a in va)
                    if denom > 1e-9:
                        for a in va:
                            hold[a] += remaining * (Q[a] / CLASSIC_SIGMA[a].get(t)) / denom
                    else:
                        qp = sum(Q[a] for a in va)
                        for a in va:
                            hold[a] += remaining * Q[a] / qp
        else:
            for a in present:
                hold[a] += CONTRIB * w[a]
        Vc = sum(hold.values()); f = 1 - CONTRIB * COST / Vc
        for a in hold:
            hold[a] *= f
        for a in assets:
            if hold[a] == 0:
                continue
            r = CLASSIC_RET[a].get(t, np.nan); hold[a] *= (1 + (0.0 if np.isnan(r) else r))
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


emit("# Study 20 -- vol-targeted / risk-parity-weighted catch-up DCA\n")
emit(f"Buy-only DCA 1000+1000/mo PLN, 15bps, CURRENT 1.5x/1.0x-trim->gold kept in every "
     f"variant (ADR-048 live rule; was 2.0x/1.5x PRIOR before the 2026-09-25 refresh). Latest common month: {END}. Cell = "
     f"XIRR%/yr (maxDD%) [Sharpe]. Sharpe = annualized mean/std of the monthly "
     f"nav-growth-factor return chain (x sqrt(12), no risk-free subtraction).\n")

emit("## Risk-on / risk-off target tables (unchanged from study 14)\n")
emit("| asset | risk-on % | risk-off % | retention |")
emit("|---|--:|--:|--:|")
for a in RISK_ON:
    on, off = RISK_ON[a], RISK_OFF.get(a, RISK_ON[a])
    emit(f"| {a} | {on:.3f} | {off:.3f} | {(off/on*100 if on else float('nan')):.0f}% |")
emit("")

# ---------------------------------------------------------------------------
# Sanity-check table: annualized realized vol per asset (sigma * sqrt(12)),
# median over each asset's own available (non-NaN) history -- confirms the
# vol ranking is sane (crypto >> equities/China ETF >> gold) before
# trusting the sigma-weighted allocation built on top of it.
emit("## Realized-vol sanity check (annualized trailing-12mo sigma, median over each "
     "asset's own available history)\n")
emit("| asset | median annualized vol | min | max | months with a valid sigma |")
emit("|---|--:|--:|--:|--:|")
for a in RISK_ON:
    ann = (SIGMA[a] * np.sqrt(12)).dropna()
    emit(f"| {a} | {ann.median()*100:.0f}% | {ann.min()*100:.0f}% | {ann.max()*100:.0f}% | {len(ann)} |")
emit("")

MODES = [("BASELINE (fixed target, no macro)", "baseline"),
         ("CURRENT (ADR-044 blend, momentum framing)", "current")]
WEIGHTINGS = [("dollar-weighted catch-up (control, = study 14)", "dollar"),
              ("vol-weighted catch-up (new)", "vol")]

emit("## Headline results -- fixed N=4 variant matrix (decided up front, not a scan)\n")
emit("| variant | " + " | ".join(STARTS) + " |")
emit("|---|" + "--:|" * len(STARTS))
results = {}
for mlabel, mode in MODES:
    for wlabel, weighting in WEIGHTINGS:
        cells = []
        for k in STARTS:
            xirr, maxdd, navs = sim(starts[k], mode, weighting)
            sharpe = calc_sharpe(navs)
            results[(mode, weighting, k)] = (xirr, maxdd, sharpe, navs)
            cells.append(f"{xirr*100:.0f} ({maxdd*100:.0f}) [{sharpe:.2f}]")
        emit(f"| {mlabel} / {wlabel} | " + " | ".join(cells) + " |")

emit(f"\nStart months: " + ", ".join(f"{k}={starts[k]}" for k in STARTS) + ".")

# ---------------------------------------------------------------------------
# CAGR-vs-Sharpe vol-targeting diagnostic (skill's "Position sizing" section):
# does a Sharpe improvement from vol-weighting also show up as a CAGR/XIRR
# improvement, or is it Sharpe-only (a vol-reduction artifact)?
emit("\n## CAGR-vs-Sharpe vol-targeting diagnostic (dollar-weighted control vs. vol-weighted)\n")
emit("| mode | window | XIRR dollar | XIRR vol | Delta XIRR (pp) | Sharpe dollar | Sharpe vol | Delta Sharpe |")
emit("|---|---|--:|--:|--:|--:|--:|--:|")
for mlabel, mode in MODES:
    for k in STARTS:
        xd, dd_d, sh_d = results[(mode, "dollar", k)][0], results[(mode, "dollar", k)][1], calc_sharpe(results[(mode, "dollar", k)][3])
        xv, dd_v, sh_v = results[(mode, "vol", k)][0], results[(mode, "vol", k)][1], calc_sharpe(results[(mode, "vol", k)][3])
        emit(f"| {mlabel.split(' ')[0]} | {k} | {xd*100:.1f}% | {xv*100:.1f}% | {(xv-xd)*100:+.1f} | "
             f"{sh_d:.2f} | {sh_v:.2f} | {sh_v-sh_d:+.2f} |")

# ---------------------------------------------------------------------------
emit("\n## Cross-check: 60/40 classic portfolio, dollar- vs. vol-weighted catch-up (BUYHOLD only)\n")
emit("Same SPY/AGG named weights and DCA convention as `classic_portfolios_backtest.py`'s "
     "BUYHOLD mode (deficit-proportional catch-up, no selling, no annual rebalance) -- this "
     "script's own self-contained `sim_classic()`, not an import of that file. Purpose: check "
     "whether any dollar-vs-vol effect seen on the user's concentrated crypto-heavy book also "
     "shows up on a plain, diversified 2-asset textbook portfolio.\n")
emit("| weighting | " + " | ".join(STARTS) + " |")
emit("|---|" + "--:|" * len(STARTS))
classic_results = {}
for wlabel, weighting in WEIGHTINGS:
    cells = []
    for k in STARTS:
        xirr, maxdd, navs = sim_classic(starts[k], weighting)
        sharpe = calc_sharpe(navs)
        classic_results[(weighting, k)] = (xirr, maxdd, sharpe)
        cells.append(f"{xirr*100:.0f} ({maxdd*100:.0f}) [{sharpe:.2f}]")
    emit(f"| {wlabel} | " + " | ".join(cells) + " |")

emit("\n| window | XIRR dollar | XIRR vol | Delta XIRR (pp) | Sharpe dollar | Sharpe vol | Delta Sharpe |")
emit("|---|--:|--:|--:|--:|--:|--:|")
for k in STARTS:
    xd, _, sh_d = classic_results[("dollar", k)]
    xv, _, sh_v = classic_results[("vol", k)]
    emit(f"| {k} | {xd*100:.1f}% | {xv*100:.1f}% | {(xv-xd)*100:+.1f} | {sh_d:.2f} | {sh_v:.2f} | {sh_v-sh_d:+.2f} |")

emit("\nCaveats: same caveats as study 14 apply unchanged (miners survivorship, gold's rally "
     "flatters de-risk-into-gold variants, overlapping windows share one underlying "
     "price/macro history, 1y/3y windows are noisy single-episode reads, the RISK_OFF table "
     "is a judgment-call adaptation not independently re-optimized); additionally: sigma_i is "
     "computed over each asset's FULL available price history (not reset per backtest "
     "window), so by the time any of the 5 windows starts, only genuinely recently-listed "
     "assets (SUI, COIN, near their own inception) can still be in the <12mo fallback state -- "
     "see the sanity-check table above for how many months of sigma each asset actually has; "
     "only ONE lookback window (12 months) was tried for sigma_i, not scanned -- if a "
     "different window is tried in a future study, that must be counted and labeled as its own "
     "scan, not silently folded into this fixed N=4 comparison; study 04's exact original "
     "Sharpe convention could not be re-verified (that script no longer exists, see module "
     "docstring) -- the convention used here (annualized mean/std, no risk-free subtraction) "
     "is the standard one, named explicitly as an assumption.")

with open(os.path.join(RESULTS, "vol_targeted_dca_backtest.md"), "w") as fh:
    fh.write("\n".join(out) + "\n")
print("\nWrote results/vol_targeted_dca_backtest.md")
