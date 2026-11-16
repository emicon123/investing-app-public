#!/bin/bash
# Runs once, on first startup of an empty db-shared data volume (docker-entrypoint-initdb.d
# convention). Creates one role + one database per sibling app, each role scoped to CONNECT
# only on its own database — physical co-location on one Postgres instance must not become
# credential co-location. See ADR-083 Decision 1 and .claude/tasks/done/132-*.task.md.
set -euo pipefail

psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname postgres <<-EOSQL
  CREATE ROLE investing_user WITH LOGIN PASSWORD '$INVESTING_DB_PASSWORD';
  CREATE ROLE portfolio_b_user WITH LOGIN PASSWORD '$PORTFOLIO_B_DB_PASSWORD';
  CREATE ROLE portfolio_c_user WITH LOGIN PASSWORD '$PORTFOLIO_C_DB_PASSWORD';

  CREATE DATABASE investing OWNER investing_user;
  CREATE DATABASE portfolio_b OWNER portfolio_b_user;
  CREATE DATABASE portfolio_c OWNER portfolio_c_user;

  REVOKE CONNECT ON DATABASE investing FROM PUBLIC;
  REVOKE CONNECT ON DATABASE portfolio_b FROM PUBLIC;
  REVOKE CONNECT ON DATABASE portfolio_c FROM PUBLIC;

  GRANT CONNECT ON DATABASE investing TO investing_user;
  GRANT CONNECT ON DATABASE portfolio_b TO portfolio_b_user;
  GRANT CONNECT ON DATABASE portfolio_c TO portfolio_c_user;
EOSQL
