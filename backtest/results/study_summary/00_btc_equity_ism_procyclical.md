# Backtest note — ISM-conditioned BTC-equity (miners + MSTR) sub-cap

**Date:** 2026-06-18
**Question:** Within total BTC exposure (BTC coin + miners + MSTR), should the equity
sleeve (BTC miners + MSTR) be **pro-cyclically** tied to ISM PMI — at most ~25% of BTC
exposure when ISM is very high, ~10% when ISM is very low — and is it worth adding **IREN**
and **WULF** as miners?

**Verdict: NO to the ISM timing rule (proposed direction is the wrong sign). If you want
miners+MSTR at all, use a single fixed cap chosen by risk tolerance, ISM-independent.**

Artifacts: `btc_equity_ism_backtest.py`, `results/btc_equity_ism_summary.json`.
Method mirrors ADR-040: measure the signal (IC), then the portfolio impact (DCA). USD
throughout (BTC and the sleeve are both USD, so the within-sleeve split is FX-invariant).

## What the sleeve is
Equal-weight, monthly-rebalanced basket of {MSTR, MARA, HUT, CIFR, IREN, WULF}, each
included only from when it actually *is* a BTC vehicle (MSTR from 2020-08 BTC treasury;
MARA from 2021 rebrand; CIFR from 2021-08 SPAC; IREN 2021-11; WULF 2021-12 — predecessor
ticker history excluded). COIN is a crypto *exchange*, not a miner/MSTR → excluded.
Premise confirmed: the sleeve is **levered BTC** — beta to BTC ≈ **1.3–1.4**, corr ≈ 0.73.

## Part A — Signal (does ISM predict the miners+MSTR − BTC spread?)
No-lookahead: signal at month start = ISM *published* just before (prior month's print).

| Window | Spearman IC (ISM→spread) | t | mean spread, ISM≥55 | mean spread, ISM<50 |
|---|---|---|---|---|
| 2021-12+ (IREN/WULF live, n=55) | **−0.237** | −1.77 | **−9.9%/mo** (n=7) | **+8.0%/mo** (n=35) |
| 2020-09+ (n=71) | −0.147 | −1.23 | +1.4%/mo (n=22) | +8.0%/mo (n=35) |

- IC is **weak and NEGATIVE** — i.e. higher ISM precedes the sleeve *under*-performing BTC.
  That is the **opposite** of the pro-cyclical thesis, and not statistically significant.
- The proposed rule wants more miners when ISM is high; the sample says miners do *worse*
  than BTC when ISM is high.

## Part B — DCA sleeve backtest ($100/mo, buy-only, split BTC vs sleeve)

| scheme | 2021-12+ ROI / IRR / maxDD / ROI÷\|DD\| | 2020-09+ ROI / IRR / maxDD / ROI÷\|DD\| |
|---|---|---|
| btc_only | 57% / 20% / −38% / 1.5 | 70% / 18% / −53% / 1.3 |
| eq_25 (static) | 221% / 54% / −33% / 6.7 | 235% / 42% / −62% / 3.8 |
| eq_40 (static) | 319% / 68% / −33% / 9.7 | 335% / 52% / −66% / 5.1 |
| **ism_pro (proposed)** | **144% / 41% / −34% / 4.3** | **172% / 35% / −61% / 2.8** |
| ism_contra | 199% / 51% / −33% / 6.0 | 200% / 38% / −57% / 3.5 |

- **ism_pro < ism_contra** in both windows → the proposed direction is the wrong sign
  (consistent with the negative IC).
- **Both ISM-conditioned schemes are beaten by a plain static fraction** (eq_25) → ISM
  conditioning adds nothing over picking one number. Same lesson as ADR-040's regression band.
- In-sample, return rises *monotonically* with the static equity fraction. That is a
  **re-rating artifact, not a signal**: over this window MSTR (mNAV premium) and IREN/WULF
  (AI/HPC pivot) re-rated far above BTC. The long window shows the cost — at high equity the
  2022 miner collapse drives **−66% drawdown** at 1.3–1.4× beta.

## Why we can't "prove" the rule (and shouldn't try to fit it)
- **One confounded cycle.** Usable history is ~5 yrs (2021–2026). ISM was high at the
  2021/early-2022 crypto top and low through the 2023–2024 recovery, so **ISM is confounded
  with the crypto cycle** — the "high ISM = bad" result is really "cycle top = bad." Only 7
  high-ISM months exist in the clean window. No ISM schedule can be robustly estimated here.
- **IREN/WULF are decoupling from BTC** (AI/HPC datacenter pivot); classifying them as pure
  "BTC exposure" is increasingly wrong. MARA/CIFR/MSTR are cleaner BTC beta.
- Absolute ROIs are survivorship fantasy (DCA into today's winners); only relative rankings count.

## Recommendation
1. **Drop the ISM-conditioned miner tilt.** No predictive signal; proposed direction is
   backwards; doesn't beat a static number. (If any ISM link were kept it would have to be
   *contrarian*, but the evidence is too thin/confounded to justify even that.)
2. If you want miners+MSTR exposure, express it as a **single fixed cap** (a risk control on
   levered-BTC concentration), sized by how much 1.3–1.4× beta / ~−60% drawdown you'll tolerate
   — not as an ISM schedule. A modest cap (e.g. ≤15–20% of BTC exposure) keeps the existing
   "BTC ≥ 50% of crypto" rule intact without betting on an unrepeatable re-rating.
3. Keep IREN/WULF small and reconsider whether they belong in the BTC bucket vs an AI/infra bucket.
4. Requires finer categorization than today (`exposure=CRYPTO` is too coarse): a tag separating
   BTC-core / BTC-equity / altcoins / experiments — only needed if you implement a cap.
