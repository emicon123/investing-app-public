# Database migrations (main app)

Flyway migrations, applied by the one-shot `flyway` service in `infra/compose.yml`.

- `V1__baseline_schema.sql` – full schema, squashed from the private development history.
- `V2__default_reference_data.sql` – default settings and the bot control singleton.

New migrations continue from `V3__…`.
