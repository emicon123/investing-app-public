package main

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"investing-app/common-go/auth"
	"investing-app/common-go/httpx"
	"investing-app/common-go/idempotency"
	scdeposit "investing-app/common-go/siblingcore/deposit"
	"investing-app/common-go/testsupport"
	"portfolio-b/backend-go/internal/book"
	"portfolio-b/backend-go/internal/deposit"
	"portfolio-b/backend-go/internal/store"
)

// TestIdempotencyKey_EndToEnd_RealPostgres drives POST /api/deposits through portfolioB's real
// authenticated route group (RequireAuth, then idempotency.Middleware over the pgx store, then
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

	authCfg := auth.Config{
		AllowedEmails: []string{"owner@example.com"},
		JWTSecret:     "test-secret-with-enough-length-32b",
		JWTExpiryDays: 1,
		JWTIssuer:     jwtIssuer,
	}
	bearer, _, err := auth.IssueBackendJWT(authCfg, authCfg.AllowedEmails[0])
	if err != nil {
		t.Fatal(err)
	}

	q := store.New(pool)
	server := &apiServer{depositAPI: deposit.NewHandler(scdeposit.NewService(book.Config, q, q, nil))}
	router := httpx.NewRouter()
	mountAuthenticatedRoutes(router, authCfg, idempotency.NewPgStore(pool), server)
	srv := httptest.NewServer(router)
	defer srv.Close()

	testsupport.AssertIdempotencyKeyEndToEnd(t, testsupport.IdempotentPOST{
		URL:       srv.URL + "/api/deposits",
		Bearer:    bearer,
		Body:      `{"depositDate":"2026-11-02","amount":100}`,
		OtherBody: `{"depositDate":"2026-11-02","amount":200}`,
		CountRows: func() int {
			var n int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM deposits`).Scan(&n); err != nil {
				t.Fatal(err)
			}
			return n
		},
	})
}
