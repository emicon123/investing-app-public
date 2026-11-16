#!/usr/bin/env python3
"""
Historical backtest of the MEXC DeMarker bot.

Replicates MexcBotService.java + DeMarkerService.java logic exactly:
  - 14-period DeMarker computed from 15 daily OHLC bars
  - Sell: DeM > 0.75 (100%/50%/25%/10% by threshold)
  - Buy:  DeM < 0.25 (50%/25%/12.5%/7%/3%/1% of *initial* USDT)
  - Runs once per weekday (Mon–Fri)
  - Starting capital: 1000 PLN converted to USDT at the historical USDPLN rate

Results are written to ./results/.
"""

import pandas as pd
import numpy as np
import os
import json
from datetime import date, timedelta

DATA_DIR    = os.path.join(os.path.join(os.path.dirname(__file__), ".."), "data", "prices")
RESULTS_DIR = os.path.join(os.path.join(os.path.dirname(__file__), ".."), "results")
os.makedirs(RESULTS_DIR, exist_ok=True)

DEMARKER_PERIOD = 14
REQUIRED_BARS   = 15  # 14 diffs needs 15 points
MIN_NOTIONAL    = 1.0
MIN_AMOUNT      = 1.0
MIN_USDT        = 5.0
INITIAL_PLN     = 1000.0

ASSETS = [
    ("Apple",       "AAPL"),
    ("ADA",         "ADA_USD"),
    ("Amazon",      "AMZN"),
    ("ASML",        "ASML"),
    ("Broadcom",    "AVGO"),
    ("Alibaba",     "BABA"),
    ("BNB",         "BNB_USD"),
    ("BTC",         "BTC_USD"),
    ("DOGE",        "DOGE_USD"),
    ("ETH",         "ETH_USD"),
    ("Gold",        "GC_F"),
    ("Google",      "GOOG"),
    ("JPMorgan",    "JPM"),
    ("Linde",       "LIN"),
    ("Meta",        "META"),
    ("Microsoft",   "MSFT"),
    ("Nvidia",      "NVDA"),
    ("NovoNordisk", "NVO"),
    ("Palladium",   "PA_F"),
    ("Silver",      "SI_F"),
    ("SOL",         "SOL_USD"),
    ("TRX",         "TRX_USD"),
    ("Tesla",       "TSLA"),
    ("TSMC",        "TSM"),
    ("Walmart",     "WMT"),
    ("XRP",         "XRP_USD"),
]

# ── Data loading ──────────────────────────────────────────────────────────────

def load_ohlc(file_key: str) -> pd.DataFrame:
    path = os.path.join(DATA_DIR, f"{file_key}.csv")
    if not os.path.exists(path):
        return pd.DataFrame()
    # yfinance 1.4+ writes a 3-row header: row0=Price/col names, row1=Ticker, row2=Date header
    df = pd.read_csv(path, skiprows=3, header=None, names=["Date", "Open", "High", "Low", "Close"])
    df = df.dropna(subset=["Date"])
    df["Date"] = pd.to_datetime(df["Date"], format="%Y-%m-%d", errors="coerce")
    df = df.dropna(subset=["Date"])
    df = df.set_index("Date")
    df = df[["High", "Low", "Close"]].apply(pd.to_numeric, errors="coerce").dropna()
    df.index = df.index.normalize()
    return df.sort_index()


def load_usdpln() -> pd.Series:
    path = os.path.join(DATA_DIR, "USDPLN_X.csv")
    if not os.path.exists(path):
        return pd.Series(dtype=float)
    df = pd.read_csv(path, skiprows=3, header=None, names=["Date", "Open", "High", "Low", "Close"])
    df = df.dropna(subset=["Date"])
    df["Date"] = pd.to_datetime(df["Date"], format="%Y-%m-%d", errors="coerce")
    df = df.dropna(subset=["Date"])
    df = df.set_index("Date")
    s = pd.to_numeric(df["Close"], errors="coerce").dropna()
    s.index = s.index.normalize()
    return s.sort_index()


# ── DeMarker computation ──────────────────────────────────────────────────────

def compute_demarker(bars: pd.DataFrame) -> float | None:
    """Compute 14-period DeMarker from exactly 15 bars (oldest first)."""
    if len(bars) < REQUIRED_BARS:
        return None
    bars = bars.iloc[-REQUIRED_BARS:]
    sum_de_max = 0.0
    sum_de_min = 0.0
    for i in range(1, DEMARKER_PERIOD + 1):
        prev = bars.iloc[i - 1]
        curr = bars.iloc[i]
        sum_de_max += max(0.0, curr["High"] - prev["High"])
        sum_de_min += max(0.0, prev["Low"]  - curr["Low"])
    sma_max = sum_de_max / DEMARKER_PERIOD
    sma_min = sum_de_min / DEMARKER_PERIOD
    denom = sma_max + sma_min
    return 0.5 if denom == 0.0 else sma_max / denom


# ── Trade logic (mirrors MexcBotService) ─────────────────────────────────────

def sell_pct(dm: float) -> float:
    if dm > 0.95: return 1.00
    if dm > 0.90: return 0.50
    if dm > 0.80: return 0.25
    return 0.10  # > 0.75


def buy_pct(dm: float) -> float:
    if dm == 0.0:  return 0.50
    if dm < 0.05:  return 0.25
    if dm < 0.10:  return 0.125
    if dm < 0.15:  return 0.07
    if dm < 0.20:  return 0.03
    return 0.01   # < 0.25


# ── Backtest engine ───────────────────────────────────────────────────────────

def run_backtest(start_date: date, label: str, ohlc_data: dict, usdpln: pd.Series):
    # Convert 1000 PLN → USDT at start date
    start_ts = pd.Timestamp(start_date)
    rate_series = usdpln[usdpln.index <= start_ts]
    if rate_series.empty:
        print(f"  [{label}] No USDPLN rate at {start_date}, using 4.0")
        usdpln_rate = 4.0
    else:
        usdpln_rate = float(rate_series.iloc[-1])
    initial_usdt = round(INITIAL_PLN / usdpln_rate, 2)
    print(f"\n[{label}] start={start_date}  USDPLN={usdpln_rate:.4f}  initial_USDT={initial_usdt:.2f}")

    usdt = initial_usdt
    holdings: dict[str, float] = {name: 0.0 for name, _ in ASSETS}

    # All trading days in range (Mon–Fri only, intersected with data availability)
    end_date = date.today()
    all_dates = pd.date_range(start=start_date, end=end_date, freq="B")  # business days

    equity_curve = []
    trade_log    = []

    for ts in all_dates:
        day = ts.date()

        # ── Compute DeMarker for every asset ──
        dem_values: dict[str, float | None] = {}
        prices: dict[str, float] = {}

        for name, file_key in ASSETS:
            df = ohlc_data.get(name)
            if df is None or df.empty:
                dem_values[name] = None
                continue
            bars_up_to = df[df.index <= ts]
            if len(bars_up_to) < REQUIRED_BARS:
                dem_values[name] = None
                continue
            dem_values[name] = compute_demarker(bars_up_to)
            prices[name] = float(bars_up_to.iloc[-1]["Close"])

        # ── Sell phase (sorted by DeM DESC) ──
        sell_candidates = [
            (name, dem_values[name])
            for name, _ in ASSETS
            if dem_values.get(name) is not None
            and dem_values[name] > 0.75
            and holdings.get(name, 0.0) > 0.0
        ]
        sell_candidates.sort(key=lambda x: x[1], reverse=True)

        for name, dm in sell_candidates:
            price = prices.get(name)
            if price is None or price <= 0:
                continue
            pct   = sell_pct(dm)
            qty   = holdings[name] * pct
            notional = qty * price
            if qty <= 0 or notional < MIN_NOTIONAL:
                continue
            holdings[name] -= qty
            usdt += notional
            trade_log.append({
                "date": str(day), "action": "SELL", "asset": name,
                "dm": round(dm, 4), "qty": round(qty, 6),
                "price": round(price, 4), "usdt": round(notional, 4),
            })

        # ── Buy phase (sorted by DeM ASC) ──
        buy_candidates = [
            (name, dem_values[name])
            for name, _ in ASSETS
            if dem_values.get(name) is not None
            and dem_values[name] < 0.25
        ]
        buy_candidates.sort(key=lambda x: x[1])

        initial_usdt_snapshot = usdt  # buy % based on current balance at start of buy phase

        for name, dm in buy_candidates:
            if usdt < MIN_USDT:
                break
            price = prices.get(name)
            if price is None or price <= 0:
                continue
            pct    = buy_pct(dm)
            amount = round(initial_usdt_snapshot * pct, 2)
            amount = min(amount, usdt)
            if amount < MIN_AMOUNT:
                continue
            qty = amount / price
            holdings[name] = holdings.get(name, 0.0) + qty
            usdt -= amount
            trade_log.append({
                "date": str(day), "action": "BUY", "asset": name,
                "dm": round(dm, 4), "qty": round(qty, 6),
                "price": round(price, 4), "usdt": round(amount, 4),
            })

        # ── Equity snapshot ──
        portfolio_value = usdt
        for name, qty in holdings.items():
            price = prices.get(name, 0.0)
            portfolio_value += qty * price

        equity_curve.append({
            "date": str(day),
            "usdt_cash": round(usdt, 4),
            "portfolio_value_usdt": round(portfolio_value, 4),
            "portfolio_value_pln": round(portfolio_value * usdpln_rate, 2),
        })

    # ── Final portfolio value ──
    final_usdpln = float(usdpln[usdpln.index <= pd.Timestamp(end_date)].iloc[-1]) if not usdpln.empty else usdpln_rate
    final_value_usdt = equity_curve[-1]["portfolio_value_usdt"] if equity_curve else 0.0
    final_value_pln  = round(final_value_usdt * final_usdpln, 2)
    roi_pct = round((final_value_pln / INITIAL_PLN - 1) * 100, 2)

    print(f"  Final USDT value: {final_value_usdt:.2f}  ({final_value_pln:.2f} PLN)  ROI: {roi_pct:+.1f}%")
    print(f"  Trades executed:  {len(trade_log)}")

    # Save
    pd.DataFrame(equity_curve).to_csv(
        os.path.join(RESULTS_DIR, f"equity_{label}.csv"), index=False)
    pd.DataFrame(trade_log).to_csv(
        os.path.join(RESULTS_DIR, f"trades_{label}.csv"), index=False)

    return {
        "label": label,
        "start_date": str(start_date),
        "initial_pln": INITIAL_PLN,
        "initial_usdt": initial_usdt,
        "usdpln_at_start": round(usdpln_rate, 4),
        "usdpln_at_end": round(final_usdpln, 4),
        "final_value_usdt": final_value_usdt,
        "final_value_pln": final_value_pln,
        "roi_pct": roi_pct,
        "trades_total": len(trade_log),
    }


# ── Main ──────────────────────────────────────────────────────────────────────

def main():
    print("Loading OHLC data...")
    ohlc_data = {}
    for name, file_key in ASSETS:
        df = load_ohlc(file_key)
        if df.empty:
            print(f"  WARNING: no data for {name} ({file_key})")
        else:
            print(f"  {name:15s} {len(df):5d} bars  {df.index[0].date()} → {df.index[-1].date()}")
        ohlc_data[name] = df

    usdpln = load_usdpln()
    print(f"USDPLN: {len(usdpln)} bars  {usdpln.index[0].date()} → {usdpln.index[-1].date()}")

    today = date.today()
    scenarios = [
        (today - timedelta(days=365 * 1), "1yr"),
        (today - timedelta(days=365 * 3), "3yr"),
        (today - timedelta(days=365 * 5), "5yr"),
        (today - timedelta(days=365 * 8), "8yr"),
    ]

    summary = []
    for start_date, label in scenarios:
        result = run_backtest(start_date, label, ohlc_data, usdpln)
        summary.append(result)

    # Print summary table
    print("\n" + "=" * 65)
    print(f"{'Scenario':<8}  {'Start':>12}  {'Initial':>10}  {'Final PLN':>10}  {'ROI':>8}")
    print("-" * 65)
    for r in summary:
        print(f"{r['label']:<8}  {r['start_date']:>12}  "
              f"{r['initial_pln']:>8.0f} PLN  {r['final_value_pln']:>10.2f}  {r['roi_pct']:>+7.1f}%")
    print("=" * 65)

    with open(os.path.join(RESULTS_DIR, "summary.json"), "w") as f:
        json.dump(summary, f, indent=2)
    print("\nResults written to backtest/results/")


if __name__ == "__main__":
    main()
