# Strategy backtest — dynamic-target macro de-risk via the +100% rule (real algo)

Buy-only DCA 1000+1000/mo PLN, 15bps, latest 2026-09. Macro 'start selling' = lower crypto targets (×cut) when net-liq & Global-M2 both decelerate → the +100% rule sells the overshoot into gold. Trigger fixed at 1.5x (ADR-048 live rule); trim_to varies 1.0x (CURRENT, live) vs 1.5x (PRIOR, pre-ADR-048, kept as reference). Each cell = XIRR%/yr (maxDD%).

| config | 13y | 8y | 5y | 3y | 1y |
|---|--:|--:|--:|--:|--:|
| NO de-risk · trim→1.0×target (CURRENT, ADR-048 live rule) | 37 (-31) | 37 (-32) | 22 (-53) | 14 (-21) | -34 (-33) |
| NO de-risk · trim→1.5×target (PRIOR, pre-ADR-048) | 41 (-35) | 40 (-37) | 23 (-53) | 15 (-22) | -34 (-33) |
| (a) cut×0.5 · trim→1.0×target (CURRENT) | 36 (-19) | 38 (-17) | 27 (-30) | 23 (-9) | -29 (-23) |
| (a) cut×0.5 · trim→1.5×target (PRIOR) | 39 (-25) | 41 (-21) | 27 (-31) | 21 (-13) | -29 (-23) |
| (b) cut×0.33 · trim→1.0×target (CURRENT) | 33 (-19) | 38 (-13) | 29 (-21) | 27 (-11) | -27 (-21) |
| (b) cut×0.33 · trim→1.5×target (PRIOR) | 35 (-22) | 41 (-14) | 29 (-22) | 25 (-9) | -27 (-21) |

Start months: 13y=2013-09, 8y=2018-09, 5y=2021-09, 3y=2023-09, 1y=2025-09. Cell = XIRR%/yr (maxDD%). risk-off months caught by both gauges decelerating.
Caveats: miners basket return overstated; gold rose through risk-off windows (flatters de-risk); 1y/3y windows noisy; buy-only re-entry after risk-off is gradual (via DCA).
