#!/usr/bin/env python3
"""
How well do Virtune's GPW-listed crypto ETNs (PLN) track their underlying
coin (USD)? All four launched 2026-02-25, so this is the ETN's *entire* trading
life (~6 months) -- a descriptive tracking-quality check, not a walk-forward
strategy backtest (no multiple starting windows are possible: there is only
one history to look at, and it will grow over time).

For each ETN:
  1. Convert the PLN close to a USD-equivalent (close_pln / USDPLN close),
     removing the FX leg so what's left isolates ETN-vs-spot tracking quality.
  2. Align to the underlying coin's USD close on the same calendar date
     (inner join on BiznesRadar's GPW trading days).
  3. Compute: correlation of daily returns, tracking-error stdev (annualised),
     total-return gap over the common window vs. the coin's own total return,
     and compare that gap to the naive fee-only expectation from each ETN's
     published TER (+ staking pass-through for ETH/SOL where relevant).

Output: results/virtune_etn_tracking_check.md
"""

import os

import numpy as np
import pandas as pd

PRICES = os.path.join(os.path.join(os.path.dirname(__file__), ".."), "data", "prices")
RESULTS = os.path.join(os.path.join(os.path.dirname(__file__), ".."), "results")

# (etn_ticker, coin_yahoo_csv, coin_label, annual_TER, annual_staking_passthrough)
# TER / staking figures per Virtune fact sheets & press coverage (analizy.pl,
# strefainwestorow.pl, atlasetf.pl), cross-checked 2026-08-21:
#   BTCP: 0.25% TER, no staking
#   ETH : 1.40% TER, includes staking (~1.12%/yr net passthrough per atlasetf)
#   SOL : 0.95% TER, ~3.0%/yr staking passthrough
#   XRP : 1.49% TER, no staking
ETNS = [
    ("ETNVIRBTCP", "BTC_USD.csv", "BTC", 0.0025, 0.0),
    ("ETNVIRETH", "ETH_USD.csv", "ETH", 0.0140, 0.0112),
    ("ETNVIRSOL", "SOL_USD.csv", "SOL", 0.0095, 0.0300),
    ("ETNVIRXRP", "XRP_USD.csv", "XRP", 0.0149, 0.0),
]


def load_etn(ticker: str) -> pd.Series:
    df = pd.read_csv(os.path.join(PRICES, f"{ticker}.csv"))
    df["date"] = pd.to_datetime(df["date"], format="%d.%m.%Y")
    df = df.set_index("date").sort_index()
    return df["close"].astype(float)


def load_yahoo_close(fname: str) -> pd.Series:
    df = pd.read_csv(os.path.join(PRICES, fname), skiprows=[1, 2])
    df = df.rename(columns={df.columns[0]: "date"})
    df["date"] = pd.to_datetime(df["date"], errors="coerce")
    df = df.dropna(subset=["date"]).set_index("date").sort_index()
    return df["Close"].astype(float)


def _df_to_markdown(df: pd.DataFrame) -> str:
    cols = list(df.columns)
    lines = ["| " + " | ".join(cols) + " |", "|" + "|".join(["---"] * len(cols)) + "|"]
    for _, row in df.iterrows():
        lines.append("| " + " | ".join(str(row[c]) for c in cols) + " |")
    return "\n".join(lines)


def main():
    usdpln = load_yahoo_close("USDPLN_X.csv")
    rows = []
    detail_sections = []

    for ticker, coin_file, label, ter, staking in ETNS:
        etn_pln = load_etn(ticker)
        coin_usd = load_yahoo_close(coin_file)

        df = pd.DataFrame({"etn_pln": etn_pln}).join(usdpln.rename("usdpln"), how="left")
        df["usdpln"] = df["usdpln"].ffill()  # GPW trades weekdays; carry last FX fix over gaps
        df["etn_usd_equiv"] = df["etn_pln"] / df["usdpln"]
        df = df.join(coin_usd.rename("coin_usd"), how="inner").dropna()

        n_days = len(df)
        start, end = df.index[0], df.index[-1]
        yrs = (end - start).days / 365.25

        etn_ret = df["etn_usd_equiv"].iloc[-1] / df["etn_usd_equiv"].iloc[0] - 1
        coin_ret = df["coin_usd"].iloc[-1] / df["coin_usd"].iloc[0] - 1
        tracking_gap = etn_ret - coin_ret  # ETN total return minus coin's own total return, USD terms

        d_etn = df["etn_usd_equiv"].pct_change()
        d_coin = df["coin_usd"].pct_change()
        corr = d_etn.corr(d_coin)
        tracking_err_daily = (d_etn - d_coin).std()
        tracking_err_ann = tracking_err_daily * np.sqrt(252)

        # Lead/lag check: confirms the correlation isn't depressed by a GPW-close vs.
        # Yahoo-crypto-close session-timing mismatch (peak should sit at lag 0).
        lag_corrs = {lag: d_etn.corr(d_coin.shift(lag)) for lag in (-2, -1, 0, 1, 2)}
        peak_lag = max(lag_corrs, key=lag_corrs.get)

        expected_annual_drag = staking - ter  # positive = ETN should OUTperform spot
        expected_gap_over_window = (1 + expected_annual_drag) ** yrs - 1

        # ETN in its own currency (PLN) vs coin translated to PLN, for a "what a PL
        # investor actually experienced" total-return check (FX leg included both sides).
        coin_pln_ret = (df["coin_usd"] * df["usdpln"]).iloc[-1] / (df["coin_usd"] * df["usdpln"]).iloc[0] - 1
        etn_pln_ret = df["etn_pln"].iloc[-1] / df["etn_pln"].iloc[0] - 1

        rows.append({
            "ETN": ticker,
            "Coin": label,
            "N days": n_days,
            "Window": f"{start.date()} -> {end.date()} ({yrs:.2f}y)",
            "Corr(daily ret)": round(corr, 4),
            "TrackErr ann.": f"{tracking_err_ann * 100:.2f}%",
            "ETN ret (USD-equiv)": f"{etn_ret * 100:+.2f}%",
            "Coin ret (USD)": f"{coin_ret * 100:+.2f}%",
            "Tracking gap (USD terms)": f"{tracking_gap * 100:+.2f}%",
            "Expected gap (TER-staking, prorated)": f"{expected_gap_over_window * 100:+.2f}%",
            "ETN ret (PLN, as-traded)": f"{etn_pln_ret * 100:+.2f}%",
            "Coin ret (PLN-equiv)": f"{coin_pln_ret * 100:+.2f}%",
        })

        detail_sections.append(
            f"### {ticker} ({label})\n\n"
            f"- Window: {start.date()} -> {end.date()} ({n_days} GPW trading days, {yrs:.2f}y) "
            f"-- this is the ETN's entire trading life (listed 2026-02-25)\n"
            f"- Daily-return correlation to spot {label}: **{corr:.4f}** "
            f"(peak at lag {peak_lag:+d}d -- {', '.join(f'{k:+d}d={v:.2f}' for k, v in lag_corrs.items())} "
            f"-- {'confirms lag-0 alignment, not a session-timing artifact' if peak_lag == 0 else 'NOTE: peak is off lag 0, check date alignment'})\n"
            f"- Annualised tracking error (stdev of daily return diff x sqrt(252)): **{tracking_err_ann*100:.2f}%**\n"
            f"- USD-equivalent total return: ETN {etn_ret*100:+.2f}% vs. spot {label} {coin_ret*100:+.2f}% "
            f"-> tracking gap **{tracking_gap*100:+.2f}%**\n"
            f"- Published cost structure: TER {ter*100:.2f}%/yr"
            + (f", staking passthrough ~{staking*100:.2f}%/yr" if staking else ", no staking")
            + f" -> naive expected gap over {yrs:.2f}y at this rate: {expected_gap_over_window*100:+.2f}%\n"
            f"- As actually experienced by a PLN investor (both legs in PLN, FX included): "
            f"ETN {etn_pln_ret*100:+.2f}% vs. coin-in-PLN {coin_pln_ret*100:+.2f}%\n"
        )

    summary_df = pd.DataFrame(rows)

    os.makedirs(RESULTS, exist_ok=True)
    csv_path = os.path.join(RESULTS, "virtune_etn_tracking_check.csv")
    summary_df.to_csv(csv_path, index=False)

    md_path = os.path.join(RESULTS, "virtune_etn_tracking_check.md")
    with open(md_path, "w") as f:
        f.write("# Virtune GPW crypto-ETN tracking-quality check\n\n")
        f.write(
            "Descriptive check, not a walk-forward backtest -- all four Virtune ETNs "
            "(ETNVIRBTCP/ETNVIRETH/ETNVIRSOL/ETNVIRXRP) launched together on GPW "
            "2026-02-25, so there is only one ~6-month window to look at (it will grow "
            "over time; no multiple starting dates are possible yet). Source: BiznesRadar "
            "historical quotes (PLN), cross-checked against BTC/ETH/SOL/XRP USD spot "
            "(Yahoo Finance) and USDPLN (Yahoo Finance).\n\n"
        )
        f.write("## Summary\n\n")
        f.write(_df_to_markdown(summary_df))
        f.write("\n\n## Detail\n\n")
        f.write("\n".join(detail_sections))

    print(summary_df.to_string(index=False))
    print(f"\nWritten: {md_path}\n         {csv_path}")


if __name__ == "__main__":
    main()
