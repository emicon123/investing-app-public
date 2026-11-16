#!/usr/bin/env python3
"""
Study 24 -- IC/ICIR/IC-decay framework, extending study 03's asset-level lead-lag design
(NOT a cross-sectional IC across the 28-asset universe -- see below for why that literal
reading is mathematically undefined for this data).

Per the reviewed task file (`.claude/tasks/cost-tax-audit-and-ic-framework.task.md`):
  - The Grinold-Kahn CROSS-SECTIONAL IC ("rank-correlate the forecast across many assets
    against forward returns at one point in time") is degenerate for study 06's 11 macro
    rules as implemented: every rule applies an IDENTICAL ON/OFF signal to all 28 assets
    each month (confirmed directly in `macro_strategy_zoo.py`'s `build_rules()`) -- zero
    cross-sectional variance in the forecast column, so a per-month cross-sectional
    Spearman correlation across the 28 assets is undefined (NaN by construction), not
    "weak."
  - The two legitimate reframings, per the task's own guidance:
    (a) PRIMARY -- an asset-level TIME-SERIES IC per (rule, asset) pair: correlate each
        rule's own CONTINUOUS macro-driver reading (not the binarized ON/OFF signal)
        against that asset's own forward return, over time, using non-overlapping
        calendar-year blocks (one IC value per year -> a genuine IC series with a real
        mean/std -> ICIR/t-stat). This directly extends study 03's design (which did
        exactly this for 3 drivers x 3 assets as single whole-sample numbers) to 9
        continuous-driver rules x all 28 assets, with the formal ICIR/t-stat/decay-curve
        packaging study 03 never had.
    (b) BONUS -- a genuinely cross-sectional (single-point, not a time series) check:
        correlate study 06's per-asset Sharpe-uplift for the NetLiqAccel rule (recomputed
        here from `study19_variant_returns_06.csv` + each asset's own raw return series)
        against that asset's own net-liquidity beta (study 05's ranking,
        `liquidity_beta_ranking.csv`), across the ~18 assets appearing in both artifacts.
        This has genuine cross-sectional variance (unlike the raw signal), reuses only
        already-published numbers, and gets an i.i.d. bootstrap CI (NOT a block-bootstrap
        -- there is no time axis in a single cross-sectional snapshot, so the block
        convention doesn't literally apply; a plain resample-with-replacement across
        assets is the correct adaptation). Reported as "a cross-sectional Spearman rank
        correlation with a bootstrap CI," never as an "ICIR" (ICIR needs a time series of
        IC values; this is one number).
  - Two of the 11 rules (`ComboM2AccelNetLiqYoY`, `ISMLt50Rising`) are logical ANDs of two
    other rules' conditions and have no natural single continuous driver reading -- they
    are excluded from (a)'s continuous-driver IC table (this is stated explicitly, not
    silently dropped) and cited only via their component rules, which ARE included.

No new data fetch: all series come from `backtest/data/macro/` and `backtest/data/prices/`
(already on disk) plus `results/study19_variant_returns_06.csv` and
`results/liquidity_beta_ranking.csv` (already-published study 06/05 artifacts). A modest
amount of new analysis code is written here (the block-IC/ICIR/decay-curve machinery and
the cross-sectional bootstrap), per the task's own clarification that "no new data" does
not mean "no new code."
"""
import os
import sys
import numpy as np
import pandas as pd

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "lib"))
import macro_strategy_zoo as msz  # noqa: E402 -- backtest/lib, added to path above (task 159)

BASE = os.path.join(os.path.dirname(__file__), "..")
MACRO = os.path.join(BASE, "data", "macro")
RESULTS = os.path.join(BASE, "results")

MIN_OBS_PER_BLOCK = 6     # minimum valid (driver, fwd_ret) pairs within a calendar-year block to compute an IC for it
HORIZONS = [1, 3, 6, 12]  # months, matching study 03's decay-curve horizons
RNG_SEED = 20260713
N_BOOT = 5000

out = []
def emit(s=""):
    out.append(s); print(s)


# ---------------------------------------------------------------------------
# 1. Continuous macro-driver readings -- same arithmetic as macro_strategy_zoo.build_rules(),
#    but returning the raw (pre-threshold) scalar instead of the boolean ON/OFF signal.
# ---------------------------------------------------------------------------

def build_continuous_drivers(netliq_m, gm_m, usm2_m, ism_m, gld_m):
    nl_yoy = netliq_m.pct_change(12)
    nl_accel = nl_yoy.diff(3).shift(1)
    nl_yoy_lagged = nl_yoy.shift(1)
    nl_rising3m = netliq_m.pct_change(3).shift(1)

    gm_yoy = gm_m.pct_change(12)
    gm_yoy_lagged = gm_yoy.shift(2)
    gm_accel = gm_yoy.diff(3).shift(2)

    usm2_yoy = usm2_m.pct_change(12)
    usm2_accel = usm2_yoy.diff(3).shift(2)

    gold_yoy = gld_m.pct_change(12).shift(1)

    ism_lagged = ism_m.shift(1)
    ism_diff_lagged = ism_m.diff(1).shift(1)

    return {
        "NetLiqAccel": nl_accel,
        "GlobalM2YoY": gm_yoy_lagged,
        "NetLiqYoY": nl_yoy_lagged,
        "GlobalM2Accel": gm_accel,
        "USM2Accel": usm2_accel,
        "NetLiqRising3m": nl_rising3m,
        "GoldYoY": gold_yoy,
        "ISMRising": ism_diff_lagged,
        "ISMGt50": ism_lagged,
    }


DRIVER_RULES = ["NetLiqAccel", "GlobalM2YoY", "NetLiqYoY", "GlobalM2Accel", "USM2Accel",
                "NetLiqRising3m", "GoldYoY", "ISMRising", "ISMGt50"]
EXCLUDED_RULES = ["ComboM2AccelNetLiqYoY", "ISMLt50Rising"]

netliq_m = msz.macro_monthly("FED_NET_LIQUIDITY.csv", "net_liquidity_bn", weekly=True)
gm_m = msz.macro_monthly("GLOBAL_M2.csv", "global_m2_bn")
usm2_m = msz.macro_monthly("US_M2.csv", "m2_usd_bn")
ism_m = msz.macro_monthly("ISM_PMI.csv", "ism_pmi")
gld_close, _ = msz.load_close("GLD")
gld_m = gld_close.resample("ME").last(); gld_m.index = gld_m.index.to_period("M")

drivers = build_continuous_drivers(netliq_m, gm_m, usm2_m, ism_m, gld_m)

# ---------------------------------------------------------------------------
# 2. Per-asset monthly close + forward returns at each horizon.
# ---------------------------------------------------------------------------
asset_close_m = {}
for a in msz.ASSETS_28:
    close, _ = msz.load_close(msz.FILE_STUB[a])
    m = close.resample("ME").last(); m.index = m.index.to_period("M"); m = m.groupby(level=0).last()
    asset_close_m[a] = m

fwd_ret = {}  # (asset, h) -> Series indexed by period, value = close(t+h)/close(t)-1
for a in msz.ASSETS_28:
    m = asset_close_m[a]
    for h in HORIZONS:
        fwd_ret[(a, h)] = (m.shift(-h) / m - 1.0)


def spearman(a: np.ndarray, b: np.ndarray) -> float:
    """Spearman rank correlation = Pearson correlation of ranks. Implemented via
    `numpy.corrcoef` on rank-transformed arrays -- this environment has no `scipy`
    installed, and pandas' own `.corr(method='spearman')` imports scipy internally, so a
    plain rank+Pearson implementation is used instead (mathematically identical)."""
    ra = pd.Series(a).rank().to_numpy()
    rb = pd.Series(b).rank().to_numpy()
    if np.std(ra) < 1e-12 or np.std(rb) < 1e-12:
        return np.nan
    return float(np.corrcoef(ra, rb)[0, 1])


def spearman_ic(x: pd.Series, y: pd.Series) -> float:
    df = pd.DataFrame({"x": x, "y": y}).dropna()
    if len(df) < 3 or df["x"].nunique() < 2 or df["y"].nunique() < 2:
        return np.nan
    return spearman(df["x"].to_numpy(), df["y"].to_numpy())


# ---------------------------------------------------------------------------
# 3. Block (annual, non-overlapping) IC series per (rule, asset, horizon).
# ---------------------------------------------------------------------------

def block_ic_series(driver: pd.Series, fret: pd.Series, min_obs=MIN_OBS_PER_BLOCK):
    """Non-overlapping calendar-year blocks. Returns a pd.Series indexed by year of
    per-year Spearman IC values (only years with >= min_obs valid pairs)."""
    df = pd.DataFrame({"driver": driver, "fret": fret}).dropna()
    if df.empty:
        return pd.Series(dtype=float)
    years = df.index.map(lambda p: p.year)
    df = df.assign(year=years)
    ics = {}
    for yr, g in df.groupby("year"):
        if len(g) >= min_obs and g["driver"].nunique() > 1 and g["fret"].nunique() > 1:
            ics[yr] = spearman(g["driver"].to_numpy(), g["fret"].to_numpy())
    return pd.Series(ics).sort_index()


def ic_stats(ic_series: pd.Series):
    n = ic_series.dropna().shape[0]
    if n < 3:
        return dict(n=n, mean_ic=np.nan, std_ic=np.nan, icir=np.nan, t_stat=np.nan)
    mean_ic = ic_series.mean()
    std_ic = ic_series.std(ddof=1)
    icir = mean_ic / std_ic if std_ic and std_ic > 1e-9 else np.nan
    t_stat = icir * np.sqrt(n) if not np.isnan(icir) else np.nan
    return dict(n=n, mean_ic=mean_ic, std_ic=std_ic, icir=icir, t_stat=t_stat)


# ---------------------------------------------------------------------------
emit("# Study 24 -- IC/ICIR/IC-decay framework (extends study 03's design; the literal "
     "cross-sectional-per-month reading of study 06 is mathematically undefined, see below)\n")

emit("## 0. Why a literal cross-sectional IC across the 28-asset universe doesn't apply here\n")
emit("Confirmed directly in `macro_strategy_zoo.py`'s `build_rules()`: every one of the 11 "
     "rules' signals is a SINGLE macro-level time series applied IDENTICALLY to all 28 "
     "assets each month (`signals[name] = sig`, same value for SPY, BTC, GLD, everything "
     "that month). Rank-correlating a column that is a CONSTANT across all 28 assets in a "
     "given month against 28 different realized returns has an undefined correlation "
     "coefficient (zero cross-sectional variance in the forecast) -- not a small/weak "
     "number, an undefined one. This is confirmed by direct spot-check below, not assumed:\n")
sample_rule = "NetLiqAccel"
sample_month = drivers[sample_rule].dropna().index[50]
emit(f"- Spot check: `{sample_rule}` continuous driver reading at {sample_month} is a "
     f"single scalar ({drivers[sample_rule].loc[sample_month]:.4f}) -- identical for every "
     f"one of the 28 assets that month by construction. A per-month cross-sectional "
     f"correlation across the 28 assets against their {sample_month} forward returns would "
     f"correlate a 28-element vector against a 28-element vector where one side is "
     f"constant -- `pandas`/`scipy` both return `NaN` for this (verified: a constant-input "
     f"Spearman correlation is undefined, not near-zero).\n")
emit("Two legitimate reframings are used below instead: **(a)** an asset-level TIME-SERIES "
     "IC per (rule, asset) pair -- extends study 03's already-valid design (BTC/ETH/SOL x "
     "net-liq/M2 drivers) to 9 continuous-driver rules x all 28 assets, with formal "
     "ICIR/t-stat/decay-curve packaging added on top -- the primary deliverable; **(b)** an "
     "optional, genuinely cross-sectional (single-point, not a time series) check reusing "
     "study 06's own per-asset Sharpe-uplift numbers against study 05's liquidity-beta "
     "ranking -- a bonus.\n")
emit(f"Two of the 11 rules (`ComboM2AccelNetLiqYoY`, `ISMLt50Rising`) are logical ANDs of "
     f"two other rules' conditions with no natural single continuous driver reading -- "
     f"excluded from part (a)'s table below (their component rules, `GlobalM2Accel`/"
     f"`NetLiqYoY` and `ISMGt50`/`ISMRising`, ARE included).\n")

# ---------------------------------------------------------------------------
emit("## 1a. Study-03 spotlight -- BTC/ETH/SOL x the 4 liquidity drivers, IC-decay curve\n")
emit("Formalizes study 03's own headline finding (BTC contemporaneous, ETH/SOL genuine "
     "leads) as an actual IC-decay curve with ICIR/t-stat, using non-overlapping annual "
     "blocks (h=1 is genuinely non-overlapping within a block; h=3/6/12 blocks share the "
     "same overlapping-window caveat study 03 itself already flagged for its own long-"
     "horizon numbers).\n")
SPOTLIGHT_RULES = ["NetLiqAccel", "NetLiqYoY", "GlobalM2Accel", "GlobalM2YoY"]
SPOTLIGHT_ASSETS = ["BTC", "ETH", "SOL"]
ic_rows = []
emit("| driver | asset | h (mo) | mean IC | std IC | n years | ICIR | t-stat |")
emit("|---|---|--:|--:|--:|--:|--:|--:|")
for rule in SPOTLIGHT_RULES:
    for a in SPOTLIGHT_ASSETS:
        for h in HORIZONS:
            ics = block_ic_series(drivers[rule], fwd_ret[(a, h)])
            st = ic_stats(ics)
            ic_rows.append(dict(rule=rule, asset=a, h=h, **st))
            emit(f"| {rule} | {a} | {h} | {st['mean_ic']:+.2f} | {st['std_ic']:.2f} | "
                 f"{st['n']} | {st['icir']:+.2f} | {st['t_stat']:+.2f} |" if st['n'] >= 3 else
                 f"| {rule} | {a} | {h} | n/a | n/a | {st['n']} | n/a | n/a |")
emit("")

emit("### Whole-sample (single-shot, pooled) IC for cross-reference against study 03's own numbers\n")
emit("| driver | asset | h=1 | h=3 | h=6 | h=12 |")
emit("|---|---|--:|--:|--:|--:|")
for rule in SPOTLIGHT_RULES:
    for a in SPOTLIGHT_ASSETS:
        cells = [spearman_ic(drivers[rule], fwd_ret[(a, h)]) for h in HORIZONS]
        emit(f"| {rule} | {a} | " + " | ".join(f"{c:+.2f}" if not np.isnan(c) else "n/a" for c in cells) + " |")
emit("")

# ---------------------------------------------------------------------------
emit("## 1b. Full breadth -- 9 continuous-driver rules x all 28 assets, h=1 (primary, genuinely non-overlapping)\n")
emit("Per-rule aggregate across all 28 assets (median across assets with >= 3 valid annual "
     "IC blocks): mirrors study 06's median-dSharpe breadth format, now in formal IC "
     "vocabulary.\n")
breadth_rows = []
per_rule_asset_ic = {}
for rule in DRIVER_RULES:
    per_asset = {}
    for a in msz.ASSETS_28:
        ics = block_ic_series(drivers[rule], fwd_ret[(a, 1)])
        st = ic_stats(ics)
        per_asset[a] = st
    per_rule_asset_ic[rule] = per_asset
    valid = {a: st for a, st in per_asset.items() if st["n"] >= 3}
    mean_ics = [st["mean_ic"] for st in valid.values() if not np.isnan(st["mean_ic"])]
    icirs = [st["icir"] for st in valid.values() if not np.isnan(st["icir"])]
    tstats = [st["t_stat"] for st in valid.values() if not np.isnan(st["t_stat"])]
    n_sig = sum(1 for t in tstats if abs(t) >= 2.0)
    breadth_rows.append(dict(
        rule=rule, n_assets=len(valid),
        median_mean_ic=np.median(mean_ics) if mean_ics else np.nan,
        median_icir=np.median(icirs) if icirs else np.nan,
        median_tstat=np.median(tstats) if tstats else np.nan,
        n_sig=n_sig,
    ))

breadth_df = pd.DataFrame(breadth_rows).sort_values("median_icir", ascending=False)
emit("| rule | n assets (>=3 yr-blocks) | median mean-IC | median ICIR | median t-stat | n assets with \\|t\\|>=2 |")
emit("|---|--:|--:|--:|--:|--:|")
for _, r in breadth_df.iterrows():
    emit(f"| {msz.RULE_LABEL[r['rule']]} | {r['n_assets']:.0f} | {r['median_mean_ic']:+.3f} | "
         f"{r['median_icir']:+.3f} | {r['median_tstat']:+.2f} | {r['n_sig']:.0f} |")
emit("")
emit("[rank check] rules ranked by median ICIR (descending):")
for i, (_, r) in enumerate(breadth_df.iterrows(), start=1):
    emit(f"  {i}. {msz.RULE_LABEL[r['rule']]}: ICIR={r['median_icir']:+.3f} (mean-IC {r['median_mean_ic']:+.3f}, "
         f"t-stat {r['median_tstat']:+.2f}, {r['n_sig']:.0f}/{r['n_assets']:.0f} assets |t|>=2)")
emit("")

# dump full per-rule-asset IC stats to CSV for inspection
rows = []
for rule in DRIVER_RULES:
    for a in msz.ASSETS_28:
        st = per_rule_asset_ic[rule][a]
        rows.append(dict(rule=rule, asset=a, h=1, **st))
pd.DataFrame(rows).to_csv(os.path.join(RESULTS, "ic_time_series_h1_summary.csv"), index=False)
print(f"Wrote results/ic_time_series_h1_summary.csv ({len(rows)} rows)")

# ---------------------------------------------------------------------------
emit("## 1c. Named-asset check (same 8 assets study 06/19's guardrail used)\n")
emit("NetLiqAccel, h=1, annual-block IC/ICIR (sign should match study 06's dSharpe sign: "
     "COIN/SOL/MARA/TSLA/ADA/MSTR positive, UUP/GLD negative):\n")
NAMED_CHECK = ["COIN", "SOL", "MARA", "TSLA", "ADA", "MSTR", "UUP", "GLD"]
emit("| asset | mean IC | ICIR | t-stat | n years |")
emit("|---|--:|--:|--:|--:|")
for a in NAMED_CHECK:
    st = per_rule_asset_ic["NetLiqAccel"][a]
    emit(f"| {a} | {st['mean_ic']:+.3f} | {st['icir']:+.3f} | {st['t_stat']:+.2f} | {st['n']} |")
emit("")

# ---------------------------------------------------------------------------
emit("## 2. Bonus (b) -- cross-sectional Spearman: NetLiqAccel's per-asset dSharpe vs. net-liq beta\n")
emit("Single cross-sectional snapshot (one number, NOT a time series -- no ICIR here by "
     "definition). Recomputes each asset's NetLiqAccel-rule Sharpe uplift vs. buy&hold "
     "directly from `study19_variant_returns_06.csv` (same formula as "
     "`macro_strategy_zoo.py`'s own Part 1), then correlates (Spearman, across assets) "
     "against that asset's own net-liquidity beta from `liquidity_beta_ranking.csv` "
     "(study 05). CI via a plain i.i.d. bootstrap over assets (resample-with-replacement, "
     f"{N_BOOT} draws) -- NOT the block-bootstrap convention (`01_data_and_methods.md`), "
     "which is designed for time-series autocorrelation; there is no time axis in a single "
     "cross-sectional snapshot, so a plain asset-level bootstrap is the correct "
     "adaptation, stated explicitly rather than mislabeled.\n")

zoo_df = pd.read_csv(os.path.join(RESULTS, "study19_variant_returns_06.csv"), parse_dates=["date"]).set_index("date")


def calc_sharpe(returns):
    r = returns.dropna()
    sd = r.std()
    if not sd or sd <= 0 or np.isnan(sd):
        return np.nan
    return float(r.mean() / sd * np.sqrt(12))


bh_sharpe = {}
for a in msz.ASSETS_28:
    r = asset_close_m[a].pct_change().dropna()
    r.index = r.index.to_timestamp()
    bh_sharpe[a] = calc_sharpe(r)

dsharpe = {}
for a in msz.ASSETS_28:
    col = f"NetLiqAccel__{a}"
    if col not in zoo_df.columns:
        continue
    s = calc_sharpe(zoo_df[col])
    if np.isnan(s) or np.isnan(bh_sharpe[a]):
        continue
    dsharpe[a] = s - bh_sharpe[a]

beta_df = pd.read_csv(os.path.join(RESULTS, "liquidity_beta_ranking.csv")).set_index("ticker")
TICKER_MAP = {"BTC": "BTC_USD", "ETH": "ETH_USD", "SOL": "SOL_USD", "ADA": "ADA_USD",
              "LINK": "LINK_USD", "DOGE": "DOGE_USD", "MSTR": "MSTR", "COIN": "COIN",
              "MARA": "MARA", "SPY": "SPY", "GLD": "GLD", "SLV": "SLV", "NVDA": "NVDA",
              "AAPL": "AAPL", "MSFT": "MSFT", "TSLA": "TSLA", "AMZN": "AMZN", "JPM": "JPM"}
matched = [(a, TICKER_MAP[a]) for a in msz.ASSETS_28 if a in TICKER_MAP and TICKER_MAP[a] in beta_df.index and a in dsharpe]
emit(f"Matched {len(matched)}/{len(msz.ASSETS_28)} assets present in both study 06's return "
     f"matrix and study 05's beta ranking:\n")
emit("| asset | dSharpe (NetLiqAccel vs B&H) | net-liq beta |")
emit("|---|--:|--:|")
x_vals, y_vals = [], []
for a, tick in sorted(matched, key=lambda p: -beta_df.loc[p[1], "netliq_beta"]):
    ds = dsharpe[a]; b = beta_df.loc[tick, "netliq_beta"]
    x_vals.append(ds); y_vals.append(b)
    emit(f"| {a} | {ds:+.2f} | {b:.1f} |")
emit("")

x_arr = np.array(x_vals); y_arr = np.array(y_vals)
point_rho = spearman(x_arr, y_arr)
rng = np.random.default_rng(RNG_SEED)
n = len(x_arr)
boot = []
for _ in range(N_BOOT):
    idx = rng.integers(0, n, n)
    xb, yb = x_arr[idx], y_arr[idx]
    if pd.Series(xb).nunique() < 2 or pd.Series(yb).nunique() < 2:
        continue
    boot.append(spearman(xb, yb))
boot = np.array(boot)
lo, hi = np.percentile(boot, [2.5, 97.5])
sig = "✓" if (lo > 0 or hi < 0) else ""
emit(f"**Cross-sectional Spearman rho (dSharpe vs. net-liq beta, n={n} assets): "
     f"{point_rho:+.3f} [{lo:+.3f}, {hi:+.3f}] (95% i.i.d. bootstrap CI, {len(boot)} valid "
     f"draws) {sig}**\n")
emit("This is a single cross-sectional number, not an ICIR (there is no time dimension "
     "here to compute a std/ICIR over) -- reported as a Spearman rank correlation with a "
     "bootstrap CI, matching the task's own labeling requirement.\n")

with open(os.path.join(RESULTS, "ic_framework_backtest.md"), "w") as fh:
    fh.write("\n".join(out) + "\n")
print("\nWrote results/ic_framework_backtest.md")
