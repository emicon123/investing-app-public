#!/usr/bin/env python3
"""
Fetch LONG daily history (2007-01-01 -> now) for the 5 tickers needed by study 16
(classic textbook portfolios: 60/40, Permanent Portfolio, Golden Butterfly, All
Weather) that are NOT already present in data/prices/. Saved as
data/prices/<TICKER>_full.csv (date,close), matching fetch_long_history.py's exact
conventions exactly (same yf.download() call, same column format) so downstream
loaders (usd_close() in the portfolio backtest scripts) treat them identically to
the pre-existing SPY/TLT/GLD/IWM _full files.

Does NOT touch any ticker already present in data/prices/ (SPY, TLT, GLD, IWM etc.).

Tickers fetched:
  AGG  - iShares Core US Aggregate Bond (inception 2003)      -> 60/40 bonds leg
  IEF  - iShares 7-10Y Treasury (inception 2002)               -> All Weather intermediate treasuries
  SHY  - iShares 1-3Y Treasury (inception 2002)                -> Golden Butterfly short-term treasuries
  IWN  - iShares Russell 2000 Value (inception 2000)           -> Golden Butterfly small-cap value
  DBC  - Invesco DB Commodity Index (inception Feb 2006)       -> All Weather broad commodities
"""
import os
import time
import yfinance as yf
import pandas as pd

OUT = os.path.join(os.path.join(os.path.dirname(__file__), ".."), "data", "prices")
START, END = "2007-01-01", pd.Timestamp.today().strftime("%Y-%m-%d")

TICKERS = {"AGG": "AGG", "IEF": "IEF", "SHY": "SHY", "IWN": "IWN", "DBC": "DBC"}


def close_series(tk, start=START, end=END):
    df = yf.download(tk, start=start, end=end, auto_adjust=True, progress=False)
    if df is None or df.empty:
        return None
    c = df["Close"]
    if isinstance(c, pd.DataFrame):
        c = c.iloc[:, 0]
    return c.dropna()


summary = []
for tk, name in TICKERS.items():
    path = os.path.join(OUT, f"{name}_full.csv")
    if os.path.exists(path):
        print(f"  SKIP {tk}: {path} already exists, not overwriting")
        continue
    try:
        s = close_series(tk)
        if s is None or len(s) < 250:
            print(f"  SKIP {tk} ({0 if s is None else len(s)} rows)"); continue
        out = s.rename("close").rename_axis("date").reset_index()
        out["date"] = pd.to_datetime(out["date"]).dt.strftime("%Y-%m-%d")
        out[["date", "close"]].to_csv(path, index=False)
        print(f"  OK   {name:6s} {len(out):5d} rows  {out['date'].iloc[0]} -> {out['date'].iloc[-1]}")
        summary.append(name)
    except Exception as e:
        print(f"  ERR  {tk}: {str(e)[:70]}")
    time.sleep(0.3)

print(f"\nSaved {len(summary)} new long-history files to data/prices/")
