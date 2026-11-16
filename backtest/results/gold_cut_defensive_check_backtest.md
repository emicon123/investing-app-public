# Study 22 -- gold-cut/equity-destination A/B, OLD (pre-ADR-047) vs LIVE (proxy-based, full 5-window)

Buy-only DCA 1000+1000/mo PLN, 15bps, CURRENT 1.5x/1.0x-trim-to-gold rule fixed throughout (ADR-048 live rule; was 2.0x/1.5x PRIOR before the 2026-09-25 refresh). Latest month: 2026-09. QQQ/FXI stand in for Nasdaq-100/China (proxy-vs-literal split -- see script header and the companion `nqse_icga_defensiveness_check.py` for the literal-ticker question this script does NOT answer). NEW is the live table through ADR-074/087, not just ADR-047's own application -- see header.

## OLD table (pre-ADR-047) vs NEW table (live through ADR-074/087), demo-portfolio scale

| asset | OLD risk-on | OLD risk-off | NEW risk-on | NEW risk-off |
|---|--:|--:|--:|--:|
| GLD | 30.000 | 30.000 | 20.000 | 30.000 |
| CASH | 10.000 | 30.625 | nan | nan |
| QQQ | 5.000 | 15.000 | 22.500 | 27.500 |
| FXI | 2.500 | 7.500 | 2.500 | 7.500 |
| MSTR | 2.500 | 2.500 | nan | nan |
| BTC_spot | 17.500 | 9.625 | 20.000 | 12.000 |
| miners | 7.500 | 2.250 | 5.000 | 0.500 |
| ETH | 13.000 | 1.300 | 10.000 | 1.000 |
| SOL | 5.000 | 0.500 | 5.000 | 0.500 |
| COIN | 4.000 | 0.400 | 3.000 | 0.300 |
| SUI | 2.000 | 0.200 | 3.000 | 0.300 |
| DOGE | 1.000 | 0.100 | 1.000 | 0.100 |
| BOND_PL | nan | nan | 0.000 | 7.500 |
| BOND_DE | nan | nan | 0.000 | 2.500 |
| LINK | nan | nan | 1.500 | 0.150 |

## 2x2 table-change x divisor matrix (N=4, decided up front)

Divisor 0.5 is the ADR-049 LIVE value (study 21's walk-forward chain also selects 0.5 at its most recent checkpoint, so the matrix keeps study 21's own {2.0, 0.5} pair -- now reading 0.5 as live, not as the walk-forward-honest alternative to a 1.0 live value).

| table | divisor | 13y | 8y | 5y | 3y | 1y |
|---|--:|--:|--:|--:|--:|--:|
| OLD (pre-ADR-047) | 2.0 | 32 (-17) | 32 (-18) | 22 (-28) | 18 (-14) | -6 (-11) |
| OLD (pre-ADR-047) | 0.5 | 35 (-16) | 37 (-16) | 28 (-21) | 23 (-9) | -5 (-8) |
| NEW (live through ADR-074/087) | 2.0 | 33 (-15) | 32 (-19) | 23 (-32) | 16 (-14) | -28 (-24) |
| NEW (live through ADR-074/087) | 0.5 | 35 (-14) | 36 (-12) | 26 (-24) | 20 (-10) | -27 (-20) |

Start months: 13y=2013-09, 8y=2018-09, 5y=2021-09, 3y=2023-09, 1y=2025-09. Cell = XIRR%/yr (maxDD%).

## Isolating the table effect vs. the divisor effect (13y/8y windows, pp deltas)

| comparison | 13y XIRR delta | 13y maxDD delta | 8y XIRR delta | 8y maxDD delta |
|---|--:|--:|--:|--:|
| Table effect (NEW-OLD) @ divisor=2.0 | +0.8pp | +2.2pp | +0.3pp | -1.1pp |
| Table effect (NEW-OLD) @ divisor=0.5 | +0.6pp | +1.4pp | -- | -- |
| Divisor effect (0.5-2.0) @ OLD table | +2.3pp | +1.6pp | -- | -- |
| Divisor effect (0.5-2.0) @ NEW table | +2.2pp | +0.8pp | -- | -- |

## Standing baseline: 60/40 classic-portfolio cross-check (SPY/AGG, BUYHOLD real catch-up DCA)

| window | XIRR%/yr | maxDD% |
|---|--:|--:|
| 13y | 10 | -12 |
| 8y | 9 | -12 |
| 5y | 8 | -12 |
| 3y | 10 | -12 |
| 1y | 12 | -3 |

## Caveats

- **This is the proxy-based portfolio-level A/B ONLY** -- QQQ/FXI stand in for NQSE.DE/ICGA.DE, exactly matching study 14's own scaffold/analogy convention ('a reasonable-but-unverified analogy, not a re-derivation from scratch'). This answers 'is a ~20pp gold-to-equity reallocation of this rough shape defensible', NOT 'do the two actual ETFs behave defensively' -- see the companion literal-ticker script for that separate question. Do not read a passing result here as proof the real NQSE.DE/ICGA.DE tickers are defensive, or vice versa.
- **New-table derivation is itself a judgment-call analogy** (live production targets reused for the long-history proxies QQQ/FXI/LINK/BONDs since the demo portfolio's OLD values already numerically matched real production's OLD values, via portfolio_spec.py) -- not independently re-optimized for this demo portfolio's own weight structure. NEW is the cumulative live state through ADR-074/087, not just ADR-047's own application -- see the header.
- **Divisor 0.5 is the ADR-049 live value** -- this matrix therefore does not include a literal '1.0' cell at all, by design (study 21's own walk-forward chain likewise never selects 1.0 at any checkpoint).
- Same caveats as study 14/21 apply unchanged: miners-basket survivorship, gold's multi-year rally flatters any gold-trim-destination variant, overlapping windows share one underlying price/macro history, 1y/3y windows are single-episode reads.
