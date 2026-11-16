# ADR-079: News Scope Broadening (amends ADR-064) and Recent-History Dedup

**Date:** 2026-08-25
**Status:** Accepted

## Context

Two user-reported problems with the nightly AI news-research feature (`GET /news`, ADR-041,
host cron `infra/news/news-research.sh`):

**1. Cross-day repeats.** Nothing in the pipeline ever tells the nightly research step "you
already reported this." `infra/news/prompt.md` has no memory of past days, and
`NewsService.assetsToResearch()` only ever returned bare `{ticker, assetName}` pairs. Every
night `claude -p` starts cold and can re-report an ongoing story (a multi-day court case, an
ongoing hack investigation, a slow-moving governance vote) as if it were new.

**2. `company_backed` scoping is conceptually wrong and excludes assets that DO have real
news.** ADR-064 added `positions.company_backed` and set it `false` for `BTC-USD`
specifically, reasoning "leaderless — no company or foundation issues or governs it." But
BTC-USD absolutely has notable asset-intrinsic news even with no company behind it: protocol
upgrade proposals (e.g. BIP-110), governance/consensus debates, hard forks, hash-rate and
on-chain trends, exchange-reserve movements, large-holder ("whale") activity, and security
incidents. ADR-064's underlying test — "does an accountable *company/team/foundation* exist" —
was the wrong question. The right question is "does this asset have its own distinguishable,
individually-researchable narrative" — and BTC-USD clearly does.

Separately, the user wants the editorial bar sharpened regardless of scope: news should come
from **inside** the asset (protocol upgrades, governance, forks, security incidents, on-chain
activity, hash rate, a company's own earnings/product/leadership/legal events) — never
**outside** macro-driven price attribution (Fed decisions, rate cuts, "ETFs bought a lot of
BTC," broad market/sector moves). The point is to judge whether the asset is fundamentally
moving in the right direction, not to explain today's price candle. `infra/news/prompt.md`
already has a version of this exclusion rule; it needs sharpening and an explicit ETF-flow
example, not replacing.

## Decision

### 1. `company_backed` is not renamed; its semantics are broadened

A full rename to something like `news_research_eligible` was considered — it fans out across
the entity, repository query method name, three DTOs, the OpenAPI schema, and both
`CreatePositionDialog.tsx`/`EditPositionDialog.tsx` for a purely cosmetic win. The column/field
name stays `company_backed` / `companyBacked`. Its documented semantics change:

> **Before (ADR-064):** true = "the asset has an identifiable company, project team, or
> foundation capable of generating entity-specific news."
>
> **After (ADR-079):** true = "the asset has its own distinguishable, individually-researchable
> narrative" — a company's own events (earnings, product launches, partnerships, leadership,
> legal/regulatory actions, hacks) for company-backed assets, **or** protocol/asset-intrinsic
> events for leaderless assets (protocol upgrades/proposals such as BIPs/EIPs, governance
> debates, hard forks, network security incidents/exploits, hash-rate/validator/on-chain
> activity, large-holder movements specific to that chain). False only for **basket/index/
> commodity vehicles** (an ETF/ETC/ETP wrapper is never itself the subject of this kind of
> headline — its underlying holdings might be, but the wrapper isn't) and **currency rows**
> (no narrative at all, just an FX rate).

This is a strictly broader test than ADR-064's — every row that was `true` under the old
semantics stays `true` under the new one (a company/foundation is one way, not the only way, to
have a distinguishable narrative). Only the `false` side of the line moves, and only for
leaderless-but-newsworthy assets. Today that is exactly one row: `BTC-USD`.

The application-layer default in `PositionService`/`CreatePositionRequest` (derive from
`assetClass`: `STOCK`/`CRYPTO` → `true`, `ETF`/`CURRENCY` → `false`) is **unchanged** — it
already computes the right answer under the broadened test for every asset class, since the
class-level default was always "assume yes unless it's a basket/currency vehicle," which is
exactly the new rule. The only thing that changes is the meaning of the one-off `false`
override a user can still apply via `UpdatePositionRequest.companyBacked`, and the copy that
explains it in the UI (frontend-web work, below).

### 2. `BTC-USD` flips to `company_backed = true`

Flyway migration `V58__flip_btc_company_backed.sql`:

```sql
UPDATE positions
SET company_backed = true
WHERE ticker = 'BTC-USD' AND asset_class = 'CRYPTO';
```

Applied across both portfolios (asset-intrinsic property, same rule ADR-064 used for the
original backfill).

**Audit of every other `company_backed = false` row** (task 116 item 3 — checked against
ADR-064's classification table and every migration touching `positions` since V49): no other
row is `false` under the "leaderless" reasoning. Every remaining `false` row is `ETF`- or
`CURRENCY`-class under the separate basket/index/bond-vehicle reasoning (gold ETC, NASDAQ/China
index trackers, bond funds, the quantum-computing thematic basket, PLN cash, `CHFPLN=X`, and
every companion row) — that reasoning is untouched by this ADR and those rows correctly stay
`false`: an ETF wrapper still has no narrative of its own distinguishable from its underlying
index. No migrations between V49 and V57 introduced a new leaderless base-layer asset (V54
promoted `OLAS-USD`→`ONDO-USD` into `CRYPTO`, but Ondo has an active foundation/team behind it,
so it was already correctly `true`). `BTC-USD` remains the sole exception in either direction.

### 3. Dedup: prompt-level primary, backend exact-match guard as a defensive backstop

Dedup is intentionally **not** a fuzzy-matching or embedding-similarity system — that's
heavyweight for a single-user, ~15-20-asset, once-a-night job. Instead:

**a) `GET /news/assets` gains per-ticker recent history**, so the model sees what it already
reported before deciding what's new. `NewsAssetDto` gains a `recentHistory` field:

```java
public record NewsAssetDto(
        String ticker,
        String assetName,
        List<NewsHistoryItemDto> recentHistory
) {}

public record NewsHistoryItemDto(
        LocalDate newsDate,
        String headline,
        String summary
) {}
```

`recentHistory` is always a non-null array (empty when the asset has no history in the
window), ordered most-recent-first. Deliberately **excludes** `sentiment` and `sources` — the
model only needs date/headline/summary to recognize "this is the same story," and every field
in this payload flows straight into the `claude -p` prompt (see below), so keeping it lean
keeps the token cost down.

`infra/news/news-research.sh` requires **zero changes** for this: it fetches `GET /news/assets`,
extracts the `data` array, and does `full_prompt="${prompt_template}\n${assets_json}"` — pure
string concatenation, no reshaping. Whatever shape `NewsAssetDto` serializes to lands in the
prompt verbatim, so adding `recentHistory` to the record is sufficient; the script does not
need to know it exists.

**b) `NewsService.assetsToResearch()` batches history in one query, not N+1**:

```java
// AssetNewsRepository — new derived query
List<AssetNews> findByTickerInAndNewsDateGreaterThanEqualOrderByTickerAscNewsDateDesc(
        Collection<String> tickers, LocalDate since);
```

`assetsToResearch()` fetches the eligible `Position` list once, collects their tickers, issues
**one** `AssetNewsRepository` query across all of them bounded by
`since = today.minus(lookbackDays)`, groups the results by ticker in Java
(`Collectors.groupingBy`), and attaches each position's slice (or an empty list) when building
its `NewsAssetDto`. No per-ticker query in a loop.

**c) Lookback window is configurable, not hardcoded**: `NewsProperties` gains
`historyLookbackDays` (default `7`), bound from `app.news.history-lookback-days` /
`NEWS_HISTORY_LOOKBACK_DAYS`, alongside the existing `ingestToken`. This one value drives both
the `recentHistory` window returned to the prompt and the backend exact-match guard below — a
single source of truth, not two independently-tuned windows that could drift apart.

**d) `infra/news/prompt.md` gets a new rule** (full content spec below): don't re-report a
story already present in an asset's `recentHistory` unless there's a genuinely NEW, material
development — and even then, report only what's new, not a restatement of the old summary.
This is the primary dedup mechanism; it's semantic (the model can actually tell "same story,
no update" from "same story, new development") in a way a backend string-match never can.

**e) `NewsService.ingest()` gets a lightweight normalized-exact-match guard** as defense in
depth for when the model doesn't comply. For each incoming `IngestNewsItem`, before upserting:
normalize its `headline` (trim, lowercase, collapse internal whitespace) and compare against
the normalized headlines of that ticker's existing rows in `[today - lookbackDays, today - 1]`
(i.e. the same window `recentHistory` drew from, excluding today itself — today's own row is
already handled by the existing `(ticker, newsDate)` upsert, not this guard). On an exact
normalized match, **skip persisting that item** (log ticker + headline at INFO, don't count it
in the `upserted` return value) rather than failing the whole batch — one duplicate item must
never block the other, non-duplicate items in the same ingest call. This is deliberately dumb
(exact string match only, no fuzzy/Levenshtein/embedding comparison) — it exists to catch the
model ignoring the prompt outright (verbatim re-report), not to second-guess genuinely-reworded
follow-up coverage of an ongoing story, which is exactly the case rule (d) asks the model to
handle semantically.

## Consequences

- **Schema change, Flyway-versioned** (`V58__flip_btc_company_backed.sql`) — single-row-class
  `UPDATE`, no `ddl-auto`, no manual DDL.
- **`NewsAssetDto` gains a field** (`recentHistory`) and a new nested DTO
  (`NewsHistoryItemDto`) is introduced. This only affects the machine-only `GET /news/assets`
  response — `shared-ts/src/schemas/news.ts` / `shared-ts/src/client/news.ts` are not touched,
  since recent-history is consumed only by the host cron script, never the web UI.
- **`NewsService.assetsToResearch()` and `NewsService.ingest()` both change** (history-shaping
  on read, exact-match guard on write) — see task 116 for the full backend touch list
  (`AssetNewsRepository`, `NewsMapper`, `NewsProperties`, `NewsController` if the envelope
  changes, and test updates across `NewsServiceTest`, `NewsIT`, `NewsServiceTokenIT`,
  `AssetNewsRepositoryTest`, `NewsControllerTest`).
- **`infra/news/prompt.md` is fully rewritten** per the content spec the Architect produced for
  this task (drop the "every asset has a company" framing; add leaderless/protocol-level
  guidance; sharpen the inside-vs-outside exclusion rule with an explicit ETF-flow example; add
  the recent-history dedup rule; state the underlying purpose up top). `infra/news/README.md`
  gets a doc update to match (backend agent).
- **Frontend-web**: `CreatePositionDialog.tsx` / `EditPositionDialog.tsx`'s `companyBacked`
  toggle helper copy currently reads "Wyłącz np. dla BTC-USD (brak lidera)" — now actively
  wrong guidance, since BTC-USD is the broadened test's own worked example of "leaderless but
  still eligible." Rephrase to match the broadened semantics, e.g. "Ma własny, śledzalny wątek
  newsowy (nawet bez firmy/fundacji — np. BTC ma newsy o BIP-ach, forkach, hashrate)." No
  schema/API shape change on the frontend side — `PositionDto.companyBacked` stays a plain
  `boolean`, only its meaning broadened.
- **Living docs updated in this change** (folded in, not left pointing back at this ADR):
  `docs/architecture/02-domain-model.md` §2.1 (`company_backed` field) and §2.17 (`asset_news`
  flow), `docs/architecture/03-api-contract.md` (Positions section `companyBacked` mentions,
  NewsController section), `docs/architecture/04-db-schema.md` (`positions.company_backed` row,
  backfill note, `asset_news` table intro), `docs/architecture/06-data-flow.md` §6 (sequence
  diagram + bullets). This ADR's own reasoning (why "leaderless" was the wrong test, why
  dedup is prompt-primary/backend-defensive rather than fuzzy-matching) has lasting reference
  value beyond what's restated in the living docs, so the file is kept rather than deleted
  after consumption.
- **No change to who calls `GET /news/assets` or how** — still machine-only,
  `serviceTokenAuth`, called once nightly by `infra/news/news-research.sh`. The script itself
  needs zero code changes (confirmed above) since it forwards the response body verbatim.
