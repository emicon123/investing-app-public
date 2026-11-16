# ADR-064: `positions.company_backed` — Entity-Backed News Scoping

**Date:** 2026-07-30
**Status:** Accepted

## Context

The nightly host-cron news-research job (`infra/news/news-research.sh`, ADR-041) researches
"every MAIN-portfolio, non-companion position with `asset_class <> 'CURRENCY'`"
(`NewsService.assetsToResearch()` →
`PositionRepository.findAllByIsCompanionRowFalseAndPortfolioAndAssetClassNot`). That scope is too
broad: it includes assets with **no identifiable company, project team, or foundation** capable
of generating entity-specific news — a NASDAQ-index ETF, gold, a bond fund, a China-index
tracker, and BTC itself (base-layer, leaderless — no company or foundation issues or governs it,
unlike an application-layer token such as `SUI-USDT` which has the Sui Foundation, or
`ETH-USD` which has the Ethereum Foundation). Researching these produces either nothing (the
`claude -p` run correctly finds no entity news and omits them) or, worse, generic
macro-price-attribution filler ("gold rose on rate-cut expectations") — exactly the low-value
content the user flagged as unwanted. That prompt-wording half of the fix is out of scope for
this ADR (Backend agent handles `infra/news/prompt.md` directly); this ADR is the scoping half.

Neither existing classification axis on `Position` cleanly answers "does this have an
accountable entity behind it": `asset_class` (`STOCK`/`CRYPTO`/`ETF`/`CURRENCY`) puts `BTC-USD`
and `SUI-USDT` in the same bucket despite one having no foundation and the other having one;
`exposure` (`CRYPTO`/`METALS`/`CASH`/`QUANTUM`/`BONDS`/`EXPERIMENTS`/`EQUITY`) is an economic
theme, orthogonal to accountability. A new, explicit boolean is needed.

Positions are not static seed data — `PositionService.createPosition`/`updatePosition` are live,
UI-reachable mutations (`CreatePositionDialog.tsx`, `EditPositionDialog.tsx`) — so this has to be
a real, settable domain field, not a hardcoded ticker/class exclusion list in Java.

## Decision

### 1. New column: `positions.company_backed BOOLEAN NOT NULL DEFAULT true`

Semantics: **true** = the asset has an identifiable company, project team, or foundation capable
of generating entity-specific news (earnings, product launches, partnerships, hacks, leadership
changes, listings, legal/regulatory actions). **false** = basket/index/commodity vehicle or a
leaderless base-layer asset — nothing exists that could be the subject of that kind of headline.

**DB-level default stays `true` permanently** (not just for the migration) as a defensive
backstop for any row inserted outside the normal application path; it is not the mechanism the
app actually relies on for correctness. The application-layer default (below) is authoritative
for anything created through `PositionService`.

**Backfill rule (Flyway migration, applied to ALL rows in both portfolios — not just the 35
MAIN rows the user hand-classified, since "has a company/team/foundation" is a property of the
*asset*, not of which portfolio holds it):**

```sql
UPDATE positions SET company_backed = false WHERE asset_class IN ('ETF', 'CURRENCY');
UPDATE positions SET company_backed = false WHERE ticker = 'BTC-USD' AND asset_class = 'CRYPTO';
```

Verified against the user's hand-classification of the current 35 MAIN, non-companion,
non-currency rows:

| Asset class | Rule | Tickers |
|---|---|---|
| STOCK | all `true` (column default, no override needed) | CIFR, COIN, HUT, IREN, MARA, WULF, CRCL, FISV, GME, MSTR |
| CRYPTO | all `true` except `BTC-USD` | DOGE-USD, ETH-USD, SOL-USD, SUI-USDT, ALGO-USD, FET-USD, FLIP-USD, HBAR-USD, LINK-USD, LOFI-USDT, OLAS-USD, TAO-USDT, TRAC-USD, TRX-USD, XPL-USD, XRP-USD, ZANO-USD, ZEC-USD **true**; `BTC-USD` **false** |
| ETF | all `false` | ETCGLDRMAU.PL (gold), ETFBCASH.PL (bonds), ZPR1.DE (bonds), QUTM.DE (quantum basket), ICGA.DE (China index), NQSE.DE (NASDAQ index) |

The same two `UPDATE` statements also correctly backfill RETIREMENT rows and companion rows
(companion rows are themselves `asset_class = ETF` in every current case — physical gold ETCs,
Bitcoin Prime ETP, Staked ETH ETP, the VanEck ETNs — so they land on `false` for free, which is
also the semantically correct answer: an ETP wrapper is not itself a company).

### 2. Application-layer default for new positions: derived from `assetClass`

`CreatePositionRequest` gains an **optional, nullable** `companyBacked: Boolean`. When the
caller omits it, `PositionService`/`PositionMapper` computes it from `assetClass`:
`STOCK`/`CRYPTO` → `true`, `ETF`/`CURRENCY` → `false` — mirroring exactly the backfill rule
above so a freshly created position is never inconsistent with how an equivalent historical row
was classified. `BTC-USD`-style exceptions (a `CRYPTO` asset with no accountable entity) can
only be reached by explicitly passing `companyBacked: false` — there is no plan to hardcode a
second base-layer-coin exception list, since the user has exactly one such asset today and the
field exists precisely so this doesn't need a code change if a second one ever shows up.

### 3. Carried on `PositionDto` (read) and `UpdatePositionRequest` (partial update)

`PositionDto.companyBacked: boolean` (always present, non-null, mirrors the DB column exactly —
no computed/derived semantics at read time, unlike `effectiveTargetAllocationPct`).
`UpdatePositionRequest` gains optional, nullable `companyBacked: Boolean`, following the same
"null = leave unchanged" partial-update convention every other mutable field on that DTO already
uses (`name`, `assetClass`, `yahooTicker`, etc.) — this is how the user corrects a
misclassification (or the one BTC-USD-style exception) after the fact from the UI, without a
dedicated endpoint.

### 4. `PositionRepository`/`NewsService.assetsToResearch()`: `company_backed` becomes the sole
entity-scope gate, replacing `assetClassNot(CURRENCY)`

```java
List<Position> findAllByIsCompanionRowFalseAndPortfolioAndCompanyBackedTrue(Portfolio portfolio);
```

replaces `findAllByIsCompanionRowFalseAndPortfolioAndAssetClassNot(Portfolio, AssetClass)`
(deleted — its only caller was `NewsService.assetsToResearch()`). `CURRENCY` exclusion is not
lost: every `CURRENCY` row backfills to `company_backed = false` (rule above) and the
application-layer default computes `false` for any future `CURRENCY` row too, so the invariant
"news scope never includes a currency position" holds without a second, redundant filter clause.
Collapsing to one boolean predicate is deliberate — two overlapping filters (`assetClass !=
CURRENCY` AND some future `companyBacked` check) would let them drift out of sync exactly the
way the *absence* of a dedicated field let ETFs and BTC leak into scope in the first place. This
one field is now the single source of truth for "should the nightly job research this."

## Consequences

- **Schema change, Flyway-versioned** (next free migration number — verify against the live
  `backend/src/main/resources/db/migration/` directory at implementation time; `V48` was latest
  as of this ADR). No `ddl-auto`, no manual DDL, per this repo's quality gates.
- **Breaking-ish for `PositionRepository`**: the old derived-query method
  (`findAllByIsCompanionRowFalseAndPortfolioAndAssetClassNot`) is deleted, not deprecated — it had
  exactly one production caller (`NewsService.assetsToResearch()`) and two test references
  (`NewsServiceTest`, `PositionRepositoryTest`) that need updating in the same change. Acceptable:
  single-user app, no external API consumers of the repository layer (ADR-004 precedent).
- **`company_backed` is asset-intrinsic, not portfolio- or account-scoped** — unlike
  `target_allocation_pct` (risk-on/off per MAIN) or `total_spent_pln` (RETIREMENT-unreliable per
  ADR-056), there is no per-portfolio variant needed; the same ticker in MAIN and RETIREMENT
  should always agree, and the backfill/default rules guarantee that by construction (both derive
  purely from `asset_class`/`ticker`, never from `portfolio`).
- **UI exposure recommended, not silent-only backend magic.** Because the asset-class-derived
  default has one known, real exception (`BTC-USD`) and could plausibly gain more (any future
  leaderless/base-layer or DAO-with-no-legal-entity asset), the field should be a visible,
  overridable checkbox in `CreatePositionDialog.tsx`/`EditPositionDialog.tsx` — defaulted from
  `assetClass` client-side for a good first guess, but never hidden. A silent backend-only default
  would force the user back into editing raw SQL the next time reality disagrees with the
  assetClass heuristic, which is exactly the kind of hardcoded-exception debt this field exists to
  avoid accumulating in Java.
- **News prompt content (macro-attribution exclusion) is a separate, non-schema fix.** This ADR
  only closes the scoping gap (*which* assets get researched); the *quality* of what's researched
  for in-scope assets is `infra/news/prompt.md` wording, owned directly by the Backend agent with
  no architecture dependency — `prompt.md` must explicitly require entity-specific
  events/decisions and explicitly forbid generic macro-price-attribution content ("price crashed
  because of Fed rate hikes"), and its BTC/gold/NASDAQ example should be swapped for an in-scope
  example so the prompt no longer contradicts the new scope rule.
- **Status: spec decided here (architect scope only) — not yet implemented.** Backend implements
  the migration, entity/DTO/mapper/repository changes, and the prompt-wording fix; Frontend-web
  adds the create/edit-form checkbox. See `docs/architecture/02-domain-model.md` §2.1,
  `03-api-contract.md` (Positions + News sections), `04-db-schema.md` (`positions` /
  `asset_news` tables), and `06-data-flow.md` §6 for the folded-in living-doc updates.
