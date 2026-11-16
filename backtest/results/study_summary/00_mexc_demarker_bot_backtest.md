# 00 — MEXC DeMarker bot: historical replay

**Date:** 2026-06-18. **Not part of the Macro ↔ Crypto thread** — this validates a
different, unrelated feature: the live MEXC trading bot (`MexcBotService.java` +
`DeMarkerService.java`, `backend/src/main/java/pl/investing/service/`), not the MAIN
portfolio's DCA/rebalancing strategy.

Script: `backtest.py` → `results/summary.json`, `results/equity_{1,3,5,8}yr.csv`,
`results/trades_{1,3,5,8}yr.csv`.

## What it replicates
Exactly the production bot logic:
- 14-period DeMarker computed from 15 daily OHLC bars.
- **Sell** when DeM > 0.75, in tiers (100%/50%/25%/10% of the position by threshold).
- **Buy** when DeM < 0.25, in tiers (50%/25%/12.5%/7%/3%/1% of *initial* USDT).
- Runs once per weekday (Mon–Fri).
- Starting capital: 1000 PLN converted to USDT at the historical USDPLN rate.

## Results (no macro/portfolio comparison — a standalone historical replay)

| window | start | ROI | final value | trades |
|---|---|--:|--:|--:|
| 1yr | 2025-06-13 | **−22.0%** | 780 PLN | 414 |
| 3yr | 2023-06-14 | **+37.9%** | 1,379 PLN | 1,515 |
| 5yr | 2021-06-14 | **+92.0%** | 1,920 PLN | 2,918 |
| 8yr | 2018-06-15 | **+337.3%** | 4,372 PLN | 5,580 |

## Reading this
This is a **reference/validation run**, not a rejected-or-accepted strategy call —
no alternative scheme was tested against it, and no recommendation was recorded. Trade
counts are high (daily signal checks over years), and the most recent 1yr window was
negative, which is worth revisiting if the bot's live performance is ever audited
again. Treat the longer-window ROIs as survivorship-flavored too: a single historical
path, not a distribution.
