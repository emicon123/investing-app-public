# ADR-112: One XIRR Implementation (Brent, Injected Valuation Date)

**Date:** 2026-11-04
**Status:** Accepted (owner decision 5, task 174 REPORT §7a → batch 178). Supersedes the
"faithful port, deliberately not Brent" rationale of `common-go/bisectxirr` (task 158 G2).
Design: `docs/architecture/diagrams/174-remediation-design.md` §2.6, §6.2.
**Scope:** common-go (`historiaquery`, `bisectxirr` → new `xirr`), main `historia` + `deposit`,
portfolio-b/portfolio-c `deposit` + `historia`.

## Context

XIRR exists four times (F2.1): `common-go/historiaquery/xirr.go` (Brent), a byte-identical copy in
main `internal/historia/xirr.go`, main `internal/deposit/xirr.go` (delegates to the copy), and
`common-go/bisectxirr` (bisection with bracket expansion) used by the siblings' **deposits** page while
their **historia** snapshots use the Brent one, so one app computes the same quantity two ways.
A 20,000-case oracle showed both solvers agree to ≤3·10⁻⁵ on every realistic series; they differ only
for annualised losses worse than −99.9 % (Brent → nil, bisection → ≈ −100 %). `bisectxirr` reads
`time.Now().UTC()` internally, which is impure and puts the valuation day one day early between 00:00
and 01:00/02:00 Warsaw time. The Brent code has a latent defect (F5.3): its acceptance test
`s < (3a+b)/4 || s > b` assumes `(3a+b)/4 < b`, so after a swap it degrades to bisection, and after
1000 iterations it returns an unconverged value as if it were a root.

## Decision

1. One package, **`common-go/xirr`**: `Solve(flows []Flow) *decimal.Decimal` (Brent on
   [−0.999, 10], accuracy 1e-7, 1000 iterations, scale 6 HALF_UP, nil when there is no meaningful
   root) and `Summarize(deposits, currentValue, valuationDate)` for the siblings' deposits summary.
2. The valuation date is always a parameter. Callers pass `historiaquery.NowInWarsaw()` (deposits) or
   the snapshot date (historia); nothing inside `xirr` reads a clock.
3. Brent fixes: accept the interpolated step only if it lies strictly between `(3a+b)/4` and `b`
   in either order; an unconverged run answers nil.
4. `historiaquery.SolveXirr`, main `historia.SolveXirr`/`brentSolve`, main `deposit.solveXirr` and
   `common-go/bisectxirr` are deleted; `historiaquery.CashFlow` and main's `historia.CashFlow` /
   `deposit.CashFlow` become aliases of `xirr.Flow`.
5. Main's deposits summary keeps its own `simpleReturnPct` (`V/I − 1`, scale 6) and `yearsElapsed`
   (`days / 365.25`, scale 4); the siblings keep theirs (`(V − I)/I`, scale 8; `days / 365`,
   scale 8) via `Summarize`. Only the solver is unified; the display formulas are per-app wire
   contracts (recorded difference, not duplication).

## Alternatives rejected

- **Keep bisection for the siblings' deposits (faithful port):** the Java backend that justified it
  is deleted (ADR-098); two solvers for one number is the bug class the owner rule targets.
- **Unify on bisection:** slower and the expansion search is dead code (`bracketed` is always true).
- **A `Solver` strategy interface:** one implementation; speculative.
- **Unify the summary formulas too:** changes visible numbers on main's deposits page for no
  correctness gain; owner decision 5 covers the solver only.

## Consequences

- portfolio-b/portfolio-c deposits show "—" instead of ≈ −100 % for annualised losses beyond −99.9 % (the
  owner-accepted visible change).
- The 00:00–02:00 Warsaw off-by-one-day on the siblings' deposits XIRR disappears.
- Golden tests: every existing XIRR test in the four deleted places moves to `xirr` (or keeps
  asserting through the aliases); a property test runs `Solve` against an independent bisection on
  random series (the review's oracle, seeded, ≥2,000 cases, tolerance 3·10⁻⁵).
