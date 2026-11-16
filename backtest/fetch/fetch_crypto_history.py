#!/usr/bin/env python3
"""
Fetch the LONGEST free daily USD price history for BTC, ETH, SOL and save a
normalised `date,close` CSV per asset into ./data/prices/<TICKER>_full.csv.

  BTC  2010-07-18 -> now   Coinmetrics community API (PriceUSD, keyless)
  ETH  2015-08-08 -> now   Coinmetrics community API
  SOL  2020-04-10 -> now   existing Yahoo OHLC CSV (SOL_USD price is gated on
                           the Coinmetrics free tier; SOL only launched 2020)

These feed macro_correlation.py. "2008" is impossible for crypto (BTC's genesis
block is Jan 2009, first market price mid-2010), so each series starts at the
asset's inception.
"""

import io
import json
import os
import urllib.request

import pandas as pd

PRICES_DIR = os.path.join(os.path.join(os.path.dirname(__file__), ".."), "data", "prices")
CM = "https://community-api.coinmetrics.io/v4/timeseries/asset-metrics"
UA = {"User-Agent": "Mozilla/5.0 (crypto-fetch)"}


def coinmetrics_close(asset: str) -> pd.Series:
    """Full daily PriceUSD history for `asset`, following pagination."""
    out = {}
    url = (f"{CM}?assets={asset}&metrics=PriceUSD&frequency=1d"
           f"&page_size=10000&paging_from=start&start_time=2009-01-01")
    while url:
        with urllib.request.urlopen(urllib.request.Request(url, headers=UA), timeout=90) as r:
            payload = json.loads(r.read())
        for row in payload.get("data", []):
            out[pd.Timestamp(row["time"][:10])] = float(row["PriceUSD"])
        url = payload.get("next_page_url")
    s = pd.Series(out).sort_index()
    s.index.name = "date"
    return s


def yahoo_close(ticker: str) -> pd.Series:
    """Close column from a Yahoo OHLC CSV (3-row MultiIndex header)."""
    path = os.path.join(PRICES_DIR, f"{ticker}.csv")
    df = pd.read_csv(path, skiprows=[1, 2])
    df = df.rename(columns={df.columns[0]: "date"})
    df["date"] = pd.to_datetime(df["date"], errors="coerce")
    df = df.dropna(subset=["date"]).set_index("date")
    return df["Close"].astype(float).sort_index()


def save(name: str, s: pd.Series):
    s = s.dropna()
    out = s.rename("close").rename_axis("date").reset_index()
    out["date"] = out["date"].dt.strftime("%Y-%m-%d")
    path = os.path.join(PRICES_DIR, f"{name}_full.csv")
    out.to_csv(path, index=False)
    print(f"  {name}_full.csv  {len(out):5d} rows  {out['date'].iloc[0]} -> {out['date'].iloc[-1]}")


if __name__ == "__main__":
    print(f"Writing long-history close CSVs to {PRICES_DIR}\n")
    save("BTC_USD", coinmetrics_close("btc"))
    save("ETH_USD", coinmetrics_close("eth"))
    save("SOL_USD", yahoo_close("SOL_USD"))
    print("\nDone.")
