// Package inflation serves the inflation endpoints (ported from the Java InflationController,
// InflationEntryService and GusInflationSyncService, ADR-094 / doc 13 §7). It owns the `inflation`
// table exclusively (both writers — manual create and the GUS sync — live here; the historia
// packages read it read-only). Since task 178 the GUS client, the sync and its scheduler are the
// shared common-go/inflationsync kernel over this package's Adapter; List/Create (GET/POST
// /inflation, main-only endpoints) stay here.
package inflation
