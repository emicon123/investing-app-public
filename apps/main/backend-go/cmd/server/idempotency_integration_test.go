package main

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/backend-go/internal/deposit"
	"investing-app/common-go/httpx"
	"investing-app/common-go/idempotency"
	"investing-app/common-go/testsupport"
)

// TestIdempotencyKey_EndToEnd_RealPostgres drives POST /api/deposits through main's real
// authenticated route group (smartAuth, then idempotency.Middleware over the pgx store, then
// the generated routes) against a migrated Postgres (task 177 B8, ADR-111).
func TestIdempotencyKey_EndToEnd_RealPostgres(t *testing.T) {
	pg := testsupport.StartMigratedPostgres(t, "../../../db/migration")
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, pg.ConnString)
	if err != nil {
		t.Fatalf("connect to migrated postgres: %v", err)
	}
	defer pool.Close()

	depositHandler := deposit.NewHandler(deposit.NewService(sqlcgen.New(pool), nil))
	router := httpx.NewRouter()
	// Only the deposit handler is needed; every other route stays unmounted (nil handlers are
	// never called by this test).
	mountAuthenticatedRoutes(router, testAuthConfig(), idempotency.NewPgStore(pool),
		&apiServer{depositAPI: depositHandler})
	srv := httptest.NewServer(router)
	defer srv.Close()

	testsupport.AssertIdempotencyKeyEndToEnd(t, testsupport.IdempotentPOST{
		URL:       srv.URL + "/api/deposits",
		Bearer:    testBearer(t),
		Body:      `{"depositDate":"2026-11-02","amount":100,"currency":"PLN","exchange":"XTB","accountType":"REGULAR","portfolio":"MAIN"}`,
		OtherBody: `{"depositDate":"2026-11-02","amount":200,"currency":"PLN","exchange":"XTB","accountType":"REGULAR","portfolio":"MAIN"}`,
		CountRows: func() int {
			var n int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM deposits`).Scan(&n); err != nil {
				t.Fatal(err)
			}
			return n
		},
	})
}
