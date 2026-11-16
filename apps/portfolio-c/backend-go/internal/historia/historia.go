// Package historia is portfolio-c's thin wiring for the historia domain the whole read
// behavior lives in the shared common-go/historiaquery kernel (paginated list + inflation
// enrichment), and this package holds only what genuinely differs per app — the adapter from this
// app's generated *sqlcgen.Queries to the kernel's Queries interface, the chi handler adapting the
// kernel to this app's generated ServerInterface method. The nightly snapshot writer is
// common-go/siblingcore/snapshot (task 180), started from cmd/server (HISTORIA_SCHEDULER_ENABLED,
// default false, true in compose — ADR-097).
//
// Per-app facts (pinned by tests here):
//
//   - Portfolio dimension REQUIRED: ?portfolio=VARIABLE|FIXED on every call —
//     absent or unknown answers 400 (portfolioB has no such param at all).
//   - INFLATION_BASE = 2026-07 (portfolioB uses 2017-01 — never unified, see the pin test).
//   - DTOs carry the extra portfolio field (portfolioB's HistoriaEntryDto has none).
//   - Settings / Xirr / PriceCache are supporting-only here: their endpoints are served
//     by their own packages (settings, deposit, marketdata); the snapshot reads
//     price_cache/fx_rate_cache read-only (task 144 lesson — never a write) and
//     solves XIRR through the kernel.
//   - PageInfo.size echoes size>0?size:50.
package historia
