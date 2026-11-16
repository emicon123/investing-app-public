// Package admin is portfolio-b's thin wiring for the admin domain POST
// /admin/history/backfill-position-values plus the once-at-startup backfill runner.
//
// SPLIT by design: only two pieces are shared — priceprovider.OHLCProvider (the history-data seam)
// and priceprovider.NearestClose. All backfill math stays per-app: portfolioB fills only rows on/after
// ACCURATE_CUTOFF (2026-07-14), folds companion rows into their parent ticker, rewinds units
// through purchase lots AND position transfers, and prices CHF rows off the CHF FX rate. PortfolioC's
// variant is flat per portfolio.
//
// The runner fires once, asynchronously, after ListenAndServe is up (main.go launches it after the
// server goroutine; ported from the Java HistoriaBackfillRunner). Fail-soft: a backfill failure
// logs a warn and never takes the process down.
package admin
