# Macro ↔ crypto correlations

Coverage (months, monthly grid): BTC 2010-07→2026-06, ETH 2015-08→2026-06, SOL 2020-04→2026-06

Headline = correlation of **changes** (asset YoY return vs macro YoY; asset 1m return vs macro MoM). Levels are omitted as spurious. `*` |t|≥2, `**` |t|≥2.6 (YoY rows overlap → t inflated). `best_lead_k` = months the driver leads the asset's next 1-month return (Spearman IC).


## BTC

| Driver | match | n | Pearson | Spearman | t | lead k | IC@lead |
|---|---|--:|--:|--:|--:|--:|--:|
| ISM (level) | yoy | 179 | 0.02 | 0.30 | 0.3 | 6 | -0.11 |
| ISM (Δ MoM) | mom | 190 | 0.01 | 0.08 | 0.1 | 2 | 0.14 |
| US M2 (YoY %) | yoy | 178 | 0.07 | 0.35 | 0.9 | 0 | 0.05 |
| US M2 (MoM %) | mom | 189 | 0.01 | 0.09 | 0.2 | 1 | 0.11 |
| Global M2 5-ctry (YoY %) | yoy | 178 | 0.22** | 0.55 | 3.0 | 5 | -0.15 |
| Global M2 5-ctry (MoM %) | mom | 189 | 0.04 | 0.01 | 0.5 | 2 | 0.20 |
| Global M2 G3 (YoY %) | yoy | 178 | 0.15* | 0.43 | 2.0 | 6 | -0.11 |
| Global M2 G3 (MoM %) | mom | 189 | 0.03 | 0.02 | 0.4 | 2 | 0.22 |
| Fed net liq (YoY %) | yoy | 180 | 0.25** | 0.56 | 3.5 | 0 | 0.11 |
| Fed net liq (MoM %) | mom | 191 | 0.12 | 0.24 | 1.7 | 0 | 0.24 |

## ETH

| Driver | match | n | Pearson | Spearman | t | lead k | IC@lead |
|---|---|--:|--:|--:|--:|--:|--:|
| ISM (level) | yoy | 118 | 0.39** | 0.48 | 4.5 | 6 | -0.04 |
| ISM (Δ MoM) | mom | 129 | 0.00 | 0.06 | 0.0 | 6 | 0.09 |
| US M2 (YoY %) | yoy | 117 | 0.05 | 0.47 | 0.5 | 0 | 0.16 |
| US M2 (MoM %) | mom | 128 | 0.12 | 0.17 | 1.4 | 1 | 0.21 |
| Global M2 5-ctry (YoY %) | yoy | 117 | 0.30** | 0.58 | 3.4 | 0 | 0.10 |
| Global M2 5-ctry (MoM %) | mom | 128 | 0.13 | 0.10 | 1.5 | 2 | 0.24 |
| Global M2 G3 (YoY %) | yoy | 117 | 0.25** | 0.54 | 2.8 | 0 | 0.12 |
| Global M2 G3 (MoM %) | mom | 128 | 0.17 | 0.13 | 2.0 | 2 | 0.20 |
| Fed net liq (YoY %) | yoy | 119 | 0.08 | 0.62 | 0.8 | 0 | 0.25 |
| Fed net liq (MoM %) | mom | 130 | 0.14 | 0.27 | 1.6 | 0 | 0.27 |

## SOL

| Driver | match | n | Pearson | Spearman | t | lead k | IC@lead |
|---|---|--:|--:|--:|--:|--:|--:|
| ISM (level) | yoy | 62 | 0.69** | 0.43 | 7.4 | 3 | -0.13 |
| ISM (Δ MoM) | mom | 73 | 0.06 | 0.11 | 0.5 | 6 | 0.28 |
| US M2 (YoY %) | yoy | 61 | 0.64** | 0.51 | 6.4 | 0 | 0.07 |
| US M2 (MoM %) | mom | 72 | 0.20 | 0.23 | 1.7 | 0 | 0.23 |
| Global M2 5-ctry (YoY %) | yoy | 61 | 0.45** | 0.48 | 3.8 | 0 | 0.21 |
| Global M2 5-ctry (MoM %) | mom | 72 | 0.18 | 0.04 | 1.6 | 2 | 0.23 |
| Global M2 G3 (YoY %) | yoy | 61 | 0.42** | 0.45 | 3.6 | 0 | 0.21 |
| Global M2 G3 (MoM %) | mom | 72 | 0.16 | 0.06 | 1.3 | 2 | 0.27 |
| Fed net liq (YoY %) | yoy | 63 | 0.72** | 0.78 | 8.2 | 1 | 0.39 |
| Fed net liq (MoM %) | mom | 74 | 0.27* | 0.23 | 2.4 | 3 | 0.29 |
