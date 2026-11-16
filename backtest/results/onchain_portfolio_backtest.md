# On-chain trigger portfolio test — LTH-SOPR vs macro vs composite

Real algo (buy-only Q_pos catch-up DCA + CURRENT 1.5x/1.0x-trim-to-target, ADR-048 live rule, cut=0.33). Only the risk-off TRIGGER definition varies. **On-chain data only exists from 2022-07 -- 5/8/13y start dates from files 07/09 are NOT testable here; only 3y and 1y.** Each cell = XIRR%/yr (maxDD%).

| config | 3y | 1y |
|---|--:|--:|
| NO de-risk (baseline, always risk-on) | 14 (-21) | -34 (-33) |
| Macro only (shipped, ADR-042) | 27 (-11) | -27 (-21) |
| On-chain only (LTH-SOPR>=1.86) | 18 (-9) | -34 (-33) |
| Composite OR (macro or on-chain) | 20 (-9) | -27 (-21) |
| Composite AND (macro and on-chain) | 21 (-11) | -34 (-33) |

Start months: 3y=2023-09, 1y=2025-09. Cell = XIRR%/yr (maxDD%).

Trigger frequency (49 months, 2022-07..2026-07): macro risk-off 14 months, on-chain risk-off 13 months, both 2 months, either 25 months.

## Caveats
Only 2 start dates testable (on-chain history is ~4 years); LTH-SOPR threshold (75th percentile, 1.86) calibrated on the SAME full sample being backtested -- real in-sample bias given how little history exists; see file 10 for the signal-level caveats (native-2022+ IC is strong but doesn't replicate over 2010+ history).
