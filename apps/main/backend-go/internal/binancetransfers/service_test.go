package binancetransfers

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/common-go/httpx"
	"investing-app/common-go/pgnum"
)

// fakeQueries is an in-memory stand-in for *sqlcgen.Queries — same convention as prior
// phases (business logic table-tested here; real Postgres covered by
// integration_test.go). It emulates the SQL-level forcing of InsertBinanceTransferManual
// (manual origin, NULL session link — the literals live in db/queries, not in Go),
// so Create tests assert the contract the real INSERT guarantees.
type fakeQueries struct {
	rows   []sqlcgen.BinanceTransfer
	nextID int64
}

func newFakeQueries() *fakeQueries {
	return &fakeQueries{nextID: 1}
}

func (f *fakeQueries) ListBinanceTransfers(_ context.Context) ([]sqlcgen.BinanceTransfer, error) {
	out := append([]sqlcgen.BinanceTransfer{}, f.rows...)
	// ORDER BY transfer_date DESC, id DESC — same as the real query.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

func (f *fakeQueries) ListBinanceTransfersByYear(_ context.Context, year int32) ([]sqlcgen.BinanceTransfer, error) {
	var out []sqlcgen.BinanceTransfer
	for _, r := range f.rows {
		if int32(r.TransferDate.Time.Year()) == year {
			out = append(out, r)
		}
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

func (f *fakeQueries) SumAmountPlnByYear(_ context.Context) ([]sqlcgen.SumAmountPlnByYearRow, error) {
	totals := map[int32]decimal.Decimal{}
	for _, r := range f.rows {
		amount, err := pgnum.ToDecimal(r.AmountPln)
		if err != nil {
			return nil, err
		}
		y := int32(r.TransferDate.Time.Year())
		totals[y] = totals[y].Add(amount)
	}
	var out []sqlcgen.SumAmountPlnByYearRow
	for y, total := range totals {
		out = append(out, sqlcgen.SumAmountPlnByYearRow{Year: y, TotalPln: pgnum.FromDecimal(total)})
	}
	return out, nil
}

func (f *fakeQueries) InsertBinanceTransferManual(_ context.Context, arg sqlcgen.InsertBinanceTransferManualParams) (sqlcgen.BinanceTransfer, error) {
	// The params struct carries no origin/session fields (compile-time fact); the
	// forcing itself lives in the SQL literals, emulated here by construction.
	row := sqlcgen.BinanceTransfer{
		ID:             f.nextID,
		TransferDate:   arg.TransferDate,
		AmountPln:      arg.AmountPln,
		AmountNative:   arg.AmountNative,
		NativeCurrency: arg.NativeCurrency,
		Comment:        arg.Comment,
		Source:         "MANUAL",
		SessionID:      nil,
		CreatedAt:      pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
	}
	f.nextID++
	f.rows = append(f.rows, row)
	return row, nil
}

func (f *fakeQueries) ExistsBinanceTransferById(_ context.Context, id int64) (bool, error) {
	for _, r := range f.rows {
		if r.ID == id {
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeQueries) DeleteBinanceTransfer(_ context.Context, id int64) error {
	for i, r := range f.rows {
		if r.ID == id {
			f.rows = append(f.rows[:i], f.rows[i+1:]...)
			return nil
		}
	}
	return nil
}

// TestNoCompletionSourcedWriteInPackage is the Phase 4 partial-port acceptance
// gate: no code path in this package may write a purchase-completion-linked row —
// only reads and the manual-entry path (a distinct, user-initiated write) exist.
// The completion path's origin is a SQL string literal; statically fail if that
// literal appears in ANY .go file of this package (implementation, comments, or
// tests — hence the needle below is assembled by concatenation so this very test
// does not trip itself).
func TestNoCompletionSourcedWriteInPackage(t *testing.T) {
	needle := "'" + "SESS" + "ION" + "'"
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
			t.Fatalf("%s contains the completion-path origin literal — Go must never write completion-linked rows (partial port, Phase 7 owns that path)", e.Name())
		}
	}
}

func TestCreate_ForcesManualOriginAndNullSession(t *testing.T) {
	svc := NewService(newFakeQueries())
	amount := decimal.NewFromInt(1000)
	dto, err := svc.Create(context.Background(), CreateInput{
		TransferDate: time.Date(2026, 6, 16, 0, 0, 0, 0, time.UTC),
		AmountPln:    amount,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if dto.Source != "MANUAL" {
		t.Fatalf("expected manual origin, got %q", dto.Source)
	}
	if dto.SessionID != nil {
		t.Fatalf("expected NULL session link, got %d", *dto.SessionID)
	}
}

func TestCreate_Validation(t *testing.T) {
	svc := NewService(newFakeQueries())
	good := CreateInput{
		TransferDate: time.Date(2026, 6, 16, 0, 0, 0, 0, time.UTC),
		AmountPln:    decimal.NewFromInt(100),
	}
	cases := []struct {
		name   string
		mutate func(*CreateInput)
		field  string
	}{
		{"zero date", func(in *CreateInput) { in.TransferDate = time.Time{} }, "transferDate"},
		{"zero amount", func(in *CreateInput) { in.AmountPln = decimal.Zero }, "amountPln"},
		{"negative amount", func(in *CreateInput) { in.AmountPln = decimal.NewFromInt(-5) }, "amountPln"},
		{"native without currency", func(in *CreateInput) {
			v := decimal.NewFromInt(10)
			in.AmountNative = &v
		}, ""},
		{"currency without native", func(in *CreateInput) {
			s := "EUR"
			in.NativeCurrency = &s
		}, ""},
		{"bad currency", func(in *CreateInput) {
			v := decimal.NewFromInt(10)
			in.AmountNative = &v
			s := "XYZ"
			in.NativeCurrency = &s
		}, "nativeCurrency"},
		{"zero native amount", func(in *CreateInput) {
			v := decimal.Zero
			in.AmountNative = &v
			s := "EUR"
			in.NativeCurrency = &s
		}, "amountNative"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := good
			tc.mutate(&in)
			_, err := svc.Create(context.Background(), in)
			var appErr *httpx.AppError
			ok := errors.As(err, &appErr)
			if !ok || appErr.Status != 422 {
				t.Fatalf("expected 422 AppError, got %v", err)
			}
			if tc.field != "" && appErr.Field != tc.field {
				t.Fatalf("expected field %q, got %q", tc.field, appErr.Field)
			}
		})
	}
}

func TestCreate_NativePairAccepted(t *testing.T) {
	svc := NewService(newFakeQueries())
	native := decimal.NewFromInt(1000)
	cur := "EUR"
	comment := "z pasywnego franki"
	dto, err := svc.Create(context.Background(), CreateInput{
		TransferDate:   time.Date(2026, 6, 16, 0, 0, 0, 0, time.UTC),
		AmountPln:      decimal.RequireFromString("4275.50"),
		AmountNative:   &native,
		NativeCurrency: &cur,
		Comment:        &comment,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if dto.AmountNative == nil || dto.AmountNative.Cmp(native) != 0 {
		t.Fatalf("expected native amount to survive, got %v", dto.AmountNative)
	}
	if dto.NativeCurrency == nil || *dto.NativeCurrency != "EUR" {
		t.Fatalf("expected EUR, got %v", dto.NativeCurrency)
	}
}

func TestList_YearFilterLeavesSumsWholeTable(t *testing.T) {
	fq := newFakeQueries()
	svc := NewService(fq)
	mustCreate := func(year int, amount string) {
		t.Helper()
		if _, err := svc.Create(context.Background(), CreateInput{
			TransferDate: time.Date(year, 3, 16, 0, 0, 0, 0, time.UTC),
			AmountPln:    decimal.RequireFromString(amount),
		}); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}
	mustCreate(2026, "6354.81")
	mustCreate(2026, "2100")
	mustCreate(2025, "34349.80")

	y2026 := 2026
	res, err := svc.List(context.Background(), &y2026)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(res.Data) != 2 {
		t.Fatalf("expected 2 rows for 2026, got %d", len(res.Data))
	}
	if len(res.YearlySums) != 2 {
		t.Fatalf("expected whole-table sums for 2 years, got %d", len(res.YearlySums))
	}
	byYear := map[int]decimal.Decimal{}
	for _, s := range res.YearlySums {
		byYear[s.Year] = s.TotalPln
	}
	if byYear[2026].StringFixed(2) != "8454.81" || byYear[2025].StringFixed(2) != "34349.80" {
		t.Fatalf("unexpected sums: %v", byYear)
	}
}

func TestDelete_UnknownIdIs404(t *testing.T) {
	svc := NewService(newFakeQueries())
	err := svc.Delete(context.Background(), 999)
	var appErr *httpx.AppError
	ok := errors.As(err, &appErr)
	if !ok || appErr.Status != 404 {
		t.Fatalf("expected 404 AppError, got %v", err)
	}
}

func TestDelete_RemovesRow(t *testing.T) {
	fq := newFakeQueries()
	svc := NewService(fq)
	dto, err := svc.Create(context.Background(), CreateInput{
		TransferDate: time.Date(2026, 6, 16, 0, 0, 0, 0, time.UTC),
		AmountPln:    decimal.NewFromInt(100),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := svc.Delete(context.Background(), dto.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	res, err := svc.List(context.Background(), nil)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(res.Data) != 0 {
		t.Fatalf("expected empty ledger after delete, got %d rows", len(res.Data))
	}
}
