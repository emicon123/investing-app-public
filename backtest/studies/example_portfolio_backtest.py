#!/usr/bin/env python3
"""
Regression-weight test on a MATURE, full-history example portfolio (all assets have
20+ years on Yahoo, so the 10-year regression window is genuinely full at every buy
date — no young-asset / survivorship noise).

Target allocation:
  Gold 30%, Silver 10%, and 60% across six obvious 2017-era US large-caps (10% each):
  AAPL, MSFT, AMZN, GOOG, JPM, JNJ.

Baseline = the fixed target allocation (strategic DCA). Regression schemes TILT around
the target the way production does:  alloc_i  ∝  target_i × bandWeight_i  (then renormalize).
So a below-trend asset gets more than its target share, an above-trend asset less.

All assets USD -> PLN via USDPLN. Monthly 1000 PLN. No lookahead.
"""

import numpy as np, pandas as pd, os, json
from dateutil.relativedelta import relativedelta
import dca_regression_backtest as bt   # load_close, band_index_for, geom_bands, SCHEMES, monthly_irr, max_drawdown

RESULTS_DIR = os.path.join(os.path.join(os.path.dirname(__file__), ".."), "results")
MONTHLY_PLN = 1000.0

# display -> (file_key, target_weight)
TARGET = {
    "GLD":  ("GLD",  0.30),
    "SLV":  ("SLV",  0.10),
    "AAPL": ("AAPL", 0.10),
    "MSFT": ("MSFT", 0.10),
    "AMZN": ("AMZN", 0.10),
    "GOOG": ("GOOG", 0.10),
    "JPM":  ("JPM",  0.10),
    "JNJ":  ("JNJ",  0.10),
}
SCHEMES = ["fixed_target", "geom_1.25", "geom_1.5", "geom_2.0"]
BANDS = {"geom_1.25": bt.geom_bands(1.25), "geom_1.5": bt.geom_bands(1.5), "geom_2.0": bt.geom_bands(2.0)}


def main():
    usdpln = bt.load_close("USDPLN_X")
    native, weekly, tgt = {}, {}, {}
    for disp, (key, w) in TARGET.items():
        s = bt.load_close(key)
        native[disp] = s; weekly[disp] = s.resample("W-FRI").last(); tgt[disp] = w
    end = max(s.index[-1] for s in native.values())
    cal = pd.DatetimeIndex(sorted(set().union(*[set(s.index) for s in native.values()])))
    fx = usdpln.reindex(cal).ffill()
    pln_px = {d: (native[d].reindex(cal).ffill() * fx) for d in native}

    scenarios = [(end.date()-relativedelta(years=y), lab) for y, lab in [(1,"1yr"),(3,"3yr"),(5,"5yr"),(8,"8yr")]]
    results = []
    for start, lab in scenarios:
        months = pd.date_range(pd.Timestamp(start), end, freq="MS")
        buy = sorted({cal[cal.searchsorted(m)] for m in months if cal.searchsorted(m) < len(cal)})
        units = {s: {d: 0.0 for d in native} for s in SCHEMES}
        hhi = {s: [] for s in SCHEMES}; maxw = {s: [] for s in SCHEMES}; eq = {s: [] for s in SCHEMES}
        wk_used = []
        for bd in buy:
            avail = {}
            for d in native:
                p = pln_px[d].loc[bd]
                if pd.isna(p) or p <= 0: continue
                bi = bt.band_index_for(weekly[d], bd)
                avail[d] = (p, bi)
                h = weekly[d][weekly[d].index <= bd]; h = h[h.index >= bd - pd.DateOffset(years=10)].dropna()
                wk_used.append(len(h))
            if not avail: continue
            for s in SCHEMES:
                if s == "fixed_target":
                    w = {d: tgt[d] for d in avail}
                else:
                    b = BANDS[s]
                    w = {d: tgt[d] * (b[bi] if bi is not None else 1.0) for d,(_,bi) in avail.items()}
                totw = sum(w.values())
                if totw <= 0: continue
                shares = np.array([w[d]/totw for d in avail])
                hhi[s].append(float((shares**2).sum())); maxw[s].append(float(shares.max()))
                for d,(p,_) in avail.items():
                    units[s][d] += (MONTHLY_PLN * w[d]/totw) / p
            for s in SCHEMES:
                eq[s].append(sum(units[s][d]*pln_px[d].loc[bd] for d in native if not pd.isna(pln_px[d].loc[bd])))
        invested = MONTHLY_PLN*len(buy)
        out = {}
        for s in SCHEMES:
            final = sum(units[s][d]*float(pln_px[d].loc[pln_px[d].index<=end].iloc[-1]) for d in native)
            roi = final/invested-1
            irr_m = bt.monthly_irr([-MONTHLY_PLN]*len(buy)+[final]); irr = ((1+irr_m)**12-1) if irr_m else None
            out[s] = {"final_pln": round(final), "roi_pct": round(roi*100,1),
                      "irr_annual_pct": round(irr*100,1) if irr is not None else None,
                      "avg_max_alloc_pct": round(float(np.mean(maxw[s]))*100,1),
                      "avg_hhi": round(float(np.mean(hhi[s])),3),
                      "max_drawdown_pct": round(bt.max_drawdown(np.array(eq[s]))*100,1)}
        results.append({"label": lab, "start": str(start), "months": len(buy), "invested_pln": invested,
                        "median_weeks_in_window": int(np.median(wk_used)), "schemes": out})

    print("="*94)
    print("EXAMPLE PORTFOLIO (Gold 30 / Silver 10 / AAPL MSFT AMZN GOOG JPM JNJ 10 ea) — DCA 1000 PLN/mo")
    print("="*94)
    for r in results:
        print(f"\n[{r['label']}] start {r['start']}  ({r['months']} mo, invested {r['invested_pln']:,.0f} PLN, "
              f"median regression window = {r['median_weeks_in_window']} wk ≈ {r['median_weeks_in_window']/52:.1f}y)")
        print(f"  {'scheme':<13}{'final PLN':>11}{'ROI%':>8}{'IRR/yr%':>9}{'maxDD%':>8}{'avgMaxAlloc%':>13}")
        for s in SCHEMES:
            o = r["schemes"][s]; tag = "  <- current scale" if s=="geom_2.0" else ("  (baseline)" if s=="fixed_target" else "")
            irr = o['irr_annual_pct'] if o['irr_annual_pct'] is not None else float('nan')
            print(f"  {s:<13}{o['final_pln']:>11,.0f}{o['roi_pct']:>8.1f}{irr:>9.1f}{o['max_drawdown_pct']:>8.1f}{o['avg_max_alloc_pct']:>13.1f}{tag}")
    with open(os.path.join(RESULTS_DIR,"example_portfolio_summary.json"),"w") as f:
        json.dump(results, f, indent=2)
    print("\nWritten: results/example_portfolio_summary.json")

if __name__ == "__main__":
    main()
