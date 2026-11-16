# 07 — Portfolio strategy (REAL algo, corrected)

Script: `portfolio_strategy_backtest.py` → `results/portfolio_strategy_backtest.md`.
Uses the **real algo (AGENTS.md): pure-proportional BUY-ONLY DCA + sell only via the +100%
rule** (asset ≥ 2× its *current* target → trim toward target → gold). DCA 1000 PLN + 1000/mo,
PLN, 15 bps, **5 start dates (1/3/5/8/13 yr)** to defuse start-timing luck. (Earlier runs that
used full-rebalance-with-selling are superseded — that's not the real algo.)

## A. Trim-rule tuning (no macro)
Buy-only with no trim → high return but brutal drawdown (13y −80%, 8y −73%). Adding the **2×
trim → gold roughly HALVES drawdown** (13y −38%, 8y −44%) at little/no return cost — confirmed
across all start dates. 1.5×–2× is the sweet zone; trimming *to 1.5× target* (≈ current
"halfway") vs *to 1.0× target* barely differs. **Keep the 2× trim, trim to ~1.5× target.**

## B. Dynamic-target macro de-risk (the faithful model — how the user operates)
The macro "start selling" signal **lowers the crypto targets (×cut)**; the existing +100% rule
then fires on the now-overshot positions and sells into gold, and contributions buy less crypto.
risk-off = net-liq accel < 0 **AND** Global-M2 accel < 0 (lagged). Cell = XIRR%/yr (maxDD%):

| config | 13y | 8y | 5y | 3y | 1y |
|---|--:|--:|--:|--:|--:|
| NO de-risk (trim→1.5×) | 39 (−35) | 40 (−43) | 24 (−48) | 25 (−23) | −5 (−17) |
| cut×0.5 (moderate) | 35 (−28) | 38 (−21) | 26 (−31) | 29 (−15) | −3 (−11) |
| **cut×0.33 (aggressive)** | 33 (−24) | 37 (**−14**) | 27 (−18) | 31 (−11) | **+3 (−8)** |

**Outcome:** de-risking by lowering crypto targets **slashes drawdown across every start date**
(aggressive cut takes 8y −43%→−14%, 5y −48%→−18%) while **holding mid-horizon return (27–31%/yr,
3–5y)**; it even flips the recent 12-month window from −5% to +3%. The give-up is only in the
very-long windows (you trim some 2013–17 explosions). Trim depth (1.5× vs 1.0×) is a wash.

## Verdict / recommendation
- **Keep buy-only + 2×-trim→gold** — the main risk control; well-calibrated.
- **Adopt the dynamic-target macro de-risk** — it's the macro overlay that finally works *and*
  fits the existing rule (no new sell mechanic; just lower the crypto targets). Aggressiveness is
  a risk dial: **×0.5 balanced, ×0.33 strong peak-protection** (lean ×0.33 for a crypto book that
  wants to sell the turn).
- **Remove ISM** — useless/counterproductive (files 02–06).
- It's a *cycle-turning* detector, not an exact top-tick → **on-chain (next study) sharpens
  the top timing**; fuse macro + on-chain into the target-lowering trigger.

## Caveats
Gold rose through every risk-off window (flatters crypto→gold — split proceeds gold/cash for
regime-neutrality); the **miners basket return is overstated**; overlapping windows / one
underlying price history; the signal will occasionally whipsaw (here it cost little).

## 2026-09-25 correction note (task 28, second pass — re-run, verdict unchanged)
Re-ran under the refreshed `portfolio_spec.py` (live ADR-074/087 weights: QQQ 22.5/27.5,
FXI 2.5/7.5, ETH 10.0/1.0, LINK-USD 1.5/0.15 added, no CASH bucket) with the CURRENT
1.5x/1.0x trim held fixed; `USDPLN_X.csv` restored to full 2003→ history (the 2b03ed0
refresh had truncated it at 2017-08, breaking 13y windows); END 2026-09 (all five windows
shifted ~+3mo vs the 2026-06 pass). Numbers moved with the windows/weights (e.g. 13y
cut×0.33 CURRENT 33 (-19) vs 37 (-34) baseline, was 33 (-19) vs 39 (-34)); the ordering
and the recommendation above are intact — cut×0.33 CURRENT remains the peak-protection
pick, PRIOR trim stays reference-only.
