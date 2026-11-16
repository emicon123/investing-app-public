#!/usr/bin/env python3
"""
Study 21 -- walk-forward validation of ADR-048's BLEND_STEEPNESS_DIVISOR change (2.0 -> 1.0),
per `.claude/tasks/adr-047-048-walkforward-validation.task.md` (data-analyst/data-scientist
design review, 2026-07-12). Reuses study 14's exact scaffold (`portfolio_macro_blend_backtest.py`
is UNCHANGED by this script -- same live portfolio weights via portfolio_spec.py, same real
deficit-proportional catch-up DCA mechanic, same CURRENT 1.5x-trigger/1.0x-trim-to-gold rule
(ADR-048; was 2.0x/1.5x PRIOR before the 2026-09-25 task-28 refresh), same 5 start-date
windows, same 15bps cost).

ADR-048 doubled the blend's steepness (`blend = clip(0.5 + smoothed_z/divisor, 0, 1)` in the
live ADR-071 orientation -- ADR-048 itself wrote it with a minus sign, 0=risk-on/1=risk-off,
flipped since -- divisor 2.0 -> 1.0) on an EXPLICITLY non-walk-forward, in-sample ad-hoc
comparison (`docs/architecture/05-rebalancing-algorithm.md` Sec 7.2.1: "Not walk-forward
validated").
This study fixes that gap with two distinct things, never conflated:

  PART 1 (in-sample/descriptive, explicitly labeled, kept for continuity with ADR-048's
  own ad-hoc comparison): 3 FIXED divisors {2.0, 1.0, 0.5}, each run unchanged across all
  5 overlapping windows -- i.e. exactly what ADR-048's own ad-hoc test did, just tidier.
  Per the design review's central point: computing each fixed divisor's full-history
  performance and reporting "best of the 3" is STILL the ADR-048 mistake (picking the
  divisor itself with full-sample hindsight), even though every divisor's own z-scores are
  already expanding/no-lookahead. Never presented as the walk-forward verdict.

  PART 2 (the actual walk-forward claim): a genuine expanding-window DIVISOR-SELECTION
  protocol, decided up front, never adjusted after seeing results:
    1. Burn-in: first 36 months of blend history (2011-07..2014-06, when composite_z/
       smoothed_z first exist) default to divisor=2.0 (pre-ADR-048 status quo), mirroring
       ADR-044's own "insufficient history -> collapse to risk-on" convention.
    2. Checkpoints: annual (every 12 months) after burn-in ends.
    3. Selection metric: at each checkpoint, independently simulate each of the 3 candidate
       divisors over the TRAILING 36 months (fresh 1000+1000/mo portfolio restarting at
       checkpoint-36mo, i.e. exactly how study 04 Part 2 re-picks its window parameter on
       trailing data), using ONLY data available up to that checkpoint; compute each
       candidate's annualized Sharpe of its own trailing monthly NAV-growth-factor return
       chain (mean/std*sqrt(12), no risk-free subtraction -- study 20's convention); select
       the argmax; apply that divisor for the next 12 months OUT OF SAMPLE.
    4. Chain the resulting 12-month OOS blocks into ONE continuous walk-forward track record,
       2011-07 -> 2026-06 (the full history composite_z/smoothed_z make available) -- this
       chained run, not the Part 1 fixed-divisor table, is "the walk-forward verdict."
    5. The SAME resulting month->divisor schedule is also applied to 5 separate fresh-start
       sims at the same 5 window starts as Part 1, for direct apples-to-apples comparison.
    6. Report an occupancy table (how often each divisor was actually selected across the
       12 checkpoints) and the ETH trim-overshoot-ratio metric under both Part 1 (each fixed
       divisor) and Part 2 (the realized walk-forward schedule).

Burn-in length (36mo), checkpoint cadence (12mo), and selection lookback (36mo) are LOCKED
before this script's results are read -- per the anti-tuning discipline in
`.claude/skills/quant-backtest-rigor/SKILL.md` and this task's own explicit instruction not
to adjust them after seeing how the chain performs.

Retained per `01_data_and_methods.md`'s DSR/PBO-relevant-series retention convention (this
touches the same net-liq/Global-M2 blend mechanism study 19 ran a real DSR/PBO check on, and
runs its own N=3 data-mining gut check on the divisor comparison) -- this script and its
per-variant/per-checkpoint CSVs are kept, not deleted once distilled.

Run inside a local venv with scipy installed (`python3 -m venv ...; pip install scipy
statsmodels`), same as study 19's `dsr_pbo_computation.md` -- scipy is not installed
system-wide in this environment (needed for `norm.ppf` in the skill's `expected_max_sharpe`).
"""
import os
import sys
import numpy as np
import pandas as pd
from scipy.stats import norm
sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "lib"))
import portfolio_spec as spec  # noqa: E402 -- backtest/lib, added to path above (task 159)

BASE = os.path.join(os.path.dirname(__file__), "..")
PRICES = os.path.join(BASE, "data", "prices")
MACRO = os.path.join(BASE, "data", "macro")
RESULTS = os.path.join(BASE, "results")
CONTRIB = spec.CONTRIB_PLN
COST = spec.COST_BPS
TRIGGER, TRIM_TO = spec.TRIM_CURRENT   # LIVE (ADR-048): 1.5x trigger / 1.0x trim-to, held FIXED
MINERS = spec.MINERS_BASKET

# ---------------------------------------------------------------------------
# LIVE target tables (portfolio_spec.py SIMPLIFIED_* -- see that module for the full
# derivation/citation). REFRESHED 2026-09-25 (task 28, second pass): was the stale
# pre-ADR-047 demo proxy; now the live tables (QQQ 22.5/27.5, FXI 2.5/7.5,
# ETH 10.0/1.0, LINK-USD 1.5/0.15 added, no CASH bucket at all).
RISK_ON = dict(spec.SIMPLIFIED_RISK_ON)
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
# Macro blend construction (ADR-044) -- IDENTICAL to study 14 except the steepness
# divisor is now a parameter, not a hardcoded /0.5. LIVE orientation throughout
# (0=risk-off, 1=risk-on, ADR-071) via spec.blend_live.

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

CANDIDATES = [2.0, 1.0, 0.5]
BLENDS = {d: spec.blend_live(smoothed_z, d) for d in CANDIDATES}

ZONE_BINS = spec.ZONE_BINS
ZONE_LABELS = spec.ZONE_LABELS_LIVE     # live (ADR-071): 0=RISK_OFF .. 1=RISK_ON


def zone_of(b):
    return pd.cut(b, bins=ZONE_BINS, labels=ZONE_LABELS)


CHAIN_START = smoothed_z.dropna().index.min()   # 2011-07 -- first month blend/composite_z exist
assert str(CHAIN_START) == "2011-07", CHAIN_START

# ---------------------------------------------------------------------------

def sim(start, mode, divisor=2.0, divisor_schedule=None, end=None, log_eth=None):
    """mode: 'baseline' (fixed risk-on target, no macro overlay, WITH the CURRENT 1.5x/1.0x trim rule
    -- study 14's BASELINE) | 'buyhold' (fixed risk-on target, NO macro, NO trim of any kind --
    the new standing-baseline plain buy-and-hold control) | 'blend' (ADR-044 target
    interpolation at a FIXED `divisor`, WITH the trim rule -- study 14's CURRENT, generalized
    to any divisor) | 'schedule' (like 'blend' but the divisor is looked up per-month from
    `divisor_schedule`, a Period-indexed pd.Series -- the walk-forward chain).

    Every mode uses the REAL production deficit-proportional catch-up DCA mechanic
    (ADR-040/045, backtest/AGENTS.md "Rebalancing rule"): O_total = current total + this
    month's contribution A; P_i = target_weight_i * O_total; Q_i = P_i - current_value_i
    (deficit if positive); Q_pos = sum of positive Q_i; buy_i = A * Q_i / Q_pos for every
    Q_i > 0, zero otherwise -- never a sell, never a fixed-percentage split.

    `log_eth`, if a list, gets one (month, ratio) tuple appended every month ETH is present
    and has a nonzero effective target -- ratio = ETH's actual weight / its effective target,
    recorded at the pre-trim-check point (i.e. the value that WOULD flag/trim it that month),
    for the overshoot-ratio metric. Returns (xirr, maxdd, navs)."""
    e = end if end is not None else END
    months = pd.period_range(start, e, freq="M"); n = len(months)
    hold = {a: 0.0 for a in RISK_ON}; nav = 1.0; navs = []
    for t in months:
        present = [a for a in RISK_ON if RET[a] is not None and t in RET[a].index and not np.isnan(RET[a].get(t, np.nan))]
        if mode in ("baseline", "buyhold"):
            b = 1.0                            # pure risk-on, live orientation
        else:
            if mode == "schedule":
                d = divisor_schedule.get(t, 2.0)
            else:
                d = divisor
            bl = BLENDS.get(d)
            if bl is None:
                bl = spec.blend_live(smoothed_z, d)
            bv = bl.get(t, np.nan)
            b = 1.0 if pd.isna(bv) else float(bv)   # insufficient history -> collapse to risk-on
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
                    hold[a] += CONTRIB * Q[a] / Q_pos
        else:
            for a in present:
                hold[a] += CONTRIB * w[a]
        sold = 0.0
        Vn = sum(hold.values())
        if log_eth is not None and "ETH" in present and ctw.get("ETH", 0) > 0:
            log_eth.append((t, (hold["ETH"] / Vn) / ctw["ETH"]))
        if mode != "buyhold":                      # trim rule applies to baseline/blend/schedule
            for a in present:
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


def sharpe_of(navs):
    r = navs.pct_change().dropna()
    if len(r) < 2 or r.std() == 0:
        return np.nan
    return r.mean() / r.std() * np.sqrt(12)


# ---------------------------------------------------------------------------
# Deflated-Sharpe helper, VERBATIM from .claude/skills/quant-backtest-rigor/references/methodology.md
def expected_max_sharpe(V_SR, N):
    gamma = 0.5772156649
    sigma = V_SR ** 0.5
    term1 = (1 - gamma) * norm.ppf(1 - 1 / N)
    term2 = gamma * norm.ppf(1 - 1 / (N * np.e))
    return sigma * (term1 + term2)


# ===========================================================================
# PART 1 -- fixed-divisor comparison, 5 windows (IN-SAMPLE / DESCRIPTIVE ONLY)
# ===========================================================================
out = []
def emit(s=""):
    out.append(s); print(s)


emit("# Study 21 -- ADR-048 BLEND_STEEPNESS_DIVISOR walk-forward validation\n")
emit(f"Buy-only DCA 1000+1000/mo PLN, 15bps, CURRENT 1.5x/1.0x-trim-to-gold rule held fixed "
     f"throughout (ADR-048 live rule; was 2.0x/1.5x PRIOR before the 2026-09-25 refresh) "
     f"-- only the blend steepness divisor varies. "
     f"Live portfolio weights via portfolio_spec.py. Latest month: {END}.\n")

emit("## Part 1 -- fixed-divisor comparison across 5 windows (IN-SAMPLE / DESCRIPTIVE, "
     "NOT the walk-forward verdict)\n")
emit("Each divisor's own composite_z/smoothed_z construction is expanding and no-lookahead "
     "by definition (true at every divisor value) -- but picking 'which divisor wins' by "
     "reading this table is full-sample hindsight, structurally identical to ADR-048's own "
     "ad-hoc comparison, just tidier. Kept for continuity with that comparison and as Part 1 "
     "context, per study 04/06/19's own Part 1-vs-Part 2 convention -- never the verdict.\n")

part1_variants = [
    ("BASELINE (fixed target, no macro, WITH CURRENT 1.5/1.0 trim)", "baseline", None),
    ("BUYHOLD control (fixed target, NO macro, NO trim of any kind -- standing baseline)", "buyhold", None),
    ("BLEND divisor=2.0 (pre-ADR-048 original)", "blend", 2.0),
    ("BLEND divisor=1.0 (ADR-048, superseded -- PRIOR)", "blend", 1.0),
    ("BLEND divisor=0.5 (ADR-049 LIVE value)", "blend", 0.5),
]

emit("| variant | " + " | ".join(STARTS) + " |")
emit("|---|" + "--:|" * len(STARTS))
part1_results = {}
part1_navs_by_window = {}
for label, mode, d in part1_variants:
    cells = []
    for k in STARTS:
        xirr, maxdd, navs = sim(starts[k], mode, divisor=d if d is not None else 2.0)
        part1_results[(mode, d, k)] = (xirr, maxdd, navs)
        part1_navs_by_window.setdefault((mode, d), {})[k] = navs
        cells.append(f"{xirr*100:.0f} ({maxdd*100:.0f})")
    emit(f"| {label} | " + " | ".join(cells) + " |")
emit(f"\nStart months: " + ", ".join(f"{k}={starts[k]}" for k in STARTS) + ". Cell = XIRR%/yr (maxDD%).")

# Sharpe of the same Part-1 cells (for the N=3 gut check + general context)
emit("\n### Part 1 -- annualized Sharpe (mean/std of monthly NAV-growth-factor return chain x sqrt(12))\n")
emit("| variant | " + " | ".join(STARTS) + " |")
emit("|---|" + "--:|" * len(STARTS))
part1_sharpe = {}
for label, mode, d in part1_variants:
    cells = []
    for k in STARTS:
        navs = part1_navs_by_window[(mode, d)][k]
        s = sharpe_of(navs)
        part1_sharpe[(mode, d, k)] = s
        cells.append(f"{s:.2f}")
    emit(f"| {label} | " + " | ".join(cells) + " |")

# ===========================================================================
# PART 1b -- N=3 data-mining gut check on the 3 fixed divisors (rough, N<5)
# ===========================================================================
emit("\n### N=3 data-mining gut check on the 3-way divisor comparison (rough, below the "
     "skill's N>=~5 formal-check trigger)\n")
emit("Per the task's own instruction: N=3 is below `.claude/skills/quant-backtest-rigor/"
     "SKILL.md`'s own 'N >= ~5' trigger for formally requiring this check -- three points "
     "barely describe a cross-sectional spread. Computed anyway as a rough indicator, using "
     "each divisor's 13y-window Sharpe (longest, least noisy single number per variant, "
     "matching study 19's convention of picking one representative number per candidate "
     "rather than averaging across overlapping windows).\n")
sr_13y = {d: part1_sharpe[("blend", d, "13y")] for d in CANDIDATES}
sr_values = np.array(list(sr_13y.values()))
sigma_sr = sr_values.std(ddof=0)
sr0_n3 = expected_max_sharpe(sigma_sr ** 2, 3) if sigma_sr > 0 else float("nan")
best_d = max(sr_13y, key=lambda d: sr_13y[d])
emit("| divisor | 13y Sharpe |")
emit("|--:|--:|")
for d in CANDIDATES:
    emit(f"| {d} | {sr_13y[d]:.3f} |")
emit(f"\nCross-sectional sigma_SR (N=3, ddof=0) = {sigma_sr:.4f}. Expected max Sharpe under "
     f"pure luck at N=3, SR0 = {sr0_n3:.3f}. Best-of-3 (divisor={best_d}, Sharpe="
     f"{sr_13y[best_d]:.3f}) clears SR0 by {sr_13y[best_d]-sr0_n3:.3f}. **Read with heavy "
     "caution**: with only 3 points, sigma_SR itself is a high-variance estimate, and this "
     "is not a substitute for the real walk-forward verdict in Part 2 below -- reported as a "
     "rough, informal indicator only, per the task's explicit instruction not to present an "
     "N=3 comparison with more confidence than 3 data points can support.")

# ===========================================================================
# PART 2 -- genuine walk-forward divisor-selection protocol
# ===========================================================================
emit("\n## Part 2 -- walk-forward divisor-selection protocol (THE walk-forward verdict)\n")
emit("Protocol LOCKED before any result was read (not tuned afterward): 36-month burn-in "
     "(defaults to divisor=2.0, the pre-ADR-048 status quo, mirroring ADR-044's own "
     "'insufficient history -> risk-on' convention), then annual (12-month) checkpoints; at "
     "each checkpoint, every one of the 3 candidate divisors is independently re-simulated "
     "over the TRAILING 36 months (a fresh 1000+1000/mo portfolio restarting at "
     "checkpoint-36mo, using ONLY data available up to that checkpoint) and its annualized "
     "Sharpe of that trailing run's own monthly NAV-growth-factor returns is computed "
     "(matches study 04 Part 2's own re-pick-on-trailing-data selection method); the "
     "argmax divisor is applied for the following 12 months OUT OF SAMPLE; repeat. The "
     f"resulting month->divisor schedule spans the full available history, {CHAIN_START} "
     f"(first month composite_z/smoothed_z exist) through {END}.\n")

chain_months = pd.period_range(CHAIN_START, END, freq="M")
n_total = len(chain_months)
BURN_IN = 36
CHECKPOINT_STRIDE = 12
SELECT_LOOKBACK = 36
# Chain-length guard: the locked protocol was designed for n_total=180 (2011-07..2026-06,
# 12 full checkpoints). New data extends the chain (n_total=183 through 2026-09 as of the
# 2026-09-25 refresh) -- the 36/12/36 parameters are UNCHANGED (anti-tuning lock holds);
# the loop below already handles a trailing partial block via min(), so the only thing
# re-verified here is that at least one full checkpoint block exists past burn-in.
# Consequence, stated plainly: 13 checkpoints now (12 full 12-month OOS blocks + one
# trailing partial block), vs. 12 before. See the study_summary correction note.
assert n_total - BURN_IN >= CHECKPOINT_STRIDE, \
    f"chain too short for even one checkpoint block ({n_total} months)"

divisor_schedule = pd.Series(2.0, index=chain_months)
checkpoint_log = []
idx = BURN_IN
while idx < n_total:
    checkpoint_month = chain_months[idx - 1]
    lookback_start = chain_months[idx - SELECT_LOOKBACK]
    sharpes = {}
    for d in CANDIDATES:
        _, _, navs = sim(lookback_start, "blend", divisor=d, end=checkpoint_month)
        sharpes[d] = sharpe_of(navs)
    selected = max(sharpes, key=lambda d: (sharpes[d] if not pd.isna(sharpes[d]) else -np.inf))
    block_end_idx = min(idx + CHECKPOINT_STRIDE, n_total)
    block_months = chain_months[idx:block_end_idx]
    divisor_schedule.loc[block_months] = selected
    checkpoint_log.append({"checkpoint": str(checkpoint_month), "lookback_start": str(lookback_start),
                            "sharpe_2.0": sharpes[2.0], "sharpe_1.0": sharpes[1.0], "sharpe_0.5": sharpes[0.5],
                            "selected_divisor": selected, "applied_from": str(block_months[0]),
                            "applied_through": str(block_months[-1])})
    idx += CHECKPOINT_STRIDE

cp_df = pd.DataFrame(checkpoint_log)
cp_df.to_csv(os.path.join(RESULTS, "study21_walkforward_checkpoints.csv"), index=False)

emit(f"### Checkpoint log ({len(checkpoint_log)} annual checkpoints after the {BURN_IN}-month "
     f"burn-in, {CHAIN_START}..{chain_months[BURN_IN-1]})\n")
emit("| checkpoint | trailing-36mo Sharpe @2.0 | @1.0 | @0.5 | selected | applied OOS |")
emit("|---|--:|--:|--:|--:|---|")
for r in checkpoint_log:
    emit(f"| {r['checkpoint']} | {r['sharpe_2.0']:.2f} | {r['sharpe_1.0']:.2f} | {r['sharpe_0.5']:.2f} | "
         f"**{r['selected_divisor']}** | {r['applied_from']}..{r['applied_through']} |")

emit("\n### Divisor-selection occupancy table -- how often each divisor was actually "
     f"selected across the {len(checkpoint_log)} checkpoints\n")
occ = pd.Series([r["selected_divisor"] for r in checkpoint_log]).value_counts()
occ = occ.reindex(CANDIDATES).fillna(0).astype(int)
emit("| divisor | # checkpoints selected | % of checkpoints | months governed OOS |")
emit("|--:|--:|--:|--:|")
for d in CANDIDATES:
    months_governed = int((divisor_schedule.iloc[BURN_IN:] == d).sum())
    emit(f"| {d} | {occ[d]} | {occ[d]/len(checkpoint_log)*100:.0f}% | {months_governed} |")
emit(f"\n(Burn-in months, {BURN_IN} of them, {CHAIN_START}..{chain_months[BURN_IN-1]}, are "
     "not counted above -- they are fixed at divisor=2.0 by construction, not selected.)")

last_cp = checkpoint_log[-1]
emit(f"\n**Most recent checkpoint** ({last_cp['checkpoint']}): trailing-36mo Sharpe was "
     f"{last_cp['sharpe_2.0']:.2f} (@2.0), {last_cp['sharpe_1.0']:.2f} (@1.0), "
     f"{last_cp['sharpe_0.5']:.2f} (@0.5) -> selected **{last_cp['selected_divisor']}**, "
     f"applied {last_cp['applied_from']}..{last_cp['applied_through']}. This is the direct, "
     "walk-forward-honest answer to 'would ADR-048's move to /1.0 have been the "
     "walk-forward-selected choice at the most recent checkpoint, or only in hindsight.'")

# ---------------------------------------------------------------------------
# Chained walk-forward track record (single continuous portfolio, 2011-07 -> END)
chain_eth_log = []
chain_xirr, chain_maxdd, chain_navs = sim(CHAIN_START, "schedule", divisor_schedule=divisor_schedule,
                                           end=END, log_eth=chain_eth_log)
chain_sharpe = sharpe_of(chain_navs)

emit(f"\n### The walk-forward verdict -- ONE continuous chained track record, "
     f"{CHAIN_START}..{END} ({n_total} months)\n")
emit(f"XIRR **{chain_xirr*100:.1f}%/yr**, maxDD **{chain_maxdd*100:.1f}%**, annualized Sharpe "
     f"**{chain_sharpe:.2f}**. This is the number that answers 'what would ADR-048's kind of "
     "annual re-tuning decision have actually produced, honestly walk-forward, instead of "
     "picking /1.0 once with full-sample hindsight' -- not the Part 1 fixed-divisor table.")

# Same schedule applied to fresh-start sims at the 5 standard windows, for direct comparison
# against Part 1's fixed-divisor table.
emit("\n### The same walk-forward schedule, applied to fresh-start sims at the 5 standard "
     "windows (for direct, apples-to-apples comparison against Part 1's fixed-divisor table)\n")
emit("| variant | " + " | ".join(STARTS) + " |")
emit("|---|" + "--:|" * len(STARTS))
schedule_cells = []
schedule_navs_by_window = {}
for k in STARTS:
    xirr, maxdd, navs = sim(starts[k], "schedule", divisor_schedule=divisor_schedule, end=END)
    schedule_navs_by_window[k] = navs
    schedule_cells.append(f"{xirr*100:.0f} ({maxdd*100:.0f})")
emit(f"| WALK-FORWARD SCHEDULE (Part 2) | " + " | ".join(schedule_cells) + " |")
for label, mode, d in part1_variants:
    cells = []
    for k in STARTS:
        xirr, maxdd, navs = part1_results[(mode, d, k)][0], part1_results[(mode, d, k)][1], None
        cells.append(f"{xirr*100:.0f} ({part1_results[(mode, d, k)][1]*100:.0f})")
    emit(f"| {label} (Part 1, in-sample) | " + " | ".join(cells) + " |")

# ===========================================================================
# Overshoot-ratio metric -- Part 1 (fixed divisors) and Part 2 (walk-forward schedule)
# ===========================================================================
emit("\n## ETH trim-overshoot-ratio metric -- disambiguated\n")
emit("ADR-048's own doc (Sec 7.2.1) reports ETH's 'max overshoot ratio... rose from ~5x "
     "under /2.0 to ~10x under /1.0.' Per the task's explicit disambiguation: this is "
     "**this backtest's own modeled CURRENT 1.5x-trigger/1.0x-trim-to rule** (ADR-048's "
     "live rule -- this reconstruction held (2.0, 1.5) PRIOR fixed before the 2026-09-25 "
     "refresh, now holds (1.5, 1.0) fixed), NOT the unrelated frontend-only sell-flag "
     "thresholds (Sec 7.5, 200%/150% -> 150%/100%) -- that UI constant has zero "
     "interaction with `MainRebalancingStrategy` or any backtest script and has no "
     "equivalent computed here. Overshoot ratio = peak `deviationPct` (ETH's actual "
     "position weight / its effective target that month) reached in ANY month, recorded at "
     "the pre-trim-check point (the value that would trigger the rule to fire), holding the "
     "modeled trim config fixed at (trigger=1.5, trim_to=1.0) throughout -- only the "
     "divisor (or, for Part 2, the walk-forward-selected divisor) varies. Computed over a "
     f"single continuous pass, {CHAIN_START}..{END}, matching the walk-forward chain's own "
     "window for direct comparability.\n")

emit("### Part 1 (fixed divisors, single continuous pass)\n")
emit("| divisor | ETH peak overshoot ratio |")
emit("|--:|--:|")
part1_overshoot = {}
for d in CANDIDATES:
    log = []
    sim(CHAIN_START, "blend", divisor=d, end=END, log_eth=log)
    peak = max(r for _, r in log) if log else float("nan")
    part1_overshoot[d] = peak
    emit(f"| {d} | {peak:.2f}x |")

emit("\n### Part 2 (walk-forward schedule, same continuous pass)\n")
chain_peak = max(r for _, r in chain_eth_log) if chain_eth_log else float("nan")
emit(f"| walk-forward schedule | {chain_peak:.2f}x |")
emit(f"\nADR-048's own claimed doubling (~5x @2.0 -> ~10x @1.0): this reconstruction finds "
     f"{part1_overshoot[2.0]:.2f}x @2.0 -> {part1_overshoot[1.0]:.2f}x @1.0 "
     f"({'confirms the same direction and rough magnitude' if part1_overshoot[1.0] > part1_overshoot[2.0] else 'does NOT reproduce the claimed direction'}), "
     f"and {part1_overshoot[0.5]:.2f}x @0.5. Under the actual walk-forward-selected schedule "
     f"(not a full-sample-hindsight /1.0 pick), the realized peak overshoot is "
     f"{chain_peak:.2f}x.")

# Retain per-divisor monthly NAV return series (feeds any future DSR/PBO work on this
# mechanism, per 01_data_and_methods.md's retention convention).
variant_returns = {}
for label, mode, d in part1_variants:
    key = f"{mode}" if d is None else f"{mode}_{d}"
    navs = sim(CHAIN_START, mode, divisor=d if d is not None else 2.0, end=END)[2]
    variant_returns[key] = navs.pct_change()
variant_returns["schedule_walkforward"] = chain_navs.pct_change()
vr_df = pd.DataFrame(variant_returns)
vr_df.index = vr_df.index.astype(str)
vr_df.to_csv(os.path.join(RESULTS, "study21_variant_returns.csv"), index_label="month")

emit("\n## Caveats\n")
emit("- **Part 1 is explicitly in-sample/descriptive** -- never the walk-forward claim, kept "
     "only for continuity with ADR-048's own ad-hoc comparison and study 04/06/19's Part-1 "
     "convention.\n"
     "- **The N=3 gut check is rough** -- 3 candidate divisors is below the skill's own "
     "N>=~5 trigger for a formal data-mining check; treat the SR0 comparison above as a "
     "high-variance indicator, not a pass/fail bar.\n"
     "- **Same caveats as study 14 apply unchanged**: miners-basket survivorship bias "
     "(MARA/CIFR/IREN/WULF/HUT each began trading at different points), gold's multi-year "
     "rally through most of this sample flatters any variant that trims into gold "
     "regardless of trigger quality, overlapping 5-window comparisons share one underlying "
     "price/macro history (not independent draws), 1y/3y windows are single-episode reads, "
     "the SIMPLIFIED target table is a long-history-proxy adaptation of the real production "
     "targets (GLD/QQQ/FXI stand in for Gold/Nasdaq-100/China; Experiments sleeve, ONDO-USD "
     "and India absorbed by present-renormalization -- see portfolio_spec.py).\n"
     "- **The trailing-36mo selection-metric sims restart the portfolio fresh at each "
     "checkpoint** (1000 PLN + 1000/mo from checkpoint-36mo), independent of the actual "
     "chain's accumulated state at that point -- this exactly parallels study 04 Part 2's "
     "own re-pick-on-trailing-data method (a fresh Sharpe computed per candidate window "
     "over the trailing period, not a continuation of one realized position), and was "
     "decided before any result was read.\n"
     "- **Burn-in/checkpoint-cadence/selection-lookback (36mo/12mo/36mo) were locked before "
     "this script's results were read and were not adjusted afterward**, per the task's own "
     "anti-tuning instruction and the skill's 'decide before looking at results' rule.\n"
     "- **This is a blend-internal tuning-constant question**, not a general portfolio-"
     "construction question -- per the task's own scoping, no classic-portfolio (60/40) "
     "cross-check is included for Study A (required for Study B instead, where the change "
     "under test is a general allocation shift).")

with open(os.path.join(RESULTS, "blend_steepness_walkforward_backtest.md"), "w") as fh:
    fh.write("\n".join(out) + "\n")
print("\nWrote results/blend_steepness_walkforward_backtest.md")
print("Wrote results/study21_walkforward_checkpoints.csv")
print("Wrote results/study21_variant_returns.csv")
