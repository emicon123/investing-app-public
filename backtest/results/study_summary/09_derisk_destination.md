# 09 — De-risk destination: GOLD vs bond ETFs (ETFBCASH.WA / ZPR1.DE)

Script: `derisk_destination_backtest.py` → `results/derisk_destination_backtest.md`.
User idea: when the +100% rule / macro dynamic-target de-risk (file 07, cut×0.33)
trims crypto, route proceeds to bond ETFs instead of gold. Same real algo as 07 —
only the trim destination varies.

## The two tickers, as fetched (Yahoo, full daily history)
- **ETFBCASH.WA** (Beta ETF, WSE, PLN) — **NOT a cash-equivalent** despite the name:
  a real-duration PLN treasury/bond fund. Fell **−23% through 2022** (rate-hike year),
  flat 2023, +5-6%/yr 2024-26. Data from 2021-06-07 (5.1y).
- **ZPR1.DE** (SPDR Bloomberg 1-3 Month T-Bill UCITS ETF, XETRA, USD) — genuinely
  near-cash: steady **~4-5%/yr, near-zero drawdown** the whole sample. Data from
  2023-11-13 (2.6y) — Yahoo's meta reports this share class in **USD**, not EUR,
  despite the Xetra listing.

## Result: GOLD beats both BONDS and MIX on every window, return AND drawdown
| destination | 13y | 8y | 5y | 3y | 1y |
|---|--:|--:|--:|--:|--:|
| **GOLD (shipped, ADR-042)** | 33 (−24) | 37 (−14) | 27 (**−18**) | 31 (−11) | 3 (−8) |
| BONDS (ETFBCASH+ZPR1) | 32 (−24) | 35 (−17) | 22 (**−22**) | 23 (−11) | 2 (−7) |
| MIX (50/50) | 32 (−24) | 36 (−15) | 25 (−20) | 27 (−9) | 2 (−7) |

Cells = XIRR%/yr (maxDD%). Even the 13y/8y windows diverge from GOLD (not identical,
as first assumed) — every simulation runs through to *today*, so trims occurring
after each ticker's 2021/2023 inception route into bonds even inside a long window;
only pre-inception trims fall back to gold.

**Why bonds lose:** gold had a historic multi-year rally through exactly this sample
(2021-2026 — COVID/inflation/de-dollarization tailwind, already flagged as a caveat in
file 07). Diverting proceeds into ETFBCASH or ZPR1 missed that rally. Worse: in the 5y
window, **drawdown gets WORSE, not better** (−18%→−22%) with BONDS, because ETFBCASH
itself carries real duration risk and drew down −23% in 2022 — exactly the kind of
correlated-drawdown risk a de-risk destination should avoid, not add.

## Verdict: keep GOLD as the primary de-risk sink; if diversifying, T-bill only
- **Reject ETFBCASH.WA as a de-risk destination.** It has genuine duration risk
  (−23% in a hiking year) — using it to "de-risk" crypto proceeds can add a second,
  correlated drawdown source right when the point is capital preservation.
- **ZPR1.DE (T-bill) is a defensible minority sleeve**, not on backtested return (it
  still lags gold here) but on **structure**: near-zero drawdown, a small yield, true
  diversification away from concentrating 100% of trimmed proceeds into a single asset
  (gold) that is itself near all-time highs after a multi-year run. That's a real,
  qualitative concentration-risk argument a short backtest sample can't price in
  either direction (can't test a regime where gold stalls/corrects while a de-risk
  event happens).
- **Recommendation:** keep gold as the default/majority destination (it's still what
  actually worked, historically and by return). If the user wants bond diversification
  for concentration-risk reasons, a small ZPR1-only sleeve (not ETFBCASH) is the more
  defensible choice — this is a risk-management/preference call, not something the
  data argues for.

## Caveats
Short history (2.6-5y for the tickers) — no rate-hike-cycle stress test for ZPR1
(yields could fall toward 0% in a future cutting cycle, and it's USD-denominated so
carries FX risk versus the PLN reporting currency); only one bond-market drawdown
observed (ETFBCASH 2022); gold's tailwind over this exact sample is a one-off, not
guaranteed to repeat (see file 07).

## 2026-09-25 correction note (task 28, second pass — re-run, verdict unchanged)
Re-ran under the refreshed `portfolio_spec.py` (live weights incl. LINK-USD, no CASH)
with two additional corrections vs. the original pass: the held-fixed trim pair is now
the live CURRENT (1.5x/1.0x, ADR-048) instead of PRIOR (2.0x/1.5x), and contributions
use the real deficit-proportional Q_pos catch-up DCA (the original used a naive
fixed-weight split, against `backtest/AGENTS.md`). `USDPLN_X.csv` restored to full
2003→ history; END 2026-09. Numbers: GOLD ≥ BONDS/MIX on every window (13y 33 vs 32/32;
8y 38 vs 36/37; 5y 29 vs 23/26; 3y 27 vs 20/24; 1y tied -27) — the verdict above
(GOLD wins; ZPR1-only sleeve the more defensible bond choice, if any) is unchanged.
