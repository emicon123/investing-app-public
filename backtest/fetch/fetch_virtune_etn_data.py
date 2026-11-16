#!/usr/bin/env python3
"""
Fetch full price history for Virtune's 4 crypto ETNs (GPW-listed, PLN-denominated,
launched 2026-02-25) from BiznesRadar's historical-quotes pages, plus refresh the
underlying crypto (BTC/ETH/SOL/XRP, USD) and USDPLN via yfinance.

Saves one CSV per ticker into ./data/prices/:
  ETNVIRBTCP.csv, ETNVIRETH.csv, ETNVIRSOL.csv, ETNVIRXRP.csv
    date,open,high,low,close,volume,turnover  (PLN, BiznesRadar, DD.MM.YYYY dates)
  BTC_USD.csv, ETH_USD.csv, SOL_USD.csv, XRP_USD.csv, USDPLN_X.csv
    Yahoo 3-row-header OHLC CSV (USD / USDPLN rate)

Feeds virtune_etn_tracking_check.py.
"""

import csv
import os
import re
import time
import urllib.request
from datetime import date, timedelta

import yfinance as yf

VIRTUNE_TICKERS = ["ETNVIRBTCP", "ETNVIRETH", "ETNVIRSOL", "ETNVIRXRP"]
CRYPTO_TICKERS = ["BTC-USD", "ETH-USD", "SOL-USD", "XRP-USD", "USDPLN=X"]

UA = {"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 "
                     "(KHTML, like Gecko) Chrome/125.0 Safari/537.36"}
OUT_DIR = os.path.join(os.path.join(os.path.dirname(__file__), ".."), "data", "prices")
os.makedirs(OUT_DIR, exist_ok=True)


def _fetch_html(url: str) -> str:
    req = urllib.request.Request(url, headers=UA)
    with urllib.request.urlopen(req, timeout=20) as r:
        return r.read().decode("utf-8", errors="replace")


def _parse_rows(html: str):
    m = re.search(r'<table class="qTableFull">(.*?)</table>', html, re.S)
    if not m:
        return []
    rows = re.findall(r"<tr>(.*?)</tr>", m.group(1), re.S)
    out = []
    for r in rows:
        cells = re.findall(r"<td[^>]*>(.*?)</td>", r, re.S)
        cells = [re.sub(r"<[^>]+>", "", c).strip() for c in cells]
        if len(cells) == 7:
            out.append(cells)
    return out


def _max_page(html: str) -> int:
    pages = re.findall(r"notowania-historyczne/\w+,(\d+)", html)
    return max([int(p) for p in pages], default=1)


def fetch_virtune():
    """BiznesRadar historical quotes, PLN, all available pages (full life of each ETN)."""
    for ticker in VIRTUNE_TICKERS:
        base = f"https://www.biznesradar.pl/notowania-historyczne/{ticker}"
        html = _fetch_html(base)
        rows = _parse_rows(html)
        mp = _max_page(html)
        for p in range(2, mp + 1):
            time.sleep(0.5)
            rows.extend(_parse_rows(_fetch_html(f"{base},{p}")))
        seen, dedup = set(), []
        for row in rows:
            if row[0] not in seen:
                seen.add(row[0])
                dedup.append(row)
        outpath = os.path.join(OUT_DIR, f"{ticker}.csv")
        with open(outpath, "w", newline="", encoding="utf-8") as f:
            w = csv.writer(f)
            w.writerow(["date", "open", "high", "low", "close", "volume", "turnover"])
            w.writerows(dedup)
        print(f"  {ticker:12s} {len(dedup):4d} rows (pages 1..{mp}) -> {outpath}")


def fetch_crypto_and_fx():
    """Refresh underlying crypto (USD) + USDPLN via yfinance, 9y lookback."""
    start = (date.today() - timedelta(days=365 * 9)).strftime("%Y-%m-%d")
    end = date.today().strftime("%Y-%m-%d")
    for t in CRYPTO_TICKERS:
        df = yf.download(t, start=start, end=end, auto_adjust=True, progress=False)
        df = df[["Open", "High", "Low", "Close"]].dropna()
        df.index.name = "Date"
        fname = os.path.join(OUT_DIR, f"{t.replace('=', '_').replace('-', '_')}.csv")
        df.to_csv(fname)
        print(f"  {t:10s} {len(df):4d} rows  {df.index[0].date()} -> {df.index[-1].date()}")


if __name__ == "__main__":
    print("Fetching Virtune ETN history (BiznesRadar, PLN)...")
    fetch_virtune()
    print("\nRefreshing underlying crypto (USD) + USDPLN (yfinance)...")
    fetch_crypto_and_fx()
    print("\nDone.")
