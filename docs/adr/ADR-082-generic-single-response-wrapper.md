# ADR-082: Generic `SingleResponse<T>` Replaces Per-Domain `{ data, meta }` Response Records

**Date:** 2026-09-01
**Status:** Accepted

**Context:** Why this decision needed to be made.

Task 127's audit found that `pl.shared.dto.common.ListResponse<T>`/`PagedResponse<T>`
(introduced 2026-08-23, T1 shared-package extraction) already generalized the "list of
items + envelope" and "page of items + envelope" shapes, each explicitly documented as
*"Replaces the N specific `*ListResponse` records that ..."* — but the same reasoning was
never applied to the third, most common shape: a **single** resource wrapped in
`{ data, meta }`. Every domain still declares its own `record XxxResponse(XxxDto data,
Meta meta) {}` (or, in two `pl.portfolio-b`/`pl.portfolio-c` cases, a hand-rolled getter/setter
class of the same shape). `packages/shared/java/src/main/java/pl/shared/controller/support/Envelopes.java`
already names this exact problem in its own Javadoc: *"Generic `{ data, meta }` wrapper
for the many per-endpoint response types (`PositionResponse`, `RebalancingResponse`,
`DepositResponse`, ...) that all share the same two-arg `(T data, Meta meta)` constructor
shape but have **no common supertype**."* `Envelopes.withMeta(BiFunction<T, Meta, R>
constructor, T data)` is a workaround for that missing supertype — it takes a constructor
reference (e.g. `RebalancingResponse::new`) precisely because there was no generic type to
call `new` on directly. `pl.portfolio-b.dto.purchases.PurchaseHistoryResponse` even carries a
doc comment reading *"Mirrors `RebalancingResponse`"*, i.e. the duplication was noticed
informally at write-time and never acted on.

**Re-grep before deciding scope (per the task brief's own instruction — the file list may
have drifted since the audit).** Running `record \w+Response\(...\)` structurally (not as
a single-line regex, which undercounts multi-line record declarations) across all three
backends plus `packages/shared/java` turned up two corrections to the task's 24-file list:

1. **2 additional live single-resource wrappers the original audit missed**, both in
   `packages/shared/java` and both consumed by portfolio-b *and* portfolio-c (not root, which keeps
   its own separate copies): `pl.shared.dto.deposit.XirrResponse` (used from both apps'
   `DepositsController`) and `pl.shared.dto.setting.SettingResponse` (used from
   `pl.shared.controller.SettingsController`, itself shared verbatim between portfolio-b/portfolio-c
   per ADR-078 — root keeps its own local `SettingsController`/`SettingResponse` untouched,
   confirmed by that shared controller's own Javadoc).
2. **2 orphaned dead files carrying this exact shape, zero call sites anywhere in the
   codebase**: `pl.investing.dto.rebalancing.RebalancingResponse` (root has no
   `/rebalancing/calculate` controller at all today — only `RebalancingTransferController`
   at `/rebalancing-transfers`; `RebalancingResultDto` itself is alive, produced internally
   by `RebalancingService`/`PurchaseSessionService`, but this particular wrapper record is
   never constructed or referenced by any controller, service, or test) and
   `pl.shared.dto.rebalancing.RefreshWeightsResponse` (zero references outside its own
   file). Both predate this task and are unrelated to it — they are called out here only
   because the required re-grep surfaced them, and this ADR recommends Backend delete them
   outright as a same-area drive-by, not convert them.

Also confirmed **out of scope, correctly**: `AccountCapUsageResponse`, `NewsListResponse`,
`NewsDatesListResponse`, `NewsAssetsListResponse` (all root) structurally match
`record Xxx(List<Y> data, Meta meta)` — the *list* shape, not the *single* shape. They are
un-migrated `ListResponse<T>` candidates (`ListResponse.java`'s own Javadoc claims to
already "replace the 4 specific `*ListResponse` records that had no page field," but these
4 are live and un-migrated — a separate, pre-existing gap). Not touched by this decision.

**The real open question — OpenAPI schema-naming risk — resolved by direct verification,
not assumption.**

The task asked whether springdoc-openapi's default generic-type schema naming (which
would flatten `DataResponse<PositionDto>` into something like `DataResponsePositionDto`)
forces renames in `docs/openapi.yaml` or in generated frontend client types, using the
already-shipped `ListResponse<T>`/`PagedResponse<T>` as the real precedent to check
against rather than assuming. Checked directly, in this repo, not from memory:

- `docs/openapi.yaml` (and its source under `docs/openapi/components/schemas/*.yaml`) is
  **hand-authored**, bundled via `redocly bundle` (`npm run openapi:bundle`) — it is *not*
  generated from springdoc's runtime introspection of the Java classes. Grepping the
  bundle for every schema built on top of the already-generic `ListResponse<T>`/
  `PagedResponse<T>` shows names like `PositionsPagedResponse`, `DepositsPagedResponse`,
  `CashPositionsListResponse`, `SettingsListResponse` — domain-specific, human-chosen
  names, each with an inline `{ data, meta[, page] }` schema body. None of them is named
  anything resembling springdoc's default generic-flattening scheme. The existing
  single-resource schemas (`PositionSingleResponse`, `BinanceTransferSingleResponse`,
  `SettingSingleResponse`, `PurchaseSessionSingleResponse`, plus several named directly
  `XxxResponse`) follow the identical hand-authored pattern. **This is the direct,
  in-repo answer**: the OpenAPI contract's schema names are chosen by the Architect and
  are already fully decoupled from whatever Java type actually implements the wrapper —
  they did not change when `ListResponse`/`PagedResponse` shipped, and they will not
  change when `SingleResponse<T>` ships, because nothing mechanically derives one from
  the other.
- Confirmed *why* that decoupling holds: `springdoc-openapi-starter-webmvc-ui` (v3.1.0,
  `backend/pom.xml`) is a real runtime dependency and does expose a live introspected
  document at `/api/api-docs` (`springdoc.api-docs.path: /api-docs` under
  `server.servlet.context-path: /api`) — but that path is **not** in
  `SecurityConfig`'s permit-all list (only `/actuator/health` and `/auth/token` are),
  confirmed by hitting it directly against the running `investing-backend-1` container
  (401, no JWT). No script, CI workflow, or `package.json` task in this repo reads that
  endpoint. `packages/shared/ts/src/schemas/envelope.ts` — the actual TypeScript-side
  analog, consumed by all three frontends via `shared-ts`/`packages/shared/ts` — already
  has a hand-written, fully generic `apiResponseSchema<T extends z.ZodTypeAny>(dataSchema)`
  Zod helper (paired with `pagedResponseSchema<T>()`), decoupled from any Java class name
  by construction. There is no `openapi-typescript`/`orval`/codegen step anywhere in this
  repo turning either the hand-authored OpenAPI doc or springdoc's live doc into frontend
  types — `shared-ts`'s Zod schemas are hand-written and were already generic before this
  ADR.
- **Conclusion: zero forced schema-name or generated-client churn.** The live springdoc
  document (Swagger UI only, human-debug use) will show whatever name springdoc's default
  generic-naming picks for `SingleResponse<PositionDto>` etc. — that is cosmetic, already
  true today for `ListResponse<T>`/`PagedResponse<T>`, not consumed by anything, and out
  of this ADR's concern.

**Decision:** What was decided.

1. **Add `pl.shared.dto.common.SingleResponse<T>(T data, Meta meta) {}`** beside
   `ListResponse<T>`/`PagedResponse<T>`, in the same package, following their exact
   Javadoc convention:

   ```java
   package pl.shared.dto.common;

   /**
    * Generic single-resource response wrapper for endpoints that return exactly one item.
    * Replaces the 26 specific *Response records/classes across investing-app,
    * portfolio-b, and portfolio-c that shared this exact
    * {@code (T data, Meta meta)} shape.
    *
    * @param <T> the DTO payload type
    */
   public record SingleResponse<T>(T data, Meta meta) {
   }
   ```

   **Named `SingleResponse<T>`, not `DataResponse<T>` as the task brief proposed** — this
   is the one deliberate deviation from the brief, made for naming consistency, not
   arbitrarily:
   - `ListResponse`/`PagedResponse` both name the **cardinality/shape** of the payload
     (a list; a page). `Single` completes that same naming axis (`List` / `Paged` /
     `Single`) — `Data` does not, since every one of these three wrapper types has a
     `data` field; "DataResponse" describes nothing that distinguishes it from the other
     two.
   - `docs/openapi.yaml` already uses `*SingleResponse` as its term of art for exactly
     this shape (`PositionSingleResponse`, `BinanceTransferSingleResponse`,
     `SettingSingleResponse`, `PurchaseSessionSingleResponse`) — reusing that word closes
     the vocabulary gap between the Java implementation type and the hand-authored
     contract instead of introducing a fourth, unrelated term. No existing
     `SingleResponse` symbol exists anywhere in the codebase today (checked).

2. **Nested payload records stay exactly as-is in shape, promoted to top-level types
   where they were previously nested inside the wrapper being deleted** (4 cases; every
   other in-scope file wraps an already-standalone DTO and needs no promotion):

   | Wrapper (deleted) | Nested type (was) | Promoted to (new top-level file, same package) |
   |---|---|---|
   | `pl.investing.dto.deposit.XirrResponse` | `XirrResponse.XirrData` | `pl.investing.dto.deposit.XirrData` |
   | `pl.shared.dto.deposit.XirrResponse` | `XirrResponse.XirrData` | `pl.shared.dto.deposit.XirrData` |
   | `pl.investing.dto.tradechart.TradeChartResponse` | `TradeChartResponse.TradeChartData` | `pl.investing.dto.tradechart.TradeChartData` |
   | `pl.investing.dto.transfer.RebalancingTransferResponse` | `RebalancingTransferResponse.RebalancingTransferResult` | `pl.investing.dto.transfer.RebalancingTransferResult` |
   | `pl.investing.dto.news.IngestNewsResponse` | `IngestNewsResponse.Data` (bare `Data` — not a fit as a top-level name) | `pl.investing.dto.news.IngestNewsResult` (renamed to match the codebase's existing `*Result` convention for computed-outcome payloads — `CryptoTaxSummaryResult`, `PurchaseSessionCompleteResult`, `RebalancingResultDto`) |

   `IngestNewsResponse.of(int upserted)`'s static factory is dropped — its one call site
   (`NewsController.ingest`) becomes `Envelopes.single(new IngestNewsResult(upserted))`
   (see point 3).

3. **`Envelopes.java` gains `single(T data)`, loses `withMeta(...)`.** Add
   `public static <T> SingleResponse<T> single(T data) { return new SingleResponse<>(data,
   Meta.create()); }`, mirroring `list()`/`paged()`. Delete
   `withMeta(BiFunction<T, Meta, R> constructor, T data)` — its entire reason for existing
   was the lack of a common supertype for these wrapper types (stated in its own Javadoc);
   once `SingleResponse<T>` exists, every one of its 9 call sites
   (`RebalancingResponse::new`, `PositionResponse::new` ×3, `PurchaseHistoryResponse::new`,
   `DepositResponse::new`, `XirrResponse::new` ×2 — portfolio-b + portfolio-c) becomes
   `Envelopes.single(...)` directly, and the constructor-reference indirection is dead
   weight.

4. **Final scope: convert all 26 live matches, delete 2 dead files, touch 0 OpenAPI
   source files.** No reduction from the audit's intent — verification in the section
   above found the churn risk that would have justified scoping down (forced OpenAPI
   schema renames, forced frontend/generated-client edits) **does not exist**, so there is
   no reason to hold any of the 26 back. Full file disposition:

   **Convert → delete wrapper file, call sites use `SingleResponse<PayloadType>` (26):**

   *Root `investing-app` (16):* `BinanceTransferResponse` → `SingleResponse<BinanceTransferDto>`;
   `CryptoTaxSummaryResponse` → `SingleResponse<CryptoTaxSummaryResult>`;
   `CryptoTradeResponse` → `SingleResponse<CryptoTradeDto>`;
   `AccountCapAdjustmentResponse` → `SingleResponse<AccountCapUsageItem>`;
   `DepositResponse` → `SingleResponse<DepositDto>`;
   `XirrResponse` → `SingleResponse<XirrData>` (promoted, see table above);
   `InflationResponse` → `SingleResponse<InflationEntryDto>`;
   `MacroStateResponse` → `SingleResponse<MacroStateDto>`;
   `IngestNewsResponse` → `SingleResponse<IngestNewsResult>` (promoted+renamed);
   `PositionResponse` → `SingleResponse<PositionDto>`;
   `PurchaseSessionCompleteResponse` → `SingleResponse<PurchaseSessionCompleteResult>`;
   `PurchaseSessionResponse` → `SingleResponse<PurchaseSessionDetail>`;
   `SettingResponse` → `SingleResponse<SettingDto>`;
   `TaxReturnResponse` → `SingleResponse<TaxReturnDto>`;
   `TradeChartResponse` → `SingleResponse<TradeChartData>` (promoted);
   `RebalancingTransferResponse` → `SingleResponse<RebalancingTransferResult>` (promoted).

   *Shared `pl.shared` (2, consumed by portfolio-b + portfolio-c):*
   `pl.shared.dto.deposit.XirrResponse` → `SingleResponse<XirrData>` (promoted);
   `pl.shared.dto.setting.SettingResponse` → `SingleResponse<SettingDto>` (`pl.shared.dto.setting.SettingDto`).

   *`apps/portfolio-b` (4):* `DepositResponse` → `SingleResponse<DepositDto>`;
   `PositionResponse` → `SingleResponse<PositionDto>`;
   `PurchaseHistoryResponse` (class → deleted) → `SingleResponse<PurchaseHistoryDataDto>`;
   `RebalancingResponse` (class → deleted) → `SingleResponse<RebalancingResultDto>`
   (portfolio-b's own per-app `pl.portfolio-b.dto.rebalancing.RebalancingResultDto`).

   *`apps/portfolio-c` (4, mirror of portfolio-b):* `DepositResponse` →
   `SingleResponse<DepositDto>`; `PositionResponse` → `SingleResponse<PositionDto>`;
   `PurchaseHistoryResponse` (class → deleted) → `SingleResponse<PurchaseHistoryDataDto>`;
   `RebalancingResponse` (class → deleted) → `SingleResponse<RebalancingResultDto>`
   (portfolio-c's own per-app type).

   **Delete outright, no conversion (2, dead code found via required re-grep):**
   `pl.investing.dto.rebalancing.RebalancingResponse`;
   `pl.shared.dto.rebalancing.RefreshWeightsResponse`.

   **Out of scope, not touched (4, wrong shape — `List<T>`, a `ListResponse<T>`
   adoption gap, not a `SingleResponse<T>` one):** `AccountCapUsageResponse`,
   `NewsListResponse`, `NewsDatesListResponse`, `NewsAssetsListResponse`.

**Pattern evaluation:**

| Candidate | Verdict | Why |
|---|---|---|
| Generics (parametric polymorphism) over 26 duplicate types | adopted | Exactly the DRY violation `ListResponse<T>`/`PagedResponse<T>` already fixed for the other two envelope shapes; same tool, same mechanism, proven safe in production. |
| Static Factory Method (`Envelopes.single(T)`) | adopted | Mirrors the already-adopted `Envelopes.list()`/`Envelopes.paged()`; keeps controllers from hand-rolling `Meta.create()`. |
| Builder | rejected | Two-field record; a builder adds ceremony with zero benefit (YAGNI). |
| Bridge / Adapter / Facade | rejected | No structural translation needed — this is a pure shape consolidation, not an integration boundary. |
| Visitor / Strategy | rejected | No varying behavior over the wrapper — `data`/`meta` are plain fields, not a family of algorithms. |
| Keeping `Envelopes.withMeta(BiFunction, T)` (constructor-reference indirection) | rejected, removed | Its own Javadoc states its sole purpose was working around "no common supertype" — that supertype now exists, so the indirection is dead weight (DRY/YAGNI). |

**Consequences:** Trade-offs, future implications.

- Backend implements this next (not this ADR) — likely split root vs. portfolio-b+portfolio-c given
  the three are independent Maven builds/deployables (same split pattern as task 126).
  Backend must re-grep each of the 26 in-scope files' test-source references before
  deleting (confirmed at least `TradeChartResponse` and `RebalancingTransferResponse` have
  direct references in `backend/src/test`; not exhaustively checked for the remaining 24 —
  that check is part of implementation, not this ADR).
- Testcontainers integration tests must still pass per-app; JSON serialization output must
  be byte-identical before/after (confirmed none of the 26 carry custom `@Json*`
  annotations that could be lost in the conversion — plain records/getters throughout, so
  Jackson's field-name-based serialization of `data`/`meta` is unaffected by the wrapper
  becoming generic; this exact mechanism is already proven safe in production by
  `ListResponse<T>`/`PagedResponse<T>`).
- No MapStruct mapper targets any of the 26 wrapper types directly (confirmed via
  `grep`) — controllers/services construct them directly, so no mapper interface changes
  are needed beyond the call-site updates themselves.
- `docs/openapi.yaml` / `docs/openapi/` require **no changes** — schema names, bodies, and
  examples for every affected endpoint stay exactly as they are today; this ADR's
  verification is the reason no `openapi:bundle`/`openapi:lint` re-run is needed for this
  change specifically (Backend should still run them if it touches any `docs/openapi/`
  source file for an unrelated reason in the same PR).
- `shared-ts`/`packages/shared/ts` requires **no changes** — `apiResponseSchema<T>()` was
  already generic before this ADR.
- The 2 dead-file deletions and the 4 out-of-scope `ListResponse<T>`-shaped files are
  recorded here only as a byproduct of the required re-grep; the latter is a legitimate
  small follow-up task (`ListResponse<T>` adoption gap in root's `AccountCapUsageResponse`/
  `News*ListResponse`) that should get its own task file rather than being folded into
  this one.
