#!/usr/bin/env python3
"""
Study 17 -- "Global 20": a 100%-individual-stock, no-ETF, no-bonds/gold-at-all
globally-diversified equity portfolio, as a second reference point alongside
study #16's classic asset-class portfolios and the user's own aggressive book.

20 companies, EQUAL WEIGHT (5% each -- simplest, standard convention for a demo
portfolio like this; not market-cap-weighted):

  USA (12, 60%):     AAPL, MSFT, AMZN, GOOGL, NVDA, JPM, JNJ, WMT, TSLA, AVGO,
                      KO (Coca-Cola), V (Visa)
  Europe (4, 20%):   ASML (Netherlands), NVO (Novo Nordisk, Denmark),
                      SAP (Germany, US-listed ADR), UL (Unilever, UK/NL, ADR)
  Rest-of-World (4, 20%): TSM (Taiwan Semi, ADR), TM (Toyota, Japan, ADR),
                      BABA (Alibaba, China, ADR), SAMSUNG (Samsung Electronics,
                      Korea -- see special handling below)

Data: reuses AAPL/MSFT/AMZN/GOOGL/NVDA/JPM/TSLA _full.csv already in data/prices/
unchanged; fetches fresh _full.csv for everything else via
fetch_global_stocks_data.py (JNJ/WMT/AVGO/KO/V/ASML/NVO/SAP/UL/TSM/TM/BABA), same
2007-01-01-start yfinance convention as fetch_long_history.py -- does NOT parse/
mix in the pre-existing short-format plain .csv files for tickers that had them.

SAMSUNG -- no direct USD ADR exists. Fetched as its primary Korea Exchange
listing (005930.KS, KRW-denominated) plus a USDKRW=X FX leg, chained
KRW -> USD -> PLN (one extra hop vs. the usual USD -> PLN chain). Samsung's data
came back clean with history to 2007-01 (Yahoo's practical start for this
ticker) -- comfortably clears the 13y-window requirement, so no ADR-proxy
fallback (e.g. SSNLF) was needed; used directly.

13-YEAR-HISTORY CHECK (same discipline as study #16): every ticker's actual
first-available date is checked against the 13y window start (2013-06).
**Only BABA fails** -- Alibaba IPO'd 2014-09-19, so it does not exist at all for
part of the 13y window (2013-06 through 2014-09) and is naturally absent from
that stretch via the same `present`-list mechanism the existing scripts already
use for assets that don't exist yet (its 5% target share is redistributed
pro-rata among whichever of the other 19 stocks are present that month); BABA IS
present for the entire 8y/5y/3y/1y windows (all start after Sept 2014).
**Samsung, contrary to the a-priori expectation in the task brief, clears 13y
comfortably** (our data starts 2007-01, well before the 2013-06 window start) --
it is NOT one of the assets requiring special no-history handling.

FOUR VARIANT FAMILIES, matching backtest/AGENTS.md "Standing baselines" (reusing
the same simulation engine built for study #16 -- classic_portfolios_backtest.py
-- but NOT modifying that script):
  1. BUYHOLD -- real deficit-proportional catch-up DCA (Q_i/Q_pos), never sells.
  2. FIXED   -- contributions at the flat 5%-each target + annual full
               rebalance-to-target. Trivial here vs. study #16's asset-class
               portfolios: there's only one target table (equal-weight), no
               risk-on/off distinction needed for FIXED since there's nothing
               dynamic about it.
  3. BLEND   -- DELIBERATELY NOT IMPLEMENTED AS AN ACTIVE OVERLAY. Rationale
               (a design call per explicit task instruction): the ADR-044
               risk-on/off mechanism works by shifting weight from a RISK bucket
               into a DEFENSE bucket (bonds/gold/cash) that actually exists
               within the portfolio -- every study #16 portfolio held at least
               one genuinely defensive asset-class leg to rotate into. This is a
               100%-individual-stock, no-bonds/gold/cash book: ALL 20 positions
               are equity risk exposure; there is no internal defensive bucket
               for a tilt to rotate into without inventing one arbitrarily
               (e.g. picking 4 "less risky" stocks as a fake DEFENSE bucket),
               which would misrepresent both the portfolio (equal-weight by
               design) and the overlay (built for asset-class buckets, not
               stock-picking). Rather than force a contrived RISK/DEFENSE split
               onto 20 stocks that are all equally "risk," this script instead
               reports the underlying ADR-044 blend regime's occupancy
               (RISK_ON/MID_ON/NEUTRAL/MID_OFF/RISK_OFF % of months) per window,
               purely as CONTEXT alongside the FIXED numbers -- informational
               only, never used to alter any weight in this study.
  4. TRIM    -- BUYHOLD's real catch-up DCA + a monthly threshold-triggered
               partial-sell check (same general/portfolio-agnostic mechanic as
               study #16: trigger x target -> trim to trim_to x target, proceeds
               redistributed pro-rata across every OTHER present position by ITS
               OWN target weight -- all equal here, so effectively an equal
               split among the other present stocks). Tested at the 3 required
               (trigger, trim_to) pairs: (2.0, 1.5), (2.0, 1.0), (1.5, 1.0).
               Individual stocks are far more likely than study #16's
               diversified asset-class buckets to have idiosyncratic runs that
               actually cross a 1.5-2x threshold (e.g. NVDA, TSLA) -- fire-
               frequency is reported per stock, same method as study #15.

Same DCA/cost/PLN conventions as study #16: buy-only 1000 PLN initial + 1000/mo,
15bps, same 5 overlapping start windows (1/3/5/8/13y back from 2026-06).

Does not modify classic_portfolios_backtest.py or any other existing script.
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

# ---------------------------------------------------------------------------
USA = ["AAPL", "MSFT", "AMZN", "GOOGL", "NVDA", "JPM", "JNJ", "WMT", "TSLA", "AVGO", "KO", "V"]
EUROPE = ["ASML", "NVO", "SAP", "UL"]
ROW = ["TSM", "TM", "BABA", "SAMSUNG"]
ALL_20 = USA + EUROPE + ROW
GLOBAL20 = {t: 5.0 for t in ALL_20}
assert len(ALL_20) == 20 and abs(sum(GLOBAL20.values()) - 100) < 1e-9

NAMES = {
    "AAPL": "Apple", "MSFT": "Microsoft", "AMZN": "Amazon", "GOOGL": "Alphabet (Google)",
    "NVDA": "Nvidia", "JPM": "JPMorgan Chase", "JNJ": "Johnson & Johnson", "WMT": "Walmart",
    "TSLA": "Tesla", "AVGO": "Broadcom", "KO": "Coca-Cola", "V": "Visa",
    "ASML": "ASML (Netherlands)", "NVO": "Novo Nordisk (Denmark)",
    "SAP": "SAP (Germany, US ADR)", "UL": "Unilever (UK/Netherlands, US ADR)",
    "TSM": "Taiwan Semiconductor (US ADR)", "TM": "Toyota (Japan, US ADR)",
    "BABA": "Alibaba (China, US ADR)",
    "SAMSUNG": "Samsung Electronics (Korea, 005930.KS via KRW->USD->PLN)",
}
REGION = {**{t: "USA" for t in USA}, **{t: "Europe" for t in EUROPE}, **{t: "Rest of World" for t in ROW}}


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
usdkrw = usd_close("USDKRW_X")   # KRW per 1 USD


def pln_ret(t):
    if t == "SAMSUNG":
        usd_price = usd_close("SAMSUNG") / usdkrw       # KRW close / (KRW per USD) = USD price
        return (usd_price * usdpln).dropna().pct_change()
    return (usd_close(t) * usdpln).dropna().pct_change()


RET = {a: pln_ret(a) for a in ALL_20}

END = usdpln.index.max()
STARTS = {"13y": 13, "8y": 8, "5y": 5, "3y": 3, "1y": 1}
starts = {k: END - v * 12 for k, v in STARTS.items()}

# 13y-history requirement check: true first-available date per ticker vs. the
# 13y window start. Confirmed separately via a 1990-2015 probe fetch (see study
# write-up) -- yfinance's practical data-start for some names (AAPL/MSFT/JPM/
# JNJ/WMT/KO/NVO/UL/TM all read "1990-01-02", Yahoo's history floor, not
# necessarily true IPO date, but irrelevant: all predate 2013-06 either way).
TRUE_INCEPTION = {
    "AAPL": "1990-01-02", "MSFT": "1990-01-02", "AMZN": "1997-05-15", "GOOGL": "2004-08-19",
    "NVDA": "1999-01-22", "JPM": "1990-01-02", "JNJ": "1990-01-02", "WMT": "1990-01-02",
    "TSLA": "2010-06-29", "AVGO": "2009-08-06", "KO": "1990-01-02", "V": "2008-03-19",
    "ASML": "1995-03-15", "NVO": "1990-01-02", "SAP": "1995-09-18", "UL": "1990-01-02",
    "TSM": "1997-10-09", "TM": "1990-01-02", "BABA": "2014-09-19", "SAMSUNG": "2000-01-04",
}

# ---------------------------------------------------------------------------
# ADR-044 macro blend -- reused UNCHANGED from portfolio_macro_blend_backtest.py /
# classic_portfolios_backtest.py, for CONTEXT ONLY in this study (see module
# docstring "BLEND" rationale) -- NOT applied as an active portfolio overlay.

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
# Context ONLY (regime-occupancy table) -- never applied to weights in this study's
# own-universe sims. REFRESHED 2026-09-25 (task 28): was the stale `(0.5 - z/2.0)`
# with pre-ADR-071 zone labels; now the live divisor/orientation/labels.
blend = spec.blend_live(smoothed_z)

ZONE_BINS = spec.ZONE_BINS
ZONE_LABELS = spec.ZONE_LABELS_LIVE     # live (ADR-071): 0=RISK_OFF .. 1=RISK_ON


def zone_of(b):
    return pd.cut(b, bins=ZONE_BINS, labels=ZONE_LABELS)


# ---------------------------------------------------------------------------
# Longest continuous history for the trim fire-frequency pass: earliest month
# all of the 19 always-present stocks (everything except BABA, which
# structurally doesn't exist before 2014-09) have a valid return.
FULL_START = max(RET[a].dropna().index.min() for a in ALL_20 if a != "BABA")

# Standing 3-pair trim set with correct currency labels (portfolio_spec.py -- the
# live/current label sits on (1.5, 1.0) per ADR-048; (2.0, 1.5) is PRIOR reference).
# REFRESHED 2026-09-25 (task 28, trim-label fix): this list used to call (2.0, 1.5)
# "user's own live rule". Same (label, trigger, trim_to) tuple shape, so all
# consumers below are untouched. Label-only fix -- sim numbers do not change.
TRIM_CONFIGS = spec.TRIM_CONFIGS


def sim(start, mode, trigger=None, trim_to=None, log=None):
    """mode: 'buyhold' | 'fixed' | 'trim'. Same mechanics as
    classic_portfolios_backtest.py's sim(), specialized to the flat 5%-each
    Global 20 target (no risk_on/risk_off tables -- BLEND is not an active
    variant in this study, see module docstring)."""
    named = GLOBAL20
    months = pd.period_range(start, END, freq="M"); n = len(months)
    assets = list(named.keys())
    hold = {a: 0.0 for a in assets}
    nav = 1.0; navs = []
    for idx, t in enumerate(months):
        present = [a for a in assets if RET[a] is not None and t in RET[a].index and not np.isnan(RET[a].get(t, np.nan))]
        tt = named
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
            else:
                for a in present:
                    hold[a] += CONTRIB * w[a]
        else:
            for a in present:
                hold[a] += CONTRIB * w[a]
        Vn = sum(hold.values())
        turnover = 0.0
        if mode == "fixed" and idx > 0 and idx % 12 == 0:      # annual full rebalance-to-target
            target_val = {a: w[a] * Vn for a in present}
            turnover = sum(abs(hold[a] - target_val[a]) for a in present) / 2.0
            for a in present:
                hold[a] = target_val[a]
        elif mode == "trim" and trigger is not None:
            ctw = {a: named[a] / 100.0 for a in present}
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
                else:
                    for a in firing:
                        hold[a] += freed / len(firing)
            turnover = freed
        Vc = sum(hold.values()); f = 1 - (CONTRIB + 2 * turnover) * COST / Vc
        for a in hold:
            hold[a] *= f
        for a in assets:
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


emit("# Study 17 -- \"Global 20\": 100%-individual-stock global equity portfolio\n")
emit("Buy-only DCA 1000+1000/mo PLN, 15bps. Equal-weight (5% each) 20-stock "
     "global portfolio: 12 USA / 4 Europe / 4 Rest-of-World. Three active variant "
     "families (BUYHOLD real catch-up DCA, FIXED annual rebalance, TRIM threshold "
     "trigger at 3 configs); BLEND is NOT implemented as an active overlay here "
     "(see script header/write-up for rationale) -- its regime occupancy is shown "
     f"as context only. Latest common month: {END}. Cell = XIRR%/yr (maxDD%).\n")

emit("## Portfolio (equal weight, 5% each)\n")
emit("| ticker | company | region | weight % |")
emit("|---|---|---|--:|")
for t in ALL_20:
    emit(f"| {t} | {NAMES[t]} | {REGION[t]} | 5.00 |")
emit("")

emit("## 13-year-history requirement — confirmed for every ticker\n")
emit(f"Window start dates: " + ", ".join(f"{k}={starts[k]}" for k in STARTS) + " (13y window "
     f"starts {starts['13y']}).\n")
emit("| ticker | true first-available date | clears 13y-window start (2013-06)? | margin |")
emit("|---|---|---|--:|")
for t in ALL_20:
    incep = pd.Period(TRUE_INCEPTION[t], freq="M")
    clears = incep < starts["13y"]
    margin = (starts["13y"] - incep).n / 12.0
    emit(f"| {t} | {TRUE_INCEPTION[t]} | {'YES' if clears else '**NO**'} | "
         f"{f'{margin:.1f}y' if clears else f'MISSING until {TRUE_INCEPTION[t]} ({-margin:.1f}y after window start)'} |")
emit("")
emit("**Only BABA (Alibaba) fails the 13y-history requirement** -- IPO'd "
     "2014-09-19, after the 13y window's 2013-06 start. It is naturally absent "
     "(redistributed pro-rata among the other present stocks via the same "
     "`present`-list mechanism used throughout this folder for not-yet-existing "
     "assets) from 2013-06 through 2014-09 inside the 13y window only; it is "
     "present for the ENTIRE 8y/5y/3y/1y windows (all start after its IPO). "
     "**Samsung clears 13y comfortably, contrary to the a-priori concern in the "
     "task brief** -- its data (005930.KS) starts 2007-01 in this dataset, well "
     "before the 2013-06 window start.\n")

emit("## Headline results — cell = XIRR%/yr (maxDD%)\n")
emit("| variant | 13y | 8y | 5y | 3y | 1y |")
emit("|---|--:|--:|--:|--:|--:|")
results = {}
for label, mode in [("BUYHOLD", "buyhold"), ("FIXED", "fixed")]:
    cells = []
    for k in STARTS:
        xirr, maxdd, navs = sim(starts[k], mode)
        results[(mode, k)] = (xirr, maxdd, navs)
        cells.append(f"{xirr*100:.0f} ({maxdd*100:.0f})")
    emit(f"| {label} | " + " | ".join(cells) + " |")
for cfg_label, trg, tto in TRIM_CONFIGS:
    cells = []
    for k in STARTS:
        xirr, maxdd, navs = sim(starts[k], "trim", trigger=trg, trim_to=tto)
        results[(("trim", trg, tto), k)] = (xirr, maxdd, navs)
        cells.append(f"{xirr*100:.0f} ({maxdd*100:.0f})")
    emit(f"| TRIM {cfg_label} | " + " | ".join(cells) + " |")
emit(f"\nStart months: " + ", ".join(f"{k}={starts[k]}" for k in STARTS) + ". Cell = XIRR%/yr (maxDD%).")

emit("\n## BLEND: not implemented as an overlay -- regime context only\n")
emit("This is a 100%-equity, no-bonds/gold/cash book: there is no internal "
     "DEFENSE bucket for the ADR-044 risk-on/off mechanism to rotate weight "
     "into without inventing one arbitrarily (see script header for the full "
     "rationale). Instead, here is the SAME underlying blend regime's month "
     "occupancy per window, purely as context for how \"risk-off\" the macro "
     "backdrop was during each window -- **not applied to any weight in this "
     "study**.\n")
emit("| window | " + " | ".join(ZONE_LABELS) + " |")
emit("|---|" + "--:|" * len(ZONE_LABELS))
for k in STARTS:
    b = blend.loc[starts[k]:END].dropna()
    if len(b) == 0:
        emit(f"| {k} | " + " | ".join("n/a" for _ in ZONE_LABELS) + " |"); continue
    zc = zone_of(b).value_counts(normalize=True).reindex(ZONE_LABELS).fillna(0) * 100
    emit(f"| {k} | " + " | ".join(f"{zc[z]:.0f}%" for z in ZONE_LABELS) + " |")
emit("")

# ---------------------------------------------------------------------------
emit("## Trim-rule fire-frequency (single continuous pass, study #15's method)\n")
n_years = (END - FULL_START).n / 12.0
emit(f"One non-overlapping pass, {FULL_START}..{END} ({n_years:.1f}y).\n")
emit("| config | total fires | fired stocks (count each) |")
emit("|---|--:|---|")
fire_logs = {}
for cfg_label, trg, tto in TRIM_CONFIGS:
    log = []
    sim(FULL_START, "trim", trigger=trg, trim_to=tto, log=log)
    fire_logs[(trg, tto)] = log
    if log:
        df = pd.DataFrame(log)
        per_asset = df.groupby("asset").size().sort_values(ascending=False)
        breakdown = ", ".join(f"{a} ({c})" for a, c in per_asset.items())
    else:
        breakdown = "(no fires)"
    emit(f"| {cfg_label} | {len(log)} | {breakdown} |")
emit("")

combined = pd.concat([pd.DataFrame(v) for v in fire_logs.values() if v], ignore_index=True) \
    if any(fire_logs.values()) else pd.DataFrame()
combined.to_csv(os.path.join(RESULTS, "global_stocks_trim_events.csv"), index=False)
print(f"Wrote results/global_stocks_trim_events.csv ({len(combined)} rows)")

# ---------------------------------------------------------------------------
emit("## Reference rows: classic portfolios (study #16) and the user's own book (study #14)\n")
emit("Cited, not recomputed here -- same END/DCA/cost/PLN conventions.\n")
emit("| portfolio | variant | 13y | 8y | 5y | 3y | 1y |")
emit("|---|---|--:|--:|--:|--:|--:|")
emit("| 60/40 (study 16) | FIXED | 10 (-12) | 9 (-12) | 8 (-12) | 10 (-12) | 15 (-3) |")
emit("| Permanent Portfolio (study 16) | FIXED | 7 (-11) | 7 (-11) | 7 (-11) | 9 (-4) | 6 (-4) |")
emit("| Golden Butterfly (study 16) | FIXED | 8 (-15) | 8 (-15) | 8 (-15) | 11 (-9) | 13 (-1) |")
emit("| Ray Dalio All Weather (study 16) | FIXED | 6 (-20) | 5 (-20) | 3 (-20) | 6 (-9) | 9 (-2) |")
emit("| User's own book (study 14, CURRENT/ADR-044, older fixed-split DCA approx.) | -- | 34 (-24) | 33 (-20) | 21 (-28) | 24 (-15) | -3 (-11) |")

emit("\nCaveats: overlapping windows share one underlying price history (not "
     "independent draws); 1y/3y windows are single-episode reads; equal-weight "
     "(5% each) is a simplifying convention, not how any real index or actual "
     "investor would size 20 individual stocks (a market-cap-weighted version "
     "would look very different, dominated by AAPL/MSFT/NVDA/AMZN/GOOGL); BABA "
     "is absent for part of the 13y window only (see above); Samsung's return "
     "series involves a two-hop FX chain (KRW->USD->PLN via USDKRW_X and "
     "USDPLN_X) not used anywhere else in this folder, a small additional "
     "source of FX noise/slippage vs. single-hop USD->PLN tickers; BLEND is "
     "NOT an active variant in this study (equity-only book has no internal "
     "defensive bucket -- see rationale above), shown as context only; the "
     "60/40/Permanent/Golden Butterfly/All Weather/study-14 reference rows are "
     "cited from studies 16/14, not recomputed in this script; study 14's row "
     "still uses the older fixed-split DCA approximation, not the real "
     "catch-up mechanic used throughout this study (see backtest/AGENTS.md "
     "'Rebalancing rule').")

with open(os.path.join(RESULTS, "global_stocks_backtest.md"), "w") as fh:
    fh.write("\n".join(out) + "\n")
print("\nWrote results/global_stocks_backtest.md")
