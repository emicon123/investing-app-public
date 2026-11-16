# Study 27 -- FXI-extended-history China-market-proxy defensiveness check

Extension of study 22's literal-ticker companion (`nqse_icga_defensiveness_check.py`), China/ICGA leg only -- NQSE.DE is not touched anywhere in this script or its output. Substitutes FXI (iShares China Large-Cap, USD, 21.7y history) for ICGA.DE (real fund, EUR, only 7.0y history) to reach all 5 of study 14's named risk-off episodes instead of just 2. **This is a China-market-proxy check (FXI stands in for ICGA.DE, same convention as study 22 Part A's portfolio-level A/B), not a literal test of the ICGA.DE fund itself** -- FXI tracks FTSE China 25, ICGA.DE tracks MSCI China, a close but not identical index. FXI: 2004-10..2026-06 (261mo). ICGA.DE (real fund, overlap check only): 2019-07..2026-07 (85mo). GLD: 2007-01..2026-07 (235mo) -- starts 5.4y before the earliest (2012-06) named episode, comfortably inside GLD's coverage, not at its data edge.

## Part 1 -- liquidity-beta regression (extends study #05's methodology; FXI, as an ICGA.DE China-market proxy, is not previously in `results/liquidity_beta_ranking.md`)

`asset YoY return = a + beta*(driver YoY)`, HAC/Newey-West standard errors (L = floor(4*(T/100)^(2/9)), Newey & West 1994's standard rule of thumb -- same convention as the original companion script), min 24 months. Native quote currency (USD for FXI/GLD, matching study 05's own no-secondary-PLN-conversion convention).

| ticker | driver | beta | t | R2 | n | lag |
|---|---|--:|--:|--:|--:|--:|
| FXI (ICGA.DE proxy) | net-liq YoY | -0.13 | -1.21 | 0.02 | 210 | 4 |
| FXI (ICGA.DE proxy) | Global-M2 YoY | 1.61 | 3.02 | 0.12 | 208 | 4 |

For scale, study 05's own category aggregates (mean net-liq beta): Crypto ~24, Crypto-equity ~6, **Equity ~1**, FX ~0.2, **Metal (incl. gold) ~0.1**. FXI is a China-equity-index ETF, so the equity aggregate (~1, not the metals aggregate ~0.1) is the directly relevant prior for what beta 'should' look like if it behaves like a typical equity index rather than like gold.

## Part 2 -- risk-off-conditional (blend>=0.6) vs. all-other-months split

Splits FXI's own monthly YoY-return-vs-net-liq-YoY regression into risk-off months (blend>=0.6, original /2.0-divisor classification, study 14's own `macro_blend_series.csv`) vs. everything else. FXI's much longer history spreads risk-off months across all 5 named episodes (not just the single 2021-12..2023-01 episode that dominated the original ICGA.DE-based split) -- see Part 3 for the per-episode breakdown.

| ticker | subsample | beta (net-liq YoY, simple OLS) | n | corr(ret, GLD ret) | n(corr) |
|---|---|--:|--:|--:|--:|
| FXI (ICGA.DE proxy) | risk-off (blend>=0.6) | 0.52 | 40 | 0.16 | 40 |
| FXI (ICGA.DE proxy) | all other months | -0.20 | 170 | 0.09 | 193 |

## Part 3 -- episode-level cumulative return & maxDD (GLD vs FXI-as-ICGA-proxy, PLN)

Study 14's own 5 named sustained-risk-off episodes -- FXI's 21.7y history reaches ALL 5 (the original ICGA.DE-based table reached only 2: 2021-05..09 and 2021-12..2023-01). GLD covers all 5 too (checked above -- 2012-06 is not at GLD's data edge).

| episode | ticker | actual covered range | cum. return | maxDD | n(mo) |
|---|---|---|--:|--:|--:|
| 2012-06..2012-08 | GLD | 2012-06..2012-08 | +2.4% | -1.8% | 3 |
| 2012-06..2012-08 | FXI (ICGA.DE proxy) | 2012-06..2012-08 | -4.3% | -4.4% | 3 |
| 2014-11..2015-03 | GLD | 2014-11..2015-03 | +14.0% | -6.4% | 5 |
| 2014-11..2015-03 | FXI (ICGA.DE proxy) | 2014-11..2015-03 | +27.3% | 0.0% | 5 |
| 2018-08..2019-01 | GLD | 2018-08..2019-01 | +10.0% | -1.1% | 6 |
| 2018-08..2019-01 | FXI (ICGA.DE proxy) | 2018-08..2019-01 | +2.3% | -6.1% | 6 |
| 2021-05..2021-09 | GLD | 2021-05..2021-09 | +5.1% | -4.1% | 5 |
| 2021-05..2021-09 | FXI (ICGA.DE proxy) | 2021-05..2021-09 | -10.6% | -11.7% | 5 |
| 2021-12..2023-01 | GLD | 2021-12..2023-01 | +13.5% | -8.8% | 14 |
| 2021-12..2023-01 | FXI (ICGA.DE proxy) | 2021-12..2023-01 | -9.6% | -36.0% | 14 |

## Part 4 -- correlation of FXI (ICGA.DE proxy) monthly return to GLD's own, risk-off-labeled months only (PLN)

The most directly interpretable 'does it move like gold when it's supposed to' number. Positive/near-1 correlation = moves with gold (defensive co-movement); negative/near-0 = does not. FXI's longer history means this n now spans all 5 named episodes' risk-off months, not just the 2 the original ICGA.DE-based check could reach.

| ticker | corr(ret, GLD ret), risk-off months only | n |
|---|--:|--:|
| FXI (ICGA.DE proxy) | 0.16 | 40 |

## Part 5 -- FXI-vs-real-ICGA.DE agreement check (internal consistency, NEW section)

Does the FXI proxy actually track the real ICGA.DE fund where both have data? Recomputes real ICGA.DE's own episode-level stats (EUR->PLN via EURPLN_X, exactly reproducing the original companion script's methodology and figures) for the 2 episodes both series cover, side by side with FXI's Part-3 numbers above.

| episode | metric | GLD | FXI (proxy) | ICGA.DE (real) | FXI - ICGA.DE delta |
|---|---|--:|--:|--:|--:|
| 2021-05..2021-09 | cum. return | +5.1% | -10.6% | -13.2% | +2.6pp |
| 2021-05..2021-09 | maxDD | -4.1% | -11.7% | -14.2% | +2.6pp |
| 2021-12..2023-01 | cum. return | +13.5% | -9.6% | -10.8% | +1.3pp |
| 2021-12..2023-01 | maxDD | -8.8% | -36.0% | -31.9% | -4.2pp |

Direct month-to-month correlation of FXI's own PLN return to real ICGA.DE's own PLN return, over their entire common history (2019-08..2026-06, n=83): **0.95**. Restricted to risk-off months only (n=25): **0.99**.

## Caveats

- **This is a China-market-proxy check, not a literal test of the ICGA.DE fund.** FXI (FTSE China 25) and ICGA.DE (MSCI China) are close but not identical indices -- Part 5 above is the direct evidence for how closely they actually track each other in this data, not an assumption.
- **FXI is USD-native; ICGA.DE is EUR-native.** Parts 2-5's PLN conversion uses USDPLN_X for FXI/GLD and EURPLN_X for ICGA.DE -- FXI and GLD now share one FX pair (both USD), which removes the EUR/PLN confound layer the original ICGA.DE-based check carried, but also means Part 5's FXI-vs-ICGA.DE comparison is not a pure like-for-like return comparison -- part of any divergence between the two columns could reflect USD/PLN vs. EUR/PLN currency moves, not just the underlying China-equity indices diverging.
- **The 3 newly-reached episodes (2012-06..08, 2014-11..2015-03, 2018-08..2019-01) have no real ICGA.DE data at all to cross-check against** (ICGA.DE's real history starts 2019-07) -- confidence in FXI as a proxy for those 3 episodes rests entirely on Part 5's overlap-window agreement check (2021-05..09, 2021-12..2023-01), not on direct evidence for the 3 pre-2019 episodes themselves.
- **Episode coverage is far broader here than the original script (5 vs. 2), but the 2021-12..2023-01 episode is still, by a wide margin, the largest and longest (14mo vs. 3-6mo for the other 4)** -- study 14's own n~1-episode caution still applies, just less severely than in the original ICGA.DE-only check.
- **Newey-West lag (L=floor(4*(T/100)^(2/9)))** is a standard rule-of-thumb default, same convention as the original script, not a re-derivation of study 05's own (unrecoverable) exact choice.
- The risk-off classification (blend>=0.6) uses the FIXED, original /2.0-divisor series from study 14 -- deliberately not re-derived from any of study 21's divisor variants, to avoid mixing two different zone-boundary definitions into one episode list.
- **Scope**: this script covers the China/ICGA leg only. NQSE.DE and the original `nqse_icga_defensiveness_check.py` script are untouched.
