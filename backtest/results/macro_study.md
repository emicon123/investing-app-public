# Deeper macro ↔ crypto study

## 1. Headline YoY Spearman with block-bootstrap 95% CI (block=12m, B=3000)

Asset YoY return vs driver. CI excluding 0 ⇒ co-movement survives autocorrelation.

| driver | BTC ρ [95% CI] | ETH ρ [95% CI] | SOL ρ [95% CI] |
|---|---|---|---|
| Global M2 5-ctry YoY% | +0.55 [+0.21,+0.78] ✓ (n=178) | +0.58 [+0.14,+0.81] ✓ (n=117) | +0.48 [-0.32,+0.86] (n=61) |
| Global M2 G3 YoY% | +0.43 [+0.03,+0.73] ✓ (n=178) | +0.54 [+0.16,+0.78] ✓ (n=117) | +0.45 [-0.32,+0.84] (n=61) |
| Fed net liq YoY% | +0.56 [+0.27,+0.78] ✓ (n=180) | +0.62 [+0.23,+0.85] ✓ (n=119) | +0.78 [+0.39,+0.88] ✓ (n=63) |
| US M2 YoY% | +0.35 [+0.06,+0.57] ✓ (n=178) | +0.47 [+0.08,+0.67] ✓ (n=117) | +0.51 [-0.24,+0.89] (n=61) |
| ISM level | +0.30 [+0.01,+0.54] ✓ (n=179) | +0.48 [-0.02,+0.73] (n=118) | +0.43 [-0.17,+0.83] (n=62) |

✓ = 95% CI excludes 0.

## 2. Elasticity: asset YoY return = a + b · driver YoY  (Newey-West HAC t, L=12)

b = % of asset YoY move per +1 unit of driver (pp for YoY drivers, index pt for ISM).

| driver | BTC b (t) R² | ETH b (t) R² | SOL b (t) R² |
|---|---|---|---|
| Global M2 5-ctry YoY% | +91.7 (t=+1.2) R²=0.05 | +81.7 (t=+1.6) R²=0.09 | +252.3 (t=+2.2) R²=0.20 |
| Global M2 G3 YoY% | +51.2 (t=+0.9) R²=0.02 | +58.8 (t=+1.5) R²=0.06 | +225.0 (t=+2.1) R²=0.18 |
| Fed net liq YoY% | +30.5 (t=+1.4) R²=0.06 | +6.0 (t=+0.8) R²=0.01 | +181.0 (t=+5.2) R²=0.52 |
| US M2 YoY% | +23.7 (t=+0.8) R²=0.00 | +10.1 (t=+0.6) R²=0.00 | +320.0 (t=+3.4) R²=0.41 |
| ISM level | +11.5 (t=+0.4) R²=0.00 | +111.2 (t=+2.0) R²=0.15 | +396.6 (t=+4.7) R²=0.47 |

## 3. No-lookahead forward returns by liquidity regime

Signal known at month start (lagged for publication). Forward = asset return over the *next* 1 and 3 months. Reported: mean fwd-1m % (hit-rate %).


**Net liq YoY > 0 (expand) vs < 0**

| asset | ON: mean1m (hit) n | OFF: mean1m (hit) n | ON−OFF 1m |
|---|--:|--:|--:|
| BTC | +20.8 (59%) 111 | +3.3 (52%) 80 | +17.5 |
| ETH | +15.5 (64%) 55 | +8.9 (44%) 75 | +6.6 |
| SOL | +30.7 (62%) 39 | +0.5 (37%) 35 | +30.2 |

**Global M2 YoY > median vs <**

| asset | ON: mean1m (hit) n | OFF: mean1m (hit) n | ON−OFF 1m |
|---|--:|--:|--:|
| BTC | +17.1 (52%) 95 | +10.0 (61%) 96 | +7.1 |
| ETH | +6.2 (46%) 65 | +17.1 (58%) 65 | -10.9 |
| SOL | +22.5 (49%) 37 | +10.4 (51%) 37 | +12.1 |

**Global M2 YoY rising vs falling**

| asset | ON: mean1m (hit) n | OFF: mean1m (hit) n | ON−OFF 1m |
|---|--:|--:|--:|
| BTC | +21.2 (61%) 95 | +5.9 (52%) 96 | +15.3 |
| ETH | +18.0 (65%) 69 | +4.6 (38%) 61 | +13.4 |
| SOL | +25.3 (51%) 39 | +6.6 (49%) 35 | +18.6 |

**ISM > 50 vs < 50**

| asset | ON: mean1m (hit) n | OFF: mean1m (hit) n | ON−OFF 1m |
|---|--:|--:|--:|
| BTC | +16.5 (55%) 138 | +5.7 (60%) 53 | +10.8 |
| ETH | +11.6 (52%) 82 | +11.8 (52%) 48 | -0.1 |
| SOL | +20.7 (47%) 36 | +12.4 (53%) 38 | +8.3 |

## 4. Liquidity-timing backtest vs buy & hold (no-lookahead, monthly)

In-market when signal ON (else cash, 0%). Net-liq rule covers full history; Global-M2 rule ends with the 5-country series (2023-11).


**BTC**  (buy&hold: CAGR 137% | vol 178% | Sharpe 0.91 | maxDD -82% | n=191)

| rule | CAGR% | vol% | Sharpe | maxDD% | time-in% | n |
|---|--:|--:|--:|--:|--:|--:|
| NetLiqYoY>0 | 119 | 175 | 0.84 | -82 | 58 | 191 |
| NetLiq>12mMA | 111 | 175 | 0.81 | -82 | 58 | 191 |
| GlobalM2YoY rising | 89 | 122 | 0.84 | -68 | 50 | 190 |

**ETH**  (buy&hold: CAGR 94% | vol 146% | Sharpe 0.96 | maxDD -90% | n=130)

| rule | CAGR% | vol% | Sharpe | maxDD% | time-in% | n |
|---|--:|--:|--:|--:|--:|--:|
| NetLiqYoY>0 | 70 | 92 | 0.92 | -64 | 42 | 130 |
| NetLiq>12mMA | 85 | 115 | 0.91 | -61 | 46 | 130 |
| GlobalM2YoY rising | 114 | 123 | 1.02 | -64 | 53 | 129 |

**SOL**  (buy&hold: CAGR 113% | vol 197% | Sharpe 1.00 | maxDD -95% | n=74)

| rule | CAGR% | vol% | Sharpe | maxDD% | time-in% | n |
|---|--:|--:|--:|--:|--:|--:|
| NetLiqYoY>0 | 269 | 178 | 1.27 | -68 | 51 | 74 |
| NetLiq>12mMA | 151 | 177 | 1.03 | -68 | 51 | 74 |
| GlobalM2YoY rising | 174 | 168 | 1.09 | -68 | 53 | 73 |

## 5. Robustness

### 5a. Ex-COVID (drop 2020-03..2021-12) — YoY Spearman vs full

| driver | BTC full→exCOVID | ETH full→exCOVID | SOL full→exCOVID |
|---|---|---|---|
| Global M2 5-ctry YoY% | +0.55 → +0.60 | +0.58 → +0.52 | +0.48 → +0.24 |
| Fed net liq YoY% | +0.56 → +0.62 | +0.62 → +0.67 | +0.78 → +0.65 |
| US M2 YoY% | +0.35 → +0.37 | +0.47 → +0.36 | +0.51 → +0.22 |

### 5b. Liquidity-beta: crypto vs SPY / GLD (asset YoY ~ Global M2 5c YoY, HAC)

| asset | b (t) | R² | n |
|---|--:|--:|--:|
| BTC | +91.7 (t=+1.2) | 0.05 | 178 |
| ETH | +81.7 (t=+1.6) | 0.09 | 117 |
| SOL | +252.3 (t=+2.2) | 0.20 | 61 |
| SPY | +1.4 (t=+2.5) | 0.29 | 95 |
| GLD | +1.5 (t=+3.0) | 0.14 | 208 |

