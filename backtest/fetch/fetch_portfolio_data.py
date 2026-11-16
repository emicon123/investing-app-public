#!/usr/bin/env python3
"""
Fetch the OLDEST available daily history (Yahoo, max range) for the actual MAIN
portfolio assets + the FX rates needed to value them in PLN.

Yahoo serves crypto from coin inception (usually deeper than an exchange's listing
date) and needs no API key, so it is the deepest free source for most of these.
The binding limit is asset age, not the source — most alts launched 2023-2025.

Writes one CSV per ticker into ./data/ (filename = ticker with =,-,. -> _) and a
depth report to ./data/_portfolio_summary.csv.
"""

import yfinance as yf
import pandas as pd
import os, time

OUT_DIR = os.path.join(os.path.join(os.path.dirname(__file__), ".."), "data", "prices")
os.makedirs(OUT_DIR, exist_ok=True)
START = "2000-01-01"   # far enough back; Yahoo returns from each asset's inception

def safe(t): return t.replace("=", "_").replace("-", "_").replace(".", "_")

# (display, yahoo_ticker, price_currency) — from the DB (MAIN, non-companion)
PORTFOLIO = [
    ("CIFR", "CIFR", "USD"), ("COIN", "COIN", "USD"), ("CRCL", "CRCL", "USD"),
    ("FISV", "FISV", "USD"), ("GME", "GME", "USD"), ("HUT", "HUT", "USD"),
    ("MARA", "MARA", "USD"), ("MSTR", "MSTR", "USD"),
    ("ALGO", "ALGO-USD", "USD"), ("BTC", "BTC-USD", "USD"), ("DOGE", "DOGE-USD", "USD"),
    ("ETH", "ETH-USD", "USD"), ("FET", "FET-USD", "USD"), ("FLIP", "FLIP-USD", "USD"),
    ("HBAR", "HBAR-USD", "USD"), ("LINK", "LINK-USD", "USD"), ("LOFI", "LOFI34187-USD", "USD"),
    ("OLAS", "OLAS-USD", "USD"), ("SOL", "SOL-USD", "USD"), ("SUI", "SUI20947-USD", "USD"),
    ("TAO", "TAO22974-USD", "USD"), ("TRAC", "TRAC-USD", "USD"), ("TRX", "TRX-USD", "USD"),
    ("XPL", "XPL-USD", "USD"), ("XRP", "XRP-USD", "USD"), ("ZANO", "ZANO-USD", "USD"),
    ("ZEC", "ZEC-USD", "USD"),
    ("ETCGLDRMAU", "ETCGLDRMAU.WA", "PLN"), ("QUTM", "QUTM.DE", "EUR"),
    ("CHFPLN", "CHFPLN=X", "PLN"),
]
FX = [("USDPLN", "USDPLN=X"), ("EURPLN", "EURPLN=X")]

rows = []
for display, ticker, cur in PORTFOLIO + [(n, t, "FX") for n, t in FX]:
    try:
        df = yf.download(ticker, start=START, auto_adjust=True, progress=False)
        if df.empty:
            print(f"  EMPTY  {ticker}")
            rows.append({"display": display, "ticker": ticker, "currency": cur,
                         "bars": 0, "from": None, "to": None, "years": 0.0})
            continue
        df = df[["Open", "High", "Low", "Close"]].dropna()
        df.index.name = "Date"
        df.to_csv(os.path.join(OUT_DIR, f"{safe(ticker)}.csv"))
        first, last = df.index[0].date(), df.index[-1].date()
        yrs = round((df.index[-1] - df.index[0]).days / 365.25, 1)
        print(f"  OK  {ticker:16s} {cur:3s} bars={len(df):5d}  {first} → {last}  ({yrs}y)")
        rows.append({"display": display, "ticker": ticker, "currency": cur,
                     "bars": len(df), "from": str(first), "to": str(last), "years": yrs})
    except Exception as e:
        print(f"  ERROR  {ticker}: {e}")
        rows.append({"display": display, "ticker": ticker, "currency": cur,
                     "bars": 0, "from": None, "to": None, "years": 0.0, "error": str(e)})
    time.sleep(0.3)

summ = pd.DataFrame(rows)
summ.to_csv(os.path.join(OUT_DIR, "_portfolio_summary.csv"), index=False)
print("\n=== depth report (sorted by history length) ===")
inv = summ[summ.currency != "FX"].sort_values("years", ascending=False)
for _, r in inv.iterrows():
    flag = "  <- < 10y (regression window cannot be full)" if r["years"] < 10 else ""
    print(f"  {r['ticker']:16s} {r['years']:5.1f}y  ({r['from']} → {r['to']}){flag}")
n10 = int((inv["years"] >= 10).sum())
print(f"\n{n10}/{len(inv)} portfolio assets have >= 10 years of history.")
