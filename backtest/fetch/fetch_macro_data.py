#!/usr/bin/env python3
"""
Fetch macro indicators into ./data/macro/ (free sources only, no API keys):

  ISM_PMI.csv            ISM Manufacturing PMI, monthly, FIRST-PRINT ("Actual")
                         Parsed from data/macro/ism_pmi_raw.txt (investing.com
                         release table). Period = the month the value describes
                         (release month - 1). Range: 2008-01 .. latest.

  FED_NET_LIQUIDITY.csv  Fed net liquidity (weekly, USD bn) =
                         WALCL - Treasury General Account (WDTGAL) - ON RRP (RRPONTSYD)
                         All three from FRED (fredgraph CSV, no key).

  US_M2.csv              US M2 money stock, monthly SA (FRED M2SL, USD bn).

  GLOBAL_M2.csv          Major-economy M2 summed in USD bn (monthly):
                         US (FRED M2SL) + Euro area (ECB BSI M2) +
                         China (FRED MYAGM2CNM189N, discontinued 2019) +
                         Japan (IMF IFS broad money) + UK (FRED M3).
                         See data/macro/README.md for the China-recency caveat.

Everything is free / keyless: FRED `fredgraph.csv`, DBnomics REST, and the
hand-saved ISM table. Re-run any time to refresh.
"""

import io
import json
import os
import re
import urllib.request
from datetime import date

import numpy as np
import pandas as pd

OUT_DIR = os.path.join(os.path.join(os.path.dirname(__file__), ".."), "data", "macro")
os.makedirs(OUT_DIR, exist_ok=True)

START = "2008-01-01"
UA = {"User-Agent": "Mozilla/5.0 (macro-fetch)"}


def _get(url: str) -> bytes:
    req = urllib.request.Request(url, headers=UA)
    with urllib.request.urlopen(req, timeout=60) as r:
        return r.read()


def fred(series_id: str) -> pd.Series:
    """Daily/weekly/monthly FRED series as a float Series indexed by date."""
    raw = _get(f"https://fred.stlouisfed.org/graph/fredgraph.csv?id={series_id}")
    df = pd.read_csv(io.BytesIO(raw), na_values=[".", ""])
    df.columns = ["date", "value"]
    df["date"] = pd.to_datetime(df["date"])
    return df.set_index("date")["value"].astype(float).dropna()


def dbnomics(series_path: str) -> pd.Series:
    """DBnomics series (provider/dataset/code) -> Series indexed by month-start."""
    url = f"https://api.db.nomics.world/v22/series/{series_path}?observations=1"
    doc = json.loads(_get(url))["series"]["docs"][0]
    s = pd.Series(doc["value"], index=pd.PeriodIndex(doc["period"], freq="M"))
    return s.replace("NA", pd.NA).astype(float).dropna()


def to_month(s: pd.Series) -> pd.Series:
    """Collapse any series to one value per calendar month (last obs), Period[M] index."""
    if isinstance(s.index, pd.PeriodIndex):
        return s
    return s.resample("ME").last().to_period("M")


def fx_month_mean(series_id: str) -> pd.Series:
    """FRED daily FX -> monthly mean, Period[M] index."""
    return fred(series_id).resample("ME").mean().to_period("M")


# ──────────────────────────────────────────────────────────────────────────
# 1) ISM PMI  (first-print, from the saved investing.com release table)
# ──────────────────────────────────────────────────────────────────────────
def build_ism():
    raw_path = os.path.join(OUT_DIR, "ism_pmi_raw.txt")
    row_re = re.compile(r"^([A-Z][a-z]{2} \d{2}, \d{4})(?: \([^)]+\))?\s+\d{1,2}:\d{2}(.*)$")
    rows = {}
    with open(raw_path) as fh:
        for line in fh:
            m = row_re.match(line.rstrip("\n"))
            if not m:
                continue
            toks = m.group(2).split()
            if not toks:
                continue
            try:
                actual = float(toks[0])
            except ValueError:
                continue
            rel = pd.Timestamp(m.group(1))
            ref = (rel.to_period("M") - 1).to_timestamp()  # release month - 1
            rows[ref] = actual
    s = pd.Series(rows).sort_index()
    s = s[s.index >= START]
    out = s.rename_axis("date").reset_index(name="ism_pmi")
    out["date"] = out["date"].dt.strftime("%Y-%m-%d")
    path = os.path.join(OUT_DIR, "ISM_PMI.csv")
    out.to_csv(path, index=False)
    print(f"  ISM_PMI.csv          {len(out):4d} rows  {out['date'].iloc[0]} -> {out['date'].iloc[-1]}")


# ──────────────────────────────────────────────────────────────────────────
# 2) Fed net liquidity  (weekly, USD bn)
# ──────────────────────────────────────────────────────────────────────────
def build_net_liquidity():
    walcl = fred("WALCL") / 1000.0       # USD millions -> billions
    tga = fred("WDTGAL") / 1000.0        # USD millions -> billions (Wed level)
    rrp = fred("RRPONTSYD")              # already USD billions

    df = pd.DataFrame({"walcl_bn": walcl, "tga_bn": tga}).dropna()
    # RRP is daily; attach the most recent value on/just before each weekly date.
    df["rrp_bn"] = pd.merge_asof(
        df.reset_index().rename(columns={"index": "date"}).sort_values("date"),
        rrp.rename_axis("date").reset_index(name="rrp_bn").sort_values("date"),
        on="date", direction="backward",
    ).set_index("date")["rrp_bn"]
    df["rrp_bn"] = df["rrp_bn"].fillna(0.0)  # RRP facility ~0 before 2013
    df["net_liquidity_bn"] = df["walcl_bn"] - df["tga_bn"] - df["rrp_bn"]
    df = df[df.index >= START].round(1)
    df.index.name = "date"
    path = os.path.join(OUT_DIR, "FED_NET_LIQUIDITY.csv")
    df.to_csv(path)
    print(f"  FED_NET_LIQUIDITY.csv {len(df):4d} rows  {df.index[0].date()} -> {df.index[-1].date()}")


# ──────────────────────────────────────────────────────────────────────────
# 3) US M2  (monthly SA, USD bn)
# ──────────────────────────────────────────────────────────────────────────
def build_us_m2():
    m2 = fred("M2SL").rename("m2_usd_bn")
    m2 = m2[m2.index >= START].round(1)
    m2.index.name = "date"
    path = os.path.join(OUT_DIR, "US_M2.csv")
    m2.to_csv(path)
    print(f"  US_M2.csv            {len(m2):4d} rows  {m2.index[0].date()} -> {m2.index[-1].date()}")
    return m2


# ──────────────────────────────────────────────────────────────────────────
# 4) Global M2  (major economies summed in USD bn, monthly)
# ──────────────────────────────────────────────────────────────────────────
def ecb_m2():
    """Euro-area M2 (EUR millions, SA) from the ECB Data Portal API — current."""
    raw = _get("https://data-api.ecb.europa.eu/service/data/BSI/"
               "M.U2.Y.V.M20.X.1.U2.2300.Z01.E?format=csvdata")
    df = pd.read_csv(io.BytesIO(raw))[["TIME_PERIOD", "OBS_VALUE"]].dropna()
    s = pd.Series(df["OBS_VALUE"].astype(float).values,
                  index=pd.PeriodIndex(df["TIME_PERIOD"].astype(str), freq="M"))
    return s.groupby(level=0).last().sort_index()


def boe_m4():
    """UK M4 (LPMAUYM, £ millions) from the Bank of England IADB — current."""
    raw = _get("https://www.bankofengland.co.uk/boeapps/database/_iadb-fromshowcolumns.asp"
               "?csv.x=yes&Datefrom=01/Jan/1999&Dateto=01/Dec/2026&SeriesCodes=LPMAUYM"
               "&CSVF=TN&UsingCodes=Y&VPD=Y&VFD=N")
    df = pd.read_csv(io.BytesIO(raw)); df.columns = ["date", "v"]
    idx = pd.to_datetime(df["date"], format="%d %b %Y", errors="coerce")
    s = pd.Series(df["v"].astype(float).values, index=idx).dropna()
    s.index = s.index.to_period("M")
    return s.groupby(level=0).last().sort_index()


def china_cny_bn():
    """China M2 in CNY billions: pasted PBOC level (亿 CNY, current) spliced onto
    OECD MEI (CNY bn) for pre-2015 history. They match to the decimal in overlap."""
    oecd = dbnomics("OECD/MEI/CHN.MABMM301.ST.M")  # CNY billions, 1999..2023-11
    rows = {}
    for line in open(os.path.join(OUT_DIR, "china_m2_raw.txt")):
        m = re.match(r"^(\d{4})-(\d{2})\s+([\d,]+\.?\d*)", line)
        if m:                                   # 100M CNY (亿) -> CNY billions = /10
            rows[pd.Period(f"{m.group(1)}-{m.group(2)}", "M")] = float(m.group(3).replace(",", "")) / 10.0
    paste = pd.Series(rows).sort_index()
    return paste.combine_first(oecd).sort_index()


def japan_m2_jpy():
    """True BoJ M2 (YEN, actual units): published levels 2008-01..2017-02
    (japan_m2_raw.txt) extended forward by chaining published YoY % from
    japan_m2_yoy_raw.txt (level_t = level_{t-12} * (1 + YoY_t)). Current to ~2026-05,
    so no trend extrapolation needed. More 'M2'-consistent than IMF broad money."""
    lvl = {}
    for line in open(os.path.join(OUT_DIR, "japan_m2_raw.txt")):
        m = re.match(r"^(\d{4})-(\d{2})-\d{2},([\d.]+)", line)
        if m:
            lvl[pd.Period(f"{m.group(1)}-{m.group(2)}", "M")] = float(m.group(3))
    s = pd.Series(lvl).sort_index()
    row = re.compile(r"^([A-Z][a-z]{2} \d{2}, \d{4})(?: \([^)]+\))?\s+\d{1,2}:\d{2}(.*)$")
    yoy = {}
    for line in open(os.path.join(OUT_DIR, "japan_m2_yoy_raw.txt")):
        mm = row.match(line.rstrip("\n"))
        if not mm:
            continue
        toks = mm.group(2).split()
        if not toks:
            continue
        try:
            v = float(toks[0].replace("%", ""))
        except ValueError:
            continue
        yoy[pd.Timestamp(mm.group(1)).to_period("M") - 1] = v   # ref = release month - 1
    yoy = pd.Series(yoy).sort_index()
    yoy = yoy.reindex(pd.period_range(yoy.index.min(), yoy.index.max(), freq="M")).interpolate()
    for t in pd.period_range(pd.Period("2017-03", "M"), yoy.index.max(), freq="M"):
        if t not in s.index:
            s[t] = s[t - 12] * (1 + yoy[t] / 100.0)
    return s.sort_index()


def extrapolate(s, end):
    """Carry a smooth series forward to `end` (Period[M]) at its trailing-24m mean
    log-growth. Returns (filled series, flag series=1 where extrapolated)."""
    s = s.dropna().sort_index()
    flag = pd.Series(0, index=s.index)
    if end is None or end <= s.index.max():
        return s, flag
    g = np.log(s).diff().dropna().tail(24).mean()
    fut = pd.period_range(s.index.max() + 1, end, freq="M")
    last = s.iloc[-1]; vals = {}
    for m in fut:
        last *= np.exp(g); vals[m] = last
    return (pd.concat([s, pd.Series(vals)]),
            pd.concat([flag, pd.Series(1, index=fut)]))


def build_global_m2():
    cnyusd = fred("DEXCHUS").resample("ME").mean().to_period("M")   # CNY per USD
    jpyusd = fred("DEXJPUS").resample("ME").mean().to_period("M")   # JPY per USD
    comp = {}
    comp["us_m2_bn"] = to_month(fred("M2SL"))                       # USD bn
    comp["euro_area_m2_bn"] = (ecb_m2() * fx_month_mean("DEXUSEU") / 1000.0).dropna()
    comp["china_m2_bn"] = (china_cny_bn() / cnyusd).dropna()
    comp["uk_m2_bn"] = (boe_m4() * fx_month_mean("DEXUSUK") / 1000.0).dropna()

    # Japan: true BoJ M2 (yen) — actual 2008-01..2017-02 + reconstruction from
    # published YoY (japan_m2_*_raw.txt). Current to ~2026-05 → no trend extrapolation.
    jp_jpy = japan_m2_jpy()                                  # YEN (actual units)
    base = pd.DataFrame({k: comp[k] for k in ["us_m2_bn", "euro_area_m2_bn", "china_m2_bn", "uk_m2_bn"]})
    end = base.dropna().index.max()
    jp_filled, jp_flag = extrapolate(jp_jpy, end)            # no-op when jp already covers `end`
    comp["japan_m2_bn"] = (jp_filled / jpyusd / 1e9).dropna()

    df = pd.DataFrame(comp).sort_index()
    df = df[df.index >= pd.Period(START, "M")]
    df["japan_extrapolated"] = jp_flag.reindex(df.index).fillna(0).astype(int)

    five = ["us_m2_bn", "euro_area_m2_bn", "china_m2_bn", "japan_m2_bn", "uk_m2_bn"]
    g3 = ["us_m2_bn", "euro_area_m2_bn", "japan_m2_bn"]   # kept for backward-compat
    df["global_m2_bn"] = df[five].sum(axis=1).where(df[five].notna().all(axis=1))
    df["global_m2_g3_bn"] = df[g3].sum(axis=1).where(df[g3].notna().all(axis=1))

    out = df.round(1)
    out["japan_extrapolated"] = df["japan_extrapolated"]
    out.index = out.index.to_timestamp().strftime("%Y-%m-%d")
    out.index.name = "date"
    out.to_csv(os.path.join(OUT_DIR, "GLOBAL_M2.csv"))
    g = df["global_m2_bn"].dropna()
    njp = int(df["japan_extrapolated"].sum())
    print(f"  GLOBAL_M2.csv        {len(out):4d} rows  (China current via paste; UK=BoE M4; Euro=ECB API)")
    print(f"     global_m2 (5-country) complete {g.index[0]} -> {g.index[-1]}  "
          f"(Japan extrapolated for last {njp} months)")


if __name__ == "__main__":
    print(f"Writing macro CSVs to {OUT_DIR}\n")
    build_ism()
    build_net_liquidity()
    build_us_m2()
    build_global_m2()
    print("\nDone.")
