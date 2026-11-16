#!/usr/bin/env python3
"""
Fetch LONG daily history (2007-01-01 -> now) for the individual-stock tickers
needed by study 17 ("Global 20" -- a 100%-individual-stock, no-ETF, no-bonds/gold
global equity portfolio) that are NOT already present as data/prices/<T>_full.csv.
Matches fetch_long_history.py's exact conventions (yf.download, auto_adjust=True,
daily close, date+close columns) so the new files are treated identically by the
existing usd_close()/pln_ret() loaders.

Already present as *_full.csv (reused, NOT refetched): AAPL, MSFT, AMZN, GOOGL,
NVDA, JPM, TSLA.

Fetched fresh here (previously either missing entirely, or only present as a
short-format plain .csv which we deliberately do NOT parse/mix in, per task
brief -- fetch a fresh clean _full.csv for all of these instead):
  USA:      JNJ, WMT, AVGO, KO, V
  Europe:   ASML, NVO, SAP, UL
  Rest-of-World: TSM, TM, BABA

Samsung Electronics (Korea) is the hard case: no direct USD ADR. Fetched here as
its primary Korea Exchange listing, 005930.KS (KRW-denominated) -> saved as
SAMSUNG_full.csv, PLUS a USDKRW=X FX leg -> saved as USDKRW_X_full.csv (clean
date,close format, matching the _full convention rather than the raw
skiprows-format used by the pre-existing USDPLN_X.csv/EURPLN_X.csv/CHFPLN_X.csv
files). Samsung's data came back clean and with history back to 2000 (Yahoo's
practical data-start for this ticker) -- comfortably clears 2007-01 needed here,
so no ADR-proxy fallback was needed; used directly.
"""
import os
import time
import yfinance as yf
import pandas as pd

OUT = os.path.join(os.path.join(os.path.dirname(__file__), ".."), "data", "prices")
START, END = "2007-01-01", pd.Timestamp.today().strftime("%Y-%m-%d")

TICKERS = {
    # USA
    "JNJ": "JNJ", "WMT": "WMT", "AVGO": "AVGO", "KO": "KO", "V": "V",
    # Europe (ADRs)
    "ASML": "ASML", "NVO": "NVO", "SAP": "SAP", "UL": "UL",
    # Rest of World (ADRs)
    "TSM": "TSM", "TM": "TM", "BABA": "BABA",
}
# Samsung + its FX leg -- fetched separately below (different Yahoo ticker vs. saved name).
KRW_LEGS = {"005930.KS": "SAMSUNG", "USDKRW=X": "USDKRW_X"}


def close_series(tk, start=START, end=END):
    df = yf.download(tk, start=start, end=end, auto_adjust=True, progress=False)
    if df is None or df.empty:
        return None
    c = df["Close"]
    if isinstance(c, pd.DataFrame):
        c = c.iloc[:, 0]
    return c.dropna()


def save(tk, name):
    path = os.path.join(OUT, f"{name}_full.csv")
    if os.path.exists(path):
        print(f"  SKIP {tk}: {path} already exists, not overwriting")
        return
    try:
        s = close_series(tk)
        if s is None or len(s) < 250:
            print(f"  SKIP {tk} ({0 if s is None else len(s)} rows)"); return
        out = s.rename("close").rename_axis("date").reset_index()
        out["date"] = pd.to_datetime(out["date"]).dt.strftime("%Y-%m-%d")
        out[["date", "close"]].to_csv(path, index=False)
        print(f"  OK   {name:10s} {len(out):5d} rows  {out['date'].iloc[0]} -> {out['date'].iloc[-1]}")
    except Exception as e:
        print(f"  ERR  {tk}: {str(e)[:70]}")
    time.sleep(0.3)


for tk, name in TICKERS.items():
    save(tk, name)

for tk, name in KRW_LEGS.items():
    save(tk, name)

print("\nDone.")
