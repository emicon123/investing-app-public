// Package inflationsync is the shared Go kernel for the inflation domain of portfolio-b
// and portfolio-c (ADR-094, doc 13 §7) — the genuine test of doc 13 §5's claim that Go's
// lack of an inheritance ceiling lets portfolioB/portfolioC collapse further than the Java Bridge pattern
// could: both apps' sync services were 100% identical after package-name normalization (same cron,
// same startup stale check, same upsert loop, same SDP client), so the whole domain lives here,
// parameterized only by the DB connection each sibling's thin wiring supplies.
//
// Since task 178 the main app uses this kernel too, through the same kind of adapter: SDP series
// (639 pre-2026 / 2874 from 2026), 300ms inter-year pacing, one retry, and the monthly cron "0 0 6
// 15 * * Europe/Warsaw" plus startup stale check are all defined here. The Service is sync-only:
// main's GET /inflation and POST /inflation (List/Create) live in main's own internal/inflation;
// portfolioB/portfolioC expose only POST /inflation/sync (SingleResponse {upserts}).
//
// The Queries interface is expressed in plain Go/decimal types (never a concrete
// sqlcgen row type) so both siblings' per-app sqlc packages satisfy it through a
// ~40-line adapter each — no per-app service, no per-app scheduler, no per-app
// client. A behavior difference between the apps here would be an ADR-level
// finding, not a tuning commit: don't force unification if one appears.
package inflationsync
