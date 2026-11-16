# ADR-067 — Round the EQUITY split (NQSE.DE / ICGA.DE)

**Date:** 2026-08-04
**Status:** Accepted
**Corrects:** ADR-066 (same session — numeric only)
**Related:** ADR-050, ADR-051 (the earlier same-session numeric correction this follows in shape)

## Context

ADR-066 deleted the PLN cash position and routed its 5.0 percentage points into EQUITY
**pro-rata**, specifically to avoid drifting the NQSE.DE : ICGA.DE ratio as a side effect of
a cash-removal change. That reasoning was sound but the arithmetic was ugly: scaling by
27.5/22.5 and 42.5/37.5 produced repeating decimals stored at 4 dp —
`19.5556 / 7.9444` risk-on and `26.6333 / 15.8667` risk-off.

Targets are read by a human every month when planning purchases. Four-decimal repeating
figures carry no information the round numbers don't, and they make the target table harder
to sanity-check by eye against the 100.0 total.

## Decision

The user set the four numbers directly, in the same working session ADR-066 landed:

| | risk-on | risk-off |
|---|--:|--:|
| NQSE.DE (Nasdaq-100) | 19.5556 → **20.0** | 26.6333 → **27.5** |
| ICGA.DE (MSCI China) | 7.9444 → **7.5** | 15.8667 → **15.0** |
| **Σ EQUITY** | **27.5** *(unchanged)* | **42.5** *(unchanged)* |

The bucket totals are untouched, so **no other position moves** and both grand totals still
sum to exactly 100.0000. Crypto (50.0 / 15.0, all internal relations intact), Gold
(20.0 / 30.0), Bonds (0.0 / 10.0) and Experiments (2.5 flat) are all unaffected.

**This is a numeric correction, not a policy change**, and it is recorded separately rather
than folded into ADR-066 because V50 had already been applied to the live database —
rewriting an applied migration breaks Flyway's checksum validation on the next boot. The
same situation, and the same resolution, as ADR-051's correction of ADR-050.

## Consequences

**The ratio moves, deliberately.** ADR-066 went out of its way to preserve it; this ADR
trades that for round numbers, with the user choosing the figures:

| | pre-ADR-066 | ADR-066 | **ADR-067** |
|---|--:|--:|--:|
| risk-on NQSE : ICGA | 2.46 | 2.46 | **2.67** |
| risk-off NQSE : ICGA | 1.68 | 1.68 | **1.83** |

Slightly more Nasdaq and less China in both regimes. The qualitative shape ADR-050
established survives — China still carries relatively more weight risk-off than risk-on,
and NQSE.DE/ICGA.DE still both rise going into risk-off rather than carrying ADR-047's
inverted risk-off premium.

Note this supersedes ADR-066's stated preference for pro-rata splits. The standing position
is now the one ADR-051 already articulated: **there is no fixed convention** for dividing a
swing between these two tickers — not 50/50, not pro-rata. Each revision picks numbers on
its own merits, and the user sets them when they care about the outcome.

Data-only change to 2 `positions` rows (`V51__equity_split_round_numbers.sql`). No schema
change, no code change, and it does not touch the buy-only invariant (ADR-045) or the blend
mechanics (§7.2 of `05-rebalancing-algorithm.md`).
