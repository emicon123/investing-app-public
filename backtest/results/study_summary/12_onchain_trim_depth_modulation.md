# 12 — On-chain-modulated trim depth for the shipped +100% rule

Script: `onchain_trim_depth_modulation.py` → `results/onchain_trim_depth_modulation.md`.
Follow-on to study #11 / file 10. Context: using on-chain euphoria (SOPR/NUPL) as an
**independent** sell/de-risk trigger — alone, or OR/AND-fused with the macro gate —
was already tested in file 10 and lost outright to the macro-only gate, largely
because on-chain-euphoria and macro-risk-off barely co-occur (2/48 months there).
This study tests a narrower, different idea instead: **don't add a new trigger at
all — use on-chain data only to set how DEEP the existing, purely price-based
+100% rule trims** (AGENTS.md: asset ≥ 2x target → trim to the target/actual
midpoint, e.g. 10%→20% trims to 15%, proceeds → GOLD). This reacts to an
already-realized overshoot rather than trying to call a top in advance.

Method: reused the exact multi-asset portfolio sim from `portfolio_strategy_backtest.py`
(same TARGET dict/RET construction/trigger condition), macro cut **disabled**
(cut=1.0) to isolate this question from the already-tested macro overlay. Primary
euphoria signal = derived NUPL (Coinmetrics-sourced, BTC 2010-07+/ETH 2015-08+,
same pipeline as files 08/10/11) → no-lookahead expanding percentile rank vs its
own history (12mo burn-in), "euphoric" = percentile ≥ 0.75. Only BTC_spot/ETH are
conditioned (the only sleeve members with a free on-chain read); every other asset
(miners, SOL, COIN, SUI, DOGE, MSTR, ...) always uses the standard fixed trim.
Native SOPR/NUPL (2022-07+) used only as a corroborating cross-check per the task
brief, not the primary test (too short a window on its own).

## Headline: real trigger events are rare, clustered, and have stopped firing entirely since 2021

**Every BTC_spot/ETH +100% trigger-fire event since BTC's 2010-07 inception: 18
total (15 BTC, 3 ETH)** in a continuous single-portfolio simulation. Of those, only
**14 have a valid on-chain reading** (4 predate the 12-month percentile burn-in) —
**6 fired in a "euphoric" state (percentile ≥0.75), 8 in a "normal" state.** They
cluster into exactly **three episodes**: the 2013 top (4 fires), the 2017 top (5
fires), and the 2021 top (5 fires) — this is fundamentally an n=3-cycle-tops
question, not n=14 independent draws, the same small-sample structure file 08
already flagged for raw MVRV-percentile.

**No BTC_spot/ETH fire has occurred since 2021-04 — over 5 years.** The 3y and 1y
AGENTS.md start-date windows contain **zero** on-chain-eligible fires; the 5y window
contains exactly **one** (a marginal ETH fire, non-euphoric). Practically: this
entire modulation idea would have made **zero difference** to the portfolio over
the period most relevant to current/near-term decisions, and won't matter again
unless/until BTC or ETH stages another blow-off run that outpaces the now much
larger, more diversified contribution base (see caveat below on why fires have
gotten rarer).

## 1. Full event inventory (2010-07 → 2026-06, baseline 1.5x trim throughout)

| month | asset | ratio (actual/target) | NUPL percentile | euphoric? |
|---|---|--:|--:|---|
| 2010-11 | BTC_spot | 2.04x | n/a (pre burn-in) | n/a |
| 2011-02 | BTC_spot | 2.60x | n/a | n/a |
| 2011-05 | BTC_spot | 3.70x | n/a | n/a |
| 2011-06 | BTC_spot | 2.68x | n/a | n/a |
| 2011-07 | BTC_spot | 2.29x | 0.08 | no |
| 2013-03 | BTC_spot | 2.68x | 0.85 | **YES** |
| 2013-04 | BTC_spot | 2.85x | 0.68 | no |
| 2013-05 | BTC_spot | 2.06x | 0.63 | no |
| 2013-11 | BTC_spot | 2.06x | 0.90 | **YES** |
| 2013-12 | BTC_spot | 3.84x | 0.64 | no |
| 2017-08 | BTC_spot | 2.32x | 0.86 | **YES** |
| 2017-09 | ETH | 2.00x | 0.42 | no |
| 2017-11 | BTC_spot | 2.34x | 0.88 | **YES** |
| 2018-01 | BTC_spot | 2.12x | 0.56 | no |
| 2018-02 | ETH | 2.49x | 0.42 | no |
| 2021-02 | BTC_spot | 2.01x | 0.87 | **YES** |
| 2021-04 | BTC_spot | 2.16x | 0.85 | **YES** |
| 2021-09 | ETH | 2.06x | 0.61 | no |

## 2. Corroborating check: native NUPL (2022-07+) vs derived percentile

Overlap window 2022-07..2026-07 (49 months): native NUPL≥0.5 vs derived
percentile≥0.75 agree in **78%** of months — consistent (both read the same
underlying quantity), not identical, as expected from an absolute vs relative
cutoff. **Zero BTC_spot/ETH fires occurred inside the native-covered window**
(2022-07+) — there is nothing to corroborate with the short native series in this
particular test; the derived series (§1) carries the entire analysis.

## 3. Portfolio-level test: does modulating trim depth change XIRR/drawdown?

Macro cut disabled; buy-only + +100%-rule only. Each cell = XIRR%/yr (maxDD%).

| mode | 13y | 8y | 5y | 3y | 1y |
|---|--:|--:|--:|--:|--:|
| baseline (fixed 1.5x, shipped) | 39 (−35) | 40 (−43) | 24 (−48) | 25 (−23) | −5 (−17) |
| deeper on euphoric only (euphoric→1.0x, normal→1.5x) | 38 (−33) | 41 (−39) | 24 (−48) | 25 (−23) | −5 (−17) |
| deeper still on euphoric (euphoric→0.75x, normal→1.5x) | 36 (−34) | 41 (−38) | 24 (−48) | 25 (−23) | −5 (−17) |
| shallower on normal only (euphoric→1.5x, normal→1.75x) | 40 (−33) | 39 (−45) | 24 (−48) | 25 (−23) | −5 (−17) |
| **skip normal fires entirely** (euphoric→1.5x, normal→no trim) | 41 (**−62**) | 37 (**−52**) | 24 (−48) | 25 (−23) | −5 (−17) |
| both (euphoric→1.0x, normal→1.75x) | 39 (−32) | 40 (−42) | 24 (−48) | 25 (−23) | −5 (−17) |

Fires actually touched by the modulation, per window (this is the real n behind
each column):

| window | euphoric fires | normal fires | pre-burn-in |
|---|--:|--:|--:|
| 13y | 5 | 5 | 0 |
| 8y | 2 | 1 | 0 |
| 5y | 0 | 1 | 0 |
| 3y | 0 | 0 | 0 |
| 1y | 0 | 0 | 0 |

3y and 1y are entirely unaffected by construction (zero eligible fires — every mode
is numerically identical to baseline). 5y has one thin fire and is essentially flat
too. **Only 13y and 8y carry any signal**, and there it's driven by 5-6 events each.

## 4. Proxy check: forward BTC drawdown, in-territory months, NUPL-state split

Broader/less strict than the discrete fire events: every month BTC's simulated
portfolio weight is at/above 1.9x its target (23 months, 2010-11..2021-04; 17 with
a valid percentile), split by NUPL state, forward maxDD:

| horizon | euphoric (n) | mean fwd maxDD | normal (n) | mean fwd maxDD |
|---|--:|--:|--:|--:|
| 3m | 11 | −28.0% | 6 | −37.6% |
| 6m | 11 | −38.6% | 6 | −43.6% |
| 12m | 11 | −54.3% | 6 | −62.6% |

Counter-intuitively, the euphoric bucket shows a *smaller* subsequent drawdown than
the normal bucket at every horizon. **Not read as a real "euphoria = safer" effect**
— the "normal" bucket (n=6) is dominated by 2011-07 (right before the −93% 2011-12
BTC bear market) and 2013-12 (the exact Dec-2013 top, followed by the −87%
2014-2015 crash), both of which happen to sit just under the 0.75 percentile cutoff
by a small margin. With only 6 normal-state months, two of history's worst BTC
drawdowns dominate the average — this reads as a small-sample artifact, not
evidence that non-euphoric overshoots are more dangerous. In-territory months also
heavily overlap (the same overshoot episode spans several consecutive months), so
even n=17 is far fewer than 17 independent observations.

## Verdict

- **"Trim deeper when euphoric" (the core hypothesis): plausible-but-unproven, not
  rejected.** At the only two windows with enough events to say anything (13y,
  8y), deeper-on-euphoric modes shift XIRR by ≤3pp and maxDD by ≤5pp vs baseline —
  small, inconsistent in sign between windows (13y: return −1 to −3pp / DD +1 to
  +2pp; 8y: return +1pp / DD +4 to +5pp), and entirely driven by **n=5-6 events
  clustered at exactly the well-known 2013/2017/2021 tops** — the same "the model
  correctly labels the known tops and nothing else" pattern file 08 already flagged
  for raw MVRV-percentile. **Not enough data to call this a real edge.**
- **The reverse (loosen the trim on non-euphoric fires): consistently bad, more
  confident conclusion.** Every "shallower/skip on normal-state fires" variant
  degrades max drawdown at both testable windows, and **skip-normal is badly
  worse** (13y: −35%→−62%; 8y: −43%→−52%) for only a marginal XIRR gain. This holds
  in both windows in the same direction, which is a more reliable signal than the
  "deeper" side despite the small n — **do not weaken the shipped +100% rule just
  because on-chain isn't showing an extreme reading; the fixed trim is already
  doing useful risk-management work on its own.**
- **The trigger has not fired for BTC or ETH since 2021-04 (5+ years) in this
  simulation.** As the sleeve has filled out (COIN 2021, SUI 2023, and a much
  larger cumulative contribution base spread across all 11 targets vs. the
  early-history portfolio where BTC absorbed a much bigger share of contributions
  under present-asset renormalization), sustained 2x overshoots have become
  structurally rarer — this modulation question is **currently moot** and would
  only become relevant again after a BTC/ETH rally large enough to outpace the
  now-much-larger diversified base.
- **Net recommendation: do not implement euphoria-conditioned trim depth.** The
  "trim harder" side isn't proven (too few, too clustered events to trust); the
  "trim softer" side is actively discouraged by the data; and the mechanism
  currently has near-zero opportunities to fire anyway. If revisited, it would need
  several more cycle-top observations (i.e. years) before the "deeper" side could
  be evaluated with any statistical confidence — flagging this as a legitimate
  "not enough data to tell" outcome rather than forcing a rule from 5-6 events.

## Caveats

Same one-cycle-per-episode structure as files 08/10/11: n=6 euphoric / 8 normal
fires span only 3 real historical episodes, not 14 independent draws; the
proxy/in-territory check (§4) further overlaps consecutive months within each
episode, inflating apparent n well beyond genuine independent information; macro
de-risk is deliberately disabled throughout to isolate this question, so these
numbers are not directly comparable to the shipped-config XIRR/DD figures in files
07/09 (which run with cut=0.33); BTC's target weight (17.5%) and the sleeve
composition have both been assumed static across the full 2010-2026 window for
this test even though the real target roster/weights evolved over time (ADR-044
sliding macro-regime targets not modeled here) — a simplification consistent with
prior studies (07/09/11) but worth flagging since it affects exactly how the +100%
threshold is computed decade over decade.
