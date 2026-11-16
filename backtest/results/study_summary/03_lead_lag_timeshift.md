# 03 — Time-shift (lead / lag): who leads whom?

Scripts: `macro_weekly_leadlag.py`, `macro_leadlag_recheck.py`, `macro_leadlag_m2.py`
Raw: `results/macro_weekly_leadlag.md`, `macro_leadlag_recheck.md`, `macro_leadlag_m2.md`

## What was tested
For each driver, the full lead/lag profile (driver shifted k vs asset) and the **predictive**
form (driver(t) → asset *forward* return over h months), with bootstrap CIs. Net liquidity
also scanned **weekly** (−13…+52 w). Windows scanned, not just one — directly answering "which
shift works": net-liq momentum {4,8,13,26,52}w; M2 acceleration Δ3m/Δ6m; horizons h=1..12m.

## Net liquidity — leads ETH/SOL, not BTC ✓
- **Lead/lag** (net-liq YoY → asset YoY): BTC peaks **0w/0m**, ETH **~8w / 1m**, SOL **~11–12w / 3m**.
- **Predictive** (net-liq 3m momentum → next-3m return): **ETH +0.33 ✓, SOL +0.54 ✓**, BTC +0.16 (ns).
- Read: BTC contemporaneous (risk-state only); ETH/SOL have a genuine ~1–3-month lead.

## ISM PMI — does NOT lead crypto ✗
- ISM level/Δ → forward 3m return ≈ 0 for all (BTC +0.01, ETH −0.04, SOL −0.11; CIs include 0).
- The ISM-vs-asset correlation is **higher at negative lags** (ETH: k=−3m 0.63 vs k=0 0.48) →
  **crypto leads ISM by ~3 months**. ISM is a coincident/lagging *confirmer*, not a signal.

## M2 YoY — the *level* doesn't lead BTC, the *acceleration* does ✓ (BTC's first forward edge)
- M2 YoY **level** → forward returns ≈ 0 (Global M2 → BTC 3m = −0.00; level is contemporaneous).
- **Global M2 YoY acceleration (Δ3m) → forward return:**

  | → next | BTC | ETH | SOL |
  |---|--:|--:|--:|
  | 3 months | +0.23 ✓ | +0.30 ✓ | +0.26 |
  | best-horizon | ~2m +0.25 ✓ | ~3m +0.30 ✓ | ~12m +0.59 ✓ |

- **Specifically Global** M2 (US M2 acceleration is weaker / ns). Magnitudes modest; SOL's
  long-horizon numbers ride overlapping windows + short sample (suggestive, not solid).

## Summary table
| Driver | Leads BTC? | Leads ETH? | Leads SOL? |
|---|---|---|---|
| Liquidity / M2 **level** | no (coincident) | ~1 mo | ~3 mo |
| Net-liquidity **momentum** | no | yes (~8w) ✓ | yes (~12w) ✓ |
| **Global M2 YoY acceleration** | **yes ~2–3 mo ✓** | yes ~3 mo ✓ | longer-horizon ✓ |
| ISM PMI | no (crypto leads it) | no | no |

## Verdict
"M2/liquidity leads BTC by ~12 weeks" is **false for the level** but **true for the
acceleration** (~2–3 mo). For ETH/SOL even the level/momentum leads. ISM is not a leading
indicator. Tradeability of these leads → file 04.
