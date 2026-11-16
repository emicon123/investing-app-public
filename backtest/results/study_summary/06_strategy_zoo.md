# 06 — Macro-strategy zoo (broad, no-lookahead)

Script: `macro_strategy_zoo.py`  Raw: `results/macro_strategy_zoo.md`
Prereq for the long traditional history: `fetch_long_history.py` (2007→now `*_full.csv`).

## What was tested
11 **fixed, a-priori** macro risk-on/off rules (no fitted parameters → inherently
no-lookahead once each macro series is lagged for publication) × **28 assets** spanning
crypto majors, crypto-equities (MSTR/COIN/MARA), mega-cap/high-beta equities, indices/
factors/sectors/EM, metals, and **bonds/credit/dollar as risk-OFF controls**. Rule = hold
asset when signal ON else T-bill cash; monthly; 15 bps/switch. Judged by **breadth**
(how many of 28 assets a rule helps) — the antidote to single-asset overfitting.

## Part 1 — breadth (median Δ vs buy&hold; %beat = # assets where timed Sharpe > B&H)
| rule | median ΔSharpe | %beat | median ΔmaxDD% |
|---|--:|--:|--:|
| **NetLiq accel>0** (net-liq YoY rising) | **+0.10** | **20/28** | +25 |
| GlobalM2 YoY>0 | +0.03 | 18/28 | +4 |
| NetLiq YoY>0 | +0.01 | 16/28 | +19 |
| GlobalM2 YoY accel>0 | −0.03 | 13/28 | +19 |
| USM2 YoY accel>0 | −0.02 | 11/28 | +23 |
| NetLiq rising(3m) | −0.13 | 7/28 | +10 |
| Gold YoY>0 (risk proxy) | −0.06 | 7/28 | +12 |
| Combo M2accel & NetLiqYoY | −0.13 | 4/28 | +25 |
| ISM rising | −0.12 | 4/28 | +23 |
| ISM>50 | −0.20 | 2/28 | +0 |
| ISM<50 & rising (early cycle) | −0.34 | 2/28 | +42 |

## Outcomes
- **Winner = "net-liquidity YoY accelerating" (NetLiq accel>0).** Broadly useful — beats
  buy&hold in **20/28** assets, +0.10 median Sharpe, and a large +25 median drawdown
  reduction. It's a genuine **risk-on** signal and it **validates against the controls**:
  helps risk assets (COIN +0.87, SOL +0.37, MARA +0.30, TSLA +0.29, ADA +0.26, MSTR +0.24,
  XLF +0.21, QQQ/MSFT +0.20, SPY/LINK/EEM/IWM +0.1–0.16) yet **hurts/ignores defensives**
  (UUP dollar −0.36, GLD −0.22, SLV −0.18; TLT/LQD ≈ +0.05). Exactly the pattern a real
  risk-on rule should show.
- **Your Global-M2 acceleration idea is real but weak as a *binary* rule** (13/28, −0.03
  median). The continuous forward correlation (file 03) holds, but a crude >0 threshold +
  being in cash half the time can't beat buy&hold for most crypto. It helps **liquidity-
  sensitive equities** (COIN +0.37, NVDA +0.20, MARA/MSTR +0.15) more than crypto.
- **ISM rules are actively harmful** (ISM>50: 2/28; early-cycle: 2/28) — being out of risk
  assets when ISM is weak misses the easing-driven rallies. Confirms ISM is the wrong tool.
- **BTC is the hardest to time** — even the winning rule is ≈flat on BTC (−0.06); its buy&hold
  Sharpe is too high. ETH/SOL/alts/equities are where liquidity timing pays.

## Part 3 — walk-forward (Global-M2 accel window picked OOS, BTC/ETH/SOL)
| asset | OOS Sharpe | B&H | OOS maxDD vs B&H |
|---|--:|--:|--|
| ETH | **1.12** | 0.65 | −39% vs −78% |
| BTC | 0.99 | 1.02 | −68% vs −76% (de-risk only) |
| SOL | n/a (short OOS) | | |

## Verdict
Trying many rules broadly **upgraded the signal**: the durable, generalisable macro-timing
edge is **net-liquidity *acceleration* → risk-on**, working across the risk complex (crypto
*and* equities) and correctly avoiding bonds/gold/dollar. Global-M2 acceleration is a
weaker, equity-tilted cousin; ISM is counterproductive; BTC stays un-timeable (hold it).
Caveat: T-bill cash leg = 0% when FRED unreachable (conservative); monthly binary rules,
no vol-targeting.
