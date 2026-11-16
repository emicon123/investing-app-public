// Package cashpositions serves the cash-positions endpoints (ported from the Java
// CashPositionService, ADR-094 / doc 13 §7). It owns the `cash_positions` table's WRITE path
// (snapshot batch insert); the rebalancing value computation reads the latest snapshot read-only
// (doc 13 §6.3).
package cashpositions
