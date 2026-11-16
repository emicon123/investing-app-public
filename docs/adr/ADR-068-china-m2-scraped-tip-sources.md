# ADR-068 — Scraped tip sources for China M2

**Date:** 2026-08-04
**Status:** Accepted
**Extends:** ADR-046 (automated Japan/China M2)
**Related:** ADR-042 (macro de-risk), ADR-044 (blend factor)

## Context

`M2_CN` has been fed since ADR-046 by `chinadata.live`, a third-party PBOC aggregator, chosen
because PBOC itself publishes only Chinese-primary HTML tables. It is a **snapshot** service,
and its snapshot went stale: on 2026-08-04 its API still ended at **2026-05** (`storage.version:
2026-06-30T101509Z`, unchanged for five weeks) while PBOC had published June in mid-July.

That single lagging series had disproportionate effect. `computeGlobalM2` only accepts a month
where US + Euro area + China + Japan are *all* present, and every other component had June:

| series | source | newest (2026-08-04) |
|---|---|---|
| M2SL (US) | FRED | 2026-06-01 |
| M2_EA | ECB | 2026-06-01 |
| M2_UK | BOE | 2026-06-01 |
| M2_JP | BOJ | 2026-06-01 |
| **M2_CN** | **CHINADATA** | **2026-05-01** |
| GLOBAL_M2_USD | DERIVED | **2026-05-01** |

So China alone capped Global M2 at May. At 95 days old `M2_CN` had also crossed
`M2_STALE_DAYS = 90`, flipping live `macro_state.global_m2_stale` to `true`. That flag only
freezes the legacy `risk_off` boolean (telemetry since ADR-044) — `blend_factor` kept computing
regardless, but its Global-M2 leg was running a month behind. **It degraded quietly**, which is
the part worth fixing.

### Every alternative API was rejected

Searched and probed on 2026-08-04:

| candidate | key? | result |
|---|---|---|
| FRED `MYAGM2CNM189N` | free | **Discontinued** — last observation 2019-08 |
| DBnomics → NBS `M_A0D01` | none | Free and clean, but ends **2026-02** |
| NBS China `data.stats.gov.cn` (authoritative) | none | **403 Forbidden** (WAF URL ACL) |
| IMF IFS (legacy + `api.imf.org`) | none | Empty / `No such dataflow found` |
| OECD SDMX | none | `NoResultsFound` for China monetary aggregates |
| TradingEconomics API | — | Guest tier **discontinued (410)**; paid only |
| FXMacroData | yes | `401 — requires a Professional API key` |
| MetricsMonster | yes | `401 invalid_api_key`; no free tier |
| econdb | yes | `Authentication credentials were not provided` |
| EODHD (`demo`) | yes | `403 Forbidden` |
| Nasdaq Data Link | yes | Incapsula bot-block |

A keyed free tier would have been acceptable — our volume is ~7 requests/month — but China M2
sits behind the paywall on every aggregator that carries it fresh, and the genuinely free ones
have dropped the series or lag worse than chinadata.live.

## Decision

Add **two scraped tip sources**, `TRADINGEC` and `YCHARTS`, that extend `M2_CN` past the
chinadata.live backbone.

**They are tip sources, not history sources.** Each returns at most one point — the latest
month. `chinadata.live` remains the backbone supplying 2015→present; a lone scraped point could
not support the 12-month YoY `computeGlobalM2` needs, so replacing the backbone with a scrape
was never on the table. This is why the user's framing of "scrape first, chinadata.live as
fallback" was inverted during implementation: chinadata.live is the base, and the scrapes are a
forward extension of its tip.

### The one rule that makes it safe

`extendChinaM2Tip` runs **after** the backbone upsert and writes only months **strictly newer**
than the newest `CHINADATA` row. Everything else follows from that:

- Scrapes can never revise history, and never contend with the backbone over a shared month.
- **Self-healing.** When chinadata.live finally publishes the month a scrape covered,
  `upsertSeries` overwrites that row back to `CHINADATA` (it overwrites unconditionally), the
  backbone tip advances, and the month drops out of scrape consideration permanently.
- With no `CHINADATA` rows at all, the method does nothing — there would be no backbone value to
  sanity-check a scrape against.

### Precedence

`MANUAL` = `CHINADATA` (0) > `TRADINGEC` (1) > `YCHARTS` (2). A candidate may overwrite an
existing row only if its precedence is at least as good, so YCharts never clobbers a
TradingEconomics read of the same month, a deliberate human paste (ADR-046's
`POST /market-data/macro/m2-manual`) outranks both scrapes, and a source can still revise its
own earlier value.

### Parsing, and why each anchor was chosen

**TradingEconomics** cross-checks two independent static-HTML anchors:
`"temporalCoverage": "<start>/<end>"` (JSON-LD) supplies the date; the headline sentence
(`Money Supply M2 in China increased to 356709.76 CNY Billion in June from …`) supplies the
value *and* a month name. **If the prose month disagrees with `temporalCoverage`, the read is
rejected** — otherwise a page that refreshed one anchor but not the other would silently pair a
new date with an old value.

**YCharts** parses the JSON-LD observation object
(`"value": 356710.84300 … "observationDate": "2026-06-30" … "priceCurrency": "CNY"`) rather than
the visible prose, because the prose rounds to 4 significant figures ("356.71T") — coarser even
than chinadata.live's own 100-million-CNY rounding — while the JSON-LD carries full precision.
Requiring `"priceCurrency": "CNY"` inside the same object stops the match drifting onto another
indicator.

Both pages need a browser `User-Agent`; a default Java UA is served a bot wall. Both quote CNY
**billions** and are converted ×1000 to CNY **millions**, matching `ChinaDataLiveClient`.

### Plausibility guards

A wrong value here propagates into `GLOBAL_M2_USD` and therefore `blend_factor`, which steers
real purchases — so a scrape that looks even slightly off is discarded, not trusted:

- strictly newer than the backbone (the core invariant);
- never a month in the future;
- at most `M2_CN_TIP_MAX_GAP_MONTHS = 18` months past the backbone;
- within `M2_CN_TIP_TOLERANCE = ±30%` of the newest backbone value. China's M2 grows ~8%/yr, so
  even a full year of backbone lag stays well inside the band; it exists to catch **misparses**,
  which fail by orders of magnitude (a units slip is 1000×, a truncated digit ≥10×) rather than
  by a few percent.

Everything is fail-soft: any network error, layout change, or failed check yields an empty list
and a `WARN`, never an exception.

## Consequences

**Verified live from the Pi on 2026-08-04**, both scrapers agreeing to within 0.0003%:

```
TE     : MacroPoint[date=2026-06-01, value=356709760.00]
YCharts: MacroPoint[date=2026-06-01, value=356710843.00]
China  : MacroPoint[date=2026-05-01, value=353670000]     ← backbone
```

June is written from `TRADINGEC` (higher precedence), which un-stales `M2_CN`, lets
`computeGlobalM2` produce a June month, and moves `blend_factor`'s Global-M2 leg forward.

**This is scraping, and it will break.** Both parsers are pinned by unit tests holding HTML
fragments copied verbatim from the live pages, so an upstream redesign surfaces as a failing
test rather than a silently empty tip. When it does break the system degrades to exactly the
pre-ADR-068 behaviour — backbone-only, `global_m2_stale` eventually true — plus a `WARN` naming
the source and the missing anchor. The manual-paste seam remains as the last resort.

**No new dependency.** Regex over the response body, no HTML parser added; both clients follow
the existing keyless-client shape (`RestClient`, 5s connect / 20s read, static package-private
`parseHtml` for testing).

**ToS.** Both are public pages read at ~7 requests/month by a single self-hosted instance for
personal use. Neither offers a free API that would serve this need. This is a pragmatic
trade-off, not a claim of endorsement; if either publisher objects the fix is to drop that
client, and the backbone plus manual paste still work.

Implementation: `TradingEconomicsChinaM2Client`, `YChartsChinaM2Client`,
`MacroDataService.extendChinaM2Tip`, `V52__china_m2_scrape_sources.sql` (extends the
`macro_observations_source_check` constraint; `source` stays `VARCHAR(10)` — `TRADINGEC` is 9
characters, `YCHARTS` is 7).
