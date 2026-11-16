# ADR-091: PPI YoY / Core PPI YoY Calendar Enrichment via TradingEconomics Public Calendar

**Date:** 2026-09-10
**Status:** Accepted
**Related:** `docs/architecture/diagrams/145-ppi-yoy-calendar-enrichment.md` (class + sequence
diagrams, required reading before implementing), `docs/architecture/01-system-context.md`
(External calls from Spring Boot), `docs/architecture/06-data-flow.md` §10 (new section this
ADR adds), `backend/src/main/java/pl/investing/service/bot/ForexFactorySyncService.java`,
`backend/src/main/java/pl/investing/entity/BotCalendarEvent.java`, `infra/bot/daily-plan.sh`,
`infra/bot/prompt.md`, `.claude/tasks/145-ppi-yoy-calendar-enrichment.task.md`

## Context

The `infra/bot/` daily-plan pipeline (a leveraged BTC scalping bot) syncs a USD economic
calendar from ForexFactory/FairEconomy's free, no-auth JSON feed
(`https://nfs.faireconomy.media/ff_calendar_thisweek.json` + `..._nextweek.json`, via
`ForexFactorySyncService`) into `bot_calendar_event`, then has `claude -p` generate
LONG/SHORT/HOLD scenarios per HIGH/MEDIUM-impact USD event.

On 2026-09-10, BTCUSDT moved -0.86% on 3.4x normal volume in the 12:30-12:45 UTC candle — a
real, high-conviction move — with **zero scenario coverage**, because the bot's calendar had
no line item for what actually surprised: **PPI y/y** came in at 5.4% vs. 5.3% consensus (BLS
actual confirmed via `bls.gov/news.release/ppi.nr0.htm`; consensus confirmed independently via
`investinglive.com`). ForexFactory's feed carried `Core PPI m/m` and `PPI m/m` for USD that day
(both landed exactly on consensus → correctly HOLD) but **no `PPI y/y` or `Core PPI y/y` row at
all** — this was confirmed by dumping the raw feed, not inferred.

### First finding: the CPI half of the ask is not actually a gap

Before designing a fix, this ADR's research re-fetched the live ForexFactory feed
(2026-09-10) and found USD `CPI y/y` and `Core CPI y/y` **do** appear as their own rows (High
impact, alongside `CPI m/m`/`Core CPI m/m`, all dated 2026-09-11):

```
Core PPI m/m | High | 2026-09-10 | 0.3%  (no PPI y/y or Core PPI y/y row exists for this date)
PPI m/m      | High | 2026-09-10 | 0.4%
Core CPI m/m | High | 2026-09-11 | 0.2%
Core CPI y/y | High | 2026-09-11 | 2.4%
CPI m/m      | High | 2026-09-11 | 0.4%
CPI y/y      | High | 2026-09-11 | 3.4%
```

So the gap is narrower than originally scoped: **only `PPI y/y` and `Core PPI y/y` are
missing from ForexFactory for USD.** CPI needs no fix. This matters for the decision below —
whatever source is chosen only needs to cover two specific indicators, not a general "PPI/CPI
y/y" surface, which keeps the fix small (YAGNI).

### Sources evaluated

| Source | Verdict | Why |
|---|---|---|
| **ForexFactory (current)** | Confirmed gap for PPI y/y / Core PPI y/y; confirmed **not** a gap for CPI y/y / Core CPI y/y | Re-fetched and dumped raw feed, see above. Stays the primary/unchanged calendar source. |
| **Investing.com** | Rejected | `getCalendarFilteredData` returns HTTP 403 behind a Cloudflare "Just a moment..." JS challenge. Headless Chromium is confirmed broken/hangs forever on this Pi (memory: `headless-chromium-broken-on-this-pi.md`) — a Playwright/Puppeteer route is not viable here, full stop. |
| **FXStreet calendar API** (`calendar-api.fxstreet.com`) | Rejected | Re-tested 2026-09-10: `GET /en/api/v1/eventDates/...` returns **HTTP 401** unauthenticated — requires a paid/registered key, not a clean free source. |
| **TradingEconomics official Calendar API** | Rejected | Paid, per prior research — not re-tested, no reason to expect otherwise. |
| **TradingEconomics indicator-history AJAX widget** (`/ws/stream.ashx`, per-indicator chart JSON) | Not pursued further | Turned out unnecessary — see next row. Would have been a reasonable fallback if the public calendar page hadn't worked. |
| **TradingEconomics public `/calendar` and `/united-states/calendar` pages** | **Adopted** | See "Decision" below — unauthenticated, server-rendered, has exactly the two missing fields with a real consensus figure. |
| **EconPulse (econpulse.io)** | Rejected | Re-probed 2026-09-10: every guessed REST path (`/api/v1/indicators`, `/api/v1/cpi`, `/api/v1/latest?key=demo`, etc.) 404s and falls through to the SPA's client-router shell, not JSON. Grepped the main JS bundle (`index-DoC56q0-.js`, 364KB) for any embedded API base URL — none found (it references only `fonts.googleapis.com` and a `storage.googleapis.com/gpt-engineer-file-uploads` asset host, consistent with a gpt-engineer-generated app). No real, discoverable, unauthenticated JSON endpoint was found within a bounded probing budget. Treated as an indie side-project of unverified reliability — not adopted as a dependency for a real-money trading bot, consistent with the original skepticism flagged going into this task. |
| **BLS API (`api.bls.gov`) / FRED (`fred.stlouisfed.org`, `api.stlouisfed.org`)** | Rejected as primary, kept as documented fallback basis | Official, free, reliable — but **actuals only, no forecast/consensus**, confirmed by the user's own note that BLS doesn't publish forecasts. Cannot alone produce the "expected value" the bot's surprise-magnitude scenarios are built around. See "Deferred fallback" below. |

### TradingEconomics public calendar page — verification detail

`https://tradingeconomics.com/united-states/calendar` (and the global `/calendar`) returns
**HTTP 200**, server-rendered HTML, **no Cloudflare challenge** (response headers show
`te-bot: false`, `x-cache: Hit from cloudfront` — TE serves this through their own CloudFront
distribution, not Cloudflare, and did not flag a plain `curl` request as a bot). The page embeds
real per-event data directly in the HTML (no further AJAX call needed):

```html
<a class='calendar-event' href='/united-states/producer-prices-change'>PPI YoY</a>
<span class="calendar-reference">AUG</span>
...
<span id='actual'>5.4%</span>
<span id='previous'>4.8%</span>
<a id='consensus' ...>5.3%</a>
<a id='forecast' ...>5.1%</a>
```

Confirmed for both `PPI YoY` (`data-symbol='UNITEDSTAPROPRICHA'`) and `Core PPI YoY`
(`data-symbol='USACPPY'`), dated exactly 2026-09-10 alongside the FF-tracked PPI m/m release.
The `consensus` value (5.3%) matches the independently-sourced figure from `investinglive.com`
used to root-cause the original incident — cross-validated, not just plausible-looking.

**Gap found in this source too:** the public calendar page has **no explicit impact/importance
column** (`Actual | Previous | Consensus | Forecast` are the only data columns; TE's
Low/Medium/High star rating is apparently rendered client-side or omitted from this
unauthenticated view). This is handled in the Decision below by not needing TE for impact at
all.

## Decision

### 1. TradingEconomics public calendar page as a narrow, second enrichment source — USA PPI y/y + Core PPI y/y only

Add a second sync path that scrapes `tradingeconomics.com/united-states/calendar`
(server-rendered HTML, Jsoup — same library/technique already used for
`packages/shared/java`'s `BiznesRadarClient`, ADR-078) for exactly two `data-symbol` values —
`UNITEDSTAPROPRICHA` (PPI YoY) and `USACPPY` (Core PPI YoY) — and extracts `actual`,
`consensus`, `previous`. No other country, indicator, or field is in scope, per the user's
explicit instruction not to spend effort generalizing to CNY/JPY/etc.

### 2. Anchor `eventTime` and `impact` to the sibling ForexFactory row, not to TE's own display

TE's calendar row does not carry a machine-parseable timezone for its "12:30 PM" display, and
carries no impact rating at all. Rather than reverse-engineer TE's display timezone or invent an
impact heuristic, the new sync service looks up the **sibling `PPI m/m` / `Core PPI m/m`**
`bot_calendar_event` row (source `FAIRECONOMY`, same USD calendar day) that `ForexFactorySyncService`
already wrote, and reuses that row's `eventTime` and `impact` verbatim for the new `PPI YoY` /
`Core PPI YoY` row. This is correct because they are literally the same BLS release, published
at the identical instant — PPI y/y is not a separate release with its own schedule. It also
means TE is never trusted for scheduling or impact, only for the three value fields, which
narrows the blast radius of "TE changes their markup" to "today's y/y enrichment is skipped,"
never to "the bot mis-schedules or mis-prioritizes an event."

This forces an **ordering dependency**: the ForexFactory sync must run and commit before the
TradingEconomics sync looks for a sibling row on the same `/bot/calendar/sync` invocation. See
the diagram doc for how this is enforced (explicit two-step orchestrator, not a Spring
`List<T>`-injected, order-unspecified strategy fan-out).

### 3. `consensus`, not TE's own `forecast`, maps to `bot_calendar_event.forecast`

TE exposes two distinct numbers — `consensus` (the market/analyst-poll expectation) and its own
`forecast` (TE's in-house model estimate). ForexFactory's single `forecast` field is
semantically the market consensus, and that is what `infra/bot/prompt.md`'s surprise-magnitude
scenarios are built against (`surprise = actual - forecast`). TE's `consensus` field is mapped
into the existing `forecast` column; TE's own `forecast` field is discarded — not stored,
not exposed — to avoid confusing two different meanings under one column name.

### 4. No schema changes

`bot_calendar_event` already has a `source` column (`"FAIRECONOMY"` default) and a globally
unique `external_id`. The new rows use `source = "TRADINGECONOMICS"` and an identity hash
namespaced with a `"TE|"` prefix (`hash("TE|" + eventTime + "|" + title + "|" + currency)`) so
they can never collide with a FairEconomy-sourced `external_id` even in the (currently
impossible, since titles differ) case of an identical `eventTime|title|currency` string. No
Flyway migration is needed for this task.

### 5. `daily-plan.sh` / `prompt.md` need no changes

`prompt.md` already instructs Claude to build a 7-scenario ladder "for EACH HIGH or
MEDIUM-impact USD event today" from the generic `CALENDAR` array
(`{ eventTime, currency, eventName, impact, forecast, previous, actual }`) and already handles
same-`eventTime` clusters explicitly (its own worked example is literally "Core PPI m/m + PPI
m/m + Unemployment Claims all at 12:30 UTC"). Since the new `PPI YoY` / `Core PPI YoY` rows
share `eventTime` and `impact` with their m/m siblings by construction (Decision #2), they
become a fourth clustered event at that same timestamp with zero prompt or shell-script changes
— the existing per-event scoring + downstream signed-leverage summation already covers this.

### 6. Deferred fallback (documented, not built)

If TE's calendar markup changes or the page becomes blocked, the new sync service fails soft
(same per-source try/catch pattern `ForexFactorySyncService.fetchAndUpsert` already uses) — that
day's `PPI YoY`/`Core PPI YoY` rows are simply absent, everything else (FF sync, the rest of the
plan) is unaffected. A documented, lower-confidence fallback for that failure mode — **not
implemented now, YAGNI** — is: pull the actual y/y value from FRED's official
`api.stlouisfed.org/fred/series/observations` (once `FRED_API_KEY` wiring lands, per the
already-open `fred-official-api.task.md`) and synthesize an "expected" value as a trailing
3-month average of the prior y/y prints (a naive momentum extrapolation), storing it with a
distinguishing `source = "FRED_HEURISTIC"` so the bot/human can tell a real consensus figure
from a synthetic stand-in. This is a real design, not hand-waving, but building it now would be
speculative — TE's public page works today and was verified end-to-end above.

## Consequences

- **New unauthenticated dependency on a third-party marketing page, not an API.**
  `tradingeconomics.com/united-states/calendar`'s HTML structure is not a contract — TE can
  change it without notice. This is the same risk class the project already accepted for
  `www.biznesradar.pl` (ADR-078) and `chinadata.live` (ADR-046), both documented as
  "best-effort, non-authoritative" scrapes; this ADR extends that established pattern rather
  than introducing a new risk category.
- **Request volume is trivially low** — one fetch per day (matching `sync-calendar.sh`'s 05:50
  cadence, the same cadence FF is already fetched at), well inside TE's own 5-second edge-cache
  window (`te-cache-smaxage: 00:00:05` observed on the response). No rate-limit or ToS-abuse
  concern at this volume for a personal, non-commercial tool.
- **Scoped narrowly on purpose** — exactly two `data-symbol`s, USA only, no general
  "TradingEconomics calendar client." Extending to another country or indicator later is a
  small, additive change (new symbol constants + a new sibling-lookup title), not a redesign.
- **Two-source calendar sync is now the shape of `bot_calendar_event`.** Any future third
  source should follow the same pattern: FF (or whichever source owns scheduling/impact for that
  currency) stays authoritative for `eventTime`/`impact`; a narrow secondary source only fills a
  named, confirmed field gap.
