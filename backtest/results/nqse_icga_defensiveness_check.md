# Study 22 (companion) -- literal NQSE.DE / ICGA.DE defensiveness check

Tests whether the two actual ETFs ADR-047 routes risk-off weight into behave defensively (vs. Gold) during real historical risk-off episodes, using their own price history (not the QQQ/FXI proxies used in the companion portfolio-level A/B). NQSE.DE: 2018-09..2026-07 (95mo). ICGA.DE: 2019-07..2026-07 (85mo).

## Part 1 -- liquidity-beta regression (extends study #05's methodology; NQSE.DE/ICGA.DE are new, not previously in `results/liquidity_beta_ranking.md`)

`asset YoY return = a + beta*(driver YoY)`, HAC/Newey-West standard errors (L = floor(4*(T/100)^(2/9)), Newey & West 1994's standard rule of thumb -- study 05's own script/lag choice no longer exists to confirm against), min 24 months. Native quote currency (EUR for NQSE.DE/ICGA.DE, matching study 05's own no-secondary-PLN-conversion convention).

| ticker | driver | beta | t | R2 | n | lag |
|---|---|--:|--:|--:|--:|--:|
| NQSE.DE | net-liq YoY | 0.60 | 3.52 | 0.33 | 82 | 3 |
| NQSE.DE | Global-M2 YoY | 2.47 | 4.39 | 0.46 | 80 | 3 |
| ICGA.DE | net-liq YoY | 0.41 | 3.01 | 0.14 | 72 | 3 |
| ICGA.DE | Global-M2 YoY | 2.04 | 7.25 | 0.35 | 70 | 3 |

For scale, study 05's own category aggregates (mean net-liq beta): Crypto ~24, Crypto-equity ~6, **Equity ~1**, FX ~0.2, **Metal (incl. gold) ~0.1**. NQSE.DE/ICGA.DE are equity-index ETFs, so the equity aggregate (~1, not the metals aggregate ~0.1) is the directly relevant prior for what beta 'should' look like if they behave like typical equities rather than like gold.

## Part 2 -- risk-off-conditional (blend>=0.6) vs. all-other-months split

Splits each ticker's own monthly YoY-return-vs-net-liq-YoY regression into risk-off months (blend>=0.6, original /2.0-divisor classification, study 14's own `macro_blend_series.csv`) vs. everything else. **Small-sample caveat (per the episode-coverage note above): treat as descriptive, single-episode-dominated, not a statistically decisive test** -- most risk-off months in either ticker's history belong to the single 2021-12..2023-01 episode.

| ticker | subsample | beta (net-liq YoY, simple OLS) | n | corr(ret, GLD ret) | n(corr) |
|---|---|--:|--:|--:|--:|
| NQSE.DE | risk-off (blend>=0.6) | 1.40 | 25 | -0.03 | 29 |
| NQSE.DE | all other months | 0.35 | 57 | -0.04 | 64 |
| ICGA.DE | risk-off (blend>=0.6) | 0.38 | 25 | 0.26 | 25 |
| ICGA.DE | all other months | 0.33 | 47 | 0.18 | 58 |

## Part 3 -- episode-level cumulative return & maxDD (GLD vs NQSE.DE vs ICGA.DE, PLN)

Study 14's own 5 named sustained-risk-off episodes -- coverage per the header note above; 2012-06..08 and 2014-11..2015-03 predate both tickers (excluded, no data).

| episode | ticker | actual covered range | cum. return | maxDD | n(mo) |
|---|---|---|--:|--:|--:|
| 2018-08..2019-01 | GLD | 2018-08..2019-01 | +10.0% | -1.1% | 6 |
| 2018-08..2019-01 | NQSE.DE | 2018-10..2019-01 (truncated, actual start 2018-10) | -10.2% | -10.6% | 4 |
| 2018-08..2019-01 | ICGA.DE | (no data) | -- | -- | 0 |
| 2021-05..2021-09 | GLD | 2021-05..2021-09 | +5.1% | -4.1% | 5 |
| 2021-05..2021-09 | NQSE.DE | 2021-05..2021-09 | +7.9% | -3.6% | 5 |
| 2021-05..2021-09 | ICGA.DE | 2021-05..2021-09 | -13.2% | -14.2% | 5 |
| 2021-12..2023-01 | GLD | 2021-12..2023-01 | +13.5% | -8.8% | 14 |
| 2021-12..2023-01 | NQSE.DE | 2021-12..2023-01 | -27.8% | -35.1% | 14 |
| 2021-12..2023-01 | ICGA.DE | 2021-12..2023-01 | -10.8% | -31.9% | 14 |

## Part 4 -- correlation of NQSE.DE/ICGA.DE monthly return to GLD's own, risk-off-labeled months only (PLN)

The most directly interpretable 'does it move like gold when it's supposed to' number. Positive/near-1 correlation = moves with gold (defensive co-movement); negative/near-0 = does not.

| ticker | corr(ret, GLD ret), risk-off months only | n |
|---|--:|--:|
| NQSE.DE | -0.03 | 29 |
| ICGA.DE | 0.26 | 25 |

## Caveats

- **Episode coverage is genuinely thin**: at most 2-3 episodes, and the largest by far (14mo, 2021-12..2023-01) is the SAME single episode study 14 already flagged as driving essentially all of its own headline result -- every conditional statistic above rests on this same n~1 episode more than it rests on any general pattern. Report as descriptive, not as a statistically decisive test.
- **NQSE.DE/ICGA.DE are EUR-denominated; the liquidity-beta regression (Part 1) uses native EUR return, unconverted** (matching study 05's own convention) -- a portion of any beta reflects EUR-specific dynamics, not purely equity-index dynamics, though this is the same convention study 05 itself used for every other non-USD-native ticker consideration.
- **Parts 2-4 use PLN-converted returns** (GLD via USDPLN_X, NQSE.DE/ICGA.DE via EURPLN_X) for apples-to-apples comparability, consistent with every other portfolio-level metric in this repo -- a design choice, not a re-derivation of study 05's own currency convention.
- **Newey-West lag (L=floor(4*(T/100)^(2/9)))** is a standard rule-of-thumb default, not a confirmed match to study 05's own (now-unrecoverable) exact choice.
- The risk-off classification (blend>=0.6) uses the FIXED, original /2.0-divisor series from study 14 -- deliberately not re-derived from any of study 21's divisor variants, to avoid mixing two different zone-boundary definitions into one episode list.
