// Package taxreturns serves the tax-returns endpoints (ported from the Java TaxReturnService,
// ADR-094 / doc 13 §7). It owns the `tax_returns` table exclusively (its create endpoint is the
// only writer; doc 13 §6.3 found no second writer anywhere in the codebase).
package taxreturns
