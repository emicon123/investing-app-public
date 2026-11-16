#!/usr/bin/env python3
"""
Single source of truth for MAIN's LIVE production rebalancing spec, so individual
backtest scripts stop hand-copying (and drifting from) it.

Created by task `.claude/tasks/28-backtest-portfolio-spec-refresh.task.md` (2026-08-06),
after an audit found most portfolio-blend/trim scripts in this folder hardcoded a stale
blend divisor (2.0 instead of live 0.5), a stale/inverted blend orientation, pre-ADR-047
target-allocation tables, and a mislabeled trim-trigger convention (calling the prior
2.0x/1.5x rule "current" when ADR-048 made 1.5x/1.0x live on 2026-07-10). Every constant
below was confirmed directly against `docs/architecture/05-rebalancing-algorithm.md`
§7.2/§7.3.1/§7.5 on 2026-08-06 (current as of ADR-067/ADR-071), then re-confirmed
2026-09-25 against the same section plus migrations V54/V55 (ADR-074 LINK/ONDO promotion),
V59 (ETFBCASH.PL -> ETFPZUCASH.PL ticker switch), V66/V67 (ADR-087 India split, live
Nasdaq ticker SXRV.DE) -- cite the ADR in a script's own comments when using a value
below, but import the number/formula from here, don't retype it.

Any script in this folder that hardcodes its own copy of the blend formula, the trim
trigger/trim-to pair, or the MAIN target-allocation table is expected to import it from
here instead, per this task's step 1. If production moves these values again, update
THIS file and re-run the scripts that import it -- don't patch N scripts individually.
"""
import numpy as np

# ---------------------------------------------------------------------------
# 0. DCA / cost conventions (backtest/AGENTS.md -- not production code, but repeated in
#    nearly every script in this folder; centralized here to cut copy-paste drift on
#    these too, not just the MAIN-specific constants below).
# ---------------------------------------------------------------------------
CONTRIB_PLN = 1000.0          # AGENTS.md: 1000 PLN starting cash + 1000 PLN/month
COST_BPS = 15 / 1e4           # 15bps flat trading cost, applied to buy + (2x) sell volume
STANDARD_WINDOWS = {"13y": 13, "8y": 8, "5y": 5, "3y": 3, "1y": 1}   # AGENTS.md's 5 windows
MINERS_BASKET = ["MARA", "HUT", "CIFR", "IREN", "WULF"]   # equal-weight mean-return basket


# ---------------------------------------------------------------------------
# 1. Blend factor (ADR-044 introduced it; steepness divisor tightened 2.0->1.0 by
#    ADR-048, then 1.0->0.5 by ADR-049; orientation flipped by ADR-071)
# ---------------------------------------------------------------------------
#   composite_z = 0.7*zscore(net-liq YoY accel) + 0.3*zscore(GlobalM2 YoY accel)
#                 [each z-score EXPANDING, no-lookahead, vs its own history]
#   smoothed_z  = 3-month rolling average of composite_z
#   blend       = clip(0.5 + smoothed_z / BLEND_DIVISOR_LIVE, 0, 1)
#                 -- 0 = full risk-off, 1 = full risk-on (LIVE orientation, ADR-071)
#   effective(position) = riskOff*(1-blend) + riskOn*blend
BLEND_DIVISOR_LIVE = 0.5        # ADR-049 (2026-07-13) -- walk-forward validated, study #21
BLEND_DIVISOR_PRIOR = 1.0       # ADR-048 (2026-07-10) -- ad-hoc/non-walk-forward, superseded
BLEND_DIVISOR_ORIGINAL = 2.0    # ADR-044 (2026-07-06) -- original value

# Orientation note: ADR-071 flipped which end of [0,1] is "risk-on". Scripts written
# before ADR-071 computed
#     blend_old = clip(0.5 - smoothed_z/divisor, 0, 1)      # 0 = risk-on, 1 = risk-off
#     effective = riskOn*(1-blend_old) + riskOff*blend_old
# which is numerically self-consistent (same months end up risk-on/off either way) but
# reads backwards against the live convention. The two are related by
# blend_live = 1 - blend_old for the same divisor. New/refreshed code should use
# blend_live()/effective_target() below so `blend` reads naturally (1 = risk-on).


def blend_live(smoothed_z, divisor=BLEND_DIVISOR_LIVE):
    """blend, LIVE (ADR-071) orientation: 0 = full risk-off, 1 = full risk-on."""
    return (0.5 + smoothed_z / divisor).clip(0, 1)


def effective_target(risk_on_pct, risk_off_pct, blend):
    """Per-position interpolation (§7.3), LIVE orientation: blend=1 -> risk_on_pct,
    blend=0 -> risk_off_pct. `risk_on_pct`/`risk_off_pct` may be dict-likes or scalars."""
    return risk_off_pct * (1 - blend) + risk_on_pct * blend


# Named zones (display-only, API layer) -- ADR-071 flipped which label sits at which
# end; the 0.2/0.4/0.6/0.8 thresholds themselves are unchanged (§7.2).
ZONE_BINS = [-0.01, 0.2, 0.4, 0.6, 0.8, 1.01]
ZONE_LABELS_LIVE = ["RISK_OFF", "MID_OFF", "NEUTRAL", "MID_ON", "RISK_ON"]     # live (ADR-071)
ZONE_LABELS_PRE_ADR071 = ["RISK_ON", "MID_ON", "NEUTRAL", "MID_OFF", "RISK_OFF"]  # pre-flip


# ---------------------------------------------------------------------------
# 2. Manual trim-assist thresholds (UI-only, NEVER automated -- ADR-045 removed all
#    algorithmic selling; ADR-048 tightened the UI's own suggested trigger/trim-to)
# ---------------------------------------------------------------------------
# (trigger, trim_to), both as multiples of a position's own current (effective) target.
TRIM_CURRENT = (1.5, 1.0)      # ADR-048 (2026-07-10) -- LIVE since that date. THE current rule.
TRIM_PRIOR = (2.0, 1.5)        # pre-ADR-048 (ADR-042/043/044 era) -- reference only, NOT current
TRIM_REFERENCE = (2.0, 1.0)    # AGENTS.md standing 3rd variant -- never live, bracket reference

# Standing 3-variant set every portfolio-level trim study should test (AGENTS.md
# "Standing baselines" #3). Label always states current-vs-not explicitly so a reader
# can't mistake one for the other again.
TRIM_CONFIGS = [
    ("CURRENT (1.5x trigger / 1.0x trim-to -- ADR-048 live rule)", TRIM_CURRENT[0], TRIM_CURRENT[1]),
    ("PRIOR (2.0x / 1.5x -- pre-ADR-048, reference only, NOT current)", TRIM_PRIOR[0], TRIM_PRIOR[1]),
    ("REFERENCE (2.0x / 1.0x -- AGENTS.md standing 3rd variant)", TRIM_REFERENCE[0], TRIM_REFERENCE[1]),
]


# ---------------------------------------------------------------------------
# 3. Full risk-on/risk-off target tables, real production tickers
#    (docs/architecture/05-rebalancing-algorithm.md §7.3.1, re-confirmed live
#    2026-09-25 -- ADR-074 (CRYPTO/EXPERIMENTS reshuffle), ADR-087 (India split),
#    V59 (PLN bond ticker switch), V66/V67 (live Nasdaq ticker SXRV.DE).
#    Was ADR-067 at the 2026-08-06 confirmation.) Both dicts sum to exactly 100.0.
# ---------------------------------------------------------------------------
REAL_RISK_ON = {
    # Crypto (13 positions, Sigma 50.0 -- ADR-074: ETH cut 13->10 funds LINK/ONDO
    # promotions from EXPERIMENTS; ONDO-USD is the OLAS-USD row renamed, not new)
    "BTC-USD": 20.0, "ETH-USD": 10.0, "SOL-USD": 5.0, "COIN": 3.0, "SUI-USDT": 3.0,
    "LINK-USD": 1.5, "ONDO-USD": 1.5,
    "CIFR": 1.0, "MARA": 1.0, "HUT": 1.0, "IREN": 1.0, "WULF": 1.0, "DOGE-USD": 1.0,
    # Metals (Sigma 20.0)
    "ETCGLDRMAU.PL": 20.0,
    # Bonds (risk-off-only sink, Sigma 0.0 risk-on -- V59 switched the PLN leg
    # ETFBCASH.PL -> ETFPZUCASH.PL, same 0.0/7.5 targets, pure ticker switch)
    "ETFPZUCASH.PL": 0.0, "ZPR1.DE": 0.0,
    # Equity (Sigma 27.5 -- ADR-087: China 7.5->2.5 funds Nasdaq 20->22.5 and new
    # India row 2.5; live Nasdaq ticker is SXRV.DE, V66/V67)
    "SXRV.DE": 22.5, "ICGA.DE": 2.5, "QDV5.DE": 2.5,
    # Experiments (17 positions, Sigma 2.5, flat/identical in both regimes --
    # ADR-074: LINK-USD/OLAS-USD left, TAO/XRP/ZEC bumped, QUTM 0.5->0.6)
    "QUTM.DE": 0.6, "CRCL": 0.2, "HBAR-USD": 0.2, "XRP-USD": 0.3,
    "MSTR": 0.1, "FET-USD": 0.1, "TRX-USD": 0.1, "FISV": 0.0, "LOFI-USDT": 0.0,
    "TAO-USDT": 0.3, "FLIP-USD": 0.1, "ZANO-USD": 0.1, "ZEC-USD": 0.2,
    "TRAC-USD": 0.1, "XPL-USD": 0.1, "ALGO-USD": 0.0, "GME": 0.0,
    # CHF retired to 0/0 by ADR-050 and its row later deleted from the live DB
    # entirely (ADR-066 era) -- no row here. CASH removed ENTIRELY by ADR-066 --
    # no row at all, not even a zero-target one; not represented here.
}
REAL_RISK_OFF = {
    "BTC-USD": 12.0, "ETH-USD": 1.0, "SOL-USD": 0.5, "COIN": 0.3, "SUI-USDT": 0.3,
    "LINK-USD": 0.15, "ONDO-USD": 0.15,
    "CIFR": 0.1, "MARA": 0.1, "HUT": 0.1, "IREN": 0.1, "WULF": 0.1, "DOGE-USD": 0.1,
    "ETCGLDRMAU.PL": 30.0,
    "ETFPZUCASH.PL": 7.5, "ZPR1.DE": 2.5,
    "SXRV.DE": 27.5, "ICGA.DE": 7.5, "QDV5.DE": 7.5,
    "QUTM.DE": 0.6, "CRCL": 0.2, "HBAR-USD": 0.2, "XRP-USD": 0.3,
    "MSTR": 0.1, "FET-USD": 0.1, "TRX-USD": 0.1, "FISV": 0.0, "LOFI-USDT": 0.0,
    "TAO-USDT": 0.3, "FLIP-USD": 0.1, "ZANO-USD": 0.1, "ZEC-USD": 0.2,
    "TRAC-USD": 0.1, "XPL-USD": 0.1, "ALGO-USD": 0.0, "GME": 0.0,
}
assert set(REAL_RISK_ON) == set(REAL_RISK_OFF), set(REAL_RISK_ON) ^ set(REAL_RISK_OFF)
assert abs(sum(REAL_RISK_ON.values()) - 100.0) < 1e-6, sum(REAL_RISK_ON.values())
assert abs(sum(REAL_RISK_OFF.values()) - 100.0) < 1e-6, sum(REAL_RISK_OFF.values())

# Real production ticker -> data/prices/*.csv basename (no "_full" suffix needed; every
# loader in this folder already prefers "{t}_full.csv" when present, falling back to
# "{t}.csv"). Proxies, all documented: SXRV.DE -> NQSE_DE (same Nasdaq-100 index,
# different ETF wrapper -- NQSE_DE.csv is the file on disk); ETFPZUCASH.PL ->
# ETFBCASH_WA (V59 was a pure ticker switch, same PLN bond market -- longer history
# wins for backtest purposes). No file on disk (yet): QDV5.DE (India, listed 2026),
# ONDO-USD. A value of None means "no price file" -- scripts must skip/renormalize,
# never crash. Spot-checked against `data/prices/` 2026-09-25 (task 28 step 0 refresh).
REAL_TICKER_FILE = {
    "BTC-USD": "BTC_USD", "ETH-USD": "ETH_USD", "SOL-USD": "SOL_USD", "COIN": "COIN",
    "SUI-USDT": "SUI20947_USD", "DOGE-USD": "DOGE_USD",
    "LINK-USD": "LINK_USD", "ONDO-USD": None,
    "CIFR": "CIFR", "MARA": "MARA", "HUT": "HUT", "IREN": "IREN", "WULF": "WULF",
    "ETCGLDRMAU.PL": "ETCGLDRMAU_WA", "ETFPZUCASH.PL": "ETFBCASH_WA", "ZPR1.DE": "ZPR1_DE",
    "SXRV.DE": "NQSE_DE", "ICGA.DE": "ICGA_DE", "QDV5.DE": None,
    "QUTM.DE": "QUTM_DE", "CRCL": "CRCL", "HBAR-USD": "HBAR_USD",
    "XRP-USD": "XRP_USD", "MSTR": "MSTR", "FET-USD": "FET_USD", "TRX-USD": "TRX_USD",
    "FISV": "FISV", "LOFI-USDT": "LOFI34187_USD",
    "TAO-USDT": "TAO22974_USD", "FLIP-USD": "FLIP_USD", "ZANO-USD": "ZANO_USD",
    "ZEC-USD": "ZEC_USD", "TRAC-USD": "TRAC_USD", "XPL-USD": "XPL_USD",
    "ALGO-USD": "ALGO_USD", "GME": "GME",
}
# ETCGLDRMAU_WA/ETFBCASH_WA/ZPR1_DE/NQSE_DE/ICGA_DE all have short real histories
# (~1.5-8y as of 2026-08) -- fine for BOND_PL/BOND_DE (risk-off-only, small weight,
# short history doesn't gate a 13y window the way a risk-on core holding's would) but
# NOT a clean drop-in for Gold/NQSE.DE/ICGA.DE in any script needing 13y+ windows; see
# SIMPLIFIED_* below and AGENTS.md's existing FXI-for-ICGA.DE standing convention.

# ---------------------------------------------------------------------------
# 3b. SIMPLIFIED roster -- what most priority-1 scripts in this folder actually use.
#     Same structure/key-naming these scripts already had (BTC_spot/miners/ETH/SOL/
#     COIN/SUI/DOGE/GLD/QQQ/FXI), just with LIVE weights and the CASH/MSTR-as-major-
#     line-item removed. GLD/QQQ/FXI are deliberately LONG-HISTORY PROXIES for real
#     Gold/NQSE.DE/ICGA.DE (same convention AGENTS.md's "Standing baselines" #2
#     already documents for FXI-as-ICGA.DE-proxy: NQSE.DE/ICGA.DE/ETCGLDRMAU.PL's own
#     real histories are 1.5-8y, too short for the 8y/13y windows every study here
#     tests) -- NOT a claim that GLD/QQQ/FXI ARE the live tickers, just that they are
#     this program's standing long-history stand-ins for what those tickers track
#     (SPDR Gold Trust for physical gold, Invesco QQQ for the same Nasdaq-100 index
#     NQSE.DE tracks, iShares FTSE China 25 for the same China-market exposure ICGA.DE
#     targets, though FXI tracks FTSE China 25 not ICGA.DE's actual MSCI China index --
#     see study #27 Part 5 for the measured 0.95-0.99 return-correlation gap).
#
#     The 19-position Experiments sleeve (Sigma 2.5, identical both regimes) is
#     OMITTED for tractability, along with the post-ADR-074 CRYPTO additions that
#     have no usable history (ONDO-USD: no file at all) and the post-ADR-087 India
#     row (QDV5.DE: no file at all) -- each script's own existing `w = w/w.sum()`
#     present-renormalization step (already there for handling not-yet-listed assets)
#     automatically scales the remainder up to 100 without any extra code, which
#     is mathematically equivalent to spreading that flat/omitted weight pro rata
#     across everything else. LINK-USD IS included (LINK_USD.csv is on disk) since
#     ADR-074 moved it into CRYPTO at a real 1.5/0.15 weight. BOND_PL/BOND_DE
#     (ETFPZUCASH.PL/ZPR1.DE) ARE included with their real tickers (not proxied) since
#     they are risk-off-only and short history doesn't bind the same way it would for
#     a risk-on core holding -- matches derisk_destination_backtest.py's existing
#     native-ticker handling. BOND_PL's file mapping stays ETFBCASH_WA (V59 was a pure
#     ticker switch; the longer Beta-ETF history is the better backtest proxy).
SIMPLIFIED_RISK_ON = {
    "GLD": 20.0, "QQQ": 22.5, "FXI": 2.5,
    "BOND_PL": 0.0, "BOND_DE": 0.0,
    "BTC_spot": 20.0, "miners": 5.0, "ETH": 10.0, "SOL": 5.0, "COIN": 3.0, "SUI": 3.0, "DOGE": 1.0,
    "LINK": 1.5,
}
SIMPLIFIED_RISK_OFF = {
    "GLD": 30.0, "QQQ": 27.5, "FXI": 7.5,
    "BOND_PL": 7.5, "BOND_DE": 2.5,
    "BTC_spot": 12.0, "miners": 0.5, "ETH": 1.0, "SOL": 0.5, "COIN": 0.3, "SUI": 0.3, "DOGE": 0.1,
    "LINK": 0.15,
}
assert set(SIMPLIFIED_RISK_ON) == set(SIMPLIFIED_RISK_OFF)
assert abs(sum(SIMPLIFIED_RISK_ON.values()) - 93.5) < 1e-6, sum(SIMPLIFIED_RISK_ON.values())
assert abs(sum(SIMPLIFIED_RISK_OFF.values()) - 89.85) < 1e-6, sum(SIMPLIFIED_RISK_OFF.values())
# Omitted from SIMPLIFIED (absorbed by callers' present-renormalization): Experiments
# sleeve 2.5 flat + ONDO-USD 1.5/0.15 (no file) + QDV5.DE India 2.5/7.5 (no file).
# 93.5 + 2.5 + 1.5 + 2.5 = 100.0 risk-on; 89.85 + 2.5 + 0.15 + 7.5 = 100.0 risk-off.

SIMPLIFIED_CRYPTO = ["BTC_spot", "miners", "ETH", "SOL", "COIN", "SUI", "DOGE", "LINK"]
SIMPLIFIED_TICKER_FILE = {
    "BTC_spot": "BTC_USD", "ETH": "ETH_USD", "SOL": "SOL_USD", "DOGE": "DOGE_USD",
    "SUI": "SUI20947_USD", "COIN": "COIN", "LINK": "LINK_USD",
    "GLD": "GLD", "QQQ": "QQQ", "FXI": "FXI",
    "BOND_PL": "ETFBCASH_WA", "BOND_DE": "ZPR1_DE",
}

# ---------------------------------------------------------------------------
# 3c. LEGACY demo/proxy table -- what most scripts in this folder used BEFORE this
#     refresh (pre-ADR-047: Gold flat 30/30, no equity risk-off premium, PLN CASH
#     residual sink, MSTR as a major 2.5% line item). Kept here ONLY as a citable
#     reference for before/after comparisons (e.g. gold_cut_defensive_check_backtest.py's
#     OLD-vs-NEW table) -- do not use for anything claiming to reflect the live system.
LEGACY_DEMO_RISK_ON = {
    "GLD": 30, "CASH": 10, "QQQ": 5, "FXI": 2.5, "MSTR": 2.5,
    "BTC_spot": 17.5, "miners": 7.5, "ETH": 13, "SOL": 5, "COIN": 4, "SUI": 2, "DOGE": 1,
}
LEGACY_DEMO_RISK_OFF = {
    "GLD": 30.000, "QQQ": 15.000, "FXI": 7.500, "MSTR": 2.500,
    "BTC_spot": 9.625, "miners": 2.250, "ETH": 1.300, "SOL": 0.500,
    "COIN": 0.400, "SUI": 0.200, "DOGE": 0.100,
}
LEGACY_DEMO_RISK_OFF["CASH"] = 100.0 - sum(v for k, v in LEGACY_DEMO_RISK_OFF.items())  # = 30.625
assert abs(sum(LEGACY_DEMO_RISK_ON.values()) - 100) < 1e-9
assert abs(sum(LEGACY_DEMO_RISK_OFF.values()) - 100) < 1e-9


if __name__ == "__main__":
    # Quick self-check when run directly: `python3 backtest/lib/portfolio_spec.py`.
    print("portfolio_spec.py OK -- REAL_RISK_ON/OFF sum to 100.0, SIMPLIFIED_* sum to "
          "93.5/89.85 (Experiments sleeve + ONDO-USD + India omitted, absorbed by "
          "callers' own present-renormalization).")
    print(f"BLEND_DIVISOR_LIVE={BLEND_DIVISOR_LIVE} (ADR-049), "
          f"TRIM_CURRENT={TRIM_CURRENT} (ADR-048), TRIM_PRIOR={TRIM_PRIOR}")
