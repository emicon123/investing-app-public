#!/usr/bin/env python3
"""
Portfolio DCA backtest: regression-weighted allocation vs. equal-weight.

Question being answered: are the production regression band weights
(geometric, ratio 2.0 — current) too aggressive? Compare against milder
ratios (1.5, 1.25) and a plain equal-weight DCA baseline.

Method (deliberately simple so the *weighting* is what's tested, not a
rebalancing controller):
  - Every month, contribute a constant 1000 PLN, converted to USD at that
    month's USDPLN close.
  - Split that contribution across all assets that have data, in proportion
    to each asset's regression weight w_i (equal-weight => all w_i = 1).
  - Buy fractional units at the month's close. Never sell. Hold to the end.

Regression weight per asset, computed with NO lookahead (data <= buy date),
mirrors backend RegressionWeightService:
  1. weekly closes (resample daily->W-FRI), trailing 10y, need >= 10 points
  2. log-linear OLS  ln(price_k) = a + b*k   (k 1-indexed, oldest first)
  3. residual sample stddev (ddof=1, matching Apache Commons DescriptiveStatistics)
  4. trendline at the latest week; 9 SD bands at offsets [+4..-4] * stddev
  5. nearest band to current price -> that band's weight (per scheme)

Outputs: console table + results/dca_regression_summary.json
"""

import pandas as pd
import numpy as np
import os
import json
from datetime import date
from dateutil.relativedelta import relativedelta

DATA_DIR    = os.path.join(os.path.join(os.path.dirname(__file__), ".."), "data", "prices")
RESULTS_DIR = os.path.join(os.path.join(os.path.dirname(__file__), ".."), "results")
os.makedirs(RESULTS_DIR, exist_ok=True)

MONTHLY_PLN   = 1000.0
MIN_WEEKS     = 10           # mirror RegressionWeightService.MIN_DATA_POINTS
TREND_YEARS   = 10           # rolling weekly-history window
SD_OFFSETS    = np.array([4.0, 3.0, 2.0, 1.0, 0.0, -1.0, -2.0, -3.0, -4.0])

# Weight schemes: ratio r => above-trend bands shrink by 1/r per SD,
# below-trend grow by r per SD. Index aligns with SD_OFFSETS (+4 .. -4).
def geom_bands(r: float) -> np.ndarray:
    # offset +4 -> r^-4 ... 0 -> 1 ... -4 -> r^4
    return np.array([r ** (-off) for off in SD_OFFSETS])

SCHEMES = {
    "equal":     None,                 # no regression tilt
    "geom_1.25": geom_bands(1.25),
    "geom_1.5":  geom_bands(1.5),
    "geom_2.0":  geom_bands(2.0),      # current production scale
}

# (display name, file key)
ASSETS = [
    ("Apple", "AAPL"), ("ADA", "ADA_USD"), ("Amazon", "AMZN"), ("ASML", "ASML"),
    ("Broadcom", "AVGO"), ("Alibaba", "BABA"), ("BNB", "BNB_USD"), ("BTC", "BTC_USD"),
    ("DOGE", "DOGE_USD"), ("ETH", "ETH_USD"), ("Gold", "GC_F"), ("Google", "GOOG"),
    ("JPMorgan", "JPM"), ("Linde", "LIN"), ("Meta", "META"), ("Microsoft", "MSFT"),
    ("Nvidia", "NVDA"), ("NovoNordisk", "NVO"), ("Palladium", "PA_F"), ("Silver", "SI_F"),
    ("SOL", "SOL_USD"), ("TRX", "TRX_USD"), ("Tesla", "TSLA"), ("TSMC", "TSM"),
    ("Walmart", "WMT"), ("XRP", "XRP_USD"),
]


# ── Data loading ────────────────────────────────────────────────────────────

def load_close(file_key: str) -> pd.Series:
    path = os.path.join(DATA_DIR, f"{file_key}.csv")
    if not os.path.exists(path):
        return pd.Series(dtype=float)
    df = pd.read_csv(path, skiprows=3, header=None,
                     names=["Date", "Open", "High", "Low", "Close"])
    df["Date"] = pd.to_datetime(df["Date"], format="%Y-%m-%d", errors="coerce")
    df = df.dropna(subset=["Date"]).set_index("Date")
    s = pd.to_numeric(df["Close"], errors="coerce").dropna()
    s.index = s.index.normalize()
    return s.sort_index()


# ── Regression weight (mirrors RegressionWeightService) ───────────────────────

def band_index_for(weekly: pd.Series, as_of: pd.Timestamp) -> int | None:
    """Return the nearest-band index (0..8 over SD_OFFSETS) or None if not enough data."""
    hist = weekly[weekly.index <= as_of]
    cutoff = as_of - pd.DateOffset(years=TREND_YEARS)
    hist = hist[hist.index >= cutoff].dropna()
    if len(hist) < MIN_WEEKS:
        return None
    prices = hist.to_numpy(dtype=float)
    if (prices <= 0).any():
        return None
    k = np.arange(1, len(prices) + 1)
    y = np.log(prices)
    # OLS slope/intercept
    b, a = np.polyfit(k, y, 1)
    resid = y - (a + b * k)
    sd = resid.std(ddof=1)            # sample stddev, like Apache Commons
    trend = np.exp(a + b * len(prices))
    band_prices = trend * np.exp(SD_OFFSETS * sd)
    current = prices[-1]
    return int(np.argmin(np.abs(band_prices - current)))


# ── Backtest one scenario for all schemes at once ─────────────────────────────

def run_scenario(start: date, label: str, closes: dict, weekly: dict, usdpln: pd.Series):
    cal = sorted(set().union(*[set(s.index) for s in closes.values() if not s.empty]))
    cal = pd.DatetimeIndex(cal)
    end = pd.Timestamp(closes["BTC"].index[-1])

    # monthly buy dates: first trading day on/after each month start
    months = pd.date_range(start=pd.Timestamp(start), end=end, freq="MS")
    buy_dates = []
    for m in months:
        idx = cal.searchsorted(m, side="left")
        if idx < len(cal):
            buy_dates.append(cal[idx])
    buy_dates = sorted(set(buy_dates))

    # ffilled price lookup for buying / valuation
    px = {n: closes[n].reindex(cal).ffill() for n, _ in ASSETS if not closes[n].empty}
    fx = usdpln.reindex(cal).ffill()
    fx_end = float(fx.loc[fx.index <= end].iloc[-1])

    schemes = list(SCHEMES.keys())
    units = {s: {n: 0.0 for n in px} for s in schemes}
    hhi_acc = {s: [] for s in schemes}      # monthly allocation concentration
    maxw_acc = {s: [] for s in schemes}
    cashflows_pln = {s: [] for s in schemes}  # (date, amount) for IRR
    equity_rows = {s: [] for s in schemes}

    for bd in buy_dates:
        rate = float(fx.loc[bd]) if bd in fx.index and not np.isnan(fx.loc[bd]) else None
        if rate is None or rate <= 0:
            continue
        contrib_usd = MONTHLY_PLN / rate

        # available assets + band index (band index is scheme-independent)
        avail = {}
        for n in px:
            price = px[n].loc[bd]
            if np.isnan(price) or price <= 0:
                continue
            bi = band_index_for(weekly[n], bd)
            avail[n] = (price, bi)
        if not avail:
            continue

        for s in schemes:
            bands = SCHEMES[s]
            if bands is None:
                w = {n: 1.0 for n in avail}
            else:
                w = {n: (bands[bi] if bi is not None else 1.0) for n, (_, bi) in avail.items()}
            tot = sum(w.values())
            if tot <= 0:
                continue
            alloc = {n: contrib_usd * w[n] / tot for n in avail}
            # concentration metrics
            shares = np.array([w[n] / tot for n in avail])
            hhi_acc[s].append(float((shares ** 2).sum()))
            maxw_acc[s].append(float(shares.max()))
            # buy
            for n, (price, _) in avail.items():
                units[s][n] += alloc[n] / price
            cashflows_pln[s].append((bd, -MONTHLY_PLN))

        # monthly equity snapshot (value all schemes)
        for s in schemes:
            val_usd = sum(units[s][n] * (px[n].loc[bd] if not np.isnan(px[n].loc[bd]) else 0.0)
                          for n in px)
            equity_rows[s].append({"date": str(bd.date()),
                                   "value_usd": round(val_usd, 2),
                                   "value_pln": round(val_usd * rate, 2)})

    # finalize
    out = {}
    n_months = len(buy_dates)
    invested_pln = MONTHLY_PLN * n_months
    for s in schemes:
        final_usd = sum(units[s][n] * float(px[n].loc[px[n].index <= end].iloc[-1])
                        for n in px if not px[n].loc[px[n].index <= end].empty)
        final_pln = final_usd * fx_end
        roi = final_pln / invested_pln - 1 if invested_pln else 0.0
        # money-weighted IRR (monthly) -> annualized
        cfs = cashflows_pln[s] + [(end, final_pln)]
        irr_m = monthly_irr([amt for _, amt in cfs])
        irr_ann = (1 + irr_m) ** 12 - 1 if irr_m is not None else None
        # max drawdown of value-vs-invested ratio is noisy early; use equity value drawdown
        vals = np.array([r["value_pln"] for r in equity_rows[s]], dtype=float)
        mdd = max_drawdown(vals)
        out[s] = {
            "final_pln": round(final_pln, 0),
            "roi_pct": round(roi * 100, 1),
            "irr_annual_pct": round(irr_ann * 100, 1) if irr_ann is not None else None,
            "avg_hhi": round(float(np.mean(hhi_acc[s])), 3) if hhi_acc[s] else None,
            "avg_max_alloc_pct": round(float(np.mean(maxw_acc[s])) * 100, 1) if maxw_acc[s] else None,
            "max_drawdown_pct": round(mdd * 100, 1),
        }
    return {
        "label": label, "start": str(start), "months": n_months,
        "invested_pln": invested_pln, "n_assets_end": len(px),
        "schemes": out,
    }


def monthly_irr(cashflows, lo=-0.9, hi=1.0):
    """Bisection IRR on a monthly cashflow list (index = month#). Returns monthly rate."""
    def npv(r):
        return sum(cf / (1 + r) ** i for i, cf in enumerate(cashflows))
    f_lo, f_hi = npv(lo), npv(hi)
    if np.isnan(f_lo) or np.isnan(f_hi) or f_lo * f_hi > 0:
        return None
    for _ in range(200):
        mid = (lo + hi) / 2
        f_mid = npv(mid)
        if abs(f_mid) < 1e-6:
            return mid
        if f_lo * f_mid < 0:
            hi, f_hi = mid, f_mid
        else:
            lo, f_lo = mid, f_mid
    return (lo + hi) / 2


def max_drawdown(values: np.ndarray) -> float:
    if values.size == 0:
        return 0.0
    peak = np.maximum.accumulate(values)
    dd = np.where(peak > 0, (values - peak) / peak, 0.0)
    return float(dd.min())


# ── Main ──────────────────────────────────────────────────────────────────────

def main():
    print("Loading close-price data...")
    closes, weekly = {}, {}
    for name, key in ASSETS:
        s = load_close(key)
        closes[name] = s
        weekly[name] = s.resample("W-FRI").last() if not s.empty else s
        if s.empty:
            print(f"  WARNING: no data for {name}")
    usdpln = load_close("USDPLN_X")
    end = closes["BTC"].index[-1].date()
    print(f"Data end: {end};  assets: {sum(1 for n in closes if not closes[n].empty)}")

    today = end  # backtest 'now' = last data point
    scenarios = [
        (today - relativedelta(years=1), "1yr"),
        (today - relativedelta(years=3), "3yr"),
        (today - relativedelta(years=5), "5yr"),
        (today - relativedelta(years=8), "8yr"),
    ]

    results = [run_scenario(sd, lab, closes, weekly, usdpln) for sd, lab in scenarios]

    # ── Report ──
    schemes = list(SCHEMES.keys())
    print("\n" + "=" * 92)
    print("MONTHLY DCA 1000 PLN — final value (PLN) / money-weighted annual IRR / max drawdown")
    print("=" * 92)
    for r in results:
        print(f"\n[{r['label']}] start {r['start']}  ({r['months']} months, invested "
              f"{r['invested_pln']:,.0f} PLN, {r['n_assets_end']} assets)")
        print(f"  {'scheme':<11} {'final PLN':>11} {'ROI%':>8} {'IRR/yr%':>9} "
              f"{'maxDD%':>8} {'avgHHI':>8} {'avgMaxAlloc%':>13}")
        base = r["schemes"]["equal"]
        for s in schemes:
            o = r["schemes"][s]
            tag = "  <-- current" if s == "geom_2.0" else ("  (baseline)" if s == "equal" else "")
            print(f"  {s:<11} {o['final_pln']:>11,.0f} {o['roi_pct']:>8.1f} "
                  f"{(o['irr_annual_pct'] if o['irr_annual_pct'] is not None else float('nan')):>9.1f} "
                  f"{o['max_drawdown_pct']:>8.1f} {o['avg_hhi']:>8.3f} "
                  f"{o['avg_max_alloc_pct']:>13.1f}{tag}")

    with open(os.path.join(RESULTS_DIR, "dca_regression_summary.json"), "w") as f:
        json.dump(results, f, indent=2)
    print("\nWritten: results/dca_regression_summary.json")


if __name__ == "__main__":
    main()
