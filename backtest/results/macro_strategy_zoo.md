# Macro-strategy zoo — broad, no-lookahead test of macro timing rules

Hold asset when rule ON else T-bill cash. Monthly, signal lagged for publication, 15bps/switch. Judge by BREADTH (how many assets a rule helps), not one asset.

## Part 1 — breadth across 28 assets (BTC, ETH, SOL, BNB, XRP, ADA, DOGE, LINK, MSTR, COIN, MARA, NVDA, TSLA, AAPL, MSFT, AMZN, GOOGL, SPY, QQQ, IWM, EEM, XLF, XLE, GLD, SLV, TLT, LQD, UUP)

Δ = strategy − buy&hold. Positive median Δ Sharpe + high %beat ⇒ broadly useful.

| rule | median ΔSharpe | %beat B&H | median ΔCAGR% | median ΔmaxDD% | median time-in% |
|---|--:|--:|--:|--:|--:|
| GlobalM2 YoY accel>0 | -0.03 | 13/28 | -3 | +19 | 45 |
| GlobalM2 YoY>0 | +0.03 | 18/28 | +1 | +4 | 81 |
| NetLiq YoY>0 | +0.01 | 16/28 | -1 | +19 | 54 |
| NetLiq rising(3m) | -0.13 | 7/28 | -4 | +10 | 53 |
| NetLiq accel>0 | +0.10 | 20/28 | -1 | +25 | 44 |
| USM2 YoY accel>0 | -0.02 | 11/28 | -4 | +23 | 44 |
| ISM>50 | -0.20 | 2/28 | -11 | +0 | 64 |
| ISM rising | -0.12 | 4/28 | -5 | +23 | 44 |
| ISM<50 & rising (early) | -0.34 | 2/28 | -10 | +42 | 11 |
| Gold YoY>0 (risk proxy) | -0.06 | 7/28 | -3 | +12 | 62 |
| Combo M2accel & NetLiqYoY | -0.13 | 4/28 | -5 | +25 | 28 |

## Part 2 — **NetLiq accel>0** per asset (ΔSharpe vs B&H)

| asset | ΔSharpe | asset | ΔSharpe | asset | ΔSharpe |
|---|--:|---|--:|---|--:|
| BTC | -0.06 | ETH | +0.14 | SOL | +0.37 |
| BNB | -0.07 | XRP | +0.05 | ADA | +0.26 |
| DOGE | +0.01 | LINK | +0.12 | MSTR | +0.24 |
| COIN | +0.87 | MARA | +0.30 | NVDA | +0.10 |
| TSLA | +0.29 | AAPL | -0.17 | MSFT | +0.20 |
| AMZN | -0.01 | GOOGL | +0.03 | SPY | +0.12 |
| QQQ | +0.20 | IWM | +0.16 | EEM | +0.11 |
| XLF | +0.21 | XLE | -0.04 | GLD | -0.22 |
| SLV | -0.18 | TLT | +0.05 | LQD | +0.05 |
| UUP | -0.36 |  |  |  |  |

## Part 2 — **GlobalM2 YoY accel>0** per asset (ΔSharpe vs B&H)

| asset | ΔSharpe | asset | ΔSharpe | asset | ΔSharpe |
|---|--:|---|--:|---|--:|
| BTC | -0.02 | ETH | -0.11 | SOL | -0.03 |
| BNB | -0.11 | XRP | +0.03 | ADA | +0.10 |
| DOGE | +0.01 | LINK | -0.07 | MSTR | +0.15 |
| COIN | +0.37 | MARA | +0.19 | NVDA | +0.20 |
| TSLA | -0.07 | AAPL | -0.22 | MSFT | +0.15 |
| AMZN | -0.03 | GOOGL | +0.06 | SPY | +0.05 |
| QQQ | +0.04 | IWM | +0.01 | EEM | -0.14 |
| XLF | +0.13 | XLE | -0.16 | GLD | -0.29 |
| SLV | -0.08 | TLT | -0.13 | LQD | -0.21 |
| UUP | -0.05 |  |  |  |  |

## Part 3 — walk-forward: pick GlobalM2 accel window {1,3,6}m on the past, trade next 6m OOS

| asset | OOS CAGR% | OOS Sharpe | OOS maxDD% | B&H CAGR% | B&H Sharpe | B&H maxDD% | OOS from |
|---|--:|--:|--:|--:|--:|--:|--|
| BTC | 49 | 0.99 | -68 | 64 | 1.02 | -76 | 2015-08 |
| ETH | 70 | 1.12 | -39 | 26 | 0.65 | -78 | 2020-09 |
| SOL | n/a (short OOS) |||||||

