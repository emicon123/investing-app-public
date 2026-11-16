#!/usr/bin/env bash
# Prints sqlc.yaml's `schema:` file list, in REAL Flyway numeric order (V1, V2, ... V10, V11,
# ... not lexicographic string order).
#
# Why this exists: sqlc's directory-based `schema: <dir>` reads files in lexicographic order
# (documented at https://docs.sqlc.dev/en/latest/howto/ddl.html, under "Handling SQL
# migrations" — "ensure numeric migration filenames have sufficient leading zeros"). Flyway's own
# `V<N>__desc.sql` convention has no leading zeros, so once a migration directory crosses 9 files,
# lexicographic order silently reorders migrations (e.g. "V45__..." sorts before "V9__..."),
# which breaks any later migration that ALTERs a table an earlier-numbered-but-later-sorted
# migration hasn't created yet — confirmed empirically against investing-app's own 71-migration
# directory during Phase 0 (`sqlc generate` failed with "relation ... does not exist" for exactly
# this reason).
#
# Flyway migration filenames are Architect/Java-owned and must never be renamed on a live system
# (would break Flyway's checksum-based `flyway_schema_history` for already-applied migrations) —
# so the fix lives entirely on the sqlc side: list the same files, unmodified, in real numeric
# order, as an explicit YAML array under `schema:` in sqlc.yaml.
#
# Usage:
#   hack/gen-sqlc-schema-list.sh <migration-dir> > /tmp/schema-list.yaml
# Paste the output under sqlc.yaml's `schema:` key (replacing the previous list) whenever a new
# Flyway migration file is added to <migration-dir>.
set -euo pipefail

migration_dir="${1:?usage: gen-sqlc-schema-list.sh <migration-dir>}"

echo "    schema:"
find "$migration_dir" -maxdepth 1 -name 'V*.sql' -printf '%f\n' | sort -V | while IFS= read -r f; do
  echo "      - \"${migration_dir}/${f}\""
done
