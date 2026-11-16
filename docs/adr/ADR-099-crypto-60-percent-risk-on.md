# ADR-099 — Scale the MAIN CRYPTO bucket's risk-on target to 60%, funded by EQUITY

**Date:** 2026-09-26
**Status:** Accepted
**Related:** ADR-044 (sliding risk-on/risk-off blend), ADR-048 (§7.5 manual sell-assist
thresholds), ADR-067 (one-decimal rounding of an EQUITY split), ADR-074 (last CRYPTO
reshuffle), ADR-087 (current three-ticker EQUITY bucket)

## Context

The owner decided (2026-09-26) to lean the MAIN portfolio harder into crypto in the **risk-on**
regime: the CRYPTO bucket's risk-on target goes from Σ50.0 to Σ60.0. This is a strategy
decision about target weights, not a change to the rebalancing algorithm.

Two constraints came with it:

- **Proportions inside CRYPTO stay as they are.** Every CRYPTO asset is multiplied by exactly
  1.2 (60/50), so BTC:ETH:SOL:… ratios set by ADR-074 are preserved and every new value is
  exact at one decimal.
- **Risk-off is not touched.** CRYPTO stays Σ15.0 and EQUITY Σ42.5 risk-off; the de-risked
  shape of the book is the owner's deliberate floor and does not move with this decision.

Both regime totals must stay exactly 100.0, so the +10.0pp has to come from somewhere in
risk-on. Gold (20.0) and Experiments (2.5) were left alone and Bonds are already 0.0 risk-on,
leaving EQUITY as the funding bucket.

## Decision

1. **CRYPTO risk-on × 1.2** (13 assets, Σ50.0 → Σ60.0):

   | asset | risk-on | risk-off *(unchanged)* |
   |---|--:|--:|
   | BTC-USD | 20.0 → **24.0** | 12.0 |
   | ETH-USD | 10.0 → **12.0** | 1.0 |
   | SOL-USD | 5.0 → **6.0** | 0.5 |
   | COIN | 3.0 → **3.6** | 0.3 |
   | SUI-USDT | 3.0 → **3.6** | 0.3 |
   | LINK-USD | 1.5 → **1.8** | 0.15 |
   | ONDO-USD | 1.5 → **1.8** | 0.15 |
   | CIFR / MARA / HUT / IREN / WULF | 1.0 → **1.2** each | 0.1 each |
   | DOGE-USD | 1.0 → **1.2** | 0.1 |
   | **Σ CRYPTO** | **50.0 → 60.0** | **15.0** |

2. **EQUITY risk-on funds it pro-rata** (Σ27.5 → Σ17.5, factor 17.5/27.5 ≈ 0.636), rounded to
   one decimal as ADR-067 did — the rounded figures still close exactly at 17.5:

   | asset | risk-on | risk-off *(unchanged)* |
   |---|--:|--:|
   | SXRV.DE (Nasdaq-100) | 22.5 → **14.3** | 27.5 |
   | ICGA.DE (MSCI China) | 2.5 → **1.6** | 7.5 |
   | QDV5.DE (MSCI India) | 2.5 → **1.6** | 7.5 |
   | **Σ EQUITY** | **27.5 → 17.5** | **42.5** |

   Pro-rata was chosen here (rather than a 50/50 or owner-set split) so the Nasdaq : China :
   India ratio inside EQUITY is kept, up to rounding. As ADR-067/ADR-087 already record, there
   is no standing convention for splitting EQUITY swings; this is this revision's own choice.

3. **Grand totals:** risk-on Crypto 60.0 + Gold 20.0 + Bonds 0.0 + Equity 17.5 + Experiments
   2.5 = **100.0**; risk-off unchanged at **100.0**.

Applied by `apps/main/db/migration/V77__crypto_60_percent_risk_on.sql`: a data-only,
guarded, idempotent migration (the V76 pattern) that updates `assets.target_allocation_pct`
for the 16 assets above, locating each through any of its MAIN subasset rows. Per asset it
updates from the reviewed old value, no-ops if already at the new value, and otherwise raises
(whole migration rolls back). It also asserts each asset's explicit
`target_allocation_pct_risk_off` equals the reviewed value — so no NULL "falls back to
risk-on" row can make risk-off move silently — and post-checks that both regime totals are
unchanged. It is gated on the presence of a MAIN `ONDO-USD` row, the same live-book gate V68
uses.

## Consequences

- **The §7.5 sell-assist trim trigger scales with the targets.** The 1.5× flag threshold is
  relative to each asset's effective target, so at full risk-on BTC-USD now flags at 36%
  (was 30%), ETH-USD at 18% (was 15%), a mining stock at 1.8% (was 1.5%). Conversely the
  EQUITY assets flag much earlier risk-on: SXRV.DE at ~21.5% (was 33.75%). The risk-off
  thresholds (BTC 18%, etc.) are unchanged.
- **The risk-on/risk-off spread widens, so the blend moves more weight.** CRYPTO's gap between
  regimes grows from 35.0pp to 45.0pp and EQUITY's from 15.0pp to 25.0pp. Any given drop in
  `blend_factor` (ADR-044/071) now shifts about 29% more weight out of crypto and into equity
  than before; a fully risk-off book is identical to what it was.
- **Monthly DCA in risk-on tilts toward crypto.** With higher crypto targets and lower equity
  targets, the buy-only DCA (ADR-045) will steer new cash into crypto and away from equity
  until the book converges; no selling is triggered automatically. Equity currently held above
  its new, lower risk-on target will show as overweight and may surface the §7.5 flag.
- **From-scratch builds are unaffected.** Testcontainers suites and disaster-recovery rebuilds
  have no MAIN `ONDO-USD` row, so V77 emits a NOTICE and changes nothing there; seed data and
  test expectations built on the pre-ADR-099 targets stay valid.
- No schema change, no code change, no API contract change; RETIREMENT and the sibling apps are
  untouched. The blend mechanics (§7.2) and the buy-only invariant (ADR-045) are unaffected.
