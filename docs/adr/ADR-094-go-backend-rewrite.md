# ADR-094: Go Backend Rewrite (Strangler-Fig, All Three Apps)

**Date:** 2026-09-14
**Status:** Accepted — **historical, superseded by as-built (Phase 9 complete 2026-09-19, ADR-098)**

---

## Context

investing-app (root), `apps/portfolio-b/`, and `apps/portfolio-c/` each run an
independent Spring Boot 4.0.6 / Java 25 JVM on the same Raspberry Pi (7.9GiB total RAM). ADR-083
(2026-09-02) measured each JVM's steady-state RSS at 385–507MB with no enforced container memory
ceiling — each one's ergonomic `MaxHeapSize` defaults to 25% of the **host's** total RAM
(`java -XX:+PrintFlagsFinal -version` confirmed `MaxHeapSize ≈ 2.01GiB` inside
`investing-backend-1`), on a host whose swap was already 95% full at measurement time. ADR-083
addressed the Postgres side of that pressure (3 processes → 1, `db-shared`) and flagged, but
explicitly did not commit to, JVM-level consolidation — it rejected a single shared JVM (would
erase real per-app divergence: JWT issuer, exception→status mapping, security filter chains) and
scoped AppCDS only as an unmeasured follow-up.

The user has now separately decided — this ADR does not re-litigate the decision, only records it
and the concrete design built on top of it — to replace all three Spring Boot backends with Go,
migrated incrementally via the Strangler Fig pattern so the Go and Java backends coexist behind
Nginx during the transition, cutting over one domain/controller-group at a time. The database
(Flyway-versioned PostgreSQL, `flyway_schema_history`, 71 migrations in root alone as of this
writing) is shared, unforked source of truth throughout — both languages read/write the same live
schema for whichever domains haven't cut over yet. The frontend (React 19 / Vite / TanStack Query /
shadcn, plus `shared-ts`/`packages/shared/ts`) is explicitly out of scope and must see zero
contract change; `docs/openapi/` (source) → `docs/openapi.yaml` (bundle) remains the interop
contract every domain's Go implementation must match exactly.

This ADR is Part 1 of the design (stack pick, GraalVM verdict, design-quality framing). The full
strangler-fig routing design, write-ownership safety audit, idiomatic pattern mapping, and the
complete phase/domain sequencing table live in
`docs/architecture/13-go-backend-target-architecture.md` — this document links out to it rather
than duplicating it, per the Architect's documentation conventions.

---

## Pattern & principle evaluation

See doc 13 §1 for the full table (Strangler Fig, Anti-Corruption Layer, Facade, Strategy,
Decorator adopted; Branch-by-Abstraction-as-code, Template-Method-as-inheritance, Singleton-as-a-
pattern, Command-as-Undo, and Visitor rejected). The short version: this ADR's own subject —
*how two backends coexist against one database* — is an integration/deployment-pattern decision
(Strangler Fig, Anti-Corruption Layer), not a GoF domain-code decision; the domain-code pattern
questions (what replaces the Java Template Method hierarchies) are answered in doc 13 §5, not
here, since they're a property of the target Go code, not of the migration mechanism.

---

## Decision

### 1. Adopted — Go 1.27, migrated via Strangler Fig, all three apps

Every new `go.mod` declares `go 1.27` and relies on `GOTOOLCHAIN=auto` (available since Go 1.21)
to fetch that exact toolchain on demand — the Pi's system `go` binary stays at the apt-installed
1.24.4 and never needs a manual reinstall. This is stated explicitly here and in doc 13 so a
future Backend-Go or DevOps agent reading `go version` on the host doesn't conclude the ADR is
wrong or stale; the discrepancy is intentional and documented, not a drift.

Confirmed via web search at ADR time (2026-09-14): Go 1.27.1 is latest stable; 1.26 added a new
GC and materially lower cgo overhead; 1.27 added generic methods; 1.23 added range-over-func
iterators; 1.22 improved `net/http` routing (method + wildcard patterns) — all considered in the
stack picks below.

### 2. Stack picks (full table and reasoning: doc 13 §2)

Each pick names its current stable major and maintenance status explicitly, per the user's
instruction not to default to a historically "safe" but now-stale choice:

- **HTTP routing:** `github.com/go-chi/chi/v5` on stdlib `net/http`. Chi remains actively
  maintained (updates as recent as May 2026), has zero external dependencies, and is 100%
  `net/http`-compatible — it adds composable middleware and per-domain sub-router mounting
  (`chi.Mount`) that plain stdlib routing still doesn't match, which matters directly for a
  per-domain strangler-fig cutover. Echo was considered and rejected: it pulls in more framework
  surface (its own context type, its own middleware signature) for no capability this project
  needs that chi lacks.
- **DB driver:** `github.com/jackc/pgx/v5`, not `lib/pq`. Verified via web search: `lib/pq` is
  confirmed in maintenance mode (infrequent review/merge of patches); `pgx` v5 is the actively
  released, actively maintained driver (a release as recent as 2026-09-07 was found), and is
  already sqlc's documented recommended driver.
- **DB access:** `sqlc`, not GORM, with `sqlx` as a documented fallback for anything sqlc's
  static-SQL model can't express. See doc 13 §3 for the domain-specific reasoning — this app's
  computed-at-read-time fields (`value_pln`, `deviation_pct`, XIRR, etc. — never persisted, per
  doc 04) mean an ORM's row-in-object-out/change-tracking machinery buys nothing here; sqlc's
  "write SQL, generate types" model mirrors the same contract-first philosophy MapStruct already
  applies one layer up.
- **Migrations:** Flyway itself stays, via the standalone `flyway/flyway:<version>-alpine` Docker
  image (confirmed ARM64-supported in the Alpine variant), run as a one-shot init container — not
  golang-migrate or goose. **This is the highest-risk-if-wrong pick in this ADR and deserves its
  own reasoning:** golang-migrate and goose each track their own migration-history table
  (`schema_migrations` / `goose_db_version`), a different shape from Flyway's
  `flyway_schema_history`. Reconciling two different migration-history formats mid-transition,
  on a live financial database, while both a Java backend (still Flyway-driven) and a Go backend
  might be deploying independently, is a correctness risk with no proportionate benefit — the
  schema-authority tool doesn't need to be fast or idiomatic-Go, it needs to be the *one* tool
  both backends agree already ran. Keeping Flyway (now with zero JVM dependency inside either
  app's own process — it only exists in a throwaway migration-runner container) removes the
  reconciliation problem entirely rather than solving it cleverly.
- **OpenAPI codegen:** `github.com/oapi-codegen/oapi-codegen/v2`, v2.8.0 or later — confirmed via
  web search this is the first version with real OpenAPI 3.1 support (this project's spec is
  3.1), generating chi-compatible server interfaces directly from `docs/openapi.yaml`.
- **Auth:** `github.com/golang-jwt/jwt/v5` (backend-issued HS256 JWT validation) +
  `google.golang.org/api/idtoken` (Google's own official Go client library, for verifying
  Google-issued RS256 ID tokens at the `/auth/token` exchange) — deliberately not one of the
  several near-identical, low-star third-party `google-auth-id-token-verifier` forks surfaced by
  research, a signal of an unmaintained original.
- **Integration testing:** `github.com/testcontainers/testcontainers-go` (confirmed actively
  maintained, requiring Go 1.22+) against real `postgres:17.10-alpine` — same "never mock the
  database" gate, same base image, schema applied via the same Flyway CLI container used in prod.
- **Logging:** stdlib `log/slog` (since Go 1.21) — no new dependency.
- **Config:** typed struct + `os.Getenv`/`github.com/caarlos0/env` + `github.com/joho/godotenv`
  for `.env.dev`/`.env.prod`, mirroring Spring's `dev`/`prod` profile split with the same env-var
  names and `.env` file locations DevOps already manages.
- **DI:** explicit constructor wiring in one composition-root `main.go` per backend — no
  wire/fx/dig. 21 controllers (root) does not clear the bar for a DI framework's indirection cost;
  explicit wiring is idiomatic Go and keeps the dependency graph literally readable.
- **Build/deploy:** multi-stage Docker, `CGO_ENABLED=0` static binary, `distroless/static-debian12`
  (or `alpine` for shell parity) runtime stage. Expected idle RSS 15–40MB per service vs. the
  ADR-083-measured 385–507MB per JVM today — directional until Phase 1 is shipped and measured on
  this exact Pi, stated as an estimate rather than a promised number, consistent with how ADR-083
  treated its own AppCDS estimate.

Full version-by-version reasoning and the complete list of rejected alternatives per component:
doc 13 §2–3.

### 3. GraalVM native-image: considered and rejected

The user has already decided to proceed with the Go rewrite; this section documents why
native-compiling the *existing* Spring Boot app was not the better call, not whether to reopen
that decision.

Native-image genuinely addresses the RAM motivation directly, with much lower migration risk than
a full rewrite: benchmarks found during research show JVM-vs-native RSS deltas in the same
ballpark as this app's own needs — one comparison measured 380MB (JVM) vs. 72MB (native) for a
comparable Spring Boot REST API, roughly a 5x reduction; another found 280MB vs. 62MB at startup.
Applied to this app's own ADR-083 baseline (385–507MB per JVM), a native-image build could
plausibly land in the 70–150MB range per backend — a real, substantial win, achievable without
touching a line of the existing Java domain code.

**Rejected anyway, for four concrete reasons:**

1. **A recurring reflection-configuration tax proportional to this app's JPA surface.** This
   codebase has 46 JPA entities (root alone) plus dozens of MapStruct-generated DTOs across
   3 apps. Spring Boot 4's native-image AOT support is real, but Hibernate's runtime bytecode
   enhancement and JPA's dynamic proxy generation remain the most fragile part of the Spring
   Native story — every future entity or DTO addition risks needing hand-written
   `RuntimeHints`/reflect-config registration that a plain-JVM deployment never needs. This is not
   a one-time migration cost; it is a permanent tax on every future schema change, forever, for as
   long as the app stays on the JVM. Go has no equivalent tax at all.
2. **Build-time cost on the exact hardware this has to run on.** `native-image` compilation is
   CPU/RAM-hungry — commonly 4-8+GB RAM and minutes-to-tens-of-minutes wall time. This Pi is
   already at 95% swap utilization (ADR-083's own measurement); cross-compiling ARM64 native
   images on the Pi itself is likely infeasible, which would force either slow/flaky QEMU-emulated
   builds or standing up a separate ARM64 build machine — new infrastructure this migration
   otherwise needs none of.
3. **It bets zero of the design-quality value the user explicitly asked this rewrite to deliver.**
   Decision context #6 (the task that produced this ADR) frames the move as "a deliberate
   design-quality improvement, not a mechanical language port." Native-image would compile the
   exact same Spring/JPA/Hibernate abstractions forward unchanged — including the specific
   over-engineered Template Method hierarchies and speculative pattern slots named in doc 13 §5 —
   banking none of that simplification. A rewrite is the only path that actually collects that
   value; native-image is orthogonal to it.
4. **Less battle-tested for this specific workload than Go's decade-plus track record.** Go has
   been the default choice for small, always-on REST services with tight memory budgets for
   years; Spring Boot native-image support, while real and improving, remains newer and carries
   more edge-case risk for a JPA-heavy app specifically (per point 1) than for a simpler Spring
   Web-only service.

**Verdict:** GraalVM native-image would cut JVM RSS roughly 3–5x with far lower migration risk
than a full rewrite, but it carries a recurring reflection-config maintenance tax scaled to this
app's 46-entity/dozens-of-DTO JPA surface, and it delivers none of the SOLID/pattern-
simplification value the user explicitly wants from this migration. Rejected as the alternative to
the Go rewrite — but kept, explicitly, as the documented Plan B for whatever remains on the JVM
side if the Go migration ever stalls or is abandoned partway through.

---

## Design quality, not just language port

Per the user's explicit request, and grounded in the same file-level audit
`.claude/tasks/done/shared-backend-refactor.task.md` already did across the 3 apps' Java code
(not re-derived from scratch), three concrete smells are named here as *not* to carry forward —
full detail and the before/after diagram are in doc 13 §5:

1. **The proposed generic, multi-type-parameter Abstract-Factory/repository-factory layer**
   (`PortfolioEntityFactory<T>`/`RepositoryFactory`) was speculative even in Java — it was never
   actually built, because Bridge (`UnitsExtractor<T>`) alone fully resolved the real per-app
   divergence. It should not be resurrected in Go either; one small interface per genuinely
   varying step, composed into a plain struct, is the whole solution.
2. **The 8-domain Template Method service-base hierarchy**
   (`AbstractHistoriaService`/`AbstractDepositService`/`AbstractXirrService`/
   `AbstractAccountCashService`/`AbstractProportionalStrategy`, etc.) is a correct Java answer to
   "no multiple inheritance." Go has no such constraint, so none of this should be ported as
   class-shaped inheritance — each collapses to a struct holding 1-2 interface fields, or a plain
   higher-order function taking the one varying step as a parameter. `AbstractProportionalStrategy`
   specifically — six required Template Method steps, five of which are identical across all 3
   apps — becomes one pure function plus one passed-in closure for the one step that differs.
3. **The Iterator/Memento/Caretaker/Visitor "behavioral quartet"** built for historia
   pagination-with-undo (`pl.shared.history`, plus root's own additional
   `HistoriaCaretaker`/`Memento`) is real ceremony solving a problem (no first-class functions, no
   natively-immutable value copies) that doesn't exist in Go — a slice already iterates, a struct
   copied by value already is a memento. No product requirement for historia undo exists today;
   the Go historia package should be a paginated query and a snapshot struct, nothing more, until
   an actual undo requirement appears.

Doc 13 §5 also documents a fourth smaller point (Command-as-object with Undo semantics, rejected
everywhere except a narrow, justified reconsideration for Phase 5's bot-trading audit trail) and
the full before/after `classDiagram` showing the Java 3-type-parameter abstract hierarchy
collapsing to one concrete Go struct composed from two interfaces.

---

## Consequences

**What's gained:** a credible path to the RAM reduction ADR-083 could only partially deliver
(Postgres consolidation) and explicitly declined to deliver at the JVM layer (rejected the JVM
merge as too risky) — an order-of-magnitude RSS reduction per backend, if the 15–40MB estimate
holds up under real measurement, achieved without merging the three apps' processes or eroding
their already-deliberate independence (JWT issuer, exception-status mapping, filter chains all
stay per-app, now in Go instead of Java). A genuine, non-speculative opportunity to fix named SRP
violations (`PurchaseSessionService`/`PurchasePlanningService`) and remove over-engineered pattern
ceremony while the code is being touched anyway.

**What's lost / at risk:**

- **Two runtimes to operate simultaneously for the duration of the migration.** Every phase adds
  operational surface (a new `backend-go` Compose service per app, new Nginx location blocks,
  a second language's build/dependency tooling) on top of, not instead of, the existing Java
  stack, until Phase 9. This is the direct cost of choosing safety/revertibility over speed, and
  is an explicit trade the user has already made.
- **The write-ownership cluster found in doc 13 §6.3** (`PositionsController`/
  `RebalancingTransferController`/`PurchaseSessionController`/`DepositsController` all writing
  `positions`/`deposits`/`asset_purchases`) means the single most money-critical part of the app
  cannot be split into small phases the way the rest of the domain map can — Phase 7 is
  necessarily a large, atomic, high-stakes cutover. This is a real constraint the sequencing had
  to design around, not a smaller risk than it looks.
- **A reconciliation risk was avoided, not eliminated, by keeping Flyway.** The Flyway-CLI-as-
  init-container approach removes the two-migration-history-table problem, but it does add "a
  Docker image most of this stack doesn't otherwise depend on" as a permanent piece of
  infrastructure (Decision 2) — a small, accepted cost against the alternative's much larger risk.

**What did not get worse:** the OpenAPI-first contract discipline this project already had. Every
phase's Go implementation is generated from, and must match, the same `docs/openapi.yaml` the
frontend already treats as ground truth — this ADR does not introduce a second contract format or
a translation layer between what Go serves and what the frontend expects.

---

## Alternatives considered

- **GraalVM native-image** — see Decision 3 above.
- **golang-migrate / goose for schema migrations** — rejected; see Decision 2's migration bullet.
- **Big-bang rewrite (build the whole Go backend, cut over once)** — not seriously considered; the
  user's decision context is explicit that strangler-fig, incremental cutover is required for a
  live app with real financial data, and no case was found in this codebase's own domain-coupling
  audit (doc 13 §6.3) where a big-bang approach would have been *lower* risk than incremental —
  if anything, the write-ownership cluster in Phase 7 is exactly the kind of thing a big-bang
  rewrite would have discovered on day one of a single giant cutover instead of being scoped and
  planned for in advance.
- **JVM merge across the 3 apps, in lieu of a Go rewrite** — already rejected by ADR-083 on
  separate, still-valid grounds (erases deliberate per-app divergence, single blast radius); not
  re-evaluated here since the user's decision to pursue Go supersedes it as the chosen path to the
  same RAM goal.
- **Echo instead of chi** — rejected per Decision 2's HTTP routing bullet: no capability gap chi
  has that this project needs, more framework surface for no benefit.
- **GORM instead of sqlc** — rejected per Decision 2 and doc 13 §3: this domain's
  computed-at-read-time field pattern doesn't benefit from an ORM's row/object machinery.

---

## References

- `docs/architecture/13-go-backend-target-architecture.md` — full stack table, package layout,
  strangler-fig routing design, write-ownership safety audit, idiomatic pattern mapping,
  complete phase/domain sequencing table.
- `docs/adr/ADR-083-container-ram-consolidation.md` — the RAM-pressure motivation and measured
  baseline this ADR builds on.
- `docs/adr/ADR-077-repo-consolidation-monorepo.md` — the single-repo, independent-deployables
  layout this migration's package structure follows.
- `docs/adr/ADR-078-shared-backend-abstract-core.md` — the Java GoF pattern map doc 13 §5
  evaluates against.
- `.claude/tasks/done/shared-backend-refactor.task.md` — the file-level Java duplication/pattern
  audit this ADR's "design quality, not just language port" section is grounded in.

---

**Status: historical — superseded by as-built (Phase 9 complete 2026-09-19, ADR-098).** The rewrite this ADR decided is complete: Java/Spring Boot is deleted (commit `ff7878f`, restore anchor `2be75ba`) and all three apps run on Go. The current-state references are `docs/architecture/00-stack-validation.md`, `08-monorepo-layout.md` and `12-portfolio-bpps-and-shared-packages.md`; this ADR remains as the record of why Go, why strangler-fig, and what was rejected (e.g. GraalVM native-image).
