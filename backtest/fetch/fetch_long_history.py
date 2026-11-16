#!/usr/bin/env python3
"""
Fetch LONG daily history (2007-01-01 → now) for liquid, macro-relevant traditional
assets from Yahoo, saved as data/prices/<TICKER>_full.csv (date,close) — the same
format as the crypto _full files, which the analysis loaders already prefer.

Goal: let the macro-strategy tests run over the full 2008-2026 macro window (incl. the
GFC) for assets that actually existed then. Crypto can't predate ~2010, so this is the
traditional-asset leg of the universe + adds bonds/credit/dollar/EM for breadth.

QQQ and FXI additionally get their pre-2007 history back-filled (see EXTEND_BACK below)
to their real Yahoo-available start: QQQ to 1999-03-10 (inception) and FXI to 2004-10-05
(inception) — these are the US100/Nasdaq-100 and China-equity proxies used by the demo
portfolio, and the longer history lets regime-blend backtests use windows beyond 13y.
"""
import os
import time
import yfinance as yf
import pandas as pd

OUT = os.path.join(os.path.join(os.path.dirname(__file__), ".."), "data", "prices")
START, END = "2007-01-01", pd.Timestamp.today().strftime("%Y-%m-%d")

# Yahoo ticker -> saved name (clean). Broad macro testbed.
TICKERS = {
    # US equity indices / factors
    "SPY": "SPY", "QQQ": "QQQ", "DIA": "DIA", "IWM": "IWM",      # large / nasdaq / dow / small-cap
    "EEM": "EEM",                                                  # emerging mkts (liquidity-sensitive)
    # rates / credit / dollar (macro-sensitive)
    "TLT": "TLT", "HYG": "HYG", "LQD": "LQD", "UUP": "UUP",        # long bonds / HY / IG / USD
    # metals
    "GLD": "GLD", "SLV": "SLV",
    # mega-cap / high-beta single names
    "NVDA": "NVDA", "AAPL": "AAPL", "MSFT": "MSFT", "TSLA": "TSLA",
    "AMZN": "AMZN", "GOOGL": "GOOGL", "META": "META", "JPM": "JPM",
    "XLF": "XLF", "XLE": "XLE",                                    # financials / energy sectors
}


def close_series(tk, start=START, end=END):
    df = yf.download(tk, start=start, end=end, auto_adjust=True, progress=False)
    if df is None or df.empty:
        return None
    c = df["Close"]
    if isinstance(c, pd.DataFrame):       # multiindex (single ticker) -> first col
        c = c.iloc[:, 0]
    return c.dropna()


summary = []
for tk, name in TICKERS.items():
    try:
        s = close_series(tk)
        if s is None or len(s) < 250:
            print(f"  SKIP {tk} ({0 if s is None else len(s)} rows)"); continue
        out = s.rename("close").rename_axis("date").reset_index()
        out["date"] = pd.to_datetime(out["date"]).dt.strftime("%Y-%m-%d")
        out[["date", "close"]].to_csv(os.path.join(OUT, f"{name}_full.csv"), index=False)
        print(f"  OK   {name:6s} {len(out):5d} rows  {out['date'].iloc[0]} -> {out['date'].iloc[-1]}")
        summary.append(name)
    except Exception as e:
        print(f"  ERR  {tk}: {str(e)[:70]}")
    time.sleep(0.3)

print(f"\nSaved {len(summary)} long-history files to data/prices/")

# --- Extend specific tickers further back than the shared 2007 START -------
# QQQ (US100/Nasdaq-100 proxy) and FXI (China equity proxy) both have real
# Yahoo history well before 2007 — QQQ back to its 1999-03-10 inception, FXI
# back to its 2004-10-05 inception. We PREPEND that older segment onto the
# already-saved 2007+ file above instead of re-downloading/overwriting the
# whole series, so any values already consumed by prior studies stay put.
EXTEND_BACK = {
    "QQQ": "1999-01-01",   # actual inception 1999-03-10
    "FXI": "2004-01-01",   # actual inception 2004-10-05
}

for tk, extend_start in EXTEND_BACK.items():
    path = os.path.join(OUT, f"{tk}_full.csv")
    if not os.path.exists(path):
        print(f"  SKIP extend {tk}: {path} not found (run main fetch first)")
        continue
    existing = pd.read_csv(path, parse_dates=["date"])
    first_existing = existing["date"].min()
    try:
        older = close_series(tk, start=extend_start, end=first_existing.strftime("%Y-%m-%d"))
    except Exception as e:
        print(f"  ERR  extend {tk}: {str(e)[:70]}")
        continue
    if older is None or older.empty:
        print(f"  SKIP extend {tk}: no older rows returned")
        continue
    older = older.rename("close").rename_axis("date").reset_index()
    older["date"] = pd.to_datetime(older["date"])
    older = older[older["date"] < first_existing]          # no overlap with existing rows
    merged = pd.concat([older[["date", "close"]], existing[["date", "close"]]], ignore_index=True)
    merged = merged.drop_duplicates(subset="date").sort_values("date").reset_index(drop=True)
    merged["date"] = merged["date"].dt.strftime("%Y-%m-%d")
    merged.to_csv(path, index=False)
    print(f"  EXTENDED {tk:6s} {len(merged):5d} rows  {merged['date'].iloc[0]} -> {merged['date'].iloc[-1]} "
          f"(+{len(older)} older rows)")
    time.sleep(0.3)
