package purchasesession

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoManualSourcedWriteInPackage is the mirror of
// backend-go/internal/binancetransfers/service_test.go's TestNoCompletionSourcedWriteInPackage
// (architect-output §2's single-writer guard pattern): this package must never write the
// manual-entry origin value to binance_transfers — only internal/binancetransfers may do that,
// via InsertBinanceTransferManual. This package's own InsertBinanceTransferSession hardcodes the
// session origin value in its own SQL (db/queries/purchasesession.sql), never touching or
// reusing InsertBinanceTransferManual. Statically fails if the completion-path's sibling literal
// appears anywhere in this package's .go files (implementation, comments, or tests — hence both
// the needle AND this doc comment deliberately avoid spelling the literal out with its quotes,
// so this very test does not trip itself).
func TestNoManualSourcedWriteInPackage(t *testing.T) {
	needle := "'" + "MAN" + "UAL" + "'"
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		content, err := os.ReadFile(filepath.Join(".", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(content), needle) {
			t.Fatalf("%s contains the manual-entry origin literal — internal/purchasesession must never write MANUAL-sourced binance_transfers rows (that's internal/binancetransfers' exclusive write path)", e.Name())
		}
	}
}

// TestInsertBinanceTransferSessionIsTheOnlyWriteQuery is a companion static check: confirms
// this package's own SQL query file hardcodes 'SESSION' (never a caller-supplied source value)
// for the one binance_transfers INSERT this package owns.
func TestInsertBinanceTransferSessionIsTheOnlyWriteQuery(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "..", "db", "queries", "purchasesession.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "'SESSION'") {
		t.Fatal("expected db/queries/purchasesession.sql to hardcode source='SESSION' for InsertBinanceTransferSession")
	}
	if strings.Contains(string(content), "'"+"MAN"+"UAL"+"'") {
		t.Fatal("db/queries/purchasesession.sql must never hardcode the MANUAL origin literal")
	}
}
