#!/usr/bin/env python3
"""
Fetch 5+ years of daily OHLC from Yahoo Finance for all MEXC bot assets
plus the USDPLN rate. Saves one CSV per ticker into ./data/.
"""

import yfinance as yf
import pandas as pd
import os
import time
from datetime import date, timedelta

START = (date.today() - timedelta(days=365 * 9)).strftime("%Y-%m-%d")  # 9yr buffer
END   = date.today().strftime("%Y-%m-%d")

ASSETS = [
    ("Apple",           "AAPL"),
    ("ADA",             "ADA-USD"),
    ("Amazon",          "AMZN"),
    ("ASML",            "ASML"),
    ("Broadcom",        "AVGO"),
    ("Alibaba",         "BABA"),
    ("BNB",             "BNB-USD"),
    ("BTC",             "BTC-USD"),
    ("DOGE",            "DOGE-USD"),
    ("ETH",             "ETH-USD"),
    ("Gold",            "GC=F"),
    ("Google",          "GOOG"),
    ("JPMorgan",        "JPM"),
    ("Linde",           "LIN"),
    ("Meta",            "META"),
    ("Microsoft",       "MSFT"),
    ("Nvidia",          "NVDA"),
    ("NovoNordisk",     "NVO"),
    ("Palladium",       "PA=F"),
    ("Silver",          "SI=F"),
    ("SOL",             "SOL-USD"),
    ("TRX",             "TRX-USD"),
    ("Tesla",           "TSLA"),
    ("TSMC",            "TSM"),
    ("Walmart",         "WMT"),
    ("XRP",             "XRP-USD"),
    ("USDPLN",          "USDPLN=X"),
]

OUT_DIR = os.path.join(os.path.join(os.path.dirname(__file__), ".."), "data", "prices")
os.makedirs(OUT_DIR, exist_ok=True)

summary = []

for name, ticker in ASSETS:
    try:
        df = yf.download(ticker, start=START, end=END, auto_adjust=True, progress=False)
        if df.empty:
            print(f"  EMPTY  {ticker}")
            summary.append({"name": name, "ticker": ticker, "bars": 0, "from": None, "to": None})
            continue

        df = df[["Open", "High", "Low", "Close"]].dropna()
        df.index.name = "Date"
        fname = os.path.join(OUT_DIR, f"{ticker.replace('=', '_').replace('-', '_')}.csv")
        df.to_csv(fname)
        first = df.index[0].date() if hasattr(df.index[0], 'date') else str(df.index[0])[:10]
        last  = df.index[-1].date() if hasattr(df.index[-1], 'date') else str(df.index[-1])[:10]
        print(f"  OK     {ticker:15s}  bars={len(df):5d}  {first} → {last}")
        summary.append({"name": name, "ticker": ticker, "bars": len(df), "from": str(first), "to": str(last)})
    except Exception as e:
        print(f"  ERROR  {ticker}: {e}")
        summary.append({"name": name, "ticker": ticker, "bars": 0, "from": None, "to": None, "error": str(e)})
    time.sleep(0.3)

pd.DataFrame(summary).to_csv(os.path.join(OUT_DIR, "_summary.csv"), index=False)
print("\nDone. Summary written to data/_summary.csv")
