package purchasesession

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/common-go/testsupport"
)

// queryCounter is a pgx QueryTracer that counts every Query/QueryRow/Exec the pool runs.
type queryCounter struct{ n atomic.Int64 }

func (c *queryCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	c.n.Add(1)
	return ctx
}

func (*queryCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

// TestListSessions_SummariesAndQueryCount_RealPostgres pins ListSessions' observable result (task
// 185 B1 / F3.5) and its query budget: one page read, one count and one batched item-totals read,
// whatever the page size.
func TestListSessions_SummariesAndQueryCount_RealPostgres(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping testcontainers-go integration test in -short mode")
	}

	pg := testsupport.StartMigratedPostgres(t, "../../../db/migration")
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	counter := &queryCounter{}
	cfg, err := pgxpool.ParseConfig(pg.ConnString)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.Tracer = counter
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("connect to migrated postgres: %v", err)
	}
	defer pool.Close()

	if _, err := pool.Exec(ctx, `TRUNCATE purchase_sessions, purchase_session_items CASCADE`); err != nil {
		t.Fatal(err)
	}

	insertSession := func(status, plannedAt string, doneAt *string, retBudget, mainBudget, included string) int64 {
		t.Helper()
		var id int64
		if err := pool.QueryRow(ctx, `INSERT INTO purchase_sessions
			(status, planned_at, done_at, retirement_budget_pln, main_budget_pln, portfolios_included)
			VALUES ($1::session_status_enum, $2::timestamptz, $3::timestamptz, $4::numeric, $5::numeric, $6::portfolios_included_enum)
			RETURNING id`, status, plannedAt, doneAt, retBudget, mainBudget, included).Scan(&id); err != nil {
			t.Fatalf("insert session: %v", err)
		}
		return id
	}
	insertItem := func(sessionID int64, ticker string, actualValuePln *string, migrationSell bool) {
		t.Helper()
		if _, err := pool.Exec(ctx, `INSERT INTO purchase_session_items
			(session_id, portfolio, ticker, account_type, planned_units, planned_value_pln, actual_value_pln, is_migration_sell)
			VALUES ($1, 'MAIN', $2::text, 'REGULAR', 1, 100, $3::numeric, $4)`,
			sessionID, ticker, actualValuePln, migrationSell); err != nil {
			t.Fatalf("insert item %s: %v", ticker, err)
		}
	}
	str := func(s string) *string { return &s }

	// Oldest first; ListSessions orders by planned_at DESC.
	s0 := insertSession("DONE", "2026-01-01T10:00:00Z", str("2026-01-02T10:00:00Z"), "0", "1000.0000", "MAIN")
	s2 := insertSession("DONE", "2026-02-01T10:00:00Z", str("2026-02-02T10:00:00Z"), "500.5000", "0", "RETIREMENT")
	insertItem(s2, "AAA", str("100.5000"), false)
	insertItem(s2, "BBB", str("200.2500"), false)
	s3 := insertSession("DONE", "2026-03-01T10:00:00Z", str("2026-03-02T10:00:00Z"), "1500.0000", "2500.0000", "BOTH")
	insertItem(s3, "AAA", str("1000.0000"), false)
	insertItem(s3, "BBB", nil, false) // actual_value_pln NULL: counted as an item, not in the total
	insertItem(s3, "CCC", str("234.5678"), false)
	// Actuals that net to zero: the summary carries no total (sum ≠ 0 rule).
	sZero := insertSession("DONE", "2026-04-01T10:00:00Z", str("2026-04-02T10:00:00Z"), "0", "0", "BOTH")
	insertItem(sZero, "DDD", str("-500.0000"), true)
	insertItem(sZero, "EEE", str("500.0000"), false)
	// A PLANNED session is never listed.
	planned := insertSession("PLANNED", "2026-05-01T10:00:00Z", nil, "0", "0", "BOTH")
	insertItem(planned, "FFF", str("999.0000"), false)

	type want struct {
		id          int64
		plannedAt   string
		doneAt      string
		retBudget   string
		mainBudget  string
		included    string
		itemCount   int
		totalActual string // "" = nil
	}
	all := []want{
		{sZero, "2026-04-01T10:00:00Z", "2026-04-02T10:00:00Z", "0", "0", "BOTH", 2, ""},
		{s3, "2026-03-01T10:00:00Z", "2026-03-02T10:00:00Z", "1500", "2500", "BOTH", 3, "1234.5678"},
		{s2, "2026-02-01T10:00:00Z", "2026-02-02T10:00:00Z", "500.5", "0", "RETIREMENT", 2, "300.75"},
		{s0, "2026-01-01T10:00:00Z", "2026-01-02T10:00:00Z", "0", "1000", "MAIN", 0, ""},
	}

	queries := sqlcgen.New(pool)
	svc := NewService(queries, newTransactFunc(pool, queries), newTransactFunc(pool, queries), realPrices{})

	cases := []struct {
		name       string
		page, size int
		want       []want
	}{
		{"one page holds every DONE session", 0, 10, all},
		{"first page of two", 0, 2, all[:2]},
		{"second page of two", 1, 2, all[2:]},
		{"page past the end", 5, 2, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := counter.n.Load()
			got, total, err := svc.ListSessions(ctx, tc.page, tc.size)
			queriesRun := counter.n.Load() - before
			if err != nil {
				t.Fatalf("ListSessions: %v", err)
			}
			if total != int64(len(all)) {
				t.Fatalf("total = %d, want %d", total, len(all))
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %d summaries, want %d: %+v", len(got), len(tc.want), got)
			}
			for i, w := range tc.want {
				g := got[i]
				if g.ID != w.id || g.Status != StatusDONE || g.PortfoliosIncluded != w.included || g.ItemCount != w.itemCount {
					t.Errorf("[%d] got id=%d status=%s included=%s items=%d, want id=%d DONE %s items=%d",
						i, g.ID, g.Status, g.PortfoliosIncluded, g.ItemCount, w.id, w.included, w.itemCount)
				}
				if g.PlannedAt.UTC().Format(time.RFC3339) != w.plannedAt {
					t.Errorf("[%d] plannedAt = %s, want %s", i, g.PlannedAt.UTC().Format(time.RFC3339), w.plannedAt)
				}
				if g.DoneAt == nil || g.DoneAt.UTC().Format(time.RFC3339) != w.doneAt {
					t.Errorf("[%d] doneAt = %v, want %s", i, g.DoneAt, w.doneAt)
				}
				if g.RetirementBudgetPln.String() != w.retBudget || g.MainBudgetPln.String() != w.mainBudget {
					t.Errorf("[%d] budgets = %s/%s, want %s/%s", i, g.RetirementBudgetPln, g.MainBudgetPln, w.retBudget, w.mainBudget)
				}
				switch {
				case w.totalActual == "" && g.TotalActualPln != nil:
					t.Errorf("[%d] totalActualPln = %s, want nil", i, g.TotalActualPln)
				case w.totalActual != "" && (g.TotalActualPln == nil || g.TotalActualPln.String() != w.totalActual):
					t.Errorf("[%d] totalActualPln = %v, want %s", i, g.TotalActualPln, w.totalActual)
				}
			}
			if queriesRun != 3 {
				t.Errorf("ListSessions ran %d queries, want 3 (page, count, item totals) for any page size", queriesRun)
			}
		})
	}
}
