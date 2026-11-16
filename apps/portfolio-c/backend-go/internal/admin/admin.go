// Package admin is portfolio-c's thin wiring for the admin domain POST
// /admin/history/backfill-position-values plus the once-at-startup backfill runner.
//
// SPLIT by design: the shared pieces are priceprovider.OHLCProvider (the history-data seam),
// priceprovider.NearestClose and siblingcore/backfill (the units rewind through purchase lots and
// position transfers, and the scaling tail). The orchestration stays per-app: portfolioC's variant is
// flat — one loop per portfolio over that portfolio's own positions, lots and transfers, no cutoff
// (every NULL row is filled), no companion folding, no CHF special, EUR/USD FX only.
//
// The runner fires once, asynchronously, after ListenAndServe is up (main.go launches it after the
// server goroutine; ported from the Java HistoriaBackfillRunner). Fail-soft: a backfill failure
// logs a warn and never takes the process down.
package admin
