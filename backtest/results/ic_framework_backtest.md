# Study 24 -- IC/ICIR/IC-decay framework (extends study 03's design; the literal cross-sectional-per-month reading of study 06 is mathematically undefined, see below)

## 0. Why a literal cross-sectional IC across the 28-asset universe doesn't apply here

Confirmed directly in `macro_strategy_zoo.py`'s `build_rules()`: every one of the 11 rules' signals is a SINGLE macro-level time series applied IDENTICALLY to all 28 assets each month (`signals[name] = sig`, same value for SPY, BTC, GLD, everything that month). Rank-correlating a column that is a CONSTANT across all 28 assets in a given month against 28 different realized returns has an undefined correlation coefficient (zero cross-sectional variance in the forecast) -- not a small/weak number, an undefined one. This is confirmed by direct spot-check below, not assumed:

- Spot check: `NetLiqAccel` continuous driver reading at 2013-07 is a single scalar (0.1119) -- identical for every one of the 28 assets that month by construction. A per-month cross-sectional correlation across the 28 assets against their 2013-07 forward returns would correlate a 28-element vector against a 28-element vector where one side is constant -- `pandas`/`scipy` both return `NaN` for this (verified: a constant-input Spearman correlation is undefined, not near-zero).

Two legitimate reframings are used below instead: **(a)** an asset-level TIME-SERIES IC per (rule, asset) pair -- extends study 03's already-valid design (BTC/ETH/SOL x net-liq/M2 drivers) to 9 continuous-driver rules x all 28 assets, with formal ICIR/t-stat/decay-curve packaging added on top -- the primary deliverable; **(b)** an optional, genuinely cross-sectional (single-point, not a time series) check reusing study 06's own per-asset Sharpe-uplift numbers against study 05's liquidity-beta ranking -- a bonus.

Two of the 11 rules (`ComboM2AccelNetLiqYoY`, `ISMLt50Rising`) are logical ANDs of two other rules' conditions with no natural single continuous driver reading -- excluded from part (a)'s table below (their component rules, `GlobalM2Accel`/`NetLiqYoY` and `ISMGt50`/`ISMRising`, ARE included).

## 1a. Study-03 spotlight -- BTC/ETH/SOL x the 4 liquidity drivers, IC-decay curve

Formalizes study 03's own headline finding (BTC contemporaneous, ETH/SOL genuine leads) as an actual IC-decay curve with ICIR/t-stat, using non-overlapping annual blocks (h=1 is genuinely non-overlapping within a block; h=3/6/12 blocks share the same overlapping-window caveat study 03 itself already flagged for its own long-horizon numbers).

| driver | asset | h (mo) | mean IC | std IC | n years | ICIR | t-stat |
|---|---|--:|--:|--:|--:|--:|--:|
| NetLiqAccel | BTC | 1 | -0.12 | 0.40 | 16 | -0.30 | -1.21 |
| NetLiqAccel | BTC | 3 | -0.11 | 0.44 | 16 | -0.25 | -1.00 |
| NetLiqAccel | BTC | 6 | -0.19 | 0.45 | 16 | -0.41 | -1.65 |
| NetLiqAccel | BTC | 12 | +0.02 | 0.38 | 16 | +0.06 | +0.24 |
| NetLiqAccel | ETH | 1 | -0.00 | 0.35 | 10 | -0.00 | -0.01 |
| NetLiqAccel | ETH | 3 | -0.03 | 0.47 | 10 | -0.06 | -0.18 |
| NetLiqAccel | ETH | 6 | -0.17 | 0.42 | 10 | -0.40 | -1.25 |
| NetLiqAccel | ETH | 12 | +0.19 | 0.32 | 10 | +0.59 | +1.88 |
| NetLiqAccel | SOL | 1 | +0.18 | 0.44 | 6 | +0.42 | +1.02 |
| NetLiqAccel | SOL | 3 | +0.01 | 0.45 | 6 | +0.02 | +0.05 |
| NetLiqAccel | SOL | 6 | -0.05 | 0.48 | 6 | -0.10 | -0.24 |
| NetLiqAccel | SOL | 12 | +0.18 | 0.33 | 6 | +0.55 | +1.35 |
| NetLiqYoY | BTC | 1 | -0.11 | 0.27 | 16 | -0.40 | -1.60 |
| NetLiqYoY | BTC | 3 | -0.15 | 0.33 | 16 | -0.45 | -1.80 |
| NetLiqYoY | BTC | 6 | -0.31 | 0.46 | 16 | -0.67 | -2.68 |
| NetLiqYoY | BTC | 12 | -0.30 | 0.54 | 16 | -0.56 | -2.23 |
| NetLiqYoY | ETH | 1 | -0.02 | 0.21 | 10 | -0.11 | -0.35 |
| NetLiqYoY | ETH | 3 | -0.02 | 0.32 | 10 | -0.07 | -0.21 |
| NetLiqYoY | ETH | 6 | -0.16 | 0.59 | 10 | -0.27 | -0.86 |
| NetLiqYoY | ETH | 12 | -0.23 | 0.60 | 10 | -0.39 | -1.23 |
| NetLiqYoY | SOL | 1 | +0.23 | 0.30 | 6 | +0.77 | +1.88 |
| NetLiqYoY | SOL | 3 | +0.15 | 0.41 | 6 | +0.35 | +0.87 |
| NetLiqYoY | SOL | 6 | -0.01 | 0.47 | 6 | -0.02 | -0.06 |
| NetLiqYoY | SOL | 12 | -0.11 | 0.50 | 6 | -0.23 | -0.56 |
| GlobalM2Accel | BTC | 1 | +0.07 | 0.39 | 16 | +0.18 | +0.73 |
| GlobalM2Accel | BTC | 3 | +0.01 | 0.38 | 16 | +0.03 | +0.14 |
| GlobalM2Accel | BTC | 6 | -0.09 | 0.51 | 16 | -0.17 | -0.68 |
| GlobalM2Accel | BTC | 12 | -0.23 | 0.59 | 16 | -0.39 | -1.57 |
| GlobalM2Accel | ETH | 1 | -0.06 | 0.28 | 10 | -0.22 | -0.68 |
| GlobalM2Accel | ETH | 3 | +0.02 | 0.22 | 10 | +0.10 | +0.33 |
| GlobalM2Accel | ETH | 6 | -0.17 | 0.54 | 10 | -0.32 | -1.02 |
| GlobalM2Accel | ETH | 12 | -0.13 | 0.67 | 10 | -0.20 | -0.63 |
| GlobalM2Accel | SOL | 1 | -0.04 | 0.28 | 6 | -0.13 | -0.32 |
| GlobalM2Accel | SOL | 3 | -0.02 | 0.23 | 6 | -0.10 | -0.26 |
| GlobalM2Accel | SOL | 6 | -0.06 | 0.49 | 6 | -0.11 | -0.28 |
| GlobalM2Accel | SOL | 12 | -0.06 | 0.53 | 6 | -0.12 | -0.29 |
| GlobalM2YoY | BTC | 1 | +0.02 | 0.36 | 16 | +0.06 | +0.24 |
| GlobalM2YoY | BTC | 3 | -0.15 | 0.45 | 16 | -0.34 | -1.36 |
| GlobalM2YoY | BTC | 6 | -0.21 | 0.59 | 16 | -0.36 | -1.45 |
| GlobalM2YoY | BTC | 12 | -0.26 | 0.53 | 16 | -0.48 | -1.93 |
| GlobalM2YoY | ETH | 1 | +0.06 | 0.28 | 10 | +0.22 | +0.71 |
| GlobalM2YoY | ETH | 3 | +0.04 | 0.42 | 10 | +0.09 | +0.28 |
| GlobalM2YoY | ETH | 6 | -0.09 | 0.71 | 10 | -0.13 | -0.41 |
| GlobalM2YoY | ETH | 12 | -0.07 | 0.70 | 10 | -0.10 | -0.32 |
| GlobalM2YoY | SOL | 1 | +0.16 | 0.22 | 6 | +0.70 | +1.71 |
| GlobalM2YoY | SOL | 3 | +0.08 | 0.46 | 6 | +0.18 | +0.45 |
| GlobalM2YoY | SOL | 6 | +0.02 | 0.80 | 6 | +0.02 | +0.06 |
| GlobalM2YoY | SOL | 12 | -0.11 | 0.73 | 6 | -0.15 | -0.37 |

### Whole-sample (single-shot, pooled) IC for cross-reference against study 03's own numbers

| driver | asset | h=1 | h=3 | h=6 | h=12 |
|---|---|--:|--:|--:|--:|
| NetLiqAccel | BTC | +0.12 | +0.14 | +0.14 | +0.14 |
| NetLiqAccel | ETH | +0.28 | +0.28 | +0.21 | +0.26 |
| NetLiqAccel | SOL | +0.37 | +0.37 | +0.31 | +0.41 |
| NetLiqYoY | BTC | +0.03 | +0.04 | -0.04 | -0.13 |
| NetLiqYoY | ETH | +0.15 | +0.16 | +0.05 | -0.15 |
| NetLiqYoY | SOL | +0.26 | +0.32 | +0.38 | +0.24 |
| GlobalM2Accel | BTC | +0.09 | +0.04 | -0.06 | -0.04 |
| GlobalM2Accel | ETH | +0.14 | +0.15 | +0.09 | +0.27 |
| GlobalM2Accel | SOL | +0.12 | +0.18 | +0.39 | +0.56 |
| GlobalM2YoY | BTC | -0.05 | -0.15 | -0.24 | -0.16 |
| GlobalM2YoY | ETH | -0.02 | -0.09 | -0.15 | -0.15 |
| GlobalM2YoY | SOL | +0.12 | +0.09 | +0.12 | -0.03 |

## 1b. Full breadth -- 9 continuous-driver rules x all 28 assets, h=1 (primary, genuinely non-overlapping)

Per-rule aggregate across all 28 assets (median across assets with >= 3 valid annual IC blocks): mirrors study 06's median-dSharpe breadth format, now in formal IC vocabulary.

| rule | n assets (>=3 yr-blocks) | median mean-IC | median ICIR | median t-stat | n assets with \|t\|>=2 |
|---|--:|--:|--:|--:|--:|
| GlobalM2 YoY>0 | 28 | +0.056 | +0.198 | +0.61 | 5 |
| NetLiq accel>0 | 28 | +0.036 | +0.118 | +0.46 | 1 |
| NetLiq rising(3m) | 28 | +0.023 | +0.073 | +0.31 | 2 |
| Gold YoY>0 (risk proxy) | 28 | +0.014 | +0.050 | +0.20 | 3 |
| NetLiq YoY>0 | 28 | +0.013 | +0.042 | +0.18 | 2 |
| ISM rising | 28 | -0.016 | -0.055 | -0.23 | 0 |
| GlobalM2 YoY accel>0 | 28 | -0.021 | -0.073 | -0.30 | 2 |
| ISM>50 | 28 | -0.020 | -0.082 | -0.35 | 2 |
| USM2 YoY accel>0 | 28 | -0.039 | -0.146 | -0.57 | 3 |

[rank check] rules ranked by median ICIR (descending):
  1. GlobalM2 YoY>0: ICIR=+0.198 (mean-IC +0.056, t-stat +0.61, 5/28 assets |t|>=2)
  2. NetLiq accel>0: ICIR=+0.118 (mean-IC +0.036, t-stat +0.46, 1/28 assets |t|>=2)
  3. NetLiq rising(3m): ICIR=+0.073 (mean-IC +0.023, t-stat +0.31, 2/28 assets |t|>=2)
  4. Gold YoY>0 (risk proxy): ICIR=+0.050 (mean-IC +0.014, t-stat +0.20, 3/28 assets |t|>=2)
  5. NetLiq YoY>0: ICIR=+0.042 (mean-IC +0.013, t-stat +0.18, 2/28 assets |t|>=2)
  6. ISM rising: ICIR=-0.055 (mean-IC -0.016, t-stat -0.23, 0/28 assets |t|>=2)
  7. GlobalM2 YoY accel>0: ICIR=-0.073 (mean-IC -0.021, t-stat -0.30, 2/28 assets |t|>=2)
  8. ISM>50: ICIR=-0.082 (mean-IC -0.020, t-stat -0.35, 2/28 assets |t|>=2)
  9. USM2 YoY accel>0: ICIR=-0.146 (mean-IC -0.039, t-stat -0.57, 3/28 assets |t|>=2)

## 1c. Named-asset check (same 8 assets study 06/19's guardrail used)

NetLiqAccel, h=1, annual-block IC/ICIR (sign should match study 06's dSharpe sign: COIN/SOL/MARA/TSLA/ADA/MSTR positive, UUP/GLD negative):

| asset | mean IC | ICIR | t-stat | n years |
|---|--:|--:|--:|--:|
| COIN | +0.231 | +0.770 | +1.72 | 5 |
| SOL | +0.182 | +0.417 | +1.02 | 6 |
| MARA | +0.035 | +0.114 | +0.42 | 14 |
| TSLA | +0.151 | +0.373 | +1.54 | 17 |
| ADA | -0.008 | -0.024 | -0.07 | 8 |
| MSTR | -0.015 | -0.047 | -0.20 | 17 |
| UUP | -0.025 | -0.077 | -0.33 | 18 |
| GLD | +0.031 | +0.097 | +0.41 | 18 |

## 2. Bonus (b) -- cross-sectional Spearman: NetLiqAccel's per-asset dSharpe vs. net-liq beta

Single cross-sectional snapshot (one number, NOT a time series -- no ICIR here by definition). Recomputes each asset's NetLiqAccel-rule Sharpe uplift vs. buy&hold directly from `study19_variant_returns_06.csv` (same formula as `macro_strategy_zoo.py`'s own Part 1), then correlates (Spearman, across assets) against that asset's own net-liquidity beta from `liquidity_beta_ranking.csv` (study 05). CI via a plain i.i.d. bootstrap over assets (resample-with-replacement, 5000 draws) -- NOT the block-bootstrap convention (`01_data_and_methods.md`), which is designed for time-series autocorrelation; there is no time axis in a single cross-sectional snapshot, so a plain asset-level bootstrap is the correct adaptation, stated explicitly rather than mislabeled.

Matched 18/28 assets present in both study 06's return matrix and study 05's beta ranking:

| asset | dSharpe (NetLiqAccel vs B&H) | net-liq beta |
|---|--:|--:|
| SOL | +0.37 | 181.0 |
| DOGE | +0.01 | 39.3 |
| MARA | +0.30 | 19.7 |
| ADA | +0.26 | 15.8 |
| COIN | +0.87 | 14.1 |
| TSLA | +0.29 | 8.3 |
| ETH | +0.14 | 7.3 |
| LINK | +0.12 | 6.8 |
| BTC | -0.06 | 3.2 |
| NVDA | +0.17 | 1.8 |
| SPY | +0.18 | 0.2 |
| MSTR | +0.40 | 0.0 |
| AAPL | -0.13 | 0.0 |
| AMZN | +0.05 | -0.0 |
| GLD | -0.21 | -0.1 |
| MSFT | +0.25 | -0.1 |
| SLV | -0.18 | -0.1 |
| JPM | +0.04 | -0.2 |

**Cross-sectional Spearman rho (dSharpe vs. net-liq beta, n=18 assets): +0.523 [+0.068, +0.843] (95% i.i.d. bootstrap CI, 5000 valid draws) ✓**

This is a single cross-sectional number, not an ICIR (there is no time dimension here to compute a std/ICIR over) -- reported as a Spearman rank correlation with a bootstrap CI, matching the task's own labeling requirement.

