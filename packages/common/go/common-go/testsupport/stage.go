package testsupport

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// StageMigrationsUpTo copies every Flyway migration of srcDir whose version is <= maxVersion into
// a fresh temp directory and returns it. Pointing StartMigratedPostgres at that directory yields a
// database at exactly the schema BEFORE a later migration, so a test can seed a pre-state and then
// run the migration under test by hand (the task-156 backfill tests' pattern, shared so the data
// migration tests of every app stage the same way). Non-migration files are skipped; a file whose
// name is not V<int>__<desc>.sql fails the test rather than being silently dropped.
func StageMigrationsUpTo(t *testing.T, srcDir string, maxVersion int) string {
	t.Helper()
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		t.Fatalf("testsupport: read migration dir %s: %v", srcDir, err)
	}
	dst := t.TempDir()
	staged := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, "V") || !strings.HasSuffix(name, ".sql") {
			continue
		}
		sep := strings.Index(name, "__")
		if sep < 2 {
			t.Fatalf("testsupport: %s is not V<version>__<description>.sql", name)
		}
		version, err := strconv.Atoi(name[1:sep])
		if err != nil {
			t.Fatalf("testsupport: cannot parse the version of %s: %v", name, err)
		}
		if version > maxVersion {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(srcDir, name))
		if err != nil {
			t.Fatalf("testsupport: read %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(dst, name), raw, 0o644); err != nil {
			t.Fatalf("testsupport: stage %s: %v", name, err)
		}
		staged++
	}
	if staged == 0 {
		t.Fatalf("testsupport: no migration with version <= %d in %s", maxVersion, srcDir)
	}
	return dst
}
