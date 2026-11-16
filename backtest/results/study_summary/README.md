# Backtest studies — master summary

**This file is the master index for every backtest study in `backtest/`.** Studies
01-16 below are one continuous "Macro ↔ Crypto" investigation thread: what drives BTC /
ETH / SOL — ISM PMI, Fed net liquidity, US M2, Global M2 — do these **co-move**, do they
**lead** (time-shift), and is any of it **tradeable**? A handful of earlier/adjacent
one-off studies that predate or sit outside that thread are indexed in **Other studies**
below.

Each summary file below distills one theme; the raw tables it's built from are the
sibling `results/*.md` files, produced by the scripts in `backtest/`. All correlations
are **Spearman** (crypto returns are fat-tailed) with **block-bootstrap 95% CIs** (✓ =
CI excludes 0). Predictive / timing work is **no-lookahead**.

**Starting a new study (#17+)?** Add a row to the Files table, a line to the Executive
summary, and a numbered `NN_slug.md` file here — see `backtest/AGENTS.md` for the
standing conventions (DCA amount, start-date windows, required baselines).

## Other studies (outside the numbered thread)

| File | Date | Theme | Verdict |
|---|---|---|---|
| [00_regression_weight_scale.md](00_regression_weight_scale.md) | 2026-06-16/17 | is MAIN's geometric regression-band weight scale (×2.0 vs ×1.5/×1.25 vs equal-weight) justified? | **Removed from production** — ADR-040 killed the tilt entirely; ×2.0 never earned its risk |
| [00_btc_equity_ism_procyclical.md](00_btc_equity_ism_procyclical.md) | 2026-06-18 | should the miners+MSTR sub-cap of BTC exposure scale pro-cyclically with ISM? | **Rejected** — IC is weak and wrong-signed; loses to a static fixed cap |
| [00_mexc_demarker_bot_backtest.md](00_mexc_demarker_bot_backtest.md) | 2026-06-18 | historical replay of the live MEXC trading bot (`MexcBotService`/`DeMarkerService`) — unrelated to the MAIN portfolio thread | reference/validation only, see file for ROI by window |
| [00_safe_asset_cushion_sizing.md](00_safe_asset_cushion_sizing.md) | 2026-07-27 | new, standalone (non-MAIN) 66/33 or 75/25 US100/China glide-path: how much Gold+bonds cushion, DCA'd in and manually flipped to target on a -20% drawdown trigger, is needed to break even at month 24 if bought at the worst historical peak? | **25-35% gold-heavy cushion covers every real crash scenario tested** — but DCA alone already breaks even in most of them; the real finding is the fixed -20%-flip rule fails in a slow dot-com-style bear (fires too early, dumps the cushion into a still-falling market) |
| [../macro_pairs.md](../macro_pairs.md) | 2026-06-20 | do pairs of macro signals (AND/OR) beat the single best signal? | no — best pairs ≈ best single (net-liq acceleration); not folded into a numbered writeup |
| [../portfolio_crypto_tilt_backtest.md](../portfolio_crypto_tilt_backtest.md) | 2026-06-20 | dynamic BTC/ETH crypto-sleeve split by net-liq acceleration (3-state) vs a static 50/50 baseline | DYNAMIC ≈ STATIC (154% vs 151% CAGR, −78 vs −81 maxDD) — not worth the added complexity |
| [../archive/portfolio_dca_passive_backtest.md](../archive/portfolio_dca_passive_backtest.md), [../archive/portfolio_rebalance_rule_backtest.md](../archive/portfolio_rebalance_rule_backtest.md) | 2026-06-20 | early precursor runs using full-rebalance-with-selling | **superseded** by study 07's real-algo model (buy-only + 2×-trim); archived |
| [00_virtune_etn_tracking_check.md](00_virtune_etn_tracking_check.md) | 2026-08-21 (burst addendum same day) | how well do the 4 GPW-listed Virtune crypto ETNs (BTCP/ETH/SOL/XRP, launched 2026-02-25) track their underlying coin, incl. during a live +20-38% multi-day burst? | directionally right but noisy (daily corr 0.51-0.72, not ~0.99); total-return gap runs 3-4x the published TER for SOL/XRP; addendum shows *why* — during the burst all four lagged spot by -3.4 to -6.7pp on the fastest day (thin volume), then overshot into a +1.9 to +3.4pp premium once volume returned — report-only, no allocation change |
| [../portfolio_dca_passive_backtest.md](../portfolio_dca_passive_backtest.md) | 2026-09-25 (re-run; ragged-end truncation fixed) | single-window (2018-01→2026-07, 103mo) passive-drift vs. monthly-rebalanced-to-target DCA on live weights — NOT the archived 2026-06-20 precursor of the same basename (that one used full-rebalance-with-selling and is superseded by study 07) | rebalanced wins on both axes (+3.4pp XIRR, −29pp maxDD: 41.8%/-53% vs 38.4%/-82%) — a baseline reference, not a mechanism claim |

## Files
| File | Theme | Built from |
|---|---|---|
| [01_data_and_methods.md](01_data_and_methods.md) | data sources, transforms, statistics | — |
| [02_correlations.md](02_correlations.md) | contemporaneous & YoY co-movement | `macro_correlation.md`, `macro_yoy_correlation.md`, `macro_study.md` |
| [03_lead_lag_timeshift.md](03_lead_lag_timeshift.md) | time-shift: who leads whom | `macro_weekly_leadlag.md`, `macro_leadlag_recheck.md`, `macro_leadlag_m2.md` |
| [04_timing_backtest.md](04_timing_backtest.md) | is it tradeable (walk-forward) | `liquidity_timing_backtest.md` |
| [05_liquidity_beta.md](05_liquidity_beta.md) | ranking holdings by liquidity beta | `liquidity_beta_ranking.md` |
| [06_strategy_zoo.md](06_strategy_zoo.md) | 11 macro rules × 28 assets, breadth test | `macro_strategy_zoo.md` |
| [07_portfolio_strategies.md](07_portfolio_strategies.md) | real-algo portfolio backtest: trim rule + macro dynamic-target de-risk | `portfolio_strategy_backtest.md` |
| [08_onchain_signal.md](08_onchain_signal.md) | does BTC/ETH MVRV sharpen top-timing beyond the macro gate? (rejected) | `onchain_mvrv_study.md` |
| [09_derisk_destination.md](09_derisk_destination.md) | de-risk proceeds: GOLD vs bond ETFs (ETFBCASH.WA / ZPR1.DE) | `derisk_destination_backtest.md` |
| [10_onchain_signals_v2.md](10_onchain_signals_v2.md) | bitcoin-data.com on-chain API (SOPR/MVRV-Z/NUPL/LTH-STH) — do sharper on-chain signals beat plain MVRV as sell/de-risk triggers? (rejected — one-cycle overfit; LTH/STH ratio reads as buy-conviction, not a trigger) | `onchain_signals_study.md` |
| [11_onchain_correlation_analysis.md](11_onchain_correlation_analysis.md) | do MVRV-Z/NUPL/LTH-SOPR collapse into one redundant signal, and is LTH/STH ratio truly orthogonal to the macro gate? (mostly collapse to 1 signal; LTH/STH orthogonal but fading) | `onchain_correlation_analysis.md` |
| [12_onchain_trim_depth_modulation.md](12_onchain_trim_depth_modulation.md) | can NUPL deepen the 2×-trim rule beyond its fixed 2.0×/1.5× parameters? (rejected — too few clustered events, rule hasn't fired since 2021-04) | `onchain_trim_depth_modulation.md` |
| [13_regression_channel_meanreversion.md](13_regression_channel_meanreversion.md) | per-asset active timing: buy/sell 100% against a rolling regression channel (±1.5σ), 10y vs 5y windows (rejected — loses to buy&hold except on bonds) | `regression_channel_backtest_10y.csv`, `_5y.csv`, `_trades.csv`, `.json`, `.md` |
| [14_macro_blend_momentum_vs_contrarian.md](14_macro_blend_momentum_vs_contrarian.md) | sequel to 07 on the REAL live mechanism (ADR-044 continuous blend, not the old binary cut): momentum-following (CURRENT) vs. a contrarian reversal (REVERSED, `blend_reversed=1-blend`) — contrarian hypothesis rejected | `portfolio_macro_blend_backtest.md`, `macro_blend_series.csv` |
| [15_trim_frequency_and_depth.md](15_trim_frequency_and_depth.md) | how often does the +100% trim rule actually fire (per-asset, non-overlapping 19y pass), and does tightening it to 1.5x-trigger/trim-to-1.0x improve risk-adjusted outcomes? Plus overshoot-at-fire, regime-clustering, and whipsaw diagnostics (**corrected 2026-07-09**: real deficit-proportional catch-up DCA mechanic, not a naive fixed-split; **refreshed 2026-07-13**: `QQQ_full.csv`/`FXI_full.csv` price-data drift meant the 2026-07-09 pass's own fire counts had gone stale — live 2.0/1.5 rule now fires 54 BASELINE / 57 CURRENT, not 22/25, and the rule is no longer BTC-dominated even at its live calibration; **re-run 2026-09-25** under live ADR-074/087 weights with CURRENT 1.5x/1.0x correctly labeled live throughout — CURRENT fires 41 BASELINE / 48 CURRENT over 19.6y, LINK fires once — see file for what changed) | `trim_frequency_backtest.md`, `trim_events.csv` |
| [16_classic_portfolios.md](16_classic_portfolios.md) | reference-point check (NOT the user's strategy): 4 canonical named portfolios (60/40, Permanent Portfolio, Golden Butterfly, All Weather) under BUYHOLD (real catch-up DCA)/FIXED (annual rebalance)/BLEND (ADR-044 sliding overlay)/TRIM (threshold-trigger partial sell) — contrasted against the user's own aggressive book | `classic_portfolios_backtest.md`, `fetch_classic_portfolio_data.py` |
| [17_global_stock_portfolio.md](17_global_stock_portfolio.md) | companion to #16: "Global 20", a 100%-individual-stock, no-ETF/bonds/gold, equal-weight global equity book (12 USA/4 Europe/4 RoW incl. Samsung via a KRW→USD→PLN FX chain) under BUYHOLD/FIXED/TRIM (BLEND not implemented — no internal defensive bucket, shown as context only) | `global_stocks_backtest.md`, `global_stocks_trim_events.csv`, `fetch_global_stocks_data.py` |
| [18_methodology_review.md](18_methodology_review.md) | **METHODOLOGY REVIEW, not a new empirical result** — researches Deflated Sharpe Ratio/PBO (multiple-testing correction), the Howell/Alden macro-liquidity frameworks, time-series-momentum/vol-targeting/HMM signal-design literature, and IC-framework formalization; estimates (rough, back-of-envelope) whether the flagship ETH net-liquidity timing result survives a multiple-testing correction; scopes candidate studies #19-22 | — (web research + re-read of 01-17; no new script) |
| [19_dsr_pbo.md](19_dsr_pbo.md) | candidate #19 — replaces file 18's back-of-envelope DSR estimate with a real computation: both studies 04 and 06's scripts (never committed to git) reconstructed from prose + validated for fidelity first, then real Deflated Sharpe Ratio at N=3/15/28/308 and real PBO via CSCV on study 06's 11×28 rule zoo. **Borderline, honestly reported, not rounded up**: the primary ETH net-liquidity-momentum edge (OOS Sharpe 0.82) clears N=3/N=15 overwhelmingly (DSR≈1.0) and N=28 comfortably (DSR=0.998), but the widest, most honest framing (N=308) lands at DSR=0.926 — just short of the conventional 95% bar | `liquidity_timing_backtest.py`, `macro_strategy_zoo.py`, `study19_variant_returns_04_insample.csv`, `study19_variant_returns_04_walkforward.csv`, `study19_variant_returns_06.csv`, `dsr_pbo_computation.md` |
| [20_vol_targeted_dca.md](20_vol_targeted_dca.md) | candidate #20 — does risk-weighting (instead of dollar-weighting) the real production catch-up DCA mechanic by each asset's own trailing realized vol improve risk-adjusted return, or is any Sharpe uplift a vol-reduction artifact (per file 18's diagnostic)? Fixed N=4 matrix {BASELINE, CURRENT (ADR-044 blend)} x {dollar-weighted, vol-weighted}, plus a 60/40 classic-portfolio cross-check. **Rejected** — no cell shows a genuine joint CAGR+Sharpe improvement; the narrow long-window Sharpe uptick that does appear is paired with a CAGR decline, i.e. exactly the vol-reduction artifact the skill warns about, not real skill | `vol_targeted_dca_backtest.py`, `vol_targeted_dca_backtest.md` |
| [21_adr048_blend_steepness_walkforward.md](21_adr048_blend_steepness_walkforward.md) | walk-forward validation of ADR-048's `BLEND_STEEPNESS_DIVISOR` change (2.0→1.0, shipped on an admittedly non-walk-forward ad-hoc comparison): Part 1 keeps the original 3-way fixed-divisor {2.0,1.0,0.5} comparison as explicitly in-sample/descriptive; Part 2 builds a genuine 36mo-burn-in/annual-checkpoint/trailing-36mo-Sharpe divisor-**selection** protocol, chaining the resulting OOS blocks 2011-07→2026-06. **Divisor=1.0 (ADR-048's shipped value; live is now 0.5 per ADR-049) was never selected at any of the original 12 checkpoints** — the process always picked either 2.0 or 0.5; **re-run 2026-09-25** (live weights, CURRENT trim, chain through 2026-09 = 13 checkpoints): 1.0 is now selected at 2/13, 0.5 at 7/13 including the last six straight — the steepening direction stays validated and the live 0.5 now has direct recent walk-forward support; ADR-048's own claimed overshoot-severity doubling (~5×→~10×) still does not reproduce (3.54×→4.66×) | `blend_steepness_walkforward_backtest.py`, `blend_steepness_walkforward_backtest.md`, `study21_walkforward_checkpoints.csv`, `study21_variant_returns.csv` |
| [22_adr047_gold_cut_defensive_check.md](22_adr047_gold_cut_defensive_check.md) | first-ever backtest of ADR-047's gold-cut/NQSE.DE+ICGA.DE risk-off-destination change (never tested anywhere in this program). Proxy-based full-5-window portfolio A/B (QQQ/FXI stand-ins, matching study 14's own scaffold) shows a modest, direction-consistent drawdown cost at the windows containing real risk-off episodes; the literal-ticker check (HAC/Newey-West liquidity-beta regression extending study 05, risk-off-conditional split, episode table, GLD correlation) is unambiguous: **NQSE.DE/ICGA.DE test as procyclical, not defensive** — in study 14's own dominant 14-month risk-off episode (2021-12→2023-01), gold rose +13.5% while both ETFs fell −28%/−11% with 3-4× gold's drawdown. **Flagged for the user's explicit revert decision, not auto-implemented** | `gold_cut_defensive_check_backtest.py`, `gold_cut_defensive_check_backtest.md`, `nqse_icga_defensiveness_check.py`, `nqse_icga_defensiveness_check.md` |
| [23_tax_drag_audit.md](23_tax_drag_audit.md) | tax/cost-drag audit — adds a 19% Belka-tax term (average-cost basis approximation, REGULAR-account assumption) on top of study 15's fire-instrumented trim-rule log, re-examining whether tightening 2.0x/1.5x→1.5x/1.0x is still worth it once realized-gain tax is counted (not just the flat 15bps trading cost). **Also discovered study 15's own published fire counts are now stale** — a price-data refresh (`QQQ_full.csv`/`FXI_full.csv`, 2026-07-10) postdates study 15's 2026-07-09 publication, so today's data now fires 54/57 at the live 2.0/1.5 rule, not the published 22/25 (a separate follow-up, not fixed here). **Tax drag does not change the standing verdict, and if anything mildly favors tightening**: total nominal tax paid is *lower* under the tighter rule (604K vs. 1.25M PLN over 19.3y) despite 31% more fires, because each trim under a tighter rule crystallizes less accumulated gain; both rules lose a similar ~1.8-3.1pp/yr of XIRR to tax, and the return/drawdown gap between the two rules narrows/widens modestly in the tighter rule's favor once tax is included (**re-run 2026-09-25** under live weights with CURRENT correctly
labeled live: CURRENT fires 48 / ~903K PLN tax vs PRIOR 41 / ~2.22M PLN over 19.6y; pattern
holds) | `tax_drag_audit_backtest.py`, `tax_drag_audit_backtest.md`, `tax_drag_events.csv` |
| [24_ic_framework.md](24_ic_framework.md) | formalizes studies 03/06 in Grinold-Kahn IC/ICIR/IC-decay vocabulary. **Found the task's literal cross-sectional-IC-per-month reading is mathematically undefined** for study 06's 11 rules (each applies one macro signal identically to all 28 assets each month — zero cross-sectional variance in the forecast, not just weak) — reframed as (a) an asset-level time-series IC per (rule, asset) pair extending study 03's design to 9 continuous-driver rules × all 28 assets (annual-block ICIR/t-stat/decay curves), and (b) a bonus genuinely-cross-sectional single-point check (study 06's per-asset Sharpe-uplift vs. study 05's liquidity-beta ranking, ρ=+0.523 ✓). Mostly restates the known big picture, but genuinely sharpens two things: the formal ICIR ranking reorders vs. study 06's dSharpe ranking (`GlobalM2YoY` edges out the flagship `NetLiqAccel`), and only ~8% of (rule,asset) pairs clear `\|t\|≥2` under genuine non-overlapping annual blocks — barely above chance, a more cautious read on signal stability than the pooled block-bootstrap CIs elsewhere in this program convey alone | `ic_framework_backtest.py`, `ic_framework_backtest.md`, `ic_time_series_h1_summary.csv` |
| [25_globalm2_leg_walkforward.md](25_globalm2_leg_walkforward.md) | does file 24's finding (Global-M2 YoY **level** has the best raw ICIR of any driver tested, +0.198, while the **acceleration** transform the live ADR-044 blend actually uses is negative, −0.073) mean the blend's Global-M2 leg should switch from acceleration to level? Locked 3-part protocol (data-analyst/data-scientist design review) so a naive full-history A/B can't launder the circularity of testing a candidate derived from this program's own full-sample data: **Part 0** (IC-stability pre-check at 6 truncation checkpoints) finds level leads accel at **6/6 checkpoints**, a stable ranking, not a recent flip; **Part 1** (in-sample A/B, explicitly not the verdict) shows the level-leg candidate losing on both XIRR and Sharpe at all 5 windows; **Part 2** (walk-forward leg-*selection*, adapting file 21's exact machinery) is the actual verdict and shows the level leg is **never selected at any of the 12 walk-forward checkpoints** — the chain is numerically identical to the live accel-formula at every window. **Rejected**: despite a temporally stable raw-signal IC advantage, the level transform never earns a walk-forward portfolio-level win once folded into the full composite blend; also loses on ETH trim-overshoot risk (9.81× vs. 6.51×). No change to the live formula recommended | `globalm2_leg_walkforward_backtest.py`, `globalm2_leg_walkforward_backtest.md`, `study25_part0_ic_stability.csv`, `study25_walkforward_checkpoints.csv`, `study25_variant_returns.csv` |
| [26_onchain_trigger_dca_mechanic_fix.md](26_onchain_trigger_dca_mechanic_fix.md) | **correction/index of a previously-orphan script, not new research**: `onchain_portfolio_backtest.py` (file 10's thread — macro-only vs. on-chain-only vs. composite AND/OR vs. no-trigger de-risk, LTH-SOPR≥1.86) was never folded into this index and, dated 2026-07-03, predated the 2026-07-09 DCA-mechanic fix pass across files 07/14/15/16/17/20 — still used the naive fixed-percentage-split allocation, not the real `Q_pos` deficit-proportional catch-up mechanic. Fixed and re-run: **top-of-ranking is unchanged** (macro-only, then composite-AND, remain the best configs at the only reliable 3y window) but on-chain-only's and composite-OR's small apparent edges over doing nothing **vanish entirely** at 3y (25/25/25 three-way tie with baseline, down from +2/+3pp) — reinforcing, not weakening, file 10's original rejection of LTH-SOPR as a portfolio-level trigger. 1y results are mixed (single noisy 12-month window) and not over-read | `onchain_portfolio_backtest.py`, `onchain_portfolio_backtest.md` |
| [27_icga_fxi_extended_history_check.md](27_icga_fxi_extended_history_check.md) | extends file 22's literal-ticker ICGA.DE defensiveness check (only 7.0y real history, reaching 2 of study 14's 5 named risk-off episodes, both the same 2021-23 macro regime) using FXI (21.7y, China-market proxy, `FXI_full.csv`) to reach all 5. **Mixed, honestly reported**: 4/5 episodes and a larger risk-off-vs-other conditional beta swing reinforce the "procyclical, not defensive" finding, and the shared 2021-12→2023-01 episode reads if anything worse (maxDD −36.0% vs. real ICGA.DE's −31.9%) — but the full-sample (21.7y) unconditional net-liq beta flips from significant-positive (ICGA.DE's own short sample, β=0.41,t=3.01) to statistically indistinguishable from zero (FXI, β=−0.13,t=−1.21), and the newly-reached 2014-11→2015-03 episode is a genuine counter-example (FXI +27.3%/0.0% maxDD vs. GLD +14.0%/−6.4%). A new Part 5 confirms FXI tracks real ICGA.DE closely where both exist (return correlation 0.95 all-history/0.99 risk-off-only). No ADR/production change recommended (report-only, per house convention) | `icga_fxi_extended_defensiveness_check.py`, `icga_fxi_extended_defensiveness_check.md` |

## Executive summary

1. **Liquidity (Global M2, Fed net liquidity) is the dominant macro co-mover with crypto**
   — strong on YoY changes, bootstrap-significant, robust ex-COVID, and monotonic in beta
   (BTC < ETH < SOL). ISM and US-M2-alone are weak.
2. **The relationship is non-linear and low-frequency** — Spearman ≫ Pearson (use ranks);
   month-to-month (MoM) links are weak; it's a regime/annual phenomenon.
3. **Time-shift — who leads whom:**
   - **BTC**: liquidity *level* is **contemporaneous** (no lead) — useful as a risk-state, not a timing lead.
   - **ETH ~1–2 mo, SOL ~3 mo**: net-liquidity *leads* (weekly: ETH ~8w, SOL ~11–12w).
   - **ISM does NOT lead** crypto — crypto actually leads ISM by ~3 mo (ISM is a confirmer).
   - **Global M2 YoY *acceleration* (2nd derivative) leads BTC ~2–3 mo (+0.23–0.25 ✓)** —
     the first forward signal that works for BTC. The *level* of M2 growth does not.
4. **Tradeable? Mostly drawdown control, with one real edge (ETH).** Walk-forward (no-lookahead):
   - **ETH** net-liquidity timing beats buy&hold OOS (Sharpe 0.65→0.81, CAGR 22→36%). ✓
   - **SOL** looked best in-sample (Sharpe 1.40) but **collapsed OOS** (0.12) — overfitting. ✗
   - **BTC** no return edge; timing only cuts drawdown (−85%→−67%).
5. **Breadth test (11 rules × 28 assets, file 06) upgraded the signal**: the broadly robust,
   generalisable macro-timing edge is **net-liquidity *acceleration* → risk-on** — beats
   buy&hold in **20/28** assets (crypto *and* equities) and correctly avoids bonds/gold/dollar.
   Global-M2 acceleration is a weaker, equity-tilted cousin; **ISM rules are counterproductive**
   (≤2/28); **BTC stays un-timeable** (hold it).
6. **Liquidity beta of holdings**: Crypto (β≈24) ≫ miners/MSTR/COIN (≈6) > equities (≈1) >
   FX/metals (≈0). TSLA is the most liquidity-levered equity; gold/forex are defensive.
7. **Data**: traditional assets now extend to 2007 (`fetch_long_history.py`) so strategies test
   over the full 2008–2026 macro window incl. the GFC; crypto starts at each coin's inception.
8. **On-chain metrics (files 08-12) add little on top of the macro gate.** Raw MVRV
   top-timing is rejected (weak/wrong-signed IC, file 08); bond-ETF de-risk destinations lose
   to gold on both return and drawdown (file 09); a wider on-chain toolkit (SOPR, MVRV-Z, NUPL,
   LTH-STH supply ratio, via the free `bitcoin-data.com` API) is tested in file 10 — **all
   rejected** as portfolio-level sell/de-risk triggers (one-cycle overfit, too few clustered
   events) **except the LTH/STH supply ratio**, which reads as a robust buy-conviction signal,
   not a sell trigger. File 11 shows MVRV-Z/NUPL/LTH-SOPR mostly **collapse into one redundant
   valuation signal** whose predictive power is concentrated in a single sub-period (now ~0),
   while LTH/STH ratio stays statistically **orthogonal to the macro gate** (partial
   correlation) — the best on-chain candidate, though its edge is fading. File 12 tries using
   NUPL to deepen the 2×-trim rule itself and is **rejected**: trim-worthy events are too rare
   and clustered (the rule hasn't fired since 2021-04) to safely loosen. **Net: the on-chain
   sell-side avenue is now closed** — only the LTH/STH ratio survives as a (fading) buy-timing
   input, not a trim/sell mechanism. (File 26, added 2026-07-13, is a DCA-mechanic
   correction of a previously-orphan portfolio-level test of file 10's LTH-SOPR trigger —
   not a new on-chain finding, but its corrected numbers reinforce this section's
   already-negative verdict rather than complicate it.)
9. **Per-asset regression-channel timing (file 13, unrelated to the macro/on-chain thread
   above) is rejected as a general rule**: buying/selling 100% against a rolling ±1.5σ
   log-linear trend channel loses to buy-and-hold for most assets and both window lengths
   tested (10y, 5y) — median CAGR edge −4.8/−5.6 pp, worst on persistently trending assets
   (TSLA/GLD/SLV never fired at all over 10y; NVDA missed 35.6 pp/yr). Its only real edge is
   on genuinely range-bound assets (bonds), where it showed a small, consistent positive edge.
10. **File 14 re-tests the portfolio macro overlay on the mechanism that's actually live today**
   (ADR-044's continuous blend, replacing file 07's since-superseded binary cut) and asks the
   user's explicit contrarian question: should the DCA instead tilt **into** risk assets when
   liquidity is decelerating ("cheap"), i.e. `blend_reversed = 1 − blend`? **Rejected** —
   momentum-following (the real mechanism) beats the reversed variant on both return and
   drawdown at 4 of 5 start dates, worst for REVERSED exactly at the windows containing the
   14-month 2021-2023 liquidity-decelerating stretch (the Fed hiking cycle/2022 crypto winter):
   reversed tilted further into crypto through the entire decline instead of away from it.
   Decelerating liquidity acceleration was a value trap here, not a buying opportunity — same
   direction of conclusion as the rejected BTC-equity ISM pro-cyclical study.
11. **File 15 audits how often the +100% trim rule (file 07/14's kept manual risk control)
   actually fires**, over a single non-overlapping 19.3-year pass. **Twice corrected, for two
   unrelated reasons**: first (2026-07-09) to use the real deficit-proportional catch-up DCA
   mechanic instead of a naive fixed-split, per `backtest/AGENTS.md` "Rebalancing rule" (the
   fixed-split had been quietly acting as a brake that slowed how fast an overweight position
   could re-cross a trim trigger); then (**refreshed 2026-07-13**, after file 23's own sanity
   check surfaced it as a side effect) because a 2026-07-10 repo-wide price-data reorganization
   added `data/prices/QQQ_full.csv`/`FXI_full.csv` for the first time — files that never existed
   before in this repo — extending QQQ's/FXI's history and removing their old, shorter fallback
   files entirely, one day after the 2026-07-09 pass published. **Today's (2026-07-13) figures:
   57 fires (CURRENT mode), 54 (BASELINE)** at the live 2.0x/1.5x rule — more than double the
   2026-07-09 pass's 25/22, and **no longer BTC-dominated even at the live rule**: BTC_spot's own
   fire count is essentially unchanged (14-15, same as 2026-07-09), but its *share* of total
   fires fell from 60-64% to 26%, because MSTR (25-26%), QQQ (23-24%, previously zero at this
   rule), and FXI (19-20%) now fire nearly as often — a genuinely broad, 4-asset mechanism, not a
   single-asset brake, already at the current calibration. **miners flipped in the opposite
   direction**: it fired twice at the live rule and 16 times at 1.5x/1.0x in the 2026-07-09 pass;
   it fires **zero times at any tested config** in today's data, joining COIN/SUI as assets this
   rule structurally never touches (SOL fires once, only at the tightest config). Tightening to
   **1.5x-trigger/trim-to-1.0x** now fires only **+56% more often** (89 vs. 57 — a much smaller
   relative jump than either prior pass found, 2026-07-09's +232% or the original pass's +57%)
   because most of the broad-based firing the tightened rule used to uniquely expose is now
   already present at the live rule; the per-asset breakdown at 1.5x/1.0x is nearly identical in
   composition to the live rule (FXI 26%, MSTR 26%, QQQ 25%, BTC_spot 15%). The XIRR/maxDD
   tradeoff itself stays essentially unchanged from both prior passes (13y: −5pp XIRR for +5pp
   less drawdown going 2.0/1.5→1.5/1.0) — the price-data drift barely touches the windowed
   return/drawdown table (within ±1pp everywhere), because all 5 standard windows start ≥2013,
   a period the drift's effect (concentrated in the pre-2013 stretch only the continuous pass
   covers) barely reaches — so **2.0x/1.5x still stays reasonably well-calibrated on the
   return/drawdown axis**, but the operational burden of the *live* rule alone (57 fires/19.3y
   across 4 assets, not 25 across essentially 1) is now understood to be far higher than either
   prior pass implied. The whipsaw finding's direction is unchanged (tighter rule still has a
   higher quick-re-fire rate, now 90% vs. 84%) but the absolute level rose sharply at both
   configs. Regime clustering is unchanged in absolute terms (still only 4 of CURRENT's fires
   fall inside a sustained MID_OFF/RISK_OFF episode) but that's now just 7% of a much larger
   total (57, not 25) — an even smaller share is macro-blend-driven than the 2026-07-09 pass
   found. See file 15 for the full per-section deltas and the mechanism trace (QQQ/FXI's longer
   history changes the shared `Q_pos` catch-up pool for every asset each month, not just their
   own fire counts — this is consistent with why MSTR/miners moved sharply despite their own
   price data being untouched, though it can't be independently re-verified since the pre-refresh
   input files no longer exist on disk).
12. **File 16 steps outside the user's own strategy** to backtest 4 well-known named
   portfolios (60/40, Permanent Portfolio, Golden Butterfly, All Weather) under the same
   DCA/cost/PLN conventions, in four variants: **BUYHOLD** (contributions routed via the
   REAL production deficit-proportional catch-up mechanic — corrected mid-task from an initial
   naive fixed-percentage-split approximation, see `backtest/AGENTS.md` "Rebalancing rule" —
   never sold/rebalanced), **FIXED** (contributions at fixed target weights + standard annual
   rebalance-to-target, not the 2x-trim rule), **BLEND** (sliding ADR-044 risk-on/off
   overlay), and **TRIM** (BUYHOLD's DCA + a threshold-triggered partial-sell check,
   general/portfolio-agnostic redistribution to every other position — the new standing
   baseline per `backtest/AGENTS.md` "Standing baselines" point 3). All land in
   single-digit-to-low-teens XIRR with ≤−20% max drawdown — a
   different regime entirely from the user's book. **Permanent Portfolio is the mildest**
   (−11% max drawdown, FIXED); **60/40 is the best risk-adjusted classic reference**; **All
   Weather is the worst** (its 55% bond weight took the brunt of the 2022 rate-hike crash).
   **Does annual rebalancing (with selling) earn its keep over real buy-and-hold DCA (no
   selling, ever)? Essentially no — the two converge.** Once BUYHOLD used the real catch-up
   mechanic (not the naive split), BUYHOLD and FIXED landed within 0–1pp of each other on both
   XIRR and maxDD at every window for all 4 portfolios, with no consistent winner — because the
   real catch-up mechanic is itself a form of continuous, cash-flow-only rebalancing, so for
   these diversified, moderate-volatility portfolios it already does almost all the work annual
   selling-rebalance would do. (The initial naive-split BUYHOLD had shown a clear buy-and-hold
   win on both axes for 3/4 portfolios — that was an artifact of the fixed-split approximation,
   not a property of real buy-and-hold DCA; corrected once flagged.) The sliding overlay
   (BLEND) adds a small, nearly-free return edge to the more equity-heavy portfolios but
   **cannot fix All Weather's drawdown at all** — its risk-off state shifts *more* weight into
   bonds, exactly the asset class that caused the drawdown, unlike the user's own book where
   risk-off moves weight out of the actual volatile assets (crypto) and into true ballast
   (gold/cash). **TRIM is nearly inert for all 4 portfolios**: the user's own live rule
   (2.0/1.5) **never fires once** across a 19.3-year continuous pass for any of the four, and
   even the loosest tested config (1.5/1.0) fires only 0–4 times total per portfolio (vs. the
   user's own crypto book's 57 fires at 2.0/1.5, study #15, refreshed 2026-07-13) — because none of these
   diversified portfolios' assets is a concentrated, high-beta position that runs to 2×+ its
   target the way BTC does; threshold-trim is a mechanism calibrated for the user's own
   concentrated book and essentially doesn't transfer to diversified textbook portfolios. Note:
   the "user's own book" reference row (study 14) still uses the older
   fixed-split DCA approximation, not yet corrected (explicit user decision, study 16 only for
   now) — not apples-to-apples with this study's corrected BUYHOLD rows. Vs. the mildest
   classic (Permanent Portfolio, FIXED), the
   user's own book trades +14–27pp/yr extra XIRR for −9–17pp more drawdown at the 4 long
   windows — a defensible aggressive-growth trade — but underperforms on **both** axes at the
   noisy 1y window.
13. **File 17 tests a completely different kind of portfolio — "Global 20," 100% individual
   stocks (12 USA/4 Europe/4 Rest-of-World incl. Samsung via a novel KRW→USD→PLN FX chain), no
   ETFs, no bonds/gold/cash at all, equal-weight (5% each) — and gets the **opposite** answer
   from file 16 on two of its headline questions.** BUYHOLD vs. FIXED **diverge sharply** here
   (13y: BUYHOLD 32%/**−34%** vs FIXED 27%/−17%) instead of converging as they did for file 16's
   diversified asset-class buckets — because a single stock (unlike a diversified bucket) can
   run to many multiples of its target faster than monthly cash flow alone can catch the rest of
   the portfolio up, so periodic hard rebalancing genuinely earns its keep for concentrated
    single-name risk. **TRIM also fires often here** — 20 times at the (2.0/1.5, now PRIOR
    reference) rule over 15.9y, now roughly a third of the user's own crypto book's 48 CURRENT
    fires (study #15, re-run 2026-09-25 under live weights/CURRENT trim — a wider gap than the two studies' original ~20-vs-25 "same scale"
   comparison, since the crypto book's fire count grew substantially more in the refresh) —
   **90% of fires are NVDA/AVGO/TSLA** (the AI/semiconductor supercycle + Tesla's run),
   structurally similar to how BTC dominates the user's own book's fires. BLEND was deliberately
   **not** implemented as an active overlay (a 100%-equity book has no internal defensive bucket
   for a risk-on/off tilt to rotate into without inventing one arbitrarily); its regime
   occupancy is shown as context only. On a 3-way comparison, Global 20's return (27-35%/yr,
   FIXED) beats every file-16 classic portfolio at every window, and its uncontrolled BUYHOLD
   drawdown (−34% at 13y) is actually **worse** than the user's own crypto/gold book's (−24%,
   study 14) — this equal-weight stock-picking book behaves like a concentrated-growth portfolio,
   not a textbook-diversified one, much closer in character to the user's own aggressive book
   than to 60/40 or Permanent Portfolio.

14. **File 18 is a methodology review, not a new empirical result** — it researches
   established quant-finance technique (web sources, not new backtests) to audit the
   program's own process rather than add a new trading claim. Key findings: (a) a
   rough, order-of-magnitude Deflated Sharpe Ratio check on the flagship ETH
   net-liquidity timing result (file 04) shows it clears a by-chance bar comfortably
   under a narrow framing (N=3 walk-forward runs) but sits uncomfortably close to the
   by-chance bar under a wider, more honest framing that counts the full rule/asset
   search that led to spotlighting ETH (study 06's 11×28 zoo, study 03's lag scans) —
   **the edge is probably real but thinner than it first looks**; (b) this repo's Fed
   net-liquidity formula already matches Lyn Alden's published framework exactly, and
   Global M2's gap vs. Michael Howell/CrossBorder Capital's much richer (paid, ~80-
   economy) Global Liquidity Index is a known, currently-unaddressable free-data
   limitation, not a bug, though FX-conversion noise in the 5-country sum has one
   concrete, untested, free fix; (c) the time-series-momentum literature warns that
   some of this program's apparent timing edges could be a vol-reduction artifact
   rather than genuine directional skill (Sharpe-only uplift without a matching CAGR
   uplift is the tell) — vol-targeted position sizing is flagged as a well-established,
   cheap, untested upgrade; (d) a formal IC/ICIR/decay-curve framework (factor-investing
   standard) would sharpen studies 03/06's existing findings using data already on hand,
   no new machinery needed; (e) White's Reality Check/SPA test and a full HMM regime
   model are both judged **not worth the complexity** for a single-user project at this
   time (same "reject over-engineering" house style as files 12/13). Ends with 4 scoped
   (not yet executed) candidate studies #19-22, prioritized: #19 (real DSR/PBO on the
   file 04/06 searches, replacing this file's back-of-envelope estimate), #20
   (vol-targeted/risk-parity-weighted catch-up DCA), #21 (cross-sectional IC framework
   applied to file 06 + IC-decay replot of file 03), #22 (HMM alternative to the
   ADR-044 blend, lowest priority). A companion Claude Code skill
   (`.claude/skills/quant-backtest-rigor/`) distills the checklist for future studies.

15. **File 19 replaces file 18's back-of-envelope DSR estimate with a real computation**,
   on the flagship ETH net-liquidity-momentum result (file 04). Both underlying scripts
   (`liquidity_timing_backtest.py` study 04, `macro_strategy_zoo.py` study 06) had been
   deleted and were **never committed to git** — reconstructed from scratch from their
   prose writeups, gated by an explicit anti-tuning rule and a numeric+pattern fidelity
   check (both passed cleanly — every walk-forward Sharpe within ±0.06 of published,
   every qualitative pattern — ETH's edge, SOL's collapse, BTC's de-risk-only story,
   NetLiqAccel's #1 rank — held exactly) **before** any DSR/PBO number was computed. Real
   DSR at all 4 required framings: **N=3 and N=15 clear overwhelmingly (DSR≈1.000000,
   7.7-8.8σ above the by-chance bar)**; **N=28 (study 06's own 28-asset breadth,
   `SR_hat`=study 06's own `NetLiqAccel×ETH` Sharpe of 1.10) clears comfortably (DSR=0.998)**;
   **N=308 (the full 11-rule×28-asset zoo) lands at DSR=0.926 — just short of the
   conventional 95% bar, a genuine borderline result, reported as such rather than rounded
   up or down.** PBO via CSCV on the 308-variant zoo tells two complementary stories:
   `NetLiqAccel×ETH` beats the whole zoo's out-of-sample median in 100% of 240 valid
   resampled splits (PBO=0.00, though the comparison set includes many structurally weak
   combos by design), while the harsher "pick the single best-of-308 raw Sharpe" process
   has a real 27% overfitting rate and never once crowns `NetLiqAccel×ETH` specifically
   (SOL- and mega-cap-equity combos usually win that race instead). A companion CSCV pass
   on study 04's smaller 15-variant in-sample scan gives PBO=0.87 — a quantitative
   confirmation of the already-known SOL in-sample-overfitting story, not a new finding,
   and explicitly does not speak to the walk-forward (Part 2) result. Bottom line: **the
   ETH edge is real, but the widest honest framing keeps it right at the edge of "clearly
   real" vs. "not cleanly distinguishable from a lucky draw out of 308 tries"** — matching
   study 18's own qualitative call, now with an actual number. Also fixed the process gap
   that made this reconstruction necessary: `01_data_and_methods.md`'s script-lifecycle
   note now requires retaining any script + per-variant series that feeds a DSR/PBO-
   relevant flagship claim, rather than deleting it once distilled.

16. **File 20 tests file 18's own top-scoped candidate (#20)**: risk-weight the
   real production catch-up DCA mechanic by each asset's own trailing-12mo
   realized vol (`buy_i = A*(Q_i/sigma_i)/sum_j(Q_j/sigma_j)`, no lookahead,
   dollar-weighted fallback for <12mo history/CASH) instead of dollar-weighting
   it, on the exact fixed N=4 grid `{BASELINE, CURRENT} x {dollar, vol}` plus a
   60/40 classic-portfolio cross-check. **Rejected — no cell in the whole grid
   shows a genuine joint CAGR-and-Sharpe improvement.** The only place Sharpe
   improves (BASELINE/CURRENT at the two longest windows, +0.03 to +0.11) is
   paired with a CAGR *decline* (−0.8 to −1.6pp), the textbook vol-reduction
   artifact the skill's "Position sizing" diagnostic warned about, not genuine
   allocation skill — flagged as such, not oversold. Everywhere else (shorter
   windows, and almost the entire 60/40 cross-check) the change is flat to
   mildly negative on both axes. The realized-vol sanity check confirms the
   mechanism itself works exactly as designed (correct vol ranking, plausible
   magnitude of reallocation) — this is a real, well-implemented idea that
   simply doesn't earn its complexity here, not a flawed one. Vol-targeting and
   the IC framework were file 18's two highest-priority, lowest-cost candidate
   upgrades; file 20 closes out vol-targeting with a negative result and file 19
   (above) closes out the real DSR/PBO computation, leaving #21 (IC framework)
   as the sole remaining scoped follow-up.

17. **File 21 walk-forward-validates ADR-048's blend-steepness change (2.0→1.0), which had
   shipped 4 days after file 19 found the underlying signal borderline (DSR=0.926) and on an
   explicitly non-walk-forward ad-hoc comparison.** Building the genuine walk-forward protocol
   this required (36-month burn-in defaulting to 2.0, annual re-selection on trailing-36-month
   Sharpe among {2.0, 1.0, 0.5}, chained OOS blocks 2011-07→2026-06) produces a striking result:
   **divisor=1.0 — ADR-048's actual live value — is never the walk-forward-selected choice at
   any of the 12 annual checkpoints.** The process always lands on either 2.0 (the pre-ADR-048
    status quo, 6 checkpoints, 2015-2021) or 0.5 (steeper still, the other 6, every checkpoint
    from 2021-06 to today) — 1.0 sits in a selection no-man's-land for this metric at every point
    tested (**2026-09-25 re-run** under live weights/CURRENT trim, chain through 2026-09 =
    13 checkpoints: 1.0 is now selected at 2/13, 0.5 at 7/13 including the last six straight —
    the old "never" claim is retired, and the live 0.5 now has direct recent walk-forward
    support). The *direction* ADR-048 took (steepen the blend) is validated — the walk-forward
   chain beats the fixed-2.0 status quo on drawdown at 4 of 5 windows and tracks divisor=0.5's
   numbers closely — but the *specific value shipped* is not what a genuine walk-forward process
   would have chosen. ADR-048's own cited justification (trim-overshoot severity roughly
   doubling, ~5×→~10×) also does not reproduce in this reconstruction (found 5.27×→4.84×, i.e.
   flat-to-lower). Recommendation reported (move to 2.0 or 0.5, not 1.0), not implemented — a
   decision for the user.
18. **File 22 runs the first-ever backtest of ADR-047's gold-cut/NQSE.DE+ICGA.DE risk-off-
   destination change** (Gold's risk-on target cut 30%→10%, freed weight plus the former
   bond-ETF allocation routed into Nasdaq-100/China-UCITS ETFs — never tested anywhere in this
   program before). A proxy-based, full-5-window portfolio A/B (QQQ/FXI stand-ins, matching
   file 14's own scaffold) shows the aggregate cost is modest — a few points of extra drawdown
   concentrated at the windows containing real risk-off episodes, roughly neutral or mildly
   positive elsewhere. **The literal-ticker check (extending file 05's own HAC/Newey-West
   liquidity-beta methodology to the actual NQSE.DE/ICGA.DE series, plus a risk-off-conditional
   split, an episode-level table, and correlation-to-gold) is unambiguous: both ETFs test as
   procyclical, not defensive.** Both land firmly on the equity side of file 05's liquidity-beta
   spectrum (not the metals side); NQSE.DE's beta roughly quadruples specifically in risk-off
   months; and in file 14's own dominant, already-flagged 14-month risk-off episode
   (2021-12→2023-01), gold rose +13.5% (maxDD −9%) while NQSE.DE fell −28% (maxDD −35%) and
   ICGA.DE fell −11% (maxDD −32%). This directly confirms the data-scientist review's flagged,
   untested, contrary hypothesis, and — per the task's guardrail — is **flagged explicitly for
   the user's own explicit revert decision, not auto-implemented**: whether ADR-047's risk-off
   destination choice should change is a live-production question affecting ~20 percentage
   points of target weight.
19. **File 23 adds a 19% Belka-tax term (average-cost basis, REGULAR-account assumption) to
   file 15's trim-rule fire log** and asks whether tightening 2.0x/1.5x→1.5x/1.0x is still
   worth it once realized-gain tax is counted, not just the flat 15bps trading cost. **Also
   surfaced, as a side effect of its own sanity check, that file 15's published fire counts
   are now stale**: `QQQ_full.csv`/`FXI_full.csv` were refreshed 2026-07-10, one day after
   file 15's 2026-07-09 publication, and re-running file 15's own unmodified script today
   produces 54/57 fires at the live 2.0/1.5 rule, not the published 22/25 (flagged as a
   separate follow-up, not corrected here — every number in file 23 itself uses today's
   data consistently for both its tax and no-tax variants). **Tax drag does not change the
   standing verdict — and, contrary to the naive expectation that more fires means more
   tax, it mildly reinforces the case for tightening**: total nominal tax paid over the
   full 19.3y pass is *lower* under the tighter rule (604K vs. 1.25M PLN) despite 31% more
   fires, because each individual trim under a tighter rule crystallizes a smaller
   accumulated gain; both rules lose a broadly similar 1.8-3.1pp/yr of XIRR to tax, and the
   full-pass return/drawdown gap between the two rules narrows (return) / widens in the
   tighter rule's favor (drawdown) once tax is included. 95-100% of all tax exposure is
   concentrated in crypto-spot (BTC/ETH/DOGE/SOL, structurally REGULAR-taxable), so the
   worst-case REGULAR assumption applied to equity/ETF fires barely affects the total.
20. **File 24 formalizes files 03/06 in Grinold-Kahn IC/ICIR/IC-decay vocabulary.** Found,
   by direct inspection, that the literal cross-sectional-IC-per-month reading originally
   asked for is **mathematically undefined** for file 06's 11 rules (each applies one
   macro signal identically to all 28 assets every month — zero cross-sectional variance
   in the forecast column, not just a weak one). Reframed as (a) an asset-level
   time-series IC per (rule, asset) pair — the primary deliverable, extending file 03's
   already-valid 3-driver/3-asset design to 9 continuous-driver rules × all 28 assets with
   real annual-block ICIR/t-stat/decay curves — and (b) a bonus, genuinely cross-sectional
   single-point check (file 06's per-asset Sharpe-uplift vs. file 05's liquidity-beta
   ranking, ρ=+0.523, CI excludes 0 ✓). Mostly restates the program's existing big picture
   (net-liq/Global-M2 remain the strongest signals, ISM remains weak, crypto/high-beta
   names respond most), but genuinely sharpens two things: the formal ICIR ranking
   reorders vs. file 06's own dSharpe ranking (`GlobalM2YoY` edges out the flagship
   `NetLiqAccel` by median ICIR of the raw signal, even though `NetLiqAccel` stays #1 by
   realized strategy Sharpe), and only ~8% of the 9×28 (rule,asset) pairs clear a
   conventional `\|t\|≥2` bar under genuine non-overlapping annual blocks — barely above
   chance, a materially more cautious read on signal *stability* than the pooled,
   full-sample block-bootstrap CIs elsewhere in this program convey on their own.
21. **File 25 tests file 24's own finding directly against the live blend formula**: should
   ADR-044's Global-M2 leg switch from the acceleration transform it actually uses (median
   ICIR −0.073, file 24) to the plain YoY level (median ICIR +0.198, the best of any driver
   tested)? Because the level candidate was itself derived by mining this program's own
   full-sample data, a data-analyst/data-scientist design review locked a 3-part protocol
   before execution so a naive full-history A/B couldn't launder that circularity. **Part 0**
   (a cheap IC-stability pre-check at 6 truncation checkpoints, 2016-12 through today) finds
   the level-beats-accel raw-signal ranking has been **stable at 6/6 checkpoints**, not a
   recent flip. **Part 1** (in-sample fixed-formula A/B, explicitly demoted to context only)
   shows the level-leg candidate losing to the live accel formula on both XIRR and Sharpe at
   all 5 standard windows. **Part 2** (the actual verdict — a genuine walk-forward
   leg-*selection* backtest adapting file 21's exact burn-in/checkpoint/trailing-Sharpe
    machinery) shows the level leg is **never once selected across 13 annual checkpoints
    spanning 2014-2026** (12 full + one trailing partial block as of the 2026-09-25 re-run) — the walk-forward chain is numerically identical to the live
   accel-formula's own fixed track record at every window, because the process never had
   reason to leave it. **Rejected, and instructively so**: a raw signal can have a stronger,
   temporally stable predictive correlation with forward returns and still never produce a
   better trailing-Sharpe outcome once folded into the actual multi-leg, z-scored,
   smoothed, divisor-clipped composite blend and trim-rule-interacting portfolio the live
   formula (and this backtest) actually trades — the same raw-IC-vs-realized-Sharpe gap file
   24 itself already flagged in the abstract, now demonstrated concretely on the specific
   change file 24's own table seemed to motivate. The level leg also shows worse ETH
   trim-overshoot risk (9.81× vs. 6.51×). No change to the live formula is recommended.
22. **File 26 corrects and indexes a previously-orphan script from the on-chain thread
   (files 08-12): `onchain_portfolio_backtest.py`, which tested whether adding an
   on-chain LTH-SOPR trigger (file 10's most defensible on-chain signal) on top of the
   shipped macro de-risk mechanism (ADR-042) improves portfolio outcomes, had never been
   folded into this index at all** — no Files-table row, no Executive-summary point,
   referenced from nowhere, despite sitting in `results/onchain_portfolio_backtest.md`
   since 2026-07-03. Because it predates the 2026-07-09 DCA-mechanic fix pass (files 07,
   14, 15, 16, 17, 20) by six days and was never indexed, it was never swept up in that
   fix either — it still used a naive fixed-percentage-split DCA allocation, not the real
   `Q_pos` deficit-proportional catch-up mechanic. Fixed (same pattern as
   `portfolio_strategy_backtest.py`/`portfolio_macro_blend_backtest.py`) and re-run:
   **the top-of-ranking answer is unchanged** — macro-only (the shipped mechanism) and
   composite-AND remain the best-performing trigger definitions at the only reliable (3y)
    testable window — **but on-chain-only's and composite-OR's already-modest apparent
    edges over doing nothing at all vanish entirely** at 3y (all three land in a dead
    25%/25%/25% XIRR tie, down from the naive-split pass's +2-3pp edges; **2026-09-25
    re-run** under live weights broke the tie mildly — 3y now macro-only 27 > AND 21 >
    OR 20 > on-chain-only 18 > baseline 14 — rejection stands on the same methodological
    grounds), while the noisy
   1y window (a single 12-month episode) moves in a mixed direction and shouldn't be
   over-read. **Net effect: the correction reinforces, not weakens, file 10's original
   rejection of LTH-SOPR as a portfolio-level de-risk trigger** — see file 10/file 26 for
   the standing caveat this doesn't remove: the 1.86 LTH-SOPR threshold is calibrated
   in-sample on the same ~4-year 2022-2026 history being backtested, the dominant
   limitation of this specific study, independent of the mechanic fix.

23. **File 27 extends file 22's literal-ticker ICGA.DE defensiveness check with 14.7 more
   years of history**, using FXI (iShares China Large-Cap, 21.7y, USD-native — the same
   China-market proxy study 22 Part A's portfolio-level A/B already used) in place of real
   ICGA.DE (only 7.0y of history, reaching just 2 of study 14's 5 named risk-off episodes,
   both from the same single 2021-23 macro regime). **The result is genuinely mixed, not a
   clean confirmation or a clean reversal.** Reinforcing the original "procyclical, not
   defensive" finding: 4 of 5 named episodes (now spanning 2012, 2018-19, and twice in
   2021-23, not just one regime) show FXI underperforming gold on both return and maxDD; the
   risk-off-vs-all-other conditional beta swing (Part 2: −0.20→0.52) is larger than either
   NQSE.DE's or ICGA.DE's own swing in file 22; and the one episode both studies share
   (2021-12→2023-01) reads if anything *worse* with FXI (maxDD −36.0% vs. real ICGA.DE's
   −31.9%). Complicating it: the full-sample (21.7y) unconditional net-liquidity beta flips
   from significant-positive-equity-like (ICGA.DE's own 7.0y sample, β=0.41/t=3.01, file 22)
   to statistically indistinguishable from zero (FXI's 21.7y sample, β=−0.13/t=−1.21) — the
   Global-M2 leg survives this flip, the net-liq leg does not — and the newly-reached
   2014-11→2015-03 episode is a genuine, sizable counter-example (FXI +27.3% return with
   **zero** drawdown vs. GLD's +14.0%/−6.4%, plausibly the early phase of mainland China's
   2014-2015 domestic equity bull run, a country-specific catalyst largely decoupled from the
   global-liquidity conditions the risk-off classification is built on). A new Part 5
   (internal-consistency check, not in file 22's original script) confirms FXI tracked real
   ICGA.DE closely over their 2019-08→2026-06 common window (return correlation 0.95
   all-history, 0.99 risk-off-months-only; every episode-level delta ≤4.2pp), which raises
   confidence FXI's read on the 3 pre-2019 episodes ICGA.DE has no data for is a reasonable
   stand-in. **Per this program's standing convention, no ADR/production change is
   recommended** — ADR-050 (2026-07-13) already shipped based on file 22's original,
   shorter-history evidence; file 27 is additional supporting-and-complicating context for the
   user's own awareness, reported plainly rather than rounded to a single verdict.

## The one-line takeaway
Liquidity is a **contemporaneous risk-state for BTC** and a **genuine lead for ETH/SOL**;
the only forward signal that works for BTC is **Global-M2-YoY acceleration (~2–3 mo)**;
ISM is a lagging confirmer; and after costs the single robust *tradeable* edge is **ETH on
net-liquidity momentum**. On-chain metrics sharpen valuation reads (LTH/STH ratio) but
produce no usable sell trigger — the shipped CURRENT 1.5x/1.0x-trim rule (ADR-048) and ADR-044 blend remain the
only live risk controls. Data foundation is current to ~2026-04 with all-real components.

_Generated 2026-06-19, last updated 2026-07-12 (added files 10-12 to the index, folded in
5 pre-thread/adjacent studies under "Other studies"; added files 16-17, the classic-portfolio
and individual-stock reference-point studies; added file 18, a methodology review — no new
backtest, scopes candidate studies #19-22; added file 20, vol-targeted catch-up DCA —
candidate #20, rejected; added file 19, the real DSR/PBO computation on the ETH flagship —
candidate #19, borderline-but-real verdict at the widest framing, reconstructing both
studies 04/06's scripts from scratch in the process; added files 21-22, walk-forward/
defensive-behavior validation of the two live, outside-the-study-process ADR-048/047 changes —
21 finds ADR-048's specific divisor value (1.0) is never the walk-forward-selected choice at
any checkpoint (though the general steepening direction is validated); 22 finds ADR-047's
NQSE.DE/ICGA.DE risk-off destination tests as procyclical, not defensive, in the literal-ticker
check, flagged for the user's explicit revert decision; added files 23-24, a tax/cost-drag
audit and an IC-framework formalization — 23 adds a 19% Belka-tax term to file 15's trim-rule
fire log (tax drag doesn't change the tightening verdict, and mildly reinforces it; also
surfaced that file 15's own published fire counts are now stale vs. refreshed price data,
flagged as a separate follow-up) and 24 formalizes files 03/06 in IC/ICIR/decay vocabulary
(found the literal cross-sectional-IC reading mathematically undefined for file 06's rules,
reframed as an asset-level time-series IC extending file 03's design instead); added file 25,
which tests file 24's own headline finding directly against the live ADR-044 blend formula
(should the Global-M2 leg switch from acceleration to level?) via a locked 3-part
IC-stability-precheck/in-sample-A-B/walk-forward-selection protocol — **rejected**: the level
transform's raw-IC edge is temporally stable (6/6 pre-check checkpoints) but is never once
selected across 12 walk-forward portfolio checkpoints, a concrete demonstration of file 24's
own raw-IC-vs-realized-Sharpe distinction.

_Updated 2026-07-13: refreshed file 15 for the price-data drift file 23 first surfaced —
`QQQ_full.csv`/`FXI_full.csv` didn't exist before 2026-07-10, so file 15's 2026-07-09 pass never
saw them; re-running file 15's unmodified script against today's data reproduces file 23's own
54/57-fire ground truth (not the published 22/25) and every downstream per-asset/overshoot/
regime/whipsaw/sensitivity figure has been regenerated to match — the live 2.0x/1.5x rule is no
longer well-described as "BTC-specific" (BTC's own fire count is unchanged, but its share of
total firing fell from 60-64% to 26% as MSTR/QQQ/FXI now fire nearly as often), and the relative
cost of tightening to 1.5x/1.0x shrank (+56% more fires, not +232%) because the live rule already
absorbs most of the breadth that used to only appear once tightened; the windowed XIRR/maxDD
table itself barely moved (±1pp). Also added file 26, a correction/index of a previously-orphan
script (`onchain_portfolio_backtest.py`, never folded into this index) that fixes the same class
of bug studies 07/14/15/16/17/20 already had corrected — a naive fixed-percentage-split DCA
allocation, not the real `Q_pos` deficit-proportional catch-up mechanic — and asks whether the
corrected mechanic changes which on-chain/macro de-risk-trigger configuration looks best (it
doesn't change the ranking at the top, but does erase on-chain-only's/composite-OR's small
apparent edge over doing nothing at the less-noisy 3y window). Re-run any script in `backtest/`
to refresh; see 01 for how._

_Updated 2026-07-13 (later same day): added file 27, extending file 22's literal-ticker
ICGA.DE defensiveness check (only 7.0y real history, 2 of 5 named episodes, one macro regime)
using FXI (21.7y, China-market proxy) to reach all 5 named episodes. Result is genuinely
mixed: 4/5 episodes and a larger risk-off-conditional beta swing reinforce the "procyclical,
not defensive" finding (and the one shared episode, 2021-12→2023-01, reads if anything worse
on maxDD with FXI than with real ICGA.DE), but the full-sample unconditional net-liquidity
beta loses statistical significance over the longer window, and the newly-reached 2014-11
→2015-03 episode is a genuine counter-example (FXI beat gold with zero drawdown). A new
internal-consistency check (Part 5, not in file 22's original script) confirms FXI tracked
real ICGA.DE closely in their common window (0.95-0.99 return correlation). No ADR/production
change recommended — ADR-050 already shipped on file 22's original evidence; file 27 is
additional context for the user's own awareness, not a new decision._

_Updated 2026-09-25 (task 28, second pass): re-ran 12 scripts under the refreshed
`backtest/lib/portfolio_spec.py` (live ADR-074/087 weights: QQQ 22.5/27.5, FXI 2.5/7.5,
ETH 10.0/1.0, LINK-USD 1.5/0.15 added, no CASH; CURRENT 1.5x/1.0x trim correctly labeled
live everywhere; live divisor 0.5 + live orientation), restored `USDPLN_X.csv` to full
2003→ history (the 2b03ed0 refresh had truncated it at 2017-08, breaking every 13y
USD-leg window), fixed a task-159 path regression in `macro_strategy_zoo.py`
(`backtest/lib/data/...` → `backtest/data/`), and appended dated correction notes to
files 07/09/15/16/17/20/21/22/23/25/26. Verdict changes: exactly one refinement — file 21
now selects divisor=1.0 at 2/13 checkpoints (old "never" retired) with live 0.5 at 7/13
incl. the last six straight; everything else re-confirmed (file 25 still never selects
the level leg; files 09/20/23/26 rejections stand; file 26's old 25/25/25 tie broke
mildly to 27/21/20/18/14). END 2026-09 — all windows shifted ~+3mo vs the 2026-06 pass.
Also fixed in passing: `portfolio_dca_passive_backtest.py` now truncates its window at
the last month with any return (ragged data end), and file 22's NEW leg is the live
table (not just ADR-047's application). Files-table rows and Executive-summary points
for 15/21/23/25/26 updated to match. Cleanup: orphaned `equity_*yr.csv`/`trades_*yr.csv`
(no producer script, no references) and the two unindexed
`strategy_review_data_{analyst,scientist}_opinion.md` advisory notes moved to
`results/archive/` (moved, not deleted); the live single-window
`results/portfolio_dca_passive_backtest.md` (distinct from the archived precursor of the
same basename) indexed above under Other studies._
