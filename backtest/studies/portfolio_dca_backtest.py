#!/usr/bin/env python3
"""
Portfolio DCA backtest on the ACTUAL MAIN portfolio universe, with maximum
available history (see fetch_portfolio_data.py) and per-currency conversion to PLN.

Same question as dca_regression_backtest.py (is the x2.0 regression scale too
aggressive?) but on the real holdings instead of a generic universe.

- Regression weight is computed on each asset's NATIVE weekly close (matching the
  backend, which regresses the Yahoo price in its own currency), no lookahead.
- Contributions/values are in PLN: USD assets via USDPLN, EUR via EURPLN, PLN as-is.
"""

import numpy as np, pandas as pd, os, json
from dateutil.relativedelta import relativedelta
import dca_regression_backtest as bt   # reuse load_close, band_index_for, geom_bands, SCHEMES

DATA_DIR    = os.path.join(os.path.join(os.path.dirname(__file__), ".."), "data", "prices")
RESULTS_DIR = os.path.join(os.path.join(os.path.dirname(__file__), ".."), "results")
MONTHLY_PLN = 1000.0

def safe(t): return t.replace("=", "_").replace("-", "_").replace(".", "_")

# (display, yahoo_ticker, currency)
PORTFOLIO = [
    ("CIFR","CIFR","USD"),("COIN","COIN","USD"),("CRCL","CRCL","USD"),("FISV","FISV","USD"),
    ("GME","GME","USD"),("HUT","HUT","USD"),("MARA","MARA","USD"),("MSTR","MSTR","USD"),
    ("ALGO","ALGO-USD","USD"),("BTC","BTC-USD","USD"),("DOGE","DOGE-USD","USD"),("ETH","ETH-USD","USD"),
    ("FET","FET-USD","USD"),("FLIP","FLIP-USD","USD"),("HBAR","HBAR-USD","USD"),("LINK","LINK-USD","USD"),
    ("LOFI","LOFI34187-USD","USD"),("OLAS","OLAS-USD","USD"),("SOL","SOL-USD","USD"),("SUI","SUI20947-USD","USD"),
    ("TAO","TAO22974-USD","USD"),("TRAC","TRAC-USD","USD"),("TRX","TRX-USD","USD"),("XPL","XPL-USD","USD"),
    ("XRP","XRP-USD","USD"),("ZANO","ZANO-USD","USD"),("ZEC","ZEC-USD","USD"),
    ("ETCGLDRMAU","ETCGLDRMAU.WA","PLN"),("QUTM","QUTM.DE","EUR"),("CHFPLN","CHFPLN=X","PLN"),
]

def load(t): return bt.load_close(safe(t))

def main():
    usdpln = load("USDPLN=X"); eurpln = load("EURPLN=X")
    native, weekly, cur = {}, {}, {}
    for disp, tk, c in PORTFOLIO:
        s = load(tk)
        if s.empty: continue
        native[disp] = s
        weekly[disp] = s.resample("W-FRI").last()
        cur[disp] = c
    end = max(s.index[-1] for s in native.values())
    cal = pd.DatetimeIndex(sorted(set().union(*[set(s.index) for s in native.values()])))

    def fx_for(c): return {"USD": usdpln, "EUR": eurpln, "PLN": None}[c]
    # PLN price series (native close * fx), ffilled on the master calendar
    pln_px = {}
    for d in native:
        s = native[d].reindex(cal).ffill()
        f = fx_for(cur[d])
        pln_px[d] = s if f is None else s * f.reindex(cal).ffill()

    schemes = list(bt.SCHEMES.keys())
    scenarios = [(end.date()-relativedelta(years=y), lab) for y, lab in
                 [(1,"1yr"),(3,"3yr"),(5,"5yr"),(8,"8yr")]]
    results = []
    for start, lab in scenarios:
        months = pd.date_range(pd.Timestamp(start), end, freq="MS")
        buy = sorted({cal[cal.searchsorted(m)] for m in months if cal.searchsorted(m) < len(cal)})
        units = {s: {d: 0.0 for d in native} for s in schemes}
        hhi = {s: [] for s in schemes}; maxw = {s: [] for s in schemes}
        eq = {s: [] for s in schemes}
        active_counts = []; short_window = 0; total = 0
        for bd in buy:
            avail = {}
            for d in native:
                p = pln_px[d].loc[bd]
                if pd.isna(p) or p <= 0: continue
                bi = bt.band_index_for(weekly[d], bd)
                avail[d] = (p, bi)
                total += 1
                if bi is None: short_window += 1
            if not avail: continue
            active_counts.append(len(avail))
            for s in schemes:
                bands = bt.SCHEMES[s]
                w = {d: (1.0 if bands is None else (bands[bi] if bi is not None else 1.0))
                     for d,(_,bi) in avail.items()}
                tot = sum(w.values())
                if tot <= 0: continue
                shares = np.array([w[d]/tot for d in avail])
                hhi[s].append(float((shares**2).sum())); maxw[s].append(float(shares.max()))
                for d,(p,_) in avail.items():
                    units[s][d] += (MONTHLY_PLN * w[d]/tot) / p
            for s in schemes:
                val = sum(units[s][d]*pln_px[d].loc[bd] for d in native if not pd.isna(pln_px[d].loc[bd]))
                eq[s].append(val)
        invested = MONTHLY_PLN*len(buy)
        out = {}
        for s in schemes:
            final = sum(units[s][d]*float(pln_px[d].loc[pln_px[d].index<=end].iloc[-1])
                        for d in native if not pln_px[d].loc[pln_px[d].index<=end].empty)
            roi = final/invested-1 if invested else 0
            cfs = [-MONTHLY_PLN]*len(buy)+[final]
            irr_m = bt.monthly_irr(cfs); irr = ((1+irr_m)**12-1) if irr_m is not None else None
            out[s] = {"final_pln": round(final), "roi_pct": round(roi*100,1),
                      "irr_annual_pct": round(irr*100,1) if irr is not None else None,
                      "avg_hhi": round(float(np.mean(hhi[s])),3) if hhi[s] else None,
                      "avg_max_alloc_pct": round(float(np.mean(maxw[s]))*100,1) if maxw[s] else None,
                      "max_drawdown_pct": round(bt.max_drawdown(np.array(eq[s]))*100,1)}
        results.append({"label": lab, "start": str(start), "months": len(buy),
                        "invested_pln": invested,
                        "avg_active_assets": round(float(np.mean(active_counts)),1) if active_counts else 0,
                        "pct_short_regression_window": round(100*short_window/total,0) if total else 0,
                        "schemes": out})

    print("="*96)
    print("ACTUAL PORTFOLIO — monthly DCA 1000 PLN — final PLN / ROI / money-weighted IRR / maxDD")
    print("="*96)
    for r in results:
        print(f"\n[{r['label']}] start {r['start']}  ({r['months']} mo, invested {r['invested_pln']:,.0f} PLN, "
              f"avg {r['avg_active_assets']} active assets, {r['pct_short_regression_window']:.0f}% of weights on a <10wk/short window)")
        print(f"  {'scheme':<11}{'final PLN':>11}{'ROI%':>8}{'IRR/yr%':>9}{'maxDD%':>8}{'avgHHI':>8}{'avgMaxAlloc%':>13}")
        for s in schemes:
            o = r["schemes"][s]; tag = "  <- current" if s=="geom_2.0" else ("  (baseline)" if s=="equal" else "")
            irr = o['irr_annual_pct'] if o['irr_annual_pct'] is not None else float('nan')
            print(f"  {s:<11}{o['final_pln']:>11,.0f}{o['roi_pct']:>8.1f}{irr:>9.1f}"
                  f"{o['max_drawdown_pct']:>8.1f}{o['avg_hhi']:>8.3f}{o['avg_max_alloc_pct']:>13.1f}{tag}")
    with open(os.path.join(RESULTS_DIR,"portfolio_dca_summary.json"),"w") as f:
        json.dump(results, f, indent=2)
    print("\nWritten: results/portfolio_dca_summary.json")

if __name__ == "__main__":
    main()
