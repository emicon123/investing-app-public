// Package inflation is portfolio-c's home for the inflation domain tests. The whole behavior
// lives in the shared kernel common-go/inflationsync (upsert loop, staleness rule, GUS client,
// scheduler, the POST /inflation/sync handler) over the shared siblingcore/refdata store; this
// app wires them in cmd/server and keeps only the round-trip against its own migrated schema here.
package inflation
