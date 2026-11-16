# 01 — Data & methods

## Macro inputs (`backtest/data/macro/`, built by `fetch_macro_data.py`)
All free / keyless. See `data/macro/README.md` for full provenance.

| Series | File | Source | Coverage |
|---|---|---|---|
| ISM Manufacturing PMI (first-print) | `ISM_PMI.csv` | investing.com release table (`ism_pmi_raw.txt`) | 2008-01 → 2026-05 |
| Fed net liquidity = WALCL − TGA − RRP | `FED_NET_LIQUIDITY.csv` | FRED (weekly) | 2008 → current |
| US M2 (SA) | `US_M2.csv` | FRED `M2SL` | 2008 → current |
| Global M2 (5-country, USD) | `GLOBAL_M2.csv` | US(FRED)+Euro(ECB API)+China(PBOC paste)+Japan(BoJ)+UK(BoE API) | 2008-01 → 2026-04 |

Global M2 notes: China = pasted PBOC level spliced onto OECD pre-2015; Japan = true BoJ M2
(levels 2008–17 + YoY-chain to present); Euro/UK via ECB/BoE APIs. **All real/published —
no extrapolation** (`japan_extrapolated`=0). Values scale with this environment's FX
(CNY≈6.8 → China ≈ $51T, global ≈ $108T).

## Asset inputs (`backtest/data/prices/`, built by `fetch_crypto_history.py`)
Longest free daily history → month-end / week-end close.
- **BTC** 2010-07 (Coinmetrics), **ETH** 2015-08 (Coinmetrics), **SOL** 2020-04 (Yahoo; SOL gated on Coinmetrics free tier).
- **Traditional assets to 2007** via `fetch_long_history.py` (Yahoo → `*_full.csv`): SPY/QQQ/IWM/EEM,
  TLT/HYG/LQD/UUP (bonds/credit/dollar), GLD/SLV, NVDA/AAPL/MSFT/TSLA/AMZN/GOOGL/JPM/XLF/XLE. Lets the
  strategy zoo test over the full 2008–2026 window incl. the GFC. (Loaders prefer `*_full.csv`.)
- Benchmarks for beta ranking: the full per-ticker universe in `data/prices/` (SPY, GLD, miners, etc.).

## Transforms
- **Levels are avoided** for correlation (two uptrending series → spurious). Use **changes**:
  asset YoY (12-mo) or 1-mo return; macro YoY %, MoM %, or **acceleration** (Δ of YoY).
- **ISM** is a stationary diffusion index → used as **level** (+ Δ).
- Everything collapsed to a **monthly** grid; net-liquidity work also done **weekly** (it's a
  true weekly series, so a sub-monthly lead would show there).

## Statistics
- **Spearman** rank correlation is primary (crypto returns are fat-tailed → Pearson is noisy;
  Pearson ≪ Spearman throughout). Pearson + OLS/HAC elasticity reported alongside in `macro_study`.
- **Significance** via **block-bootstrap** (12-mo blocks) 95% CI — needed because YoY/overlapping
  windows are autocorrelated, which inflates naive t-stats. ✓ = CI excludes 0.
- **No-lookahead**: predictive tests use driver(t) → *forward* asset return; timing/regime work
  lags each macro by its publication delay (ISM/net-liq ~+1mo, M2 ~+2mo). Walk-forward picks any
  parameter on past data only.

## Pitfalls surfaced (and handled)
- Small-sample mirages (SOL × Global-M2 was 0.92 at n=32 → 0.46 at n=61). Extending data fixed it.
- Overlapping YoY windows inflate significance → bootstrap CIs + lean on non-overlapping MoM.
- In-sample parameter scans overfit (SOL timing) → walk-forward exposes it.

## Data pipeline (kept)
```
cd backtest
python3 fetch_macro_data.py          # -> data/macro/*.csv  (ISM, net-liq, US/Global M2)
python3 fetch_crypto_history.py      # -> data/prices/{BTC,ETH,SOL}_USD_full.csv
python3 fetch_long_history.py        # -> data/prices/*_full.csv (traditional assets, 2007+)
```

## Note on the study scripts
The per-study analysis scripts (correlation / lead-lag / timing / beta / strategy-zoo /
portfolio rules) were **removed once their conclusions were distilled here**. The durable
record is this `study_summary/` plus the raw tables in `results/*.md`. Only the **data
fetchers** above are retained (so the inputs can be refreshed). To re-run a specific study,
restore its script from git history or rebuild from the method described in its summary file.

**Updated 2026-07-12 (study #19) — one important exception to "removed once distilled":**
if a study's per-variant return series feeds (or could plausibly later feed) a Deflated
Sharpe Ratio / PBO multiple-testing check on a flagship claim, **retain that script and its
per-variant return CSVs going forward** — do not delete them once the study is folded into
its summary `.md`. This exception exists because studies 02-06's scripts were deleted
*and never committed to git* (`git log --all --follow` returns nothing for either
`liquidity_timing_backtest.py` or `macro_strategy_zoo.py`), which meant study #19's real
DSR/PBO computation on the flagship ETH net-liquidity timing result required a from-scratch
reconstruction of both scripts from prose descriptions (`04_timing_backtest.md`,
`06_strategy_zoo.md`), gated by an explicit anti-tuning rule and a numeric+pattern fidelity
check before any DSR/PBO number could be trusted — a multi-step, multi-checkpoint task that
a single `git checkout` of a preserved script would have made unnecessary. See
`results/study_summary/19_dsr_pbo.md` and `.claude/tasks/done/20-vol-targeting-then-19-dsr-pbo.task.md`
for the full account. Going forward: `liquidity_timing_backtest.py`, `macro_strategy_zoo.py`,
and their `results/study19_variant_returns_*.csv` outputs are now permanently retained
(not subject to the general "removed once distilled" rule above), and any future study that
computes a DSR/PBO-relevant flagship claim should do the same for its own scripts and
per-variant series from the start, rather than relying on this note to catch it after the
fact.
