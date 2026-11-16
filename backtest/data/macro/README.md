# Macro indicators (`data/macro/`)

Free / keyless sources only. Regenerate with `python3 backtest/fetch/fetch_macro_data.py`.
Asset price CSVs live in the sibling `data/prices/` folder.

| File | What | Source | Coverage | Units |
|---|---|---|---|---|
| `ISM_PMI.csv` | ISM Manufacturing PMI, **first-print** ("Actual") | investing.com release table (saved in `ism_pmi_raw.txt`) | 2008-01 → 2026-05, monthly | index |
| `FED_NET_LIQUIDITY.csv` | Fed net liquidity + components | FRED (keyless `fredgraph.csv`) | 2008-01 → present, weekly (Wed) | USD billions |
| `US_M2.csv` | US M2 money stock, seasonally adjusted | FRED `M2SL` | 2008-01 → present, monthly | USD billions |
| `GLOBAL_M2.csv` | Major-economy M2 summed in USD | FRED + ECB + IMF (see below) | 2008-01 → present, monthly | USD billions |

## ISM PMI
- **Period convention:** the value is dated to the **month it describes**, i.e. the
  release month minus one (the Feb-2008 print released 2008-03-03 is stored as `2008-02-01`).
- These are **original first releases** (no later revisions / seasonal re-adjustment),
  which is what you want for no-lookahead backtests. Forecast/Previous columns were dropped.
- `ism_pmi_raw.txt` is the provenance file; the parser lives in `fetch_macro_data.py`.
- Note FRED's old ISM series (`NAPM`) was discontinued (licensing); DBnomics' `ISM/pmi`
  is short and partly corrupted — hence the saved investing.com table.

## Fed net liquidity
`net_liquidity_bn = WALCL - WDTGAL - RRPONTSYD`
- `WALCL` — Fed total assets (balance sheet), weekly Wednesday level.
- `WDTGAL` — Treasury General Account at the Fed (drains liquidity), weekly Wednesday level.
- `RRPONTSYD` — overnight reverse repo (drains liquidity), daily; attached to each
  weekly date via backward as-of join. ~0 before the facility ramped (2013+).
Columns: `walcl_bn, tga_bn, rrp_bn, net_liquidity_bn`.

## US M2
`FRED M2SL`, seasonally adjusted, already in USD billions. (`WM2NS` is the weekly
not-seasonally-adjusted alternative if finer cadence is ever needed.)

## Global M2  ⚠️ read this
A transparent, reproducible reconstruction of "global M2" (the bgeometrics chart the
brief referenced is a **paid** product and is not scraped). It sums the major economies'
M2 / broad money, each converted to USD at the monthly-mean FX rate:

| Leg | Series | Native unit | FX | Coverage |
|---|---|---|---|---|
| US | FRED `M2SL` | USD bn | — | → current |
| Euro area | ECB Data Portal API `BSI/M.U2.Y.V.M20.X.1.U2.2300.Z01.E` | EUR mn | `DEXUSEU` | → current |
| China | PBOC level (TradingView, in `china_m2_raw.txt`) spliced onto OECD MEI `CHN.MABMM301.ST.M` pre-2015 | CNY bn | `DEXCHUS` | → current |
| Japan | True BoJ M2: levels 2008-01..2017-02 (`japan_m2_raw.txt`) + YoY-chain (`japan_m2_yoy_raw.txt`) | JPY | `DEXJPUS` | → current (~2026-05) |
| UK | Bank of England M4 `LPMAUYM` (IADB) | GBP mn | `DEXUSUK` | → current |

Notes:
- **China** uses the pasted PBOC level (`china_m2_raw.txt`, 亿 CNY → /10 = CNY bn), which
  matches OECD MEI to the decimal in overlap (¥193,549 bn @2019-08) and runs to the latest
  month; OECD MEI backfills pre-2015. Live trackers (bgeometrics/CEIC/TradingView/YCharts)
  are paid/gated, so the level is hand-pasted and version-controlled here.
- **Euro** switched to the ECB API and **UK** to the Bank of England API — both keyless and
  current (DBnomics' mirrors lagged ~1yr; FRED's UK M3 ended 2023-11).
- **Japan** uses **true BoJ M2** (yen): published levels 2008-01..2017-02, then the level is
  carried forward by chaining the published **M2 YoY %** (`level_t = level_{t-12}·(1+YoY)`),
  current to ~2026-05 — so **no trend extrapolation** is needed (`japan_extrapolated` = 0
  throughout; the column/`extrapolate()` safety net only triggers if Japan ever falls short
  of the other legs). This replaced IMF IFS "broad money", which was both stale (~2025-02)
  and ~30% wider than strict M2; true M2 (¥1,300T vs ¥1,634T) is the more M2-consistent leg.
  Self-validates: the level series' implied YoY matches the published YoY at the 2017 overlap.

Columns:
- `us_m2_bn, euro_area_m2_bn, china_m2_bn, japan_m2_bn, uk_m2_bn` — per-leg, USD bn.
- `japan_extrapolated` — 1 where the Japan leg (hence the totals) is extrapolated.
- `global_m2_bn` — **5-country** sum → complete **2008-01 → ~current** (Japan tail estimated).
- `global_m2_g3_bn` — US + Euro + Japan (kept for backward-compat with the analysis scripts).

For correlation vs assets, prefer **YoY / MoM % change** over raw levels (sidesteps FX/scale
and the staggered ends). The China paste covers 2015→current; OECD/FRED cover 2008–2019 (a
user also supplied the 2008–2019 CNY series, which matches exactly — kept as the OECD backfill).
