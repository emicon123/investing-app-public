# ADR-093: BLS Direct API as the Primary Actuals Path for CPI (ForexFactory Retained as Schedule/Forecast Authority and Fallback)

**Date:** 2026-09-11
**Status:** Accepted, design + Backend build 2026-09-11. **Enabled in production 2026-09-14** —
see "Amendment 2026-09-14 — enabled" below for the go-ahead and rollout state. Original status
line preserved for history: Accepted — design + Backend build only. NOT deployed, wired live, or
enabled against `LiveExecutionService`/`OKX_DEMO_MODE=false` by this ADR. See Consequences.
**Related:** `.claude/tasks/147-bls-direct-fetch-race.task.md` (full day's empirically-gathered
findings this ADR is built on — read first, not re-derived here),
`.claude/tasks/147-architect-output.md` (file-by-file Backend handoff),
`docs/architecture/diagrams/147-bls-direct-actuals.md` (class/sequence/state diagrams, pattern
evaluation — required reading before implementing),
`docs/architecture/06-data-flow.md` §12,
`backend/.../service/bot/EventReleaseWatcherService.java`,
`backend/.../service/bot/ForexFactorySyncService.java`,
`backend/.../service/bot/BotCalendarSyncOrchestrator.java`,
`backend/.../service/bot/TradingEconomicsPpiYoySyncService.java`,
`backend/.../service/bot/NumericEventValueParser.java`,
`backend/.../repository/BotCalendarEventRepository.java`,
`docs/adr/ADR-090-okx-live-execution.md`, `ADR-091-ppi-yoy-calendar-enrichment.md`,
`ADR-092-post-trade-claude-trigger.md`

## Context

Task 147's session (2026-09-11) established, empirically, not hypothetically:

1. Scraping `bls.gov/news.release/*.htm` is dead — instant Akamai bot block, and BLS's own
   policy states automated retrieval is prohibited. Permanently out of scope, not revisited here.
2. The official **BLS Public Data API v2** (`POST api.bls.gov/publicAPI/v2/timeseries/data/`) is
   a legitimate, sanctioned machine endpoint. Verified CPI series IDs, fetchable together in one
   request: `CUUR0000SA0` (CPI-U NSA → y/y), `CUSR0000SA0` (CPI-U SA → m/m), `CUUR0000SA0L1E`
   (core NSA → core y/y), `CUSR0000SA0L1E` (core SA → core m/m). BLS returns raw index levels,
   not percent change — the consumer computes m/m and y/y itself. PPI's equivalent series IDs
   are **not yet verified**.
3. Unregistered daily quota is ~25 requests/day (no quota-remaining header exists to
   self-monitor); free registration (email only, a one-time user action) raises this to ~500/day.
4. **Live test during the real 2026-09-11 12:30 UTC CPI release**: the BLS API returned
   `HTTP 503` on every single request in the T-6s..T+6s window (presumably every algorithmic
   consumer hitting the identical endpoint the identical second). A manual check ~3 minutes later
   succeeded easily with August data present. Separately, a `"Database is locked for Series X"`
   message was observed inside an otherwise-200 response ~14 minutes *before* release — a
   second, distinct non-exceptional failure shape.
5. **In the same window, `ForexFactorySyncService` self-inflicted a real failure**: rate-limited
   (`429`) by `nfs.faireconomy.media` at T-19s from this morning's own `actuals-refresh-seconds`
   tightening (20s→5s), backing off 5 minutes; when the backoff cleared (~T+4.5min) FF's own
   upstream *still* had no CPI actual — rate-limited again 11 seconds later. Net: zero trade
   fired, root-caused to a data-acquisition failure on both ends, not a bad `stale-after-seconds`
   value (the gate was never even exercised — no data arrived in time to reach it).
6. **The two failure modes are uncorrelated** — BLS failed at the exact release second and
   recovered inside minutes; FF failed from its own polling cadence and its upstream lagged by an
   order of magnitude beyond what `actuals-refresh-seconds`/`stale-after-seconds` were tuned
   against. This is the concrete evidence this ADR argues from, not an abstract "more sources is
   better."
7. A same-day rushed attempt to wire a BLS-direct write path directly into production was
   correctly aborted: two real bugs surfaced in 15 minutes of ad hoc dry runs (an `IndexError` on
   an empty-data response; mishandling of the "Database is locked" message), and the harness's
   own permission classifier independently blocked the direct-DB-write mechanism three times.
   This sets the minimum bar for review/testing below — not an obstacle, a signal.

The task frames the core question as "race N sources, first HTTP response wins" but flags that
framing itself as unsafe (a fast-but-stale cached response could win) and asks the Architect to
resolve, on the evidence above, whether BLS-direct should be a race leg or a primary source for
BLS-native series, and to specify freshness detection with the same rigor
`NumericEventValueParser`/ADR-090's cluster netting already got.

## Decision

### 1. BLS-direct becomes the de facto primary writer of CPI `actual`; ForexFactory is neither disabled nor literally "raced" — it stays the sole schedule/impact/forecast authority and an always-on fallback actual-writer, made safe by one bug fix (§1b)

Rejected: literally demoting FF to forecast/schedule-only for CPI (never writing `actual` for
these events again), which the task raises as a bigger reframe worth arguing on merits. Also
rejected: the literal "N threads race, first HTTP response wins" mechanism from the task's
origin section. Both are unnecessary once the actual failure mode is examined closely (§1c).

**What is adopted:** `ForexFactorySyncService` is completely unchanged in scope — it remains the
only source for `eventTime`/`impact`/`forecast` on every currency and event, CPI included, and it
keeps attempting to write `actual` exactly as today. A new `BlsDirectActualsSyncService` runs
alongside it, polling BLS's official API only inside a narrow window around a CPI release, and
writes `actual` into the **same existing row** FF already created that morning (see §2) the
moment it detects a genuinely fresh value (see §3). Nothing disables FF's write path.

**Why not disable FF for CPI actuals:** today's evidence shows FF's upstream lag (≈4.5 minutes,
confirmed) makes it *functionally* irrelevant to trading once `stale-after-seconds` (45s) is
respected — by the time FF would post a real value, the cluster has already been marked
`STALE_SKIP` regardless of anything BLS does. FF's late value therefore costs nothing to keep as
a write path (it is a no-op for trading, useful only for the historical/audit record), and
removing it would need new gating logic to reintroduce that exact behavior for free (via
`stale-after-seconds`) if BLS-direct is ever disabled, misconfigured, or its series IDs go stale
before FF's does. Keeping FF's write path is simpler than building a disable mechanism, and the
project's YAGNI discipline (already applied identically in ADR-091 §"scoped narrowly on purpose")
favors the simpler option here too.

### 1b. Corequisite bug fix — a source's own missing data must never blank out a previously known value

`ForexFactorySyncService.upsertFeed` and `TradingEconomicsPpiYoySyncService.upsert` currently call
`e.setActual(actual)` **unconditionally** on every re-sync, including when the freshly parsed
value is `null` (feed hasn't posted yet). Today this is close to harmless because nothing else
writes `actual` faster than FF. Once BLS-direct can populate a CPI row's `actual` within seconds
of release — well before FF's own feed reflects it (today: BLS recoverable within ~3 minutes, FF
still blank at +4.5 minutes) — `EventReleaseWatcherService`'s unchanged 5-second `maybeRefresh`
loop (ADR-092) will call `ForexFactorySyncService.sync()` several times *after* BLS has written a
correct value and *before* FF's own upstream catches up. Each of those calls would silently erase
the good BLS-derived actual back to `null`, defeating the entire point of this feature.

**Fix (both call sites, `actual` field only — `forecast`/`previous`/`eventTime`/`impact` are
untouched since nothing else races to write those):**

```java
// ForexFactorySyncService.upsertFeed — was unconditional e.setActual(actual);
if (actual != null) {
    e.setActual(actual);
}
```

```java
// TradingEconomicsPpiYoySyncService.upsert — was unconditional e.setActual(row.actual());
if (row.actual() != null) {
    e.setActual(row.actual());
}
```

TE's own scope (PPI YoY/Core PPI YoY) is untouched by BLS-direct in this ADR (CPI-only, §6), so
this second fix has no live effect yet — it is applied now, at zero risk, because it is the same
bug pattern and leaving it inconsistent would silently reopen this exact failure the moment a
PPI-direct follow-up lands. A dedicated regression test (`ffReSync_afterBlsWroteActual_doesNotBlankItOut`,
architect-output §8) locks this in — this is arguably the single most load-bearing test in the
whole feature.

### 1c. This is not a race-orchestration problem — the existing DB-tick architecture already resolves "first fresh write wins"

`EventReleaseWatcherService.doTick` reads `bot_calendar_event.actual` fresh from the database on
every 1-second tick (ADR-090 §2). Because BLS-direct and FF both write into the **same row**
(§2 — not separate rows), "whichever source's value lands in the database first is what the very
next tick sees and trades on," with zero new concurrency-race code. PostgreSQL's own row-level
write serialization already adjudicates "which write happened last" correctly; the only thing
that could go wrong is a *later* write blanking out an *earlier, correct* one, which §1b closes.
This is a deliberate simplification of the task's original "N concurrent threads, first HTTP
response wins" framing — no `CompletableFuture.anyOf`, no thread pool, no dedicated race
primitive. It is also safer: it never has to adjudicate "which HTTP call technically finished
first," a question that in the naive framing already produced the flagged risk (a fast, cached,
stale response beating a slow, correct one). Here, "stale beats fresh" is impossible **except** via
the §1b bug, because a source's own genuinely fresh value always eventually overwrites a stale
one, and only an unconditional-null-write can move data backward in time.

### 2. BLS-direct updates the existing FF-authored row — it never creates a new row (diverges from ADR-091's TE-PPI-YoY precedent, deliberately)

ADR-091's `TradingEconomicsPpiYoySyncService` creates **brand-new** `bot_calendar_event` rows
because ForexFactory had zero PPI y/y coverage at all — no row, no scenario, nothing to attach to.
CPI is different: FF **already** carries `"CPI m/m"`, `"CPI y/y"`, `"Core CPI m/m"`,
`"Core CPI y/y"` rows (exact strings verified live 2026-09-10, quoted in ADR-091's own context
section) with real `eventTime`/`impact`/`forecast` populated hours before release by the 05:50
cron sync, and the 06:00 `daily-plan.sh` run has already created `bot_plan_scenario` rows whose
`calendar_event_id` FK points at those **specific existing row IDs**. If `BlsDirectActualsSyncService`
followed TE's create-a-new-row pattern, the scenario ladder would never reference the new row and
BLS's `actual` would be invisible to `EventReleaseWatcherService.loadSnapshots` (which resolves
`actual`/`forecast`/`previous` by following the scenario's own `calendarEvent` FK, falling back to
an `eventName`+`eventTime` lookup only when that FK is null — see `loadSnapshots`'s existing
fallback branch). So `BlsDirectActualsSyncService` must look up the **existing** same-day USD
`FAIRECONOMY` row for each of the four event names (reusing
`BotCalendarEventRepository.findByCurrencyAndEventTimeBetweenAndSource`, the exact same query
TE's sibling lookup already uses) and `UPDATE` only that row's `actual` + new `actualSource`
column (§8) — never `INSERT`.

### 3. Freshness detection — period-code comparison against a captured pre-release baseline

"Did the HTTP call succeed" is not freshness — a 200 OK during the pre-release window correctly
returns July data every time, and naively treating any 200 as a win would trade on stale data. The
mechanism, precisely:

**Baseline capture (once per release day, hours before release, cheap):** `BotCalendarSyncOrchestrator.syncAll()`
gains a third step, `blsDirectActualsSyncService.captureBaselineIfNeeded(now)`, called after FF
and TE. It looks up today's FF-authored `"CPI m/m"` row (the anchor, mirroring TE's own
portfolio-bnchor pattern) to confirm a CPI event exists today; if none, no-op, zero HTTP calls. If
one exists and no baseline has been captured yet **today** (in-memory `LocalDate` guard,
Europe/Warsaw), it makes exactly **one** BLS API call for all four series and records, per series,
the latest `(year, periodNum)` currently on file. This runs in practice at the 05:50/06:00 cron
pass — hours before the 12:30/13:30 UTC release — specifically so that the baseline-capture call
itself is never exposed to the pre-release congestion/lock window (the "Database is locked"
message was observed just 14 minutes before release; capturing the baseline at 06:00 avoids that
entirely). The guard is idempotent, so every later `syncAll()` call during the watch window
(triggered every 5s by `EventReleaseWatcherService.maybeRefresh`, unchanged) costs nothing beyond
a date check once a baseline exists. If the backend restarted between 06:00 and release (baseline
lost), the next `syncAll()` call — including one inside the watch window — captures it late,
which is a real but accepted residual risk (an extremely narrow timing window), not engineered
around further per YAGNI.

**Poll-eligible window, per event:** `[eventTime + pollStartOffsetSeconds, eventTime + staleAfterSeconds]`
(§7 on the offset; `staleAfterSeconds` is `BotExecutionProperties`'s existing 45s value, reused —
polling past that boundary can never produce a tradeable signal, since `EventReleaseWatcherService`
will have already resolved the cluster to `STALE_SKIP` by then).

**Per-poll freshness test (all four series, one HTTP call):**
1. Parse each series' `data[]` into `(year, periodNum, value)` tuples, **explicitly sorting
   descending by `year*100+periodNum`** rather than trusting response array order or BLS's own
   `"latest":"true"` convenience flag (defense in depth — the task's own evidence shows BLS's API
   is under real stress near release, an environment where trusting an unverified ordering
   convention is exactly the kind of assumption that bites).
2. If `data[]` is empty or unparseable for any series → **not fresh**, log and retry next tick.
   This is the direct, deliberate fix for today's `IndexError` dry-run bug: the empty-list case is
   checked *before* any indexed access, never after.
3. A response's `status` field or a non-empty `message[]` array is checked for known soft-fail
   substrings (case-insensitive): `"database is locked"`, `"results not found"`, `"no data
   available"`, `"series does not exist"`. **Any match, or any unrecognized non-empty `message[]`
   at all, is treated as not-fresh and retried** — a conservative default (fail toward "keep
   waiting," never toward "trust an unfamiliar message"), because a wrong guess here feeds a real
   leveraged order.
4. A series is **candidate-fresh** only if its sorted-descending head `(year, periodNum)` is
   strictly newer than the captured baseline for that exact series.
5. All **four** series must be candidate-fresh with the **same** `(year, periodNum)` before
   anything is written. A same-release mismatch across series (one series ahead of the others) is
   logged at WARN and treated as not-yet-ready, deliberately more conservative than the existing
   cluster-netting's own "missing sibling contributes 0" tolerance (ADR-090 §3) — that tolerance
   exists for genuinely separate events sharing a timestamp; here all four numbers describe the
   same monthly release and must appear as a single atomic unit before any of them influences a
   trade decision.
6. Only once all four are aligned-fresh does §5 (value computation) run and write. On success the
   per-event poll state is marked `resolved` for the day — no further BLS calls for this event.

### 4. Failure handling for BLS's two failure shapes, and quota discipline

Per-event poll state (`attempts`, `lastAttemptAt`, `backoffUntil`, `resolved`), keyed by the
anchor row's `eventTime` and pruned once older than the watch window closes (same
`removeIf`-on-tick pattern `EventReleaseWatcherService` already uses for `resolvedClusters`):

- **HTTP 503 / timeout / connection failure:** logged at WARN, `backoffUntil = now + backoffSeconds
  + jitter(0..jitterMaxSeconds)`. The random jitter is a direct, low-cost response to the
  documented risk that a fixed-cadence poller lands on the exact same second as every other
  algorithmic consumer of the same public endpoint (today's 503 storm spanned every tick of a
  fixed 2-second cadence for 12 seconds straight) — it does not solve congestion, but it stops
  this bot from being deterministically synchronized to it.
- **200 OK with a soft-fail message (`"Database is locked for Series X"` and the other patterns in
  §3.3):** treated identically to a 503 for backoff purposes — the two are symmetric "try again
  shortly" signals. The ADR does not claim confidence about which failure mode resolves faster;
  today's data doesn't distinguish them well enough to justify different backoff constants.
- **200 OK, structurally valid, but not yet the new period:** the expected steady state before the
  true release second — no backoff, just wait for the next normal-cadence tick.
- **Per-event hard cap:** `maxRequestsPerEvent` (default 10) stops polling for that event even if
  the poll-eligible window (45s wide at defaults) isn't over, deliberately trading a small amount
  of window coverage for a bounded worst-case quota spend on a single bad day.
- **Global daily hard cap:** `maxRequestsPerDay` (default 20, Europe/Warsaw midnight reset)
  independent of the per-event cap, sized to stay safely under the historically-observed ~25/day
  unregistered ceiling even after the 1 baseline-capture call.
- **Registration (500/day) is a stated operational prerequisite, not merely a nice-to-have,** per
  the task's own conclusion — this ADR does not require it to build or test the feature (the
  20/day default default is deliberately unregistered-safe), but flipping `app.bot.bls.enabled`
  to `true` in a real production window should not happen before the free BLS registration is
  done and `BLS_API_KEY` is configured, so that a bad day (the exact day this feature matters
  most) doesn't also become a quota-exhaustion day.

### 5. Value computation — exact formulas, series mapping, and format-matching against `NumericEventValueParser`

BLS returns raw index levels, not percent changes. Getting the series-to-computation mapping
backwards is a *silent wrong-number* bug, not a crash — exactly the class of mistake this ADR
must foreclose, since a rushed reimplementation would still compile and still "work" while feeding
a subtly wrong `surprise` into the ladder matcher.

| Series ID | Meaning | Feeds |
|---|---|---|
| `CUSR0000SA0` | CPI-U, seasonally adjusted | **m/m** for `"CPI m/m"` |
| `CUUR0000SA0` | CPI-U, not seasonally adjusted | **y/y** for `"CPI y/y"` |
| `CUSR0000SA0L1E` | Core CPI (ex food/energy), SA | **m/m** for `"Core CPI m/m"` |
| `CUUR0000SA0L1E` | Core CPI (ex food/energy), NSA | **y/y** for `"Core CPI y/y"` |

**m/m** (SA series only): find the entry immediately preceding the latest by calendar month
(handling December→January year rollover explicitly, not by a fixed array offset — the request
window is 3 calendar years wide specifically so this lookup never falls off the edge):
`momPct = (latest.value − prior.value) / prior.value × 100`.

**y/y** (NSA series only): find the entry with `year == latest.year − 1` and the **same**
`periodNum` as latest (not a fixed 12-entries-back offset, in case of any gap in the returned
series): `yoyPct = (latest.value − sameMonthLastYear.value) / sameMonthLastYear.value × 100`.

Both computed in `BigDecimal` (never `double`), divided to at least 6 fractional digits with
`RoundingMode.HALF_UP`, then `.setScale(1, RoundingMode.HALF_UP)` — **one decimal place**, matching
the convention CPI/PPI headline prints are always publicly reported in (confirmed by ADR-091's own
verified real values: `"5.4%"`, `"0.3%"`). Formatting to two decimals instead of one would not
crash anything downstream but would introduce a systematic ~0.05 percentage-point mismatch against
every `forecast` figure (which FF/TE always quote to one decimal), corrupting every `surprise`
calculation by an amount comparable to a real month-to-month move — exactly the kind of error that
looks like working code and produces wrong trades.

**Formatted string:** `pct.toPlainString() + "%"` — e.g. `"0.3%"`, `"-0.1%"`, `"5.4%"`. No leading
`+` for positive values, `BigDecimal.toPlainString()`'s native leading `-` for negative ones,
matching FF/TE's own observed convention (never a `"+0.3%"`) and confirmed against
`NumericEventValueParser.parse` directly: its `DECIMAL` regex accepts an optional leading `[+-]`
and its suffix-strip step handles a trailing `%` without multiplying — `parse("-0.1%")` →
strips `%` → `"-0.1"` → matches → `BigDecimal("-0.1")`. No transformation is needed on the
consumer side; this format was chosen specifically because it round-trips through the existing
parser unmodified.

**Write:** all four rows in one method, all-or-nothing per §3.6 — `actual = formatted`,
`actualSource = "BLS_DIRECT"`, `updatedAt = now`. A target row missing entirely (FF failed to
create it that morning, unlikely but not impossible) is logged at WARN and skipped individually —
this is not stricter than the existing "missing sibling contributes 0" tolerance in cluster
netting, it only governs whether *this* write attempt proceeds for *that specific* row.

### 6. Scope: CPI only. PPI is explicitly deferred, not built in this pass

The task scopes CPI+PPI as in-bounds but states PPI's series IDs are unverified. Verifying them
requires an actual research pass against the live BLS API (not code-only work), and guessing them
risks the exact silent-wrong-series-mapping failure class §5 is designed to foreclose — for PPI
specifically, worse than a formatting mistake, since a wrong series ID could silently source data
for the wrong indicator entirely. Building it now on an unverified guess would be worse than
deferring by one verification step. Consistent with this codebase's own established narrow-scoping
precedent (ADR-091: "Scoped narrowly on purpose... Extending to another country or indicator later
is a small, additive change... not a redesign"), `BlsDirectActualsSyncService` is structured around
an explicit per-indicator-family descriptor (series IDs + event names + change-type mapping) so
that adding PPI later is exactly that kind of small additive change — a second descriptor entry —
once its series IDs are verified. This is flagged as a likely near-term follow-up (the task's own
origin section notes the *next* live BLS release this feature could catch is plausibly PPI, not
CPI), not a speculative one, but is not shipped speculatively today.

### 7. Poll start-offset: tunable config, default 0, explicitly not validated

The task raises a real, plausible-but-unproven theory: polling exactly at `T=0` may be the worst
time to start (thundering herd of every other algorithmic consumer hitting the identical endpoint
the identical second — consistent with today's observed 503 storm spanning exactly T-6s..T+6s), and
a short deliberate delay might reach a less congested API sooner in wall-clock terms than
repeatedly retrying through the congestion spike. **This ADR does not adopt a non-zero default
on one data point.** `pollStartOffsetSeconds` defaults to `0` (poll starting exactly at
`eventTime`, the intellectually honest "we don't yet have evidence to justify being clever"
choice) and is fully tunable via `BlsDirectProperties` without a deploy. One occurrence of
congestion could be release-specific (a widely-watched CPI print) rather than structural. The
delayed-start theory should be validated across multiple real releases — the random jitter in §4
is a partial, low-cost hedge against exact lockstep congestion that does not require betting on
the offset theory being right, and is adopted independently of it.

### 8. Schema, config, integration points

**Flyway `V71__bot_calendar_actual_source.sql`** (next free — head is V70):

```sql
ALTER TABLE bot_calendar_event ADD COLUMN actual_source VARCHAR(20);
```

One nullable column, no CHECK constraint — deliberately matching `source`'s own unconstrained
`VARCHAR(20)` precedent (V63) for consistency, not an oversight. `actual_source` is intentionally
**separate from `source`**: `source` means "who authored this row's `eventTime`/`impact`/
`forecast`/schedule" (FF or TE, unchanged meaning) and must never be repurposed to mean "who last
wrote `actual`" — those are two different questions once more than one path writes into the same
row (this is the concrete reason CPI diverges from ADR-091's simpler TE-owns-its-own-new-row
case). Existing rows and any FF/TE-only row simply carry `actual_source = NULL` (equivalent to
"unknown/implicitly FAIRECONOMY," the pre-existing behavior — no backfill needed).

**No OpenAPI change** — matches ADR-090/091/092's own precedent: the entire bot module still has
zero OpenAPI paths, and this feature adds no new HTTP endpoint (BLS-direct is an internal
scheduled component exactly like FF/TE). `BotCalendarEventDto`/`BotMapper` gain the additive
`actualSource` field for calendar-view visibility (useful for debugging exactly this kind of
multi-source race) — a DTO change, not a contract change, same distinction ADR-090's handoff
already drew.

**Config — new `BlsDirectProperties` (`app.bot.bls.*`), off by default:**

```yaml
app:
  bot:
    bls:
      enabled: false                       # explicit deploy gate — see Consequences
      base-url: https://api.bls.gov/publicAPI/v2/timeseries/data/
      registration-key: ${BLS_API_KEY:}
      poll-interval-seconds: 3
      poll-start-offset-seconds: 0         # tunable; NOT validated across multiple releases (§7)
      backoff-seconds: 3
      backoff-jitter-max-seconds: 1
      max-requests-per-event: 10
      max-requests-per-day: 20
```

**Integration points (both additive, no existing call site restructured):**

1. `BotCalendarSyncOrchestrator.syncAll()` gains a third step,
   `blsDirectActualsSyncService.captureBaselineIfNeeded(now)`, after TE's `sync()`. Does not
   affect the `int` upsert-count return value (baseline capture writes no calendar rows).
2. `EventReleaseWatcherService.doTick`, immediately alongside the existing `maybeRefresh(now)`
   call (same `if (clusters.isEmpty()) return;` gate, so BLS polling never runs on a day with zero
   live scenario clusters), adds `blsDirectActualsSyncService.maybeRefresh(now)`.

Both new methods check `enabled` first, before any repository query — the cheapest possible
no-op when the feature is off.

## Alternatives considered

| Alternative | Verdict | Why |
|---|---|---|
| **Literal N-thread race, first HTTP response wins** (task's original framing) | Rejected | The task's own flagged risk (fast-but-stale beats slow-but-correct) is real for this framing and would need a bespoke freshness arbiter built on top anyway. §1c shows the existing DB-tick architecture already gives the correct "first fresh write wins" behavior for free once §1b's null-guard fix is applied — building a race primitive would be solving an already-solved problem with more code. |
| **Demote FF to forecast/schedule-only for CPI** (disable its `actual` writes entirely) | Rejected | Costs a new gating mechanism to build; buys nothing, since `stale-after-seconds` already makes FF's late `actual` a trading no-op, and keeping it costs nothing once §1b is fixed. Also reduces robustness for the case BLS-direct itself is disabled, misconfigured, or breaks before FF does — removes a free fallback for no measured benefit. |
| **BLS-direct writes new rows, mirroring ADR-091's TE pattern exactly** | Rejected | Correctness-breaking, not just suboptimal — CPI rows and their scenarios already exist by the time BLS-direct would run (§2); a new row would be invisible to the scenario ladder built at 06:00. This is the single most important divergence from the ADR-091 precedent and the reason this design cannot be "TE, but for BLS." |
| **Scrape `bls.gov/news.release/*.htm` with better headers/fingerprinting** | Rejected, out of scope permanently | Already ruled out in the task itself — Akamai bot-blocks on request #1, and BLS's own usage policy states this is prohibited. Not revisited. |
| **Build PPI support now alongside CPI** | Deferred | PPI series IDs are unverified; guessing risks silently sourcing the wrong indicator. See §6. |
| **Hardcode a delayed poll-start offset from today's single 503-storm observation** | Rejected | One data point is not evidence of a structural pattern (today's release could have been unusually contested). See §7 — default stays 0, offset is tunable, jitter is adopted independently as a lower-risk hedge. |

## Consequences

- **This ADR's implementation is design-and-build only.** `app.bot.bls.enabled` defaults to
  `false`; both new integration points (§8) no-op instantly when disabled. Backend implements,
  tests (Testcontainers + WireMock, no live network calls), and merges this code, but **it must
  not be flipped to `true` in the running backend, and must not be wired into any path that
  reaches `LiveExecutionService`/`OKX_DEMO_MODE=false` beyond what already exists (the flag gates
  the write path itself, so this is inherent, not an additional precaution) without a separate,
  explicit go-ahead** — the same demo-gate discipline ADR-090 established for OKX, applied here to
  a data-acquisition feature instead of an execution feature. This directly carries forward the
  user's explicit instruction from today's session (task 147, "Decision, 2026-09-11 14:32 CEST").
- **§1b's null-guard fix is a small, real bug fix to existing, already-live code**
  (`ForexFactorySyncService`, `TradingEconomicsPpiYoySyncService`), shipped as part of this
  change even though `app.bot.bls.enabled=false` makes it currently a no-op in production — it is
  strictly safer than today's behavior (it can only ever prevent a value from being erroneously
  blanked, never prevent a legitimate update) and is safe to ship immediately regardless of the
  BLS feature's own enablement state.
- **PPI is explicitly out of scope for this pass** (§6) — a follow-up ADR amendment (not a new
  ADR) is the right shape once PPI's series IDs are verified, since the architecture
  (per-indicator-family descriptor) is designed to absorb it as an additive change.
- **Quota discipline is a hard operational dependency.** Default caps (`maxRequestsPerEvent=10`,
  `maxRequestsPerDay=20`) are sized to be safe even unregistered, but free BLS registration
  (`BLS_API_KEY`, 500/day) is a stated prerequisite before relying on this in a real trading
  window — not required to build or test it.
- Frontend: no required change. `shared-ts/src/schemas/bot.ts`'s calendar event schema gains an
  optional `actualSource` field when convenient (additive, non-blocking).
- Docs: `docs/architecture/06-data-flow.md` §12 documents this flow (built, not enabled).

## Amendment 2026-09-14 — enabled

- User gave the separate, explicit go-ahead this Consequences section required, and registered a
  free BLS API key (`data.bls.gov/registrationEngine/`, 500 queries/day tier). Key stored in
  `infra/.env` as `BLS_API_KEY` (gitignored, not committed), passed through by
  `infra/compose.yml`'s existing `BLS_API_KEY: ${BLS_API_KEY:-}` line.
- `app.bot.bls.enabled` flipped `false → true` in `application.yml`. No other config changed —
  `poll-interval-seconds`, `backoff-seconds`, `max-requests-per-event`, `max-requests-per-day`
  remain the reasoned-but-unvalidated defaults from the original design (see Open risks below,
  which now describe a live monitoring concern rather than a purely hypothetical one).
- This does not change what gets executed against `LiveExecutionService`/OKX — BLS-direct only
  ever updates the same `bot_calendar_event` row FF already created (§2), through the same
  matching/execution path FF's actuals have always fed. It changes *how fast* the actual can
  arrive, not the trade-decision logic downstream of it.
- Rollout plan: no synthetic/staging validation exists for this path (BLS has no sandbox and the
  only realistic test is a real release). The next scheduled US CPI or PPI release is today's
  validation point — watch `blsDirectActualsSyncService` logs (baseline capture, poll attempts,
  soft-failure handling, write) live during that release, same posture as ADR-092's own
  first-live-event verification. FF remains the schedule/forecast authority and fallback actuals
  path throughout — if BLS never returns a fresh, agreeing 4-series read within the existing
  `stale-after-seconds` window, FF's own actuals still land in the same row as before this ADR
  existed.

## Open risks (not blocking, tracked)

1. BLS's true recovery time after a 503 storm is unmeasured beyond "somewhere between the storm
   ending and a manual check ~3 minutes later succeeding" — the per-event `maxRequestsPerEvent`/
   backoff numbers are reasoned defaults, not empirically tuned, same evidentiary caveat as ADR-090
   and ADR-092's own numbers when first set.
2. The four-series-must-agree freshness gate (§3.5) is more conservative than necessary if BLS
   ever legitimately publishes the four series a few seconds apart — no evidence either way; if
   this proves to delay real detections in production, relaxing it to "any 2 of 4, compute what's
   available" is a documented, bounded follow-up, not assumed correct now.
3. `pollStartOffsetSeconds=0` and the jitter constants are unvalidated against more than one
   release (§7) — explicitly flagged, not to be treated as tuned.
4. No quota-remaining signal exists from BLS's API — the daily/per-event caps are a self-imposed
   ceiling, not a verified-safe number; if BLS's real unregistered ceiling is lower than ~25/day,
   the caps in §8 could still be too high on days with unusually many retries. Registration removes
   this risk category entirely.
