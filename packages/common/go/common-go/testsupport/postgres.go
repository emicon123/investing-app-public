// Package testsupport gives every backend-go module the same "never mock the database" fixture: a
// real postgres:17.11-alpine container, migrated by the same flyway/flyway CLI image used in prod
// (ADR-094 §2 / doc 13 §2 — never golang-migrate/goose, never a hand-rolled migration), so
// integration tests assert against literally the production schema. This is the one fixture every
// *_test.go file should reuse rather than each re-deriving its own Postgres+Flyway wiring.
package testsupport

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	// DefaultPostgresImage matches the version pinned across this migration (ADR-094 §2):
	// postgres:17.11-alpine.
	DefaultPostgresImage = "postgres:17.11-alpine"
	// DefaultFlywayImage is the same flyway/flyway CLI image tag already pulled for this
	// migration's Docker Compose wiring (DevOps-owned; kept here in sync, not duplicated
	// authority — see the doc comment on Options.FlywayImage).
	DefaultFlywayImage = "flyway/flyway:13.10.3"

	testDBName     = "testdb"
	testDBUser     = "testuser"
	testDBPassword = "testpass"
	pgNetworkAlias = "pg"
)

// MigratedPostgres is a running Postgres container whose schema has already been migrated,
// ready for a pgx pool (or any postgres:// client) to connect to.
type MigratedPostgres struct {
	Container testcontainers.Container
	// ConnString is a host-reachable postgres://user:pass@host:port/db?sslmode=disable URL.
	ConnString string
}

// Options overrides the default image tags — mainly useful if a future phase needs to pin a
// different Flyway CLI version than the one this package defaults to.
type Options struct {
	PostgresImage string
	FlywayImage   string
}

// StartMigratedPostgres starts a Postgres container, applies every *.sql file in migrationDir
// against it via a throwaway Flyway CLI container on a private Docker network, and returns a
// host-reachable connection string. The Postgres container, the Flyway container, and the network
// are all torn down via t.Cleanup automatically. Skips the test (not a hard failure) in `go test
// -short` mode, for environments without Docker.
func StartMigratedPostgres(t *testing.T, migrationDir string, opts ...Options) *MigratedPostgres {
	t.Helper()
	if testing.Short() {
		t.Skip("testsupport: skipping testcontainers-go integration test in -short mode")
	}

	cfg := Options{PostgresImage: DefaultPostgresImage, FlywayImage: DefaultFlywayImage}
	if len(opts) > 0 {
		if opts[0].PostgresImage != "" {
			cfg.PostgresImage = opts[0].PostgresImage
		}
		if opts[0].FlywayImage != "" {
			cfg.FlywayImage = opts[0].FlywayImage
		}
	}

	ctx := context.Background()

	nw, err := network.New(ctx)
	if err != nil {
		t.Fatalf("testsupport: create network: %v", err)
	}
	testcontainers.CleanupNetwork(t, nw)

	pgContainer := startPostgres(ctx, t, cfg.PostgresImage, nw.Name)
	runFlywayMigrations(ctx, t, cfg.FlywayImage, nw.Name, migrationDir)

	host, err := pgContainer.Host(ctx)
	if err != nil {
		t.Fatalf("testsupport: postgres host: %v", err)
	}
	mappedPort, err := pgContainer.MappedPort(ctx, "5432/tcp")
	if err != nil {
		t.Fatalf("testsupport: postgres mapped port: %v", err)
	}

	connString := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
		testDBUser, testDBPassword, host, mappedPort.Port(), testDBName)

	return &MigratedPostgres{Container: pgContainer, ConnString: connString}
}

func startPostgres(ctx context.Context, t *testing.T, image, networkName string) testcontainers.Container {
	t.Helper()

	req := testcontainers.ContainerRequest{
		Image:          image,
		ExposedPorts:   []string{"5432/tcp"},
		Networks:       []string{networkName},
		NetworkAliases: map[string][]string{networkName: {pgNetworkAlias}},
		Env: map[string]string{
			"POSTGRES_DB":       testDBName,
			"POSTGRES_USER":     testDBUser,
			"POSTGRES_PASSWORD": testDBPassword,
		},
		WaitingFor: wait.ForLog("database system is ready to accept connections").
			WithOccurrence(2).
			WithStartupTimeout(60 * time.Second),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	testcontainers.CleanupContainer(t, container)
	if err != nil {
		t.Fatalf("testsupport: start postgres: %v", err)
	}
	return container
}

// runFlywayMigrations runs `flyway migrate` against the already-running Postgres container,
// using its network alias (not a host-mapped port) since both containers share the private
// network created for this test. Fails the test with the Flyway container's logs attached if
// the migration run exits non-zero.
func runFlywayMigrations(ctx context.Context, t *testing.T, flywayImage, networkName, migrationDir string) {
	t.Helper()

	files, err := collectSQLFiles(migrationDir)
	if err != nil {
		t.Fatalf("testsupport: collect migration files from %s: %v", migrationDir, err)
	}

	req := testcontainers.ContainerRequest{
		Image:    flywayImage,
		Networks: []string{networkName},
		Files:    files,
		Cmd: []string{
			"-url=jdbc:postgresql://" + pgNetworkAlias + ":5432/" + testDBName,
			"-user=" + testDBUser,
			"-password=" + testDBPassword,
			"-locations=filesystem:/flyway/sql",
			"-connectRetries=30",
			"migrate",
		},
		WaitingFor: wait.ForExit().WithExitTimeout(90 * time.Second),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	testcontainers.CleanupContainer(t, container)
	if err != nil {
		t.Fatalf("testsupport: start flyway: %v", err)
	}

	state, err := container.State(ctx)
	if err != nil {
		t.Fatalf("testsupport: inspect flyway container: %v", err)
	}
	if state.ExitCode != 0 {
		logs, _ := container.Logs(ctx)
		var logBytes []byte
		if logs != nil {
			logBytes, _ = io.ReadAll(logs)
			_ = logs.Close()
		}
		t.Fatalf("testsupport: flyway migrate exited %d against %s:\n%s", state.ExitCode, migrationDir, logBytes)
	}
}

// collectSQLFiles builds one ContainerFile per top-level file in migrationDir. Enumerating files
// explicitly (rather than relying on directory-copy support in ContainerFile) keeps this correct
// regardless of the testcontainers-go version's exact directory-copy behavior.
func collectSQLFiles(migrationDir string) ([]testcontainers.ContainerFile, error) {
	entries, err := os.ReadDir(migrationDir)
	if err != nil {
		return nil, err
	}

	var files []testcontainers.ContainerFile
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		files = append(files, testcontainers.ContainerFile{
			HostFilePath:      filepath.Join(migrationDir, e.Name()),
			ContainerFilePath: "/flyway/sql/" + e.Name(),
			FileMode:          0o644,
		})
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no migration files found in %s", migrationDir)
	}
	return files, nil
}
