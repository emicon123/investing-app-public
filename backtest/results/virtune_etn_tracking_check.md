# Virtune GPW crypto-ETN tracking-quality check

Descriptive check, not a walk-forward backtest -- all four Virtune ETNs (ETNVIRBTCP/ETNVIRETH/ETNVIRSOL/ETNVIRXRP) launched together on GPW 2026-02-25, so there is only one ~6-month window to look at (it will grow over time; no multiple starting dates are possible yet). Source: BiznesRadar historical quotes (PLN), cross-checked against BTC/ETH/SOL/XRP USD spot (Yahoo Finance) and USDPLN (Yahoo Finance).

## Summary

| ETN | Coin | N days | Window | Corr(daily ret) | TrackErr ann. | ETN ret (USD-equiv) | Coin ret (USD) | Tracking gap (USD terms) | Expected gap (TER-staking, prorated) | ETN ret (PLN, as-traded) | Coin ret (PLN-equiv) |
|---|---|---|---|---|---|---|---|---|---|---|---|
| ETNVIRBTCP | BTC | 123 | 2026-02-25 -> 2026-08-20 (0.48y) | 0.51 | 40.35% | +6.04% | +7.46% | -1.43% | -0.12% | +9.47% | +10.94% |
| ETNVIRETH | ETH | 123 | 2026-02-25 -> 2026-08-20 (0.48y) | 0.5941 | 51.85% | +12.44% | +13.23% | -0.79% | -0.14% | +16.08% | +16.89% |
| ETNVIRSOL | SOL | 123 | 2026-02-25 -> 2026-08-20 (0.48y) | 0.6139 | 48.94% | +3.63% | -0.32% | +3.95% | +0.98% | +6.98% | +2.91% |
| ETNVIRXRP | XRP | 123 | 2026-02-25 -> 2026-08-20 (0.48y) | 0.7158 | 38.77% | -16.59% | -11.53% | -5.06% | -0.72% | -13.89% | -8.67% |

## Detail

### ETNVIRBTCP (BTC)

- Window: 2026-02-25 -> 2026-08-20 (123 GPW trading days, 0.48y) -- this is the ETN's entire trading life (listed 2026-02-25)
- Daily-return correlation to spot BTC: **0.5100** (peak at lag +0d -- -2d=0.12, -1d=-0.03, +0d=0.51, +1d=0.38, +2d=-0.09 -- confirms lag-0 alignment, not a session-timing artifact)
- Annualised tracking error (stdev of daily return diff x sqrt(252)): **40.35%**
- USD-equivalent total return: ETN +6.04% vs. spot BTC +7.46% -> tracking gap **-1.43%**
- Published cost structure: TER 0.25%/yr, no staking -> naive expected gap over 0.48y at this rate: -0.12%
- As actually experienced by a PLN investor (both legs in PLN, FX included): ETN +9.47% vs. coin-in-PLN +10.94%

### ETNVIRETH (ETH)

- Window: 2026-02-25 -> 2026-08-20 (123 GPW trading days, 0.48y) -- this is the ETN's entire trading life (listed 2026-02-25)
- Daily-return correlation to spot ETH: **0.5941** (peak at lag +0d -- -2d=0.03, -1d=-0.05, +0d=0.59, +1d=0.26, +2d=0.08 -- confirms lag-0 alignment, not a session-timing artifact)
- Annualised tracking error (stdev of daily return diff x sqrt(252)): **51.85%**
- USD-equivalent total return: ETN +12.44% vs. spot ETH +13.23% -> tracking gap **-0.79%**
- Published cost structure: TER 1.40%/yr, staking passthrough ~1.12%/yr -> naive expected gap over 0.48y at this rate: -0.14%
- As actually experienced by a PLN investor (both legs in PLN, FX included): ETN +16.08% vs. coin-in-PLN +16.89%

### ETNVIRSOL (SOL)

- Window: 2026-02-25 -> 2026-08-20 (123 GPW trading days, 0.48y) -- this is the ETN's entire trading life (listed 2026-02-25)
- Daily-return correlation to spot SOL: **0.6139** (peak at lag +0d -- -2d=0.01, -1d=-0.03, +0d=0.61, +1d=0.28, +2d=0.04 -- confirms lag-0 alignment, not a session-timing artifact)
- Annualised tracking error (stdev of daily return diff x sqrt(252)): **48.94%**
- USD-equivalent total return: ETN +3.63% vs. spot SOL -0.32% -> tracking gap **+3.95%**
- Published cost structure: TER 0.95%/yr, staking passthrough ~3.00%/yr -> naive expected gap over 0.48y at this rate: +0.98%
- As actually experienced by a PLN investor (both legs in PLN, FX included): ETN +6.98% vs. coin-in-PLN +2.91%

### ETNVIRXRP (XRP)

- Window: 2026-02-25 -> 2026-08-20 (123 GPW trading days, 0.48y) -- this is the ETN's entire trading life (listed 2026-02-25)
- Daily-return correlation to spot XRP: **0.7158** (peak at lag +0d -- -2d=-0.06, -1d=-0.02, +0d=0.72, +1d=0.30, +2d=-0.07 -- confirms lag-0 alignment, not a session-timing artifact)
- Annualised tracking error (stdev of daily return diff x sqrt(252)): **38.77%**
- USD-equivalent total return: ETN -16.59% vs. spot XRP -11.53% -> tracking gap **-5.06%**
- Published cost structure: TER 1.49%/yr, no staking -> naive expected gap over 0.48y at this rate: -0.72%
- As actually experienced by a PLN investor (both legs in PLN, FX included): ETN -13.89% vs. coin-in-PLN -8.67%
