// Command server is portfolio-b's backend-go composition root (doc 13 §4): a pgxpool ->
// sqlc Queries -> Adapter -> Service -> chi-mounted handler chain per domain (thin wiring over the
// shared common-go kernels: inflationsync, historiaquery, accountcash, settingscore,
// marketdatacore, siblingcore, and the per-app backfill over the shared priceprovider OHLC seam),
// wired explicitly here per ADR-094 §2's "no wire/fx/dig" DI pick.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	accountcashkernel "investing-app/common-go/accountcash"
	"investing-app/common-go/auth"
	"investing-app/common-go/authexchange"
	"investing-app/common-go/config"
	"investing-app/common-go/historiaquery"
	"investing-app/common-go/historiascheduler"
	"investing-app/common-go/httpx"
	"investing-app/common-go/idempotency"
	"investing-app/common-go/inflationsync"
	"investing-app/common-go/lifecycle"
	"investing-app/common-go/marketdatacore"
	"investing-app/common-go/pgtx"
	"investing-app/common-go/priceprovider"
	"investing-app/common-go/settingscore"
	scdeposit "investing-app/common-go/siblingcore/deposit"
	scposition "investing-app/common-go/siblingcore/position"
	screbalancing "investing-app/common-go/siblingcore/rebalancing"
	"investing-app/common-go/siblingcore/refdata"
	scsnapshot "investing-app/common-go/siblingcore/snapshot"
	sctransfer "investing-app/common-go/siblingcore/transfer"
	"portfolio-b/backend-go/db/sqlcgen"
	"portfolio-b/backend-go/internal/accountcash"
	"portfolio-b/backend-go/internal/admin"
	"portfolio-b/backend-go/internal/book"
	"portfolio-b/backend-go/internal/deposit"
	"portfolio-b/backend-go/internal/historia"
	"portfolio-b/backend-go/internal/position"
	"portfolio-b/backend-go/internal/positiontransfer"
	"portfolio-b/backend-go/internal/rebalancing"
	"portfolio-b/backend-go/internal/settings"
	"portfolio-b/backend-go/internal/store"
	"portfolio-b/backend-go/openapi"
)

// providerCacheTTL is the TTL (60s) of the CachingDecorator wrapping both Yahoo and
// Binance-with-fallback — see common-go/marketdatacore's own doc comment for why this is fully
// shareable business logic, just wired per app with its own DB connection.
const providerCacheTTL = 60 * time.Second

// defaultPort is documented for DevOps's Nginx/Compose wiring — chosen to avoid every port bound by
// the other services on this Pi (see each app's infra/compose.yml).
const defaultPort = "8092"

// jwtIssuer is this app's literal backend-JWT issuer (never overridden via the environment, so it
// is a literal here — see common-go/auth.Config).
const jwtIssuer = "portfolio-b"

// healthcheckTimeout bounds the `backend-go healthcheck` self-probe (inside the compose
// healthcheck's own 5 s timeout; the handler's DB ping has 1 s of it).
const healthcheckTimeout = 3 * time.Second

// main is the binary's only exit point (D174 §4.1): run owns every defer, so a startup error
// or a failed healthcheck exits 1 only after run's deferred cleanup has finished.
func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if err := run(); err != nil {
		slog.Error("backend-go: exiting", "error", err)
		os.Exit(1)
	}
}

// run wires the app and serves it until SIGINT/SIGTERM, then shuts down in order via
// lifecycle.Run (stop workers, drain HTTP, await workers — ADR-110). With the single argument
// `healthcheck` it instead probes the running server's /api/health-go and returns.
func run() error {
	// Best-effort: Docker Compose already injects real environment variables directly with no
	// .env file present inside the container; this only helps a developer running
	// `go run ./cmd/server` from the backend-go/ directory on the host.
	_ = config.LoadDotEnv("../infra/.env")

	port := os.Getenv("PORT")
	if port == "" {
		port = defaultPort
	}
	if len(os.Args) == 2 && os.Args[1] == "healthcheck" {
		return healthcheck(port)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := config.ConnectDB(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()

	authCfg, err := auth.LoadConfig(jwtIssuer)
	if err != nil {
		return fmt.Errorf("auth config: %w", err)
	}

	inflationService := newInflationService(pool)
	inflationHandler := inflationsync.NewHandler(inflationService)

	queries := sqlcgen.New(pool)
	// q is this app's one siblingcore adapter (ADR-113); it is also the cache-only PriceSource.
	q := store.New(pool)
	historiaService := historiaquery.NewService(historia.NewAdapter(pool), historia.Config())
	historiaHandler := historia.NewHandler(historiaService)
	historiaSnapshotter := scsnapshot.NewSnapshotter(book.Config, q, nil)

	cashService := accountcash.NewService(accountcash.NewStore(queries))
	var _ *accountcashkernel.Service = cashService //nolint:staticcheck // QF1011: the explicit type IS the assertion; behavior lives in the shared kernel
	cashHandler := accountcash.NewHandler(cashService)

	backfillService := admin.NewService(pool,
		// Task 138 (ADR-085): reconstruct Yahoo-missing ETNVIR* history from Virtune
		// coin-entitlement ratios + Binance spot + USDPLN=X; every other ticker passes
		// straight through to Yahoo, and any reconstruction failure degrades to Yahoo.
		priceprovider.NewReconstructingOHLCProvider(priceprovider.NewYahooOHLCProvider()))
	// ADR-097: the historia-snapshot backfill (POST /admin/historia/backfill-snapshot) is a
	// second, unrelated ops entry point on this same admin.Handler — reuses the same
	// historiaSnapshotter the scheduler above wires, per Decision §2 ("no new package needed").
	backfillHandler := admin.NewHandler(backfillService, historiaSnapshotter)

	// Settings + market data (prices/fx-rates) — the common-go/settingscore and
	// common-go/marketdatacore kernels.
	settingsService := settings.NewService(refdata.New(pool))
	settingsHandler := settings.NewHandler(settingsService)

	marketDataService := newMarketDataService(pool)
	marketDataHandler := marketdatacore.NewHandler(marketDataService)

	// Core money cluster (Positions, Rebalancing/Confirm/History, Deposits, PositionTransfer; task
	// 151). q (above) is this app's one siblingcore adapter. money is the ADR-111 serialised runner
	// every money-writing siblingcore service takes.
	money := pgtx.SerializedRunner(pool, pgtx.MoneyWriteLockKey, store.Bind)

	positionHandler := position.NewHandler(scposition.NewService(book.Config, q, money, q))

	depositHandler := deposit.NewHandler(scdeposit.NewService(book.Config, q, q, nil))

	rebalancingHandler := rebalancing.NewHandler(screbalancing.NewService(book.Config, q, money, q, rebalancing.NewFunding(), nil))
	positionTransferHandler := positiontransfer.NewHandler(sctransfer.NewService(book.Config, money))

	// /auth/token (doc 13 §6.4, task 152). Reuses the same authCfg RequireAuth validates bearer
	// tokens against (common-go/auth.Service).
	authExchangeHandler := authexchange.NewHandler(auth.NewService(authCfg))

	router := httpx.NewRouter()
	router.Get("/api/health-go", httpx.HealthHandler("portfolio-b-backend-go", pool.Ping))
	mountAuthenticatedRoutes(router, authCfg, idempotency.NewPgStore(pool), &apiServer{
		inflationAPI: inflationHandler, historiaAPI: historiaHandler, cashAPI: cashHandler, adminAPI: backfillHandler,
		positionAPI: positionHandler, depositAPI: depositHandler, rebalancingAPI: rebalancingHandler,
		positionTransferAPI: positionTransferHandler, settingsAPI: settingsHandler,
		marketDataAPI: marketDataHandler, authExchangeAPI: authExchangeHandler,
	})

	// Every background job below is owned by workers: cancelled and awaited on shutdown
	// (lifecycle.Run, ADR-110).
	workers := lifecycle.NewGroup(ctx)

	// GUS inflation sync triggers (monthly, 15th 06:00 Europe/Warsaw, plus a startup stale check) —
	// the same idempotent, fail-soft Sync the endpoint serves.
	workers.Go("inflation-sync", func(ctx context.Context) { inflationsync.StartScheduler(ctx, inflationService) })

	// Price cache refresh (15 min, 5s initial delay) —
	// common-go/marketdatacore.StartPriceRefreshScheduler, identical in all three apps.
	workers.Go("price-refresh", func(ctx context.Context) { marketdatacore.StartPriceRefreshScheduler(ctx, marketDataService) })

	// Historia backfill runner: once, asynchronously — readiness never waits on Yahoo history
	// fetches. Fail-soft by construction (see admin.StartBackfillRunner).
	workers.Go("historia-backfill", func(ctx context.Context) { admin.StartBackfillRunner(ctx, backfillService) })

	// Historia nightly snapshots (23:00 Europe/Warsaw), gated by HISTORIA_SCHEDULER_ENABLED
	// (default false; true in compose, ADR-097). With the flag off nothing writes snapshots.
	if historiascheduler.SchedulerEnabled() {
		// SnapshotAll snapshots every portfolio; one failure never skips the others (task 180 D4).
		workers.Go("historia-snapshot", func(ctx context.Context) { historiascheduler.RunDaily(ctx, historiaSnapshotter.SnapshotAll) })
	} else {
		slog.Info("historia: scheduler disabled (HISTORIA_SCHEDULER_ENABLED != true); no historia snapshots will be written")
	}

	srv := httpx.NewServer(":"+port, router)
	slog.Info("backend-go starting", "profile", config.Profile())
	return lifecycle.Run(ctx, srv, workers, lifecycle.Options{})
}

// healthcheck is the `backend-go healthcheck` subcommand the compose healthcheck executes
// (exec form — the distroless image has no shell or wget): it probes this container's own
// /api/health-go, which answers 200 only while the database ping succeeds.
func healthcheck(port string) error {
	ctx, cancel := context.WithTimeout(context.Background(), healthcheckTimeout)
	defer cancel()
	return lifecycle.Probe(ctx, "http://127.0.0.1:"+port+"/api/health-go")
}

// newInflationService wires the inflation domain: the shared reference-table adapter
// (common-go/siblingcore/refdata), a single-transaction runner over the same pgxpool (the sync loop
// commits atomically), and the real GUS SDP client (SDP JSON API with CSV fallback) from the shared
// kernel.
func newInflationService(pool *pgxpool.Pool) *inflationsync.Service {
	transact := pgtx.Runner(pool, func(tx pgx.Tx) inflationsync.Queries { return refdata.New(tx) })
	return inflationsync.NewService(refdata.New(pool), transact, inflationsync.NewClient())
}

// newMarketDataService wires the market-data domain: the shared reference-table adapter
// (common-go/siblingcore/refdata) over this app's pool, plus a Yahoo provider (serving stock/ETF/FX
// prices) and a Binance-with-Yahoo-fallback provider (serving BINANCE-exchange positions), both
// wrapped in the same CachingDecorator TTL — fully shareable business logic
// (common-go/marketdatacore's own doc comment), wired here only with this app's own DB connection.
func newMarketDataService(pool *pgxpool.Pool) *marketdatacore.Service {
	yahoo := priceprovider.NewYahooProvider()
	binance := priceprovider.NewBinanceProvider()
	binanceWithFallback := priceprovider.NewFallbackProvider(binance, yahoo)

	cachedYahoo := priceprovider.NewCachingDecorator(yahoo, providerCacheTTL)
	cachedBinanceWithFallback := priceprovider.NewCachingDecorator(binanceWithFallback, providerCacheTTL)

	return marketdatacore.NewService(refdata.New(pool), cachedYahoo, cachedBinanceWithFallback)
}

// mountAuthenticatedRoutes mounts every authenticated route in one chi.Group: the bearer-JWT
// gate, then the ADR-111 Idempotency-Key middleware, then the generated ServerInterface routes and
// the hand-mounted ops route. /api/health-go (mounted by run) stays the one unauthenticated
// route.
func mountAuthenticatedRoutes(router chi.Router, authCfg auth.Config, idemStore idempotency.Store, server *apiServer) {
	router.Group(func(r chi.Router) {
		// RequireAuth's own isPublicAuthTokenRoute exception (common-go/auth/middleware.go)
		// exempts POST /auth/token from the bearer-JWT gate below — the generated ExchangeToken
		// route is still mounted in this same chi.Group via openapi.HandlerWithOptions, never a
		// second chain/mount point (task 152 FIX 1).
		r.Use(auth.RequireAuth(authCfg))
		// ADR-111: Idempotency-Key on money-moving POSTs. Only requests carrying the header are
		// affected (the web client's money dialogs); scripts that never send it pass through.
		r.Use(idempotency.Middleware(idemStore))

		openapi.HandlerWithOptions(server, openapi.ChiServerOptions{
			BaseURL:    "/api",
			BaseRouter: r,
			ErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, err error) {
				// oapi-codegen's own default here is a plain http.Error with no envelope at all —
				// route every request-binding failure through the same shared ErrorResponse
				// envelope every other error uses, so every backend answers identically on the wire
				// even for this failure mode.
				httpx.WriteError(w, httpx.NewValidationError("", err.Error()))
			},
		})

		// POST /admin/historia/backfill-snapshot: ADR-097's one-off recovery entry point for a
		// missed nightly historia snapshot (the 2026-09-18 scheduler-gap incident). Deliberately
		// NOT in docs/openapi.yaml (ADR-097 Decision §2: internal ops tooling only, never proxied
		// through Nginx, never linked from any UI) — mounted directly rather than through
		// openapi.HandlerWithOptions above.
		r.Post("/api/admin/historia/backfill-snapshot", server.BackfillSnapshot)
	})
}

// Every domain package names its handler type Handler, and an embedded field is named after its
// type, so each handler is embedded through one of these aliases (the alias is the field name).
type (
	inflationAPI        = inflationsync.Handler
	historiaAPI         = historia.Handler
	cashAPI             = accountcash.Handler
	adminAPI            = admin.Handler
	positionAPI         = position.Handler
	depositAPI          = deposit.Handler
	rebalancingAPI      = rebalancing.Handler
	positionTransferAPI = positiontransfer.Handler
	settingsAPI         = settingscore.Handler
	marketDataAPI       = marketdatacore.Handler
	authExchangeAPI     = authexchange.Handler
)

// fallback answers every operation no handler serves with a plain 501 (openapi.Unimplemented).
// It is a named wrapper so apiServer embeds it one level deeper than the handlers: Go resolves
// a method at the shallowest depth, so a handler's method always beats the 501 default.
type fallback struct{ openapi.Unimplemented }

// apiServer implements openapi.ServerInterface by embedding every domain handler — the explicit,
// no-framework wiring ADR-094 §2 calls for. The handlers' operation methods (named after the
// OpenAPI operationIds) are promoted, so no per-operation forwarding exists; two handlers
// exporting the same operation name would make the selector ambiguous and fail the interface
// check at compile time. A handler whose method signature differs from the generated one keeps
// an explicit forwarder below. It is also the single parameter bundling the handlers for
// mountAuthenticatedRoutes.
type apiServer struct {
	fallback
	*inflationAPI
	*historiaAPI
	*cashAPI
	*adminAPI
	*positionAPI
	*depositAPI
	*rebalancingAPI
	*positionTransferAPI
	*settingsAPI
	*marketDataAPI
	*authExchangeAPI
}

// Explicit forwarders: the operations whose generated signature carries a parameter the handler's
// method does not take, so plain promotion would not match the interface — the Idempotency-Key
// header (enforced by the ADR-111 middleware, not by the handler) and the prices tickers filter
// (handed to the handler as a bare *string).

func (s *apiServer) GetPrices(w http.ResponseWriter, r *http.Request, params openapi.GetPricesParams) {
	s.marketDataAPI.GetPrices(w, r, params.Tickers)
}

func (s *apiServer) CreateDeposit(w http.ResponseWriter, r *http.Request, _ openapi.CreateDepositParams) {
	s.depositAPI.CreateDeposit(w, r)
}

func (s *apiServer) Confirm(w http.ResponseWriter, r *http.Request, _ openapi.ConfirmParams) {
	s.rebalancingAPI.Confirm(w, r)
}

func (s *apiServer) Execute(w http.ResponseWriter, r *http.Request, _ openapi.ExecuteParams) {
	s.positionTransferAPI.Execute(w, r)
}
