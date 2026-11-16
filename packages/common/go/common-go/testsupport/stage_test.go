package testsupport

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestStageMigrationsUpTo_CopiesOnlyVersionsUpToMax(t *testing.T) {
	src := t.TempDir()
	for _, name := range []string{"V1__init.sql", "V2__a.sql", "V10__b.sql", "V75__c.sql", "V76__d.sql", "README.md"} {
		if err := os.WriteFile(filepath.Join(src, name), []byte("-- "+name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(src, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}

	dst := StageMigrationsUpTo(t, src, 75)

	entries, err := os.ReadDir(dst)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		got = append(got, e.Name())
	}
	sort.Strings(got)
	want := []string{"V10__b.sql", "V1__init.sql", "V2__a.sql", "V75__c.sql"}
	if len(got) != len(want) {
		t.Fatalf("staged %v, want %v (numeric, not lexicographic, version compare; README and dirs skipped)", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("staged %v, want %v", got, want)
		}
	}
	raw, err := os.ReadFile(filepath.Join(dst, "V75__c.sql"))
	if err != nil || string(raw) != "-- V75__c.sql" {
		t.Fatalf("staged file content = %q, %v", raw, err)
	}
}
