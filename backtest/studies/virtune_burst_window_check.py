#!/usr/bin/env python3
"""
Session-aligned tracking check for the Virtune GPW crypto ETNs over the most
recent few days -- built for checking tracking quality during a fast crypto
move ("burst"), where the daily-close comparison in
virtune_etn_tracking_check.py is too coarse: Yahoo's crypto daily bar closes
~00:00 UTC while GPW's session closes ~17:00 Warsaw time (~15:00 UTC in
summer) -- a ~9h lag that matters when the underlying is moving fast
intraday, and would otherwise smear a same-day move across two "daily" bars.

Combines:
  - ETN PLN closes from data/prices/<ETN>.csv (BiznesRadar, run
    fetch_virtune_etn_data.py first to refresh)
  - crypto spot + USDPLN at the matching ~17:00 Warsaw timestamp each day
    (hourly yfinance bars, 7d lookback)
  - the live BiznesRadar quote + live spot/USDPLN "now", if the GPW session
    is currently open, as an extra same-timestamp data point

Reports day-over-day and cumulative tracking gap (ETN USD-equivalent total
return minus spot coin's own USD total return) from a chosen baseline day.

Usage: python3 virtune_burst_window_check.py [N_DAYS]
  N_DAYS: how many trailing GPW trading days to include (default 4)
"""

import os
import re
import sys
import urllib.request

import pandas as pd
import yfinance as yf

UA = {"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 "
                     "(KHTML, like Gecko) Chrome/125.0 Safari/537.36"}
PRICES = os.path.join(os.path.join(os.path.dirname(__file__), ".."), "data", "prices")
RESULTS = os.path.join(os.path.join(os.path.dirname(__file__), ".."), "results")

ETNS = [("ETNVIRBTCP", "BTC-USD", "BTC"), ("ETNVIRETH", "ETH-USD", "ETH"),
        ("ETNVIRSOL", "SOL-USD", "SOL"), ("ETNVIRXRP", "XRP-USD", "XRP")]


def fetch_live_quote(ticker: str):
    """Current BiznesRadar session price + as-of time, if the GPW session is live."""
    req = urllib.request.Request(f"https://www.biznesradar.pl/notowania/{ticker}", headers=UA)
    with urllib.request.urlopen(req, timeout=20) as r:
        html = r.read().decode("utf-8", errors="replace")
    m_close = re.search(r'id="pr_t_close"><span[^>]*>([^<]+)</span>', html)
    m_time = re.search(r'id="pr_t_date"><time[^>]*>([^<]+)</time>', html)
    if not m_close:
        return None, None
    return float(m_close.group(1).replace(",", ".")), (m_time.group(1) if m_time else None)


def hourly_close_series(yf_ticker: str) -> pd.Series:
    df = yf.download(yf_ticker, period="7d", interval="1h", progress=False, auto_adjust=True)
    df.columns = [c[0] if isinstance(c, tuple) else c for c in df.columns]
    df.index = df.index.tz_convert("Europe/Warsaw")
    return df["Close"].sort_index()


def value_near_1700(series: pd.Series, day) -> float:
    window = series.between_time("16:00", "18:00")
    day_rows = window[window.index.date == day]
    if day_rows.empty:
        raise ValueError(f"No ~17:00 Warsaw bar for {day}")
    target = pd.Timestamp(day, tz="Europe/Warsaw").replace(hour=17)
    idx = (day_rows.index - target).map(abs).argmin()
    return float(day_rows.iloc[idx])


def load_etn_pln(ticker: str) -> pd.Series:
    df = pd.read_csv(os.path.join(PRICES, f"{ticker}.csv"))
    df["date"] = pd.to_datetime(df["date"], format="%d.%m.%Y")
    return df.set_index("date").sort_index()["close"].astype(float)


def main():
    n_days = int(sys.argv[1]) if len(sys.argv) > 1 else 4

    usdpln_hourly = hourly_close_series("USDPLN=X")
    gpw_days = sorted(set(usdpln_hourly.between_time("16:00", "18:00").index.date))[-n_days:]

    now_usdpln = float(usdpln_hourly.iloc[-1])
    now_ts = usdpln_hourly.index[-1]

    lines = [
        "# Virtune ETN tracking over the last few days (session-aligned)\n\n",
        f"Session-aligned to GPW's ~17:00 Warsaw close (not Yahoo's ~00:00 UTC crypto-bar "
        f"close), to avoid smearing a fast intraday move across the wrong \"day\". "
        f"Includes a live intraday snapshot as of {now_ts.strftime('%Y-%m-%d %H:%M')} Warsaw. "
        f"USD-equivalent = ETN PLN price / USDPLN at the matching timestamp (FX leg removed).\n\n",
    ]

    for etn, yf_ticker, label in ETNS:
        etn_pln_hist = load_etn_pln(etn)
        coin_hourly = hourly_close_series(yf_ticker)
        live_pln, live_time = fetch_live_quote(etn)

        timeline = []  # (label, etn_usd_equiv, spot_usd)
        for d in gpw_days:
            etn_pln = float(etn_pln_hist.loc[pd.Timestamp(d)])
            usdpln_d = value_near_1700(usdpln_hourly, d)
            spot_d = value_near_1700(coin_hourly, d)
            timeline.append((f"{d} 17:00", etn_pln / usdpln_d, spot_d))
        if live_pln is not None:
            timeline.append((f"now ({live_time} Warsaw)", live_pln / now_usdpln, float(coin_hourly.iloc[-1])))

        base_etn, base_spot = timeline[0][1], timeline[0][2]
        lines.append(f"## {etn} ({label})\n\n")
        lines.append("| Timestamp | ETN USD-equiv | day % | cum % | spot USD | day % | cum % | gap (pp, cum) |\n")
        lines.append("|---|---|---|---|---|---|---|---|\n")
        prev_etn = prev_spot = None
        for ts, etn_v, spot_v in timeline:
            cum_etn = etn_v / base_etn - 1
            cum_spot = spot_v / base_spot - 1
            day_etn = (etn_v / prev_etn - 1) if prev_etn else float("nan")
            day_spot = (spot_v / prev_spot - 1) if prev_spot else float("nan")
            gap = (cum_etn - cum_spot) * 100
            lines.append(
                f"| {ts} | {etn_v:,.4f} | {day_etn*100:+.2f}% | {cum_etn*100:+.2f}% | "
                f"{spot_v:,.4f} | {day_spot*100:+.2f}% | {cum_spot*100:+.2f}% | {gap:+.2f}pp |\n"
            )
            prev_etn, prev_spot = etn_v, spot_v
        lines.append("\n")

    os.makedirs(RESULTS, exist_ok=True)
    outpath = os.path.join(RESULTS, "virtune_burst_window_check.md")
    with open(outpath, "w") as f:
        f.writelines(lines)
    print("".join(lines))
    print(f"Written: {outpath}")


if __name__ == "__main__":
    main()
