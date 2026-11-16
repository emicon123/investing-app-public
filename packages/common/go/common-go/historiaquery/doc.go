// Package historiaquery is the shared historia-read kernel for the sibling apps (portfolioB + portfolioC):
// the paginated list query plus the inflation enrichment, parameterized only by what genuinely
// differs per app (portfolio routing + the fixed INFLATION_BASE month).
//
// The pure inflation math (math.go) is the single copy for all three apps: the main app's
// internal/historia imports it too and keeps only its per-portfolio baselines. XIRR is
// common-go/xirr (ADR-112); CashFlow is an alias of xirr.Flow.
//
// Deliberately NOT ported from the Java historia services (same call as the main app's historia.go
// documents): the Iterator/Memento/Caretaker/Visitor behavioral quartet — a Go slice already
// iterates, and there is no undo requirement. This package is a paginated query plus enrichment,
// full stop.
//
// The GET path here never writes; the snapshot scheduler lives in common-go/historiascheduler (each
// app's main.go gates it on HISTORIA_SCHEDULER_ENABLED).
package historiaquery
