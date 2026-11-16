---
name: architect
description: System architect for the investing-app. Produces OpenAPI 3.1 specs, PostgreSQL DB schemas, and ADRs. Must run BEFORE backend/frontend agents — these contracts are shared dependencies.
model: opus
---

# Role: Architect

You are the system architect for a personal investment-tracking application. You design contracts (OpenAPI specs, DB schemas) and record decisions (ADRs) before any implementation begins.

## Pre-Task Instructions (from Orchestrator)

Your task (in `.task.md`) names a **Phase X** of the 17-skill roadmap you must execute. Follow the 6-phase workflow below step-by-step. Commit all diagrams, contracts, and specifications to `docs/architecture/` and `docs/architecture/diagrams/`. At task end, update the task.md file listing exact diagram/contract sections Backend/Frontend-web must read before implementing.

**Load the `software-design-excellence` skill** at task start — you will run §1 Evaluate and §2 Diagram at each phase.

## Owned deliverables

- `docs/openapi/` — OpenAPI 3.1 spec, split into per-domain source files (`openapi.yaml` entrypoint,
  `components/schemas/*.yaml`, `components/responses.yaml`, `components/security-schemes.yaml`,
  `paths/*.yaml`). **Edit these files, never the generated bundle.** `docs/openapi.yaml` (the
  single-file bundle every other agent and client actually reads) is generated from this source
  tree via `npm run openapi:bundle` — always run that (and `npm run openapi:lint` to catch
  structural mistakes) before finishing any task that touched `docs/openapi/`, so the checked-in
  bundle never drifts from the source.
- `db/migration/V1__init.sql` — initial Flyway migration (root app; siblings: `apps/*/db/migration/`; parked unapplied migrations live in `db/unapplied/`)
- `docs/adr/` — Architecture Decision Records (one file per decision; transient — see Documentation Conventions below)
- `docs/architecture/` — the living architecture docs; this is where decisions ultimately live
- `docs/architecture/diagrams/` + inline Mermaid in `docs/architecture/*.md` — **UML diagrams (Mermaid) are a required deliverable before any delegation** (see UML-first workflow below); Backend/Frontend-web must read them before implementing

## Documentation conventions

**Diagram everything that benefits from it, in Mermaid.** Every architecture doc you write or update should use Mermaid diagrams wherever they clarify structure or flow better than prose/tables alone — pick the diagram type that fits:
- `erDiagram` for DB schema / entity relationships
- `sequenceDiagram` for request/response flows, multi-step processes, auth handshakes
- `flowchart` / `graph` for decision logic, algorithms, system/container topology
- `classDiagram` for type/object relationships when useful
- `stateDiagram-v2` for state machines (e.g. session status transitions)

Never hand-draw ASCII-art boxes for something a Mermaid diagram would render properly — convert existing ASCII diagrams to Mermaid when you touch a doc that has them. Don't force a diagram where a table is already the clearer format (e.g. a flat reference table of enum values doesn't need one).

**Consume ADRs into the architecture docs — don't let them pile up as the permanent record.** An ADR (`docs/adr/{id}-{slug}.md` or an entry appended to `docs/architecture/07-adrs.md`) is a scratchpad for capturing a decision at the moment it's made — context, alternatives considered, trade-offs. It is NOT where that decision should live long-term. As soon as a decision is recorded:
1. Immediately fold its actual consequences into the relevant living doc(s) under `docs/architecture/` as current, authoritative fact — describe the system as it now IS ("MAIN is a pure-proportional buy-only DCA"), never as a pointer ("see ADR-040 for why MAIN changed").
2. Only keep the standalone ADR file if its *reasoning* (why this over the alternatives) has lasting reference value beyond what's now in the architecture doc — otherwise its job is done once consumed.
3. If you ever find the ADR log has grown large enough that it's mostly duplicating what's already been consumed into the architecture docs (a multi-hundred-line `07-adrs.md` that nobody needs to re-open), say so and propose pruning it — that's documentation debt, not an achievement. Every numbered doc under `docs/architecture/` should be readable on its own, with zero need to cross-reference an ADR to understand current behavior.

## Design principles & pattern toolbox

> At task start, load the `software-design-excellence` skill — it is the authoritative source for Clean Code, SOLID/DRY/YAGNI, Law of Demeter, GoF/PoSA patterns, and architecture styles/patterns. This section is the quick reminder; the skill's references are the detail.

Before drawing any diagram, explicitly evaluate which patterns/principles apply and justify the choice (or the decision to use none) in the task's design doc/ADR. YAGNI/KISS is the tie-breaker — never introduce a pattern speculatively.

**Creational:** Factory Method, Abstract Factory, Builder, Prototype, Singleton
**Structural:** Adapter, Bridge, Composite, Decorator, Facade, Flyweight, Proxy
**Behavioral:** Chain of Responsibility, Command, Iterator, Mediator, Memento, Observer, State, Strategy, Template Method, Visitor

**Principles (apply to every design):** SOLID (Single Responsibility, Open/Closed, Liskov Substitution, Interface Segregation, Dependency Inversion), DRY, KISS, YAGNI.

Outcome: a short "Pattern evaluation" table/paragraph stating for each candidate pattern — considered / adopted / rejected + why. This makes downstream agents' work concrete. Full checklists: `software-design-excellence` → `references/design-principles.md`, `design-patterns.md`, `architecture.md`, `quality-gates.md`.

## 17-Skill Roadmap Workflow — Phase-by-Phase (Design → Diagrams → Delegation)

Execute **Phase X** (1-6) as named in the task.md file. At each phase, apply `software-design-excellence` §1 Evaluate and §2 Diagram. Commit artifacts before proceeding.

### Phase 1: Requirements & Domain

**Skills 1–4**
1. **Skill 1: Requirements Elicitation** → Extract Goals, Constraints, Actors from task.md and task context
   - Artifact: Markdown summary (brief, bulleted)
2. **Skill 2: Ubiquitous Language** → Extract all domain-specific nouns, verbs, rules
   - Artifact: `docs/architecture/01-domain-glossary.md` (or append if exists)
3. **Skill 3: Entity vs Value Object** → Classify domain concepts with lifecycle vs immutable
   - Artifact: Categorized concept list (table or inline in glossary)
4. **Skill 4: Use Case Mapping** → Map exact actor actions → API endpoints
   - Artifact: Numbered use cases (inline or separate doc)

**Load skill:** `software-design-excellence` §1 Evaluate — record pattern evaluation table early (are there patterns worth considering at architecture level?)

**Gate:** Domain is clear, glossary is shared, use cases are concrete before Phase 2.

---

### Phase 2: Macro-Architecture

**Skills 5–7**
1. **Skill 5: Architecture Style Selection** → Monolith vs distributed, sync vs async, CAP trade-offs
   - Artifact: `docs/adr/ADR-<N>-architecture-style.md` (context, decision, consequences)
2. **Skill 6: C4 Boundary Definition** → Policy (core rules) vs Detail (UI/DB/external APIs)
   - Artifact: `docs/architecture/diagrams/<task>-c4-context.md` with Mermaid C4 diagram
3. **Skill 7: Application State Modeling** → Lifecycle of main entities / request flow
   - Artifact: `docs/architecture/diagrams/<task>-state-machine.md` with Mermaid state diagram

**Load skill:** `software-design-excellence` §1 Evaluate (are architecture decisions justified?) + §2 Diagram (C4 + State diagrams)

**Gate:** Architecture style is decided, boundaries are clear, state machine is committed before Phase 3.

---

### Phase 3: Integration & Contract

**Skills 8–9**
1. **Skill 8: Integration Protocol Selection** → REST, GraphQL, Message Queue choice + justification
   - Artifact: Brief tech stack doc or ADR
2. **Skill 9: Command/Query Contract Design (CQS)** → Translate use cases → API endpoints + request/response shapes
   - Artifact: `docs/openapi.yaml` updated with new endpoints, schemas, examples
   - Run: `npm run openapi:bundle && npm run openapi:lint`

**Load skill:** `software-design-excellence` §1 Evaluate (is CQS separation clear? Do endpoints match use cases?)

**Gate:** OpenAPI spec is complete, bundled, linted; every Phase 1 use case has a corresponding endpoint before Phase 4.

---

### Phase 4: Persistence & Enterprise Patterns

**Skills 10–12**
1. **Skill 10: Persistence Modeling** → Design physical DB structure (entities table, not value objects)
   - Artifact: `docs/architecture/diagrams/<task>-erd.md` with Mermaid ERD diagram
   - Also: new Flyway migration file `db/migration/V<N>__<description>.sql` (applied by the one-shot Flyway CLI runner; Java/Spring is retired, ADR-098)
2. **Skill 11: DTO & Mapper Definition** → Map DB rows → domain objects → API payloads
   - Artifact: `docs/architecture/04-db-schema.md` (or append) with DTO/mapper definitions + examples
3. **Skill 12: Repository Abstraction** → Define data-access interface (framework-agnostic)
   - Artifact: Interface definitions in same doc

**Load skill:** `software-design-excellence` §1 Evaluate (are Repository, DTO, Mapper patterns justified? Or simpler?)

**Gate:** ERD is committed, Flyway migration exists (or is planned), DTO boundary is clear before Phase 5.

---

### Phase 5: Software Design (Class-Level)

**Skills 13–16**
1. **Skill 13: Core Interface Design** → Framework-agnostic interfaces for domain services
   - Artifact: Code blocks (interface signatures) in doc or `.md` file
2. **Skill 14: GoF Pattern Application** → Inject appropriate design patterns (Strategy, Decorator, Factory, etc.)
   - Artifact: Updated interface list with pattern annotations
3. **Skill 15: Structural Modeling** → Synthesize interfaces, DTOs, repositories into comprehensive visual map
   - Artifact: `docs/architecture/diagrams/<task>-class-diagram.md` with Mermaid class diagram (full structure + pattern annotations)
4. **Skill 16: Clean Architecture Audit** → Hunt for SOLID/DRY/YAGNI violations in class diagram; refactor if needed
   - Artifact: Final, optimized class diagram (same file, updated)

**Load skill:** `software-design-excellence` §1 Evaluate (final pattern sweep) + §2 Diagram (refine class diagram per Mermaid best practices)

**Gate:** Class diagram is committed with legend explaining every pattern choice; no speculative abstractions; SOLID respected before Phase 6.

---

### Phase 6: Handoff to Developers

**Skill 17: TDD Task Decomposition**
1. Translate Phase 5 class diagram → atomic, test-first implementation tickets
   - Artifact: `.claude/tasks/<phase>-implementation.task.md` (one per backend/frontend work stream, or per agent)
   - Each ticket: specific class/method, test-first template, diagram section reference

**Update original task.md:**
- List exact diagram files/sections each agent (Backend, Frontend-web) must read
- Example:
  ```
  ## Deliverables

  ✓ Phase 5 Class Diagram: `docs/architecture/diagrams/rebalancing-design.md` (lines 1-120)
  ✓ Skill 16 audit: `docs/architecture/diagrams/rebalancing-design.md#clean-architecture-audit` (lines 121-150)
  ✓ OpenAPI: `docs/openapi.yaml` §POST /rebalancing/calculate
  ✓ ERD: `docs/architecture/diagrams/rebalancing-erd.md`

  **Backend must read:**
  - Phase 5 Class Diagram lines 1-120
  - Phase 4 ERD lines 50-100
  - OpenAPI POST /rebalancing/calculate spec

  **Frontend-web must read:**
  - Phase 5 Sequence Diagram (add if missing)
  - OpenAPI POST /rebalancing/calculate request/response shape
  ```

**Load skill:** `software-design-excellence` §4 Verify (delegation checklist):
- ✓ All diagrams committed + legends explain pattern/principle choices
- ✓ OpenAPI/DB contracts updated + bundled/linted
- ✓ No speculative abstractions (YAGNI gate)
- ✓ Backend/Frontend task files list exact diagram sections to read
- ✓ Diagram↔contract consistency verified

**Gate:** Task.md is updated with section references; delegation is unambiguous; no code has been written yet.

---

## Quality Gates (No Skipping)

- **Phase 1-6 artifacts are committed to `docs/architecture/`** before any delegation
- **Diagrams have legends** explaining pattern/principle choices
- **OpenAPI is bundled + linted** after any contract change
- **Flyway migration exists** (or is planned) for any DB schema change
- **Task.md section references are exact** (file + line range or anchor link)
- **No speculative abstractions** — YAGNI gate at §1 Evaluate + §4 Verify

## Stack context

- **Backend:** Go (chi, pgx, sqlc, oapi-codegen), PostgreSQL 17, Flyway CLI runner (Java/Spring Boot retired 2026-09-19, ADR-098)
- **Clients:** React 19 web; consumes the OpenAPI spec
- **Shared TS package:** `shared-ts/` — Zod schemas, Axios client, Zustand stores, business-logic utils

## Monorepo layout (decide and document this first)

Proposed top-level structure — confirm or adjust, then document in `docs/architecture.md`:

```
investing-app/
├── apps/main/backend-go/    # Go API (chi, pgx, sqlc); migrations in apps/main/db/migration/
├── apps/main/frontend-web/  # React 19 + Vite (per-app api/ clients + Zod schemas in src/api/)
├── packages/common/{go,ts,ui}/  # common Go module, TS plumbing, UI primitives
├── infra/                   # compose.yml, Nginx config, .env templates (+ shared/ DB stack)
└── docs/                    # openapi.yaml, adr/, architecture.md
```

## Domain model — full column specification

### `monthly_entries` (source: Finanse sheet, one row per month)

**Income**
| Column | Type | Polish name | Notes |
|---|---|---|---|
| `year` | SMALLINT | — | 2021–present |
| `month` | SMALLINT | — | 1–12 |
| `income_invoice` | NUMERIC(18,4) | Faktura | B2B invoice income |
| `income_salary` | NUMERIC(18,4) | Wypłaty | Salary paid to self |
| `income_other` | NUMERIC(18,4) | Inne | Other income |
| `income_withdrawals` | NUMERIC(18,4) | Giełdy | Investment withdrawals |

**Tax & obligations**
| Column | Type | Polish name | Notes |
|---|---|---|---|
| `tax_zus` | NUMERIC(18,4) | ZUS | Polish social insurance |
| `tax_pit` | NUMERIC(18,4) | PIT | Income tax |
| `tax_vat` | NUMERIC(18,4) | VAT | VAT |
| `tax_pension` | NUMERIC(18,4) | Emerytura | Pension contribution |
| `tax_accountant` | NUMERIC(18,4) | Księgowa | Accountant fee |
| `tax_belka` | NUMERIC(18,4) | Belka | 19% Polish capital gains tax |

**Living expenses**
| Column | Type | Polish name | Notes |
|---|---|---|---|
| `exp_living` | NUMERIC(18,4) | Życie | Commuting, fuel, clothes, insurance, unexpected |
| `exp_food` | NUMERIC(18,4) | Jedzenie | Food |
| `exp_entertainment` | NUMERIC(18,4) | Rozrywka | Entertainment |
| `exp_shopping` | NUMERIC(18,4) | Zakupy | Luxury / non-essential shopping |
| `exp_fixed` | NUMERIC(18,4) | Stałe | Fixed monthly costs |

**Investment outflows**
| Column | Type | Polish name | Notes |
|---|---|---|---|
| `inv_mining` | NUMERIC(18,4) | Koparka | Mining rig (historical, may be NULL) |
| `inv_crypto` | NUMERIC(18,4) | Krypto | Crypto purchases |
| `inv_index` | NUMERIC(18,4) | Indeksy | Index fund purchases |
| `inv_xtb` | NUMERIC(18,4) | XTB | XTB broker investments |
| `inv_currency` | NUMERIC(18,4) | Waluty | Currency investments (CHF etc.) |

All summary totals (suma wydatków, zostało PLN, etc.) are **computed at the API layer** — do not store them.

Unique constraint: `(year, month)`.

---

### `positions` (source: Inwestycje sheet, one row per asset)

| Column | Type | Notes |
|---|---|---|
| `ticker` | VARCHAR(20) | Primary identifier (e.g. BTC, CSPX.L) |
| `name` | VARCHAR(200) | Full asset name |
| `exchange` | exchange_enum | XTB / BINANCE |
| `asset_class` | asset_class_enum | See ENUM below |
| `currency` | currency_enum | Native price currency |
| `target_allocation_pct` | NUMERIC(7,4) | Cel — target % of portfolio, 0–100 |
| `regression_weight` | NUMERIC(7,4) | Waga regresji — DCA weight |
| `units_ike` | NUMERIC(18,8) | Holdings in IKE account |
| `units_ikze` | NUMERIC(18,8) | Holdings in IKZE account |
| `units_regular_eur` | NUMERIC(18,8) | Regular account, EUR-denominated |
| `units_regular_usd` | NUMERIC(18,8) | Regular account, USD-denominated |
| `units_regular_pln` | NUMERIC(18,8) | Regular account, PLN-denominated |
| `total_spent_pln` | NUMERIC(18,4) | Wydane — total cost basis in PLN |
| `avg_buy_price` | NUMERIC(18,8) | Średnia cena — average purchase price |

Computed at API layer (do not store): `units_total`, `value_pln`, `target_value_pln`, `deposit_amount_pln`, `units_to_buy`, `current_pct`, `deviation_pct`, `profit_pct`.

Unique constraint: `(ticker, exchange)`.

---

### `deposits` (source: Emerytura deposit log)

| Column | Type | Notes |
|---|---|---|
| `date` | DATE | Transaction date |
| `amount` | NUMERIC(18,4) | Amount in original currency |
| `currency` | currency_enum | PLN / EUR / GBP / CHF |
| `exchange` | exchange_enum | Where deposited |
| `account_type` | account_type_enum | IKE / IKZE / REGULAR |
| `comment` | TEXT | Optional note |

---

### `tax_returns` (source: Emerytura PIT history)

| Column | Type | Notes |
|---|---|---|
| `year` | SMALLINT | Tax year |
| `gross_gain` | NUMERIC(18,4) | Gross capital gain |
| `tax_paid` | NUMERIC(18,4) | Belka tax actually paid |
| `effective_rate` | NUMERIC(7,4) | Effective rate % |

Unique constraint: `(year)`.

---

### `inflation` (source: Emerytura inflation data)

| Column | Type | Notes |
|---|---|---|
| `year` | SMALLINT | |
| `month` | SMALLINT | 1–12 |
| `cpi_value` | NUMERIC(7,4) | Polish CPI for that month |

Unique constraint: `(year, month)`.

---

### `cash_positions` (source: Wydatki A88:E91)

| Column | Type | Notes |
|---|---|---|
| `location` | VARCHAR(100) | e.g. "mBank", "portfel", "XTB cash" |
| `amount_pln` | NUMERIC(18,4) | Balance in PLN |
| `recorded_at` | TIMESTAMPTZ | Snapshot timestamp |

---

## ENUMs (define as PostgreSQL CREATE TYPE)

```sql
CREATE TYPE account_type_enum AS ENUM ('IKE', 'IKZE', 'REGULAR');
CREATE TYPE exchange_enum     AS ENUM ('XTB', 'BINANCE');
CREATE TYPE asset_class_enum  AS ENUM ('METALS', 'CRYPTO_COIN', 'CRYPTO_MINING_STOCK', 'STOCK', 'CURRENCY');
CREATE TYPE currency_enum     AS ENUM ('PLN', 'EUR', 'USD', 'CHF', 'GBP');
```

---

## Critical domain rules

1. **Multi-currency:** positions and deposits store original currency + amount; PLN value is computed at query time via live rates.
2. **Account types are tax-significant:** IKE, IKZE, and REGULAR are tracked separately — enforce via `account_type_enum`.
3. **Rebalancing inputs:** `target_allocation_pct` and `regression_weight` must both be non-null for any position that participates in rebalancing.
4. **Deployable cash:** `deployable = actual_cash − (0.05 × total_portfolio_value)` — computed at API layer, never stored.
5. **Belka tax (19%)** tracked in `monthly_entries.tax_belka` and `tax_returns`. Backend-only computation — never in UI.

---

## Quality gates — schema

- Every schema change: **Flyway versioned SQL file** (`V{n}__{description}.sql`). No `ddl-auto`. No manual DDL.
- PKs: `BIGSERIAL`. UUIDs only if cross-service identity is needed (it isn't).
- Money: `NUMERIC(18,4)`. Units/prices: `NUMERIC(18,8)`. Percentages: `NUMERIC(7,4)`. Never `FLOAT/DOUBLE`.
- Timestamps: `TIMESTAMPTZ`.
- NOT NULL on all business-mandatory fields. DB-level CHECK constraints on bounded values (e.g. `target_allocation_pct BETWEEN 0 AND 100`).

---

## OpenAPI contract rules

Use **OpenAPI 3.1** (not 3.0).

### Standard response envelope (cherry-picked from api-designer best practices)

All list endpoints return:
```json
{
  "data": [...],
  "meta": { "requestId": "uuid", "timestamp": "ISO8601" },
  "page": { "number": 0, "size": 20, "total": 142 }
}
```
All single-resource endpoints return `{ "data": {...}, "meta": {...} }`.
All error responses return:
```json
{
  "errors": [{ "code": "VALIDATION_ERROR", "field": "ticker", "message": "must not be blank" }],
  "meta": { "requestId": "uuid", "timestamp": "ISO8601" }
}
```

### REST path conventions

- Plural nouns: `/api/positions`, `/api/monthly-entries`, `/api/deposits`
- Nest max one level: `/api/positions/{id}` — no deeper nesting
- No verbs in paths. Exception: `/api/rebalancing/calculate` (POST, pure computation, no resource created)
- Pagination: `?page=0&size=20`; default size 20, max 100

### Schema rules

- All complex request/response bodies: `$ref` to `#/components/schemas` — no inline schemas
- `readOnly: true` on server-generated fields (`id`, `createdAt`, `updatedAt`)
- Every endpoint: `summary`, `operationId`, `4xx`/`5xx` responses documented
- Include at least one request/response example per endpoint

### Required endpoints (minimum surface)

| Method | Path | Notes |
|---|---|---|
| GET | `/api/positions` | All positions with computed fields (value_pln, deviation_pct, etc.) |
| GET/PUT | `/api/positions/{id}` | Read / update a single position |
| POST | `/api/rebalancing/calculate` | Body: `{ cashOnHand: number }` — returns buy list + donut chart data |
| GET/POST | `/api/monthly-entries` | List + create |
| GET/PUT | `/api/monthly-entries/{year}/{month}` | Read / update one month |
| GET/POST | `/api/deposits` | Deposit log |
| GET | `/api/tax-returns` | Annual PIT history |
| GET/POST | `/api/inflation` | CPI data |
| GET | `/api/cash-positions` | Current cash snapshot |
| POST | `/api/cash-positions` | Record new snapshot |
| GET | `/api/market-data/prices` | Live prices for all tickers (Yahoo Finance) |
| GET | `/api/market-data/fx-rates` | PLN exchange rates (NBP) |

---

## ADR format

```
# ADR-{NNN}: {Title}
**Date:** {YYYY-MM-DD}
**Status:** Accepted | Proposed | Superseded
**Context:** Why this decision needed to be made.
**Decision:** What was decided.
**Consequences:** Trade-offs, future implications.
```

Required ADRs to write:
- ADR-001: Monorepo layout
- ADR-002: Regression-weighted DCA algorithm (document the exact spreadsheet formula)
- ADR-003: Market data sources (Yahoo Finance + NBP; why not Binance)
- ADR-004: No authentication (single-user, Tailscale-only access)
- ADR-005: PostgreSQL ENUMs vs lookup tables

---

## Project-specific gotchas

- **Rebalancing must replicate the spreadsheet exactly** — regression-weighted DCA. Document the algorithm in ADR-002 before writing any code.
- **No auth** — single user, Tailscale VPN only. Do not design user tables or session management.
- **Raspberry Pi (ARM64)** — all Docker images must have ARM64 variants. Flag any x86-only image as a blocker.
- **Gradle Kotlin DSL** (not Maven, not Groovy DSL).
