package config

import (
	"os"
	"path/filepath"
	"testing"
)

type testConfig struct {
	Port int    `env:"TEST_PORT" envDefault:"8080"`
	Name string `env:"TEST_NAME"`
}

func TestParse_UsesDefaultsAndEnv(t *testing.T) {
	t.Setenv("TEST_NAME", "backend-go")

	cfg, err := Parse[testConfig]()
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Port != 8080 {
		t.Errorf("expected default port 8080, got %d", cfg.Port)
	}
	if cfg.Name != "backend-go" {
		t.Errorf("expected name backend-go, got %q", cfg.Name)
	}
}

func TestProfile_DefaultsToProd(t *testing.T) {
	t.Setenv("SPRING_PROFILES_ACTIVE", "")
	if got := Profile(); got != "prod" {
		t.Errorf("expected default profile prod, got %q", got)
	}
	if IsDev() {
		t.Error("expected IsDev() to be false when profile defaults to prod")
	}
}

func TestProfile_ReadsSpringProfilesActive(t *testing.T) {
	t.Setenv("SPRING_PROFILES_ACTIVE", "dev")
	if got := Profile(); got != "dev" {
		t.Errorf("expected profile dev, got %q", got)
	}
	if !IsDev() {
		t.Error("expected IsDev() to be true when SPRING_PROFILES_ACTIVE=dev")
	}
}

func TestLoadDotEnv_MissingFileIsNotAnError(t *testing.T) {
	if err := LoadDotEnv(filepath.Join(t.TempDir(), "does-not-exist.env")); err != nil {
		t.Errorf("expected no error for a missing .env file, got %v", err)
	}
}

func TestPostgresDSN_BuildsFromJDBCStyleEnvVars(t *testing.T) {
	t.Setenv("DB_URL", "jdbc:postgresql://db-shared:5432/investing")
	t.Setenv("DB_USERNAME", "investing_user")
	t.Setenv("DB_PASSWORD", "s3cret")

	dsn, err := PostgresDSN()
	if err != nil {
		t.Fatalf("PostgresDSN: %v", err)
	}
	want := "postgres://investing_user:s3cret@db-shared:5432/investing?sslmode=disable"
	if dsn != want {
		t.Errorf("expected %q, got %q", want, dsn)
	}
}

func TestPostgresDSN_MissingDBURL(t *testing.T) {
	t.Setenv("DB_URL", "")
	t.Setenv("DB_USERNAME", "u")
	t.Setenv("DB_PASSWORD", "p")
	if _, err := PostgresDSN(); err == nil {
		t.Fatal("expected an error when DB_URL is missing")
	}
}

func TestPostgresDSN_RejectsNonJDBCURL(t *testing.T) {
	t.Setenv("DB_URL", "postgres://already-a-pgx-dsn/investing")
	t.Setenv("DB_USERNAME", "u")
	t.Setenv("DB_PASSWORD", "p")
	if _, err := PostgresDSN(); err == nil {
		t.Fatal("expected an error for a non-jdbc:postgresql:// DB_URL")
	}
}

func TestLoadDotEnv_LoadsExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("TEST_DOTENV_VAR=loaded\n"), 0o600); err != nil {
		t.Fatalf("write fixture .env: %v", err)
	}

	if err := LoadDotEnv(path); err != nil {
		t.Fatalf("LoadDotEnv: %v", err)
	}
	if got := os.Getenv("TEST_DOTENV_VAR"); got != "loaded" {
		t.Errorf("expected TEST_DOTENV_VAR=loaded, got %q", got)
	}
}
