"""
Study #28: safe-asset cushion sizing for a NEW, standalone glide-path portfolio.

Question: user is starting a brand-new portfolio (NOT MAIN/retirement -- none of the
ADR-040/044/045/048 mechanics apply here). Target end-state is US100/China at 66/33
(also tested: 75/25). Instead of deploying straight to target, start with a safe_frac%
cushion (Gold + bonds) and DCA monthly; a fully manual rule says: when "the index"
(QQQ, used as the US100 proxy) drops 20% from its all-time high, convert 100% of the
accumulated safe sleeve into the 66/33 (or 75/25) target in that same month, and route
all future contributions there permanently.

Goal: for a handful of a-priori worst-case entry dates (peak-buy scenarios chosen on
economic/historical grounds, not cherry-picked post-hoc), find the minimum safe_frac
such that portfolio value at month 24 >= nominal capital contributed by month 24.

Conventions follow backtest/AGENTS.md where applicable (1000 PLN/month DCA, PLN as
settlement currency via USDPLN_X/EURPLN_X) but this is explicitly NOT a MAIN-portfolio
backtest -- no deficit-proportional catch-up, no ADR-044 blend, no trim rule.

Reviewed by data-analyst + data-scientist before running (see chat/session for their
critique). Key methodology decisions driven by that review:
  - Gold and bonds are modeled as separate sub-allocations within the "safe" sleeve,
    not blended -- ETFBCASH.PL's real ~-35% drawdown in the 2022 rate shock showed
    "bonds" are not always low-vol, so a gold-heavy default (70/30) is used, with
    gold-100 and a real-ETFBCASH-stress sensitivity shown alongside.
  - Report per-scenario minimum safe_frac AND the worst-across-scenarios figure --
    never one blended number (n=6 historical windows is a small sample).
  - Report month-24 breakeven AND the worst point on the 0-24mo path (a route that
    dips deep mid-way and recovers by month 24 is riskier than a flat one).
  - Include an explicit opportunity-cost scenario (grinding bull, no crash) so safe_frac
    isn't sized as if the downside were free.
  - Scenario C (FXI's own 2021 peak) is reported separately, never folded into
    "worst-across-scenarios" -- it's a China-concentration-risk finding (the QQQ-based
    trigger doesn't protect a China-only shock), not a cushion-sizing finding.
  - Out of scope: the quant-backtest-rigor skill's DSR/PBO data-mining check -- this is
    a single monotonic parameter (safe_frac) scanned against fixed a-priori scenarios,
    not a search across many free parameters for the best-looking result.
"""
import csv
import datetime
import calendar
import json
import os
import statistics

ROOT = os.path.join(os.path.dirname(__file__), "..")  # backtest/ root (task 159)
PRICES = os.path.join(ROOT, "data", "prices")


def load_simple(name):
    rows = []
    with open(f"{PRICES}/{name}") as f:
        r = csv.reader(f)
        next(r)
        for row in r:
            try:
                rows.append((row[0], float(row[1])))
            except (ValueError, IndexError):
                pass
    return rows


def load_ohlc(name):
    with open(f"{PRICES}/{name}") as f:
        r = list(csv.reader(f))
    rows = []
    for row in r[3:]:
        try:
            rows.append((row[0], float(row[4])))
        except (ValueError, IndexError):
            pass
    return rows


QQQ = load_simple("QQQ_full.csv")
FXI = load_simple("FXI_full.csv")
GLD = load_simple("GLD_full.csv")
ZPR1 = load_ohlc("ZPR1_DE.csv")          # EUR-native
ETFBCASH = load_ohlc("ETFBCASH_WA.csv")  # PLN-native
USDPLN = load_simple("USDPLN_X.csv")
EURPLN = load_simple("EURPLN_X.csv")

BOND_INCEPTION = {"zpr1": "2023-11-13", "etfbcash": "2021-06-07"}


def price_on_or_after(series, date_str):
    for d, c in series:
        if d >= date_str:
            return d, c
    return None


def fx(series, date_str):
    got = price_on_or_after(series, date_str)
    if got is None:
        raise ValueError(f"no price on/after {date_str} in series")
    return got[1]


def add_months(date_str, months):
    d0 = datetime.date.fromisoformat(date_str)
    y = d0.year + (d0.month - 1 + months) // 12
    m = (d0.month - 1 + months) % 12 + 1
    day = min(d0.day, calendar.monthrange(y, m)[1])
    return f"{y:04d}-{m:02d}-{day:02d}"


def usd_pln(date_str):
    return fx(USDPLN, date_str)


def eur_pln(date_str):
    return fx(EURPLN, date_str)


def qqq_price_pln(date_str):
    return fx(QQQ, date_str) * usd_pln(date_str)


def fxi_price_pln(date_str):
    return fx(FXI, date_str) * usd_pln(date_str)


def gld_price_pln(date_str):
    return fx(GLD, date_str) * usd_pln(date_str)


def qqq_peak_drawdown(date_str, since_date):
    """Running QQQ (USD) high-water-mark drawdown, tracked from since_date (the
    portfolio's own start date) up to and including date_str -- NOT the absolute
    all-time high across all 27y of QQQ history. Using the true all-time-high would
    wrongly measure 2007 against the un-retested 2000 dot-com peak; a person starting
    a portfolio today naturally tracks the high since they started watching, which is
    also what lets the peak keep climbing through a fresh bull run (e.g. scenario C's
    QQQ peak keeps rising from Feb 2021 to Dec 2021 before the 2022 trigger fires)."""
    peak = 0.0
    price = None
    for d, c in QQQ:
        if d < since_date:
            continue
        if d > date_str:
            break
        peak = max(peak, c)
        price = c
    return price, (price / peak - 1.0)


def real_bond_price_pln(date_str, source):
    if source == "zpr1":
        return fx(ZPR1, date_str) * eur_pln(date_str)
    if source == "etfbcash":
        return fx(ETFBCASH, date_str)
    raise ValueError(source)


def bond_growth_factor(date_a, date_b, source, synth_rate):
    """Total-return growth factor from date_a to date_b for the bond sub-sleeve.
    Uses a flat synth_rate nominal annual return before the real ETF existed,
    splicing continuously into its real price path from inception onward."""
    if source == "synthetic":
        years = (datetime.date.fromisoformat(date_b) - datetime.date.fromisoformat(date_a)).days / 365.25
        return (1 + synth_rate) ** years

    incep = BOND_INCEPTION[source]
    if date_b < incep:
        years = (datetime.date.fromisoformat(date_b) - datetime.date.fromisoformat(date_a)).days / 365.25
        return (1 + synth_rate) ** years
    if date_a >= incep:
        return real_bond_price_pln(date_b, source) / real_bond_price_pln(date_a, source)
    # splice: synthetic up to inception, then real
    years_pre = (datetime.date.fromisoformat(incep) - datetime.date.fromisoformat(date_a)).days / 365.25
    synth_factor = (1 + synth_rate) ** years_pre
    real_factor = real_bond_price_pln(date_b, source) / real_bond_price_pln(incep, source)
    return synth_factor * real_factor


def simulate(start_date, months, monthly_contribution, safe_frac, gold_frac_in_safe,
             bond_source, us100_frac, trigger_dd=-0.20, synth_rate=0.035, use_fxi=True):
    china_frac = 1 - us100_frac
    bond_frac_in_safe = 1 - gold_frac_in_safe

    units_qqq = 0.0
    units_fxi = 0.0
    units_gld = 0.0
    bond_val = 0.0
    bond_last_date = None
    triggered = False
    trigger_month = None
    contributed = 0.0
    path = []  # (month, date, total_value_pln, contributed_pln)

    def grow_bonds_to(date_str):
        nonlocal bond_val, bond_last_date
        if bond_last_date is not None and bond_val > 0:
            gf = bond_growth_factor(bond_last_date, date_str, bond_source, synth_rate)
            bond_val *= gf
        bond_last_date = date_str

    for m in range(months):
        date_str = add_months(start_date, m)
        qqq_p, dd = qqq_peak_drawdown(date_str, start_date)
        qqq_pln = qqq_p * usd_pln(date_str)
        fxi_pln = fxi_price_pln(date_str) if use_fxi else None
        gld_pln = gld_price_pln(date_str)

        if not triggered and dd <= trigger_dd:
            triggered = True
            trigger_month = m
            grow_bonds_to(date_str)
            safe_val = units_gld * gld_pln + bond_val
            units_gld = 0.0
            bond_val = 0.0
            units_qqq += (safe_val * us100_frac) / qqq_pln
            if use_fxi:
                units_fxi += (safe_val * china_frac) / fxi_pln
            else:
                units_qqq += (safe_val * china_frac) / qqq_pln  # no China proxy available (dot-com sensitivity)

        if triggered:
            safe_amt, risk_amt = 0.0, monthly_contribution
        else:
            safe_amt = monthly_contribution * safe_frac
            risk_amt = monthly_contribution * (1 - safe_frac)
        contributed += monthly_contribution

        units_qqq += (risk_amt * us100_frac) / qqq_pln
        if use_fxi:
            units_fxi += (risk_amt * china_frac) / fxi_pln
        else:
            units_qqq += (risk_amt * china_frac) / qqq_pln

        if safe_amt > 0:
            units_gld += (safe_amt * gold_frac_in_safe) / gld_pln
            grow_bonds_to(date_str)
            bond_val += safe_amt * bond_frac_in_safe
        else:
            grow_bonds_to(date_str)

        total_val = units_qqq * qqq_pln + units_gld * gld_pln + bond_val
        if use_fxi:
            total_val += units_fxi * fxi_pln
        path.append((m, date_str, total_val, contributed))

    # final mark-to-market one month after the last contribution, no new money
    final_date = add_months(start_date, months)
    grow_bonds_to(final_date)
    qqq_p = fx(QQQ, final_date) * usd_pln(final_date)
    gld_pln = gld_price_pln(final_date)
    total_val = units_qqq * qqq_p + units_gld * gld_pln + bond_val
    if use_fxi:
        total_val += units_fxi * fxi_price_pln(final_date)
    path.append((months, final_date, total_val, contributed))

    return path, trigger_month


SCENARIOS = [
    ("A_gfc_2007", "2007-10-31", "GFC: US+China equity peaked together (Oct 2007), real QQQ/FXI/GLD data", True),
    ("B_qqqath_2021", "2021-12-27", "QQQ all-time-high before 2022 bear (stock/bond correlation break year)", True),
    ("C_fxi_peak_2021", "2021-02-17", "FXI's own peak -- China structural derate (trigger is QQQ-based, may lag/miss this leg)", True),
    ("D_china_deval_2015", "2015-04-27", "2015 China deval/crash (FXI-specific, distinct mechanism from 2021)", True),
    ("E_covid_2020", "2020-02-19", "COVID V-shaped crash -- fast-recovery counterexample", True),
    ("F_grinding_bull_2018", "2018-12-24", "No-crash grinding bull (opportunity-cost ceiling, not a downside case)", True),
]

COMPOSITIONS = [
    ("gold100", 1.0, "synthetic"),
    ("gold70_bond30_synth", 0.7, "synthetic"),
    ("gold60_bond40_realstress", 0.6, "etfbcash"),
]

TARGETS = [("66_33", 2 / 3), ("75_25", 0.75)]

SAFE_GRID = [round(x * 0.05, 2) for x in range(0, 21)]  # 0.00 .. 1.00 step 0.05

MONTHLY = 1000.0
MONTHS = 24


def run_all():
    results = []
    for scen_key, start_date, desc, _ in SCENARIOS:
        for comp_key, gold_frac, bond_source in COMPOSITIONS:
            for target_key, us100_frac in TARGETS:
                for safe_frac in SAFE_GRID:
                    path, trig = simulate(
                        start_date, MONTHS, MONTHLY, safe_frac, gold_frac, bond_source,
                        us100_frac,
                    )
                    ratios = [v / c for _, _, v, c in path if c > 0]
                    final_val = path[-1][2]
                    contributed = path[-1][3]
                    final_ratio = final_val / contributed
                    min_ratio = min(ratios)
                    min_ratio_month = ratios.index(min_ratio)
                    results.append({
                        "scenario": scen_key, "start_date": start_date, "desc": desc,
                        "composition": comp_key, "target": target_key,
                        "safe_frac": safe_frac, "trigger_month": trig,
                        "final_value_pln": round(final_val, 1),
                        "contributed_pln": round(contributed, 1),
                        "final_ratio": round(final_ratio, 4),
                        "min_ratio": round(min_ratio, 4),
                        "min_ratio_month": min_ratio_month,
                    })
    return results


def dotcom_sensitivity():
    """Sensitivity only: QQQ-only (no FXI/GLD data pre-2007), 100% synthetic safe sleeve
    at a conservative 3%/yr (real gold likely did better historically over 2000-2002,
    not modeled here since we don't have repo GLD data that far back)."""
    out = []
    for safe_frac in SAFE_GRID:
        path, trig = simulate(
            "2000-03-27", MONTHS, MONTHLY, safe_frac, gold_frac_in_safe=0.0,
            bond_source="synthetic", us100_frac=1.0, synth_rate=0.03, use_fxi=False,
        )
        ratios = [v / c for _, _, v, c in path if c > 0]
        final_ratio = ratios[-1]
        min_ratio = min(ratios)
        out.append({
            "safe_frac": safe_frac, "trigger_month": trig,
            "final_ratio": round(final_ratio, 4), "min_ratio": round(min_ratio, 4),
        })
    return out


if __name__ == "__main__":
    results = run_all()
    with open(os.path.join(ROOT, "results", "safe_cushion_glidepath_backtest.json"), "w") as f:
        json.dump({"main": results, "dotcom_sensitivity": dotcom_sensitivity()}, f, indent=2)
    print(f"Wrote {len(results)} main rows + dot-com sensitivity to results/safe_cushion_glidepath_backtest.json")
