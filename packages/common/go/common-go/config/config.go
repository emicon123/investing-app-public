// Package config is typed environment-variable configuration: a thin wrapper over os.Getenv (via
// github.com/caarlos0/env/v11's struct-tag parsing) plus optional local .env-file loading
// (github.com/joho/godotenv) for a developer running `go run ./cmd/server` outside Docker.
//
// Deliberate deviation from doc 13 §2 / ADR-094 §2's literal example ("APP_ENV selects
// .env.dev/.env.prod"): this repo has no .env.dev/.env.prod files and no APP_ENV variable anywhere
// (confirmed by inspection of infra/compose.yml and infra/.env.example). The actual convention —
// which the stack table's own overriding principle says to keep ("same variable names DevOps
// already manages") — is: one .env file per app (gitignored, managed by DevOps), consumed as
// literal environment variables via docker-compose's `environment:` block, with
// SPRING_PROFILES_ACTIVE (default "prod", a name inherited from the Java backends) selecting the
// dev/prod split. Config.Profile follows that real convention instead of the ADR's
// illustrative-but-inaccurate example.
package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

// LoadDotEnv loads the given .env file into the process environment if it exists. It is a no-op
// (not an error) when the file is missing, matching how Docker Compose already injects real
// environment variables directly with no .env file present inside the container — this is purely
// a convenience for `go run ./cmd/server` on a developer machine.
func LoadDotEnv(path string) error {
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	return godotenv.Load(path)
}

// Parse populates a new T from environment variables using `env:"NAME"` / `envDefault:"..."` struct
// tags.
func Parse[T any]() (T, error) {
	var cfg T
	if err := env.Parse(&cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// Profile returns the active profile ("dev" or "prod"), read from SPRING_PROFILES_ACTIVE (the
// variable name is inherited from the Java backends and kept so the .env files did not change),
// defaulting to "prod" to match infra/compose.yml's `SPRING_PROFILES_ACTIVE:
// ${SPRING_PROFILES_ACTIVE:-prod}`.
func Profile() string {
	if p := os.Getenv("SPRING_PROFILES_ACTIVE"); p != "" {
		return p
	}
	return "prod"
}

// IsDev reports whether the active profile is "dev".
func IsDev() bool { return Profile() == "dev" }

// PostgresDSN builds a pgx-compatible "postgres://user:pass@host:port/db" connection string from
// the env vars DevOps manages (DB_URL, DB_USERNAME, DB_PASSWORD — infra/compose.yml,
// infra/.env.example). DB_URL is JDBC-shaped ("jdbc:postgresql://db-shared:5432/investing") since
// that is the literal value in infra/.env — this function strips the "jdbc:" prefix and injects the
// credentials pgx needs (a JDBC URL has no userinfo component; username/password are separate
// properties). sslmode=disable matches common-go/testsupport's fixture — the DB is reached over the
// private investing-shared Docker network, never TLS between containers.
func PostgresDSN() (string, error) {
	rawURL := os.Getenv("DB_URL")
	if rawURL == "" {
		return "", fmt.Errorf("config: DB_URL is required")
	}
	username := os.Getenv("DB_USERNAME")
	password := os.Getenv("DB_PASSWORD")
	if username == "" || password == "" {
		return "", fmt.Errorf("config: DB_USERNAME and DB_PASSWORD are required")
	}

	hostAndPath := strings.TrimPrefix(rawURL, "jdbc:postgresql://")
	if hostAndPath == rawURL {
		return "", fmt.Errorf("config: DB_URL %q is not a jdbc:postgresql:// URL", rawURL)
	}

	sep := "?"
	if strings.Contains(hostAndPath, "?") {
		sep = "&"
	}
	return fmt.Sprintf("postgres://%s:%s@%s%ssslmode=disable", username, password, hostAndPath, sep), nil
}
