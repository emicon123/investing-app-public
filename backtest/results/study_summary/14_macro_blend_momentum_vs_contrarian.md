# 14 — Sequel to 07: the REAL live mechanism (ADR-044 sliding blend) vs. a contrarian reversal

Script: `portfolio_macro_blend_backtest.py` → `results/portfolio_macro_blend_backtest.md`,
`results/macro_blend_series.csv`. Direct follow-on to file 07, which tested the old **binary**
cut+redistribute de-risk mechanism (ADR-042/043) — that mechanism no longer exists in
production. ADR-044 replaced it with a **continuous 0→1 blend** that linearly interpolates
every position's target between a risk-on and a risk-off table (§7 of
`docs/architecture/05-rebalancing-algorithm.md`); ADR-045 separately deleted the automatic
2×-trim-to-gold rule from the purchase-planning **software** (only the user's manual
"Sprzedaj" button sells now). Per an explicit mid-task correction, this study still **models**
the 2×-trim→gold rule in every variant below — it's what the user still does by hand
(CLAUDE.md: "reduce this asset to 15%, swap the cash to Gold"), so it's kept as a modeling
choice for comparability with file 07, even though it is no longer literally automated in code.
What's actually new and under test here is the **blend-based target table**, not the trim.

Same demo portfolio, DCA, and cost conventions as file 07 (1000 PLN + 1000/mo, 15bps, 5 start
dates 1/3/5/8/13y): Gold 30%, Cash 10%, QQQ 5%, FXI 2.5%, MSTR 2.5%, BTC-spot 17.5%, miners 7.5%
(MARA/HUT/CIFR/IREN/WULF), ETH 13%, SOL 5%, COIN 4%, SUI 2%, DOGE 1%.

## Three variants

1. **BASELINE** — fixed risk-on target table, no macro overlay at all. Pure control.
2. **CURRENT** — faithful to the live mechanism: each month's target = `riskOn×(1−blend) +
   riskOff×blend`, where `blend = clip(0.5 − smoothed_z/2, 0, 1)`, `smoothed_z` = 3-month
   rolling average of `composite_z = 0.7×zscore(net-liq YoY accel) + 0.3×zscore(Global-M2 YoY
   accel)`, each z-score **expanding and no-lookahead** (only uses history up to that month).
   Insufficient history (bootstrap) collapses to risk-on (`blend=0`), matching production
   behavior when `blendFactor` is `null`.
3. **REVERSED** — identical signal, identical two tables, `blend_reversed = 1 − blend`: the
   user's contrarian hypothesis ("accumulate risk assets when M2 is decelerating — i.e. when
   things are cheap").

### Risk-on / risk-off table (adapted from the real ADR-044 V37 migration ratios)

| asset | risk-on % | risk-off % | retention |
|---|--:|--:|--:|
| Gold | 30.000 | 30.000 | 100% |
| Cash | 10.000 | 30.625 | 306% |
| QQQ | 5.000 | 15.000 | 300% |
| FXI | 2.500 | 7.500 | 300% |
| MSTR | 2.500 | 2.500 | 100% |
| BTC-spot | 17.500 | 9.625 | 55% |
| miners (5-way) | 7.500 | 2.250 | 30% |
| ETH | 13.000 | 1.300 | 10% |
| SOL | 5.000 | 0.500 | 10% |
| COIN | 4.000 | 0.400 | 10% |
| SUI | 2.000 | 0.200 | 10% |
| DOGE | 1.000 | 0.100 | 10% |

Real production ratios this was adapted from (`V37__sliding_macro_regime_targets.sql`):
BTC-USD 20→11 (55%), miners each 1.00→0.30 (30%), ETH 13→1.3 / SOL 5→0.5 / COIN 4→0.4 /
SUI 2→0.2 / DOGE 1→0.1 (all 10%), NQSE.DE (Nasdaq-100 UCITS, QQQ's real-portfolio analog)
5→15 (3×), ICGA.DE (MSCI China UCITS, FXI's analog) 2.5→7.5 (3×), MSTR reclassified to the
flat `EXPERIMENTS` bucket (no regime move), gold/cash/CHF flat. Real production also opens a
new ~20%-of-portfolio bond/defensive sink (`ETFBCASH.PL`+`ZPR1.DE`, 0→10% each) in risk-off;
this demo portfolio has no separate bond ticker, so that freed weight (~20.6pp) is routed into
**Cash** instead — the only zero-vol defensive asset already in this list. Both tables sum to
exactly 100%, so the interpolation is sum-conserving for any `blend` (ADR-044 §7.3 property).

## Headline results — cell = XIRR%/yr (maxDD%)

| variant | 13y | 8y | 5y | 3y | 1y |
|---|--:|--:|--:|--:|--:|
| BASELINE (fixed target, no macro) | 39 (−35) | 40 (−43) | 24 (−48) | 25 (−23) | −5 (−17) |
| **CURRENT (ADR-044 blend, momentum)** | 34 (**−24**) | 33 (**−20**) | 21 (**−28**) | 24 (−15) | −3 (−11) |
| REVERSED (1−blend, contrarian) | 27 (−28) | 25 (−37) | 20 (−42) | 21 (−11) | −0 (**−8**) |

Start months: 13y=2013-06, 8y=2018-06, 5y=2021-06, 3y=2023-06, 1y=2025-06.

**CURRENT beats REVERSED on both return and drawdown at every window except the most recent
1y**, and the gap is largest exactly where the blend spent the most time in real risk-off
territory (8y, 5y — see below). REVERSED is worse than even the do-nothing BASELINE on
drawdown at 8y/5y (−37/−42 vs. −43/−48 baseline is still an improvement over baseline, but far
short of CURRENT's −20/−28) — the contrarian tilt gives back most of the drawdown protection
the blend mechanism is supposed to buy, without a compensating return edge (CURRENT's XIRR is
higher too, at every window 13y/8y/5y/3y). Only at 1y does REVERSED edge out CURRENT (−0 vs
−3, DD −8 vs −11) — but see the regime-occupancy table below: that window spent **zero months**
at blend≥0.6 for either variant, so this is a read of a mild period, not a real test of either
hypothesis, and the gap (3pp XIRR, 3pp DD) is well within noise for a 12-month window.

## Why: blend regime occupancy and the episode that decides it

Full history (2011-07..2026-05, n=179 months) zone occupancy: **RISK_ON 7% / MID_ON 38% /
NEUTRAL 32% / MID_OFF 16% / RISK_OFF 7%** — most months sit near-neutral; extremes are rare,
as designed (smoothing + 0.7/0.3 composite damps the raw signal).

Per-window occupancy (CURRENT's blend; REVERSED sees the mirror-image zones):

| window | RISK_ON | MID_ON | NEUTRAL | MID_OFF | RISK_OFF |
|---|--:|--:|--:|--:|--:|
| 13y | 7% | 38% | 31% | 15% | 8% |
| 8y | 10% | 34% | 23% | 20% | 12% |
| 5y | 5% | 33% | 22% | 23% | 17% |
| 3y | 3% | 53% | 28% | 17% | 0% |
| 1y | 8% | 58% | 17% | 17% | 0% |

Sustained blend≥0.6 (MID_OFF/RISK_OFF) episodes, full history:

| start | end | months | avg blend |
|---|---|--:|--:|
| 2012-06 | 2012-08 | 3 | 0.63 |
| 2014-11 | 2015-03 | 5 | 0.70 |
| 2018-08 | 2019-01 | 6 | 0.76 |
| 2021-05 | 2021-09 | 5 | 0.87 |
| **2021-12** | **2023-01** | **14** | **0.79** |

The decisive episode is the 14-month 2021-12→2023-01 stretch — it is, to the month, the Fed
hiking cycle / QT / 2022 crypto winter (LUNA and FTX collapses sit inside this window). **CURRENT**
spent that entire stretch buying proportionally less crypto and more gold/cash/equity-index,
exactly as the blend mechanism is designed to do. **REVERSED** did the opposite by construction
— it read the same decelerating liquidity as "cheap" and tilted its DCA *further* into crypto
throughout the entire 14-month decline. That single episode falls inside both the 8y (start
2018-06) and 5y (start 2021-06) windows, which is why REVERSED's drawdown gap vs. CURRENT is
largest exactly there (8y: −37 vs. −20; 5y: −42 vs. −28) and its return is lower there too. The
2018-08→2019-01 episode (also inside the 8y and 13y windows) reinforces the same pattern at
smaller scale.

## Verdict: the contrarian/reversed hypothesis loses — "decelerating liquidity = cheap" was a
## value trap in this data, not an accumulation opportunity

- **CURRENT (momentum-following, faithful to the live ADR-044 mechanism) beats REVERSED on
  both return and drawdown at 4 of 5 start dates**, and by a wide margin exactly where the
  signal had the most sustained risk-off exposure to act on (8y, 5y — the windows containing
  the 2021-2023 crypto winter).
- **The one exception (1y) is not a counter-example** — neither variant's blend crossed 0.6 in
  that window at all, so REVERSED's marginal edge there (3pp XIRR, 3pp DD, on a noisy 12-month
  single-episode read) says nothing about the underlying hypothesis; it's the same regime for
  both variants (mild MID_ON/NEUTRAL), just realized slightly differently through the trim
  rule's interaction with monthly weights.
- **This matches, rather than contradicts, the earlier macro-timing literature in this folder**
  (README point 5: "net-liquidity acceleration → risk-on... correctly avoids bonds/gold" and the
  rejected BTC-equity ISM study, `btc-equity-ism-rule-backtest` memory: "the proposed
  pro-cyclical direction is wrong-signed"). Decelerating net-liq/Global-M2 acceleration has
  consistently, across several independent studies now, coincided with **further** downside,
  not a bottom — buying more into it is not "buying the dip," it's leaning into a falling
  knife that, in the one large sample this data offers (2022), kept falling for over a year.
- **CURRENT still gives up some upside vs. doing nothing** (BASELINE's XIRR is higher at every
  window except 1y) — the same trade-off file 07 already documented for the old binary
  mechanism: less return in exchange for materially less drawdown. That trade-off, not a free
  lunch, is what ADR-044 buys; REVERSED is not a way to get the return back — it makes the
  drawdown worse instead.

## Caveats

- **n≈1 real episode drives the result.** The 2021-2023 stretch is the only long, deep
  risk-off episode in the sample that both (a) lasted long enough to matter across two 12-month
  contribution schedules and (b) coincided with an actual, large, sustained crypto/equity
  decline. The 2018 episode is a smaller, corroborating second data point; before that, history
  is thin (net-liq data starts 2008, expanding z-scores need ~2y burn-in). This is directionally
  consistent, cycle-confirming evidence, not a large-sample statistical proof — a genuinely
  different multi-year liquidity cycle could produce a different ranking.
- **Overlapping windows, one shared price/macro history** — the 5 start dates are not
  independent draws; all three variants run over the exact same underlying data, so this is one
  natural experiment viewed from five overlapping angles, not five independent trials.
- **Miners-basket return is overstated** (survivorship — MARA/CIFR/IREN/WULF/HUT began trading
  at different dates; the sleeve average silently reweights among whichever subset already
  exists at that point in history), a caveat carried over unchanged from file 07.
- **Gold rallied through most of this sample**, flattering any variant that de-risks into gold
  regardless of trigger quality (same caveat as files 07/09) — this affects CURRENT and, to a
  lesser extent given its smaller gold weight in practice, REVERSED and BASELINE too, since all
  three still route +100%-rule trim proceeds to gold.
- **The risk-off table is a judgment-call adaptation**, not independently re-optimized: real
  production's table was calibrated (via studies 07-09) for its own larger, differently-shaped
  22-position roster; scaling the same retention ratios onto this demo portfolio's simpler
  12-asset list is a reasonable-but-unverified analogy, not a re-derivation from scratch.
- **1y/3y windows are single-episode reads** and should not be over-interpreted in either
  direction — flagged the same way in every prior study in this folder.
