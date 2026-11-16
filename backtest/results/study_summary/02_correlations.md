# 02 — Correlations (co-movement)

Scripts: `macro_correlation.py`, `macro_yoy_correlation.py`, `macro_study.py`
Raw: `results/macro_correlation.md`, `macro_yoy_correlation.md`, `macro_study.md`

## What was tested
Spearman of asset returns vs each macro driver, on **levels (rejected as spurious)**,
**MoM changes**, and **YoY changes**; with block-bootstrap CIs, HAC elasticity, ex-COVID
robustness, and a cross-asset liquidity-beta baseline.

## Headline — asset YoY return vs macro YoY (Spearman, current data)

| driver | BTC | ETH | SOL |
|---|--:|--:|--:|
| **Global M2 YoY** | +0.55 ✓ | +0.58 ✓ | +0.46 |
| **Fed net liquidity YoY** | +0.56 ✓ | +0.62 ✓ | +0.78 ✓ |
| US M2 YoY | +0.35 ✓ | +0.47 ✓ | +0.51 |
| ISM level | +0.30 | +0.48 | +0.43 |

✓ = bootstrap 95% CI excludes 0.

## Outcomes
- **Global M2 & Fed net liquidity are the dominant, significant co-movers.** US M2 is
  moderate; **ISM is not significant** (CIs include 0).
- **Monotonic in beta** (BTC < ETH < SOL) — crypto is a geared expression of liquidity.
- **Non-linear**: Spearman ≫ Pearson (e.g. BTC×GlobalM2 Pearson 0.22 vs Spearman 0.55);
  HAC-OLS slopes for BTC/ETH have low t / R²≈0.05–0.10. Use ranks; don't expect a tight
  linear elasticity.
- **Low-frequency**: MoM correlations are weak (mostly 0.0–0.27). It's an annual/regime link.
- **Robust ex-COVID**: dropping 2020-03…2021-12 holds or strengthens the Global-M2/net-liq
  correlations (BTC 0.55→~0.70 in the original cut) → structural, not just the COVID surge.
  US M2's link weakens (more COVID-driven).
- **Small-sample caution proven**: SOL × Global-M2 was 0.92 (n=32, to 2023-11) → **0.46
  (n=61)** once data was extended to 2026 — the early number was a one-cycle artifact.
- **Liquidity beta vs benchmarks**: SPY/GLD ≈ 1.6–1.7 per 1pp global-M2 YoY (tight, R²≈0.3–0.5);
  crypto ≈ 50× larger but noisy (BTC b≈95, R²≈0.05).

## Verdict
Liquidity is a **real, significant, structural co-mover** with crypto — strongest for higher
beta — but **contemporaneous and non-linear**. For timing you must look at *leads* (file 03),
not contemporaneous correlation.
