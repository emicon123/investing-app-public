# 05 — Liquidity-beta ranking of the holdings universe

Script: `liquidity_beta_ranking.py`  Raw: `results/liquidity_beta_ranking.md` (+ `.csv`)

## What was tested
Every per-ticker price series in `data/prices/` regressed (HAC / Newey-West) on a liquidity
driver's YoY growth: **asset YoY return = a + β·(driver YoY)**, for Fed net liquidity and
Global M2. β = liquidity sensitivity; higher = more liquidity-levered. Min 24 months.

## Outcome — clean monotonic gradient (mean net-liq β by category)
| category | mean β | read |
|---|--:|---|
| Crypto | **≈ 24** | most liquidity-levered |
| Crypto-equity (miners, MSTR, COIN) | ≈ 6 | |
| Equity | ≈ 1 | |
| FX | ≈ 0.2 | defensive |
| Metal (gold/silver) | ≈ 0.1 | defensive |

Notable single names: SOL/TAO/DOGE top the list; **TSLA** is the most liquidity-levered
*equity* (β≈8, R²≈0.69); **MSTR** is idiosyncratic (net-liq β≈0 but Global-M2 β≈10);
gold/silver/forex ≈ 0. Global-M2 explains *equities* better than net-liq does (TSM R²≈0.80,
SPY 0.48).

## Caveat
β conflates **volatility** with **sensitivity** (high-vol assets get big β by construction) —
read **R²** and the **category aggregates** as the robust signal, not individual crypto β
point estimates.

## Use
A liquidity-tilt map for the portfolio: lean into high-β (alts, miners, TSLA) when liquidity
is expanding / Global-M2 accelerating; rotate toward metals/FX/defensive equities when
contracting. Pairs with the regime signals in files 03–04.
