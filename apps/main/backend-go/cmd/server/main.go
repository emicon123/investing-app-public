// Command server is investing-app's backend-go composition root (doc 13 §4): a pgxpool -> sqlc
// Queries -> Service -> chi-mounted handler chain per domain, wired explicitly here per ADR-094
// §2's "no wire/fx/dig" DI pick.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/backend-go/internal/binancetransfers"
	"investing-app/backend-go/internal/bot/botutil"
	"investing-app/backend-go/internal/bot/calendar"
	"investing-app/backend-go/internal/bot/control"
	"investing-app/backend-go/internal/bot/plan"
	botsignal "investing-app/backend-go/internal/bot/signal"
	"investing-app/backend-go/internal/bot/stats"
	"investing-app/backend-go/internal/bot/trade"
	"investing-app/backend-go/internal/cashpositions"
	"investing-app/backend-go/internal/cryptotrades"
	"investing-app/backend-go/internal/deposit"
	"investing-app/backend-go/internal/historia"
	"investing-app/backend-go/internal/inflation"
	"investing-app/backend-go/internal/linearregression"
	"investing-app/backend-go/internal/marketdata"
	"investing-app/backend-go/internal/marketdata/macro"
	"investing-app/backend-go/internal/news"
	"investing-app/backend-go/internal/position"
	"investing-app/backend-go/internal/purchasesession"
	"investing-app/backend-go/internal/push"
	"investing-app/backend-go/internal/rebalancing"
	"investing-app/backend-go/internal/settings"
	"investing-app/backend-go/internal/taxreturns"
	"investing-app/backend-go/internal/tradechart"
	"investing-app/backend-go/openapi"
	"investing-app/common-go/auth"
	"investing-app/common-go/authexchange"
	"investing-app/common-go/config"
	"investing-app/common-go/historiascheduler"
	"investing-app/common-go/httpx"
	"investing-app/common-go/idempotency"
	"investing-app/common-go/inflationsync"
	"investing-app/common-go/lifecycle"
	"investing-app/common-go/marketdatacore"
	"investing-app/common-go/pgtx"
	"investing-app/common-go/priceprovider"
	"investing-app/common-go/settingscore"
)

// defaultPort is documented for DevOps's Nginx/Compose wiring — chosen to avoid every port bound by
// the other services on this Pi (see infra/compose.yml et al.; 8090 and 8123 are taken).
const defaultPort = "8091"

// jwtIssuer is investing-app's literal backend-JWT issuer (never read from the environment — see
// common-go/auth.Config).
const jwtIssuer = "investing-app"

// providerCacheTTL is the in-memory freshness window for common-go/priceprovider's
// CachingDecorator, wrapping both the Yahoo and Binance-with-fallback providers. Deliberately
// short: the original Java force-refresh had no caching layer between itself and the upstream
// client at all, so this is a disclosed, intentional addition — its purpose is to dedupe
// near-simultaneous calls for the same ticker (the 15-minute scheduled refresh overlapping with a
// manual "Refresh" click, or concurrent requests during an FX-rate cache miss) without materially
// changing what "force refresh" means to a user 60 seconds apart.
const providerCacheTTL = 60 * time.Second

// healthcheckTimeout bounds the `backend-go healthcheck` self-probe (inside the compose
// healthcheck's own 5 s timeout; the handler's DB ping has 1 s of it).
const healthcheckTimeout = 3 * time.Second

// botWorkerTimeout is how long shutdown waits for the workers: the bot's commit phase (45 s) plus
// its alert (10 s) must finish before exit (ADR-114). 15 s HTTP drain + 60 s stays under the
// compose stop_grace_period of 90 s.
const botWorkerTimeout = 60 * time.Second

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
	// .env file present inside the container (see infra/compose.yml); this only helps a
	// developer running `go run ./cmd/server` from the backend-go/ directory on the host.
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

	marketDataService := newMarketDataService(pool)
	marketDataHandler := marketdatacore.NewHandler(marketDataService)
	macroService := newMacroService(pool)
	macroHandler := macro.NewHandler(macroService)
	inflationService := inflation.NewService(sqlcgen.New(pool))
	inflationSync := newInflationSync(pool)
	inflationHandler := inflation.NewHandler(inflationService, inflationSync)
	taxReturnsService := newTaxReturnsService(pool)
	taxReturnsHandler := taxreturns.NewHandler(taxReturnsService)
	cashPositionsService := newCashPositionsService(pool)
	cashPositionsHandler := cashpositions.NewHandler(cashPositionsService)
	settingsService := newSettingsService(pool)
	settingsHandler := settings.NewHandler(settingsService)
	historiaService := newHistoriaService(pool, marketDataService)
	historiaHandler := historia.NewHandler(historiaService)
	cryptoTradesService := newCryptoTradesService(pool)
	cryptoTradesHandler := cryptotrades.NewHandler(cryptoTradesService)
	historyService := newHistoryService()
	linearRegressionService := linearregression.NewService(historyService)
	linearRegressionHandler := linearregression.NewHandler(linearRegressionService)
	tradeChartService := tradechart.NewService(sqlcgen.New(pool), historyService)
	tradeChartHandler := tradechart.NewHandler(tradeChartService)
	binanceTransfersService := newBinanceTransfersService(pool)
	binanceTransfersHandler := binancetransfers.NewHandler(binanceTransfersService)

	// /auth/token (doc 13 §6.4, task 152). Reuses the same authCfg every other route validates
	// bearer tokens against (common-go/auth.Service).
	authExchangeHandler := authexchange.NewHandler(auth.NewService(authCfg))

	// Core money cluster (positions, rebalancing-transfers, purchase-sessions, deposits), all
	// sharing one moneyClusterPriceAdapter over marketDataService (doc 13 §6.3's read-only
	// cross-package pattern, same convention as botPriceAdapter/historiaFxAdapter).
	moneyPrices := moneyClusterPriceAdapter{marketData: marketDataService}
	positionService := newPositionService(pool, moneyPrices)
	positionHandler := position.NewHandler(positionService)
	rebalancingService := newRebalancingService(pool)
	rebalancingHandler := rebalancing.NewHandler(rebalancingService)
	depositService := deposit.NewService(sqlcgen.New(pool), moneyPrices)
	depositHandler := deposit.NewHandler(depositService)
	purchaseSessionService := newPurchaseSessionService(pool, moneyPrices)
	purchaseSessionHandler := purchasesession.NewHandler(purchaseSessionService)

	pushService := newPushService(pool)
	pushHandler := push.NewHandler(pushService)
	newsService := newNewsService(pool, pushService)
	newsHandler := news.NewHandler(newsService)
	execCfg := trade.LoadExecutionConfig()
	botPrices := botPriceAdapter{marketData: marketDataService}
	controlService := control.NewService(sqlcgen.New(pool))
	controlHandler := control.NewHandler(controlService)
	calendarService, calendarOrchestrator := newCalendarService(pool, execCfg.CalendarRetentionDays)
	calendarHandler := calendar.NewHandler(calendarService)
	planService := newPlanService(pool)
	planHandler := plan.NewHandler(planService)
	signalService := botsignal.NewService(sqlcgen.New(pool))
	signalHandler := botsignal.NewHandler(signalService)
	tradeService := newTradeService(pool, controlService, botPrices, execCfg)
	tradeHandler := trade.NewHandler(tradeService)
	statsService := stats.NewService(sqlcgen.New(pool), botPrices)
	statsHandler := stats.NewHandler(statsService)
	okxClient := trade.NewOkxTradingClient(trade.OkxConfig{
		BaseURL: os.Getenv("OKX_BASE_URL"), APIKey: os.Getenv("OKX_API_KEY"),
		APISecret: os.Getenv("OKX_API_SECRET"), Passphrase: os.Getenv("OKX_API_PASSPHRASE"),
		Demo: execCfg.OkxDemoMode,
	})
	liveExecution := trade.NewLiveExecutionService(controlService, okxClient, tradeService, botPrices, execCfg, nil,
		trade.WithNotifier(pushService))
	botWatcher := trade.NewWatcher(sqlcgen.New(pool), signalService, tradeService, controlService, liveExecution, calendarOrchestrator, execCfg)

	router := httpx.NewRouter()
	router.Get("/api/health-go", httpx.HealthHandler("investing-backend-go", pool.Ping))
	mountAuthenticatedRoutes(router, authCfg, idempotency.NewPgStore(pool), &apiServer{
		marketDataAPI: marketDataHandler, macroAPI: macroHandler, inflationAPI: inflationHandler,
		taxReturnsAPI: taxReturnsHandler, cashPositionsAPI: cashPositionsHandler,
		settingsAPI: settingsHandler, historiaAPI: historiaHandler, cryptoTradesAPI: cryptoTradesHandler,
		linearRegressionAPI: linearRegressionHandler, tradeChartAPI: tradeChartHandler,
		binanceTransfersAPI: binanceTransfersHandler, newsAPI: newsHandler, calendarAPI: calendarHandler,
		controlAPI: controlHandler, planAPI: planHandler, signalAPI: signalHandler, tradeAPI: tradeHandler,
		statsAPI: statsHandler, positionAPI: positionHandler, rebalancingAPI: rebalancingHandler,
		purchaseSessionAPI: purchaseSessionHandler, depositAPI: depositHandler,
		authExchangeAPI: authExchangeHandler, pushAPI: pushHandler,
	})

	// Keep price_cache warm in the background: a startup warm-up plus the 15-minute refresh, in one
	// goroutine. Every background job below is owned by workers (stopped and awaited on shutdown,
	// ADR-110), the bot's two goroutines included (ADR-114 reverses ADR-110 Decision 6's exception: a
	// shutdown must wait for an execution between the fill and its stop-loss/record).
	workers := lifecycle.NewGroup(ctx)
	workers.Go("price-refresh", func(ctx context.Context) { marketdatacore.StartPriceRefreshScheduler(ctx, marketDataService) })

	// Macro refresh on its three triggers (Fri 18:00, 1st 17:30, 5th 18:30 Europe/Warsaw) — same
	// idempotent, fail-soft Refresh the endpoint serves.
	workers.Go("macro-refresh", func(ctx context.Context) { macro.StartScheduler(ctx, macroService) })

	// GUS inflation sync triggers (monthly, 15th 06:00 Europe/Warsaw, plus a startup stale check) —
	// same idempotent, fail-soft Sync the endpoint serves.
	workers.Go("inflation-sync", func(ctx context.Context) { inflationsync.StartScheduler(ctx, inflationSync) })

	// Historia nightly snapshots (23:00 Europe/Warsaw), gated by HISTORIA_SCHEDULER_ENABLED
	// (default false; true in compose, ADR-097). With the flag off nothing writes snapshots.
	if historiascheduler.SchedulerEnabled() {
		workers.Go("historia-snapshot", func(ctx context.Context) { historiascheduler.RunDaily(ctx, historiaService.SnapshotAll) })
	} else {
		slog.Info("historia: scheduler disabled (HISTORIA_SCHEDULER_ENABLED != true); no historia snapshots will be written")
	}

	// Bot background work (release watcher 1s tick + calendar 04:00 purge), gated by
	// BOT_SCHEDULER_ENABLED (default false; true in compose, ADR-097). With the flag off nothing
	// runs the watcher or the purge. Separately, BOT_LIVE_EXECUTION_ENABLED (also default false)
	// gates actual OKX order placement inside LiveExecutionService even when the watcher runs.
	if calendar.SchedulerEnabled() {
		workers.Go("bot-calendar-purge", func(ctx context.Context) { calendar.StartScheduler(ctx, calendarService) })
		workers.Go("bot-watcher", func(ctx context.Context) { trade.StartWatcher(ctx, botWatcher) })
	} else {
		slog.Info("bot: schedulers disabled (BOT_SCHEDULER_ENABLED != true); the release watcher and the calendar purge are not running, live execution stays gated by BOT_LIVE_EXECUTION_ENABLED")
	}

	srv := httpx.NewServer(":"+port, router)
	slog.Info("backend-go starting", "profile", config.Profile())
	return lifecycle.Run(ctx, srv, workers, lifecycle.Options{WorkerTimeout: botWorkerTimeout})
}

// healthcheck is the `backend-go healthcheck` subcommand the compose healthcheck executes
// (exec form — the distroless image has no shell or wget): it probes this container's own
// /api/health-go, which answers 200 only while the database ping succeeds.
func healthcheck(port string) error {
	ctx, cancel := context.WithTimeout(context.Background(), healthcheckTimeout)
	defer cancel()
	return lifecycle.Probe(ctx, "http://127.0.0.1:"+port+"/api/health-go")
}

// newMarketDataService wires common-go/priceprovider's real Yahoo/Binance implementations behind
// the CachingDecorator, mirroring PriceProviderRegistry.forExchange's routing (BINANCE exchange ->
// Binance-with-Yahoo-fallback; everything else -> Yahoo). This is the composition point every
// later phase needing prices (crypto trades, the bot subsystem, rebalancing) should replicate —
// construct once here, or thread the same *Service/providers through as those phases land.
func newMarketDataService(pool *pgxpool.Pool) *marketdatacore.Service {
	yahoo := priceprovider.NewYahooProvider()
	binance := priceprovider.NewBinanceProvider()
	binanceWithFallback := priceprovider.NewFallbackProvider(binance, yahoo)

	cachedYahoo := priceprovider.NewCachingDecorator(yahoo, providerCacheTTL)
	cachedBinanceWithFallback := priceprovider.NewCachingDecorator(binanceWithFallback, providerCacheTTL)

	queries := sqlcgen.New(pool)
	return marketdata.NewService(queries, cachedYahoo, cachedBinanceWithFallback)
}

// newMacroService wires the macro-state service: sqlc queries + a single-transaction runner over
// the same pgxpool, the real 8-client fetch fan-out (FRED key from FRED_API_KEY — blank in dev/test
// means the FRED client degrades fail-soft to empty), and the 12h-cached Yahoo benchmark overlay.
func newMacroService(pool *pgxpool.Pool) *macro.Service {
	queries := sqlcgen.New(pool)
	transact := pgtx.Runner(pool, func(tx pgx.Tx) macro.Queries { return queries.WithTx(tx) })
	fetcher := macro.NewProdFetcher(os.Getenv("FRED_API_KEY"))
	return macro.NewService(queries, transact, fetcher, macro.NewBenchmarkService())
}

// newInflationSync wires the shared GUS sync (common-go/inflationsync, task 178): main's sqlc
// queries behind inflation.Adapter + a single-transaction runner over the same pgxpool (the sync
// loop commits atomically) + the real GUS SDP client (SDP JSON API with CSV fallback).
func newInflationSync(pool *pgxpool.Pool) *inflationsync.Service {
	queries := sqlcgen.New(pool)
	transact := pgtx.Runner(pool, func(tx pgx.Tx) inflationsync.Queries { return inflation.NewAdapter(queries.WithTx(tx)) })
	return inflationsync.NewService(inflation.NewAdapter(queries), transact, inflationsync.NewClient())
}

// newTaxReturnsService wires the tax-returns service: sqlc queries only (single-row insert, no
// multi-statement transaction needed).
func newTaxReturnsService(pool *pgxpool.Pool) *taxreturns.Service {
	return taxreturns.NewService(sqlcgen.New(pool))
}

// newCashPositionsService wires the cash-positions service: sqlc queries + a single-transaction
// runner over the same pgxpool (the snapshot batch commits atomically).
func newCashPositionsService(pool *pgxpool.Pool) *cashpositions.Service {
	queries := sqlcgen.New(pool)
	transact := pgtx.Runner(pool, func(tx pgx.Tx) cashpositions.Queries { return queries.WithTx(tx) })
	return cashpositions.NewService(queries, transact)
}

// newSettingsService wires the settings service: sqlc queries only (single-row validated update,
// find-then-save).
func newSettingsService(pool *pgxpool.Pool) *settings.Service {
	return settings.NewService(sqlcgen.New(pool))
}

// newHistoriaService wires the historia service: sqlc queries + a single-transaction runner over
// the same pgxpool (one transaction per portfolio write) + the marketdata service behind historia's
// FxProvider interface (its cache-read + fetch-on-stale path — this package adds no new write path
// to price_cache/fx_rate_cache).
func newHistoriaService(pool *pgxpool.Pool, marketData *marketdatacore.Service) *historia.Service {
	queries := sqlcgen.New(pool)
	transact := pgtx.Runner(pool, func(tx pgx.Tx) historia.Queries { return queries.WithTx(tx) })
	return historia.NewService(queries, transact, historiaFxAdapter{marketData: marketData})
}

// newCryptoTradesService wires the crypto-trades service: sqlc queries only (single-row insert,
// paginated reads, year-filtered tax summary — no multi-statement transaction needed).
func newCryptoTradesService(pool *pgxpool.Pool) *cryptotrades.Service {
	return cryptotrades.NewService(sqlcgen.New(pool))
}

// newHistoryService wires the shared OHLC history facade both chart domains (linear-regression +
// trade-chart) read through: Yahoo chart client, Virtune ETP client, and Binance klines fallback.
func newHistoryService() *linearregression.HistoryService {
	return linearregression.NewHistoryService(
		linearregression.NewYahooChartClient(),
		priceprovider.NewVirtuneClient(),
		linearregression.NewBinanceKlinesClient(),
	)
}

// newBinanceTransfersService wires the binance-transfers service: sqlc queries only (single-row
// manual insert, reads, delete — no multi-statement transaction needed). The purchase-completion
// insert belongs to the purchasesession service; this service has no path producing it.
func newBinanceTransfersService(pool *pgxpool.Pool) *binancetransfers.Service {
	return binancetransfers.NewService(sqlcgen.New(pool))
}

// newPositionService wires the position service: sqlc queries + a serialised transaction runner
// (ADR-111: every write method reads and writes inside one transaction that first takes the
// money-write advisory lock) + the shared money-cluster price adapter.
func newPositionService(pool *pgxpool.Pool, prices position.PriceSource) *position.Service {
	queries := sqlcgen.New(pool)
	transact := pgtx.SerializedRunner(pool, pgtx.MoneyWriteLockKey,
		func(tx pgx.Tx) position.Queries { return queries.WithTx(tx) })
	return position.NewService(queries, transact, prices)
}

// newRebalancingService wires the rebalancing-transfer service: sqlc queries + a serialised
// transaction runner spanning the WHOLE ExecuteTransfer call (architect-output §4; ADR-111: the
// money-write advisory lock is its first statement).
func newRebalancingService(pool *pgxpool.Pool) *rebalancing.Service {
	queries := sqlcgen.New(pool)
	transact := pgtx.SerializedRunner(pool, pgtx.MoneyWriteLockKey,
		func(tx pgx.Tx) rebalancing.Queries { return queries.WithTx(tx) })
	return rebalancing.NewService(queries, transact)
}

// newPurchaseSessionService wires the purchase-session service: sqlc queries + TWO
// transaction-runner roles (architect-output §4) — lifecycleTx for session/item CRUD (cheap,
// session-table-only, plain Runner) and executionTx for CompleteSession (the money-critical,
// FOR-UPDATE-gated one, serialised with every other money write by the ADR-111 advisory lock). They
// are never nested — createSession and completeSession are always two separate HTTP requests.
func newPurchaseSessionService(pool *pgxpool.Pool, prices purchasesession.PriceSource) *purchasesession.Service {
	queries := sqlcgen.New(pool)
	bind := func(tx pgx.Tx) purchasesession.Queries { return queries.WithTx(tx) }
	lifecycleTx := pgtx.Runner(pool, bind)
	executionTx := pgtx.SerializedRunner(pool, pgtx.MoneyWriteLockKey, bind)
	return purchasesession.NewService(queries, lifecycleTx, executionTx, prices)
}

// newNewsService wires the news service: sqlc queries only (single-statement upserts, reads — no
// multi-statement transaction needed), plus the push fan-out behind news.Notifier for POST
// /news/notifications (ADR-108).
func newNewsService(pool *pgxpool.Pool, notifier news.Notifier) *news.Service {
	return news.NewService(sqlcgen.New(pool), newsLookbackDays(), notifier)
}

// newPushService wires Web Push (ADR-108, task 172): VAPID_* from the environment, the
// webpush-go adapter with a 5 s per-send timeout, and the browser push-service allowlist.
// It logs exactly one "web push enabled=<bool>" line (WARN when only some VAPID_* are set);
// no key material is ever logged.
func newPushService(pool *pgxpool.Pool) *push.Service {
	cfg := push.LoadConfig()
	switch {
	case cfg.Enabled():
		slog.Info("web push enabled=true")
	case cfg.Partial():
		slog.Warn("web push enabled=false: VAPID_PUBLIC_KEY, VAPID_PRIVATE_KEY and VAPID_SUBJECT (mailto: or https:) must all be set")
	default:
		slog.Info("web push enabled=false")
	}
	sender := push.NewWebPushSender(cfg, &http.Client{Timeout: 5 * time.Second})
	return push.NewService(sqlcgen.New(pool), sender, cfg, push.DefaultAllowedHosts)
}

// newCalendarService wires the calendar service + sync orchestrator: the ForexFactory source
// (constructor-injected feed URLs, fail-soft) sequenced before the TradingEconomics/BLS no-op stubs
// (documented gap — those sources are not ported).
func newCalendarService(pool *pgxpool.Pool, retentionDays int) (*calendar.Service, *calendar.Orchestrator) {
	queries := sqlcgen.New(pool)
	orchestrator := calendar.NewOrchestrator(calendar.NewForexSource(queries, "", ""))
	return calendar.NewService(queries, orchestrator, retentionDays), orchestrator
}

// newPlanService wires the bot plan service: sqlc queries + a single-transaction runner over the
// same pgxpool (plan upsert + scenario replace commit atomically).
func newPlanService(pool *pgxpool.Pool) *plan.Service {
	queries := sqlcgen.New(pool)
	transact := pgtx.Runner(pool, func(tx pgx.Tx) plan.Queries { return queries.WithTx(tx) })
	return plan.NewService(queries, transact)
}

// newTradeService wires the bot trade service: sqlc queries + a single-transaction runner (scalp
// claim/finish status update + SYSTEM action row commit atomically, multi-tick inserts commit
// atomically) + the kill-switch gate + read-only prices.
func newTradeService(pool *pgxpool.Pool, gate *control.Service, prices botutil.BTCPriceSource, execCfg trade.ExecutionConfig) *trade.Service {
	queries := sqlcgen.New(pool)
	transact := pgtx.Runner(pool, func(tx pgx.Tx) trade.Queries { return queries.WithTx(tx) })
	return trade.NewService(queries, transact, gate, prices, execCfg)
}

// newsLookbackDays reads the ingest/assets history window
// (app.news.history-lookback-days, default 7 — NEWS_HISTORY_LOOKBACK_DAYS).
func newsLookbackDays() int {
	if raw := strings.TrimSpace(os.Getenv("NEWS_HISTORY_LOOKBACK_DAYS")); raw != "" {
		var n int
		if _, err := fmt.Sscanf(raw, "%d", &n); err == nil && n > 0 {
			return n
		}
	}
	return 7
}

// mountAuthenticatedRoutes mounts every route requiring authentication (doc 13 §6.4 — bearer tokens
// validated against JWT_SECRET/ALLOWED_EMAIL_*) in one chi.Group, so /api/health-go (mounted above,
// outside this group) stays the one deliberately unauthenticated exception.
//
// Authentication is one path-based dispatcher (see botauth.go; it replaced the Java
// SecurityConfig's three filter chains): machine-only news/bot writes take X-Service-Token,
// dual-audience bot reads take either credential, everything else takes the backend-JWT bearer
// token exactly as before. Behind the auth gate, the ADR-111 middleware deduplicates any write that
// carries an Idempotency-Key header.
func mountAuthenticatedRoutes(router chi.Router, authCfg auth.Config, idemStore idempotency.Store, server *apiServer) {
	router.Group(func(r chi.Router) {
		// smartAuth's own isPublicAuthToken case (botauth.go) exempts POST /auth/token from
		// every credential check below — the generated ExchangeToken route is still mounted in
		// this same chi.Group via openapi.HandlerWithOptions, never a second chain/mount point
		// (task 152 FIX 1).
		r.Use(smartAuth(authCfg, auth.LoadServiceTokenConfig()))
		// ADR-111: Idempotency-Key on money-moving POSTs. Only requests carrying the header are
		// affected (the web client's money dialogs); bot, service-token and script requests never
		// send it and pass through untouched.
		r.Use(idempotency.Middleware(idemStore))

		openapi.HandlerWithOptions(server, openapi.ChiServerOptions{
			BaseURL:    "/api",
			BaseRouter: r,
			ErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, err error) {
				// oapi-codegen's own default here is a plain http.Error with no envelope at all —
				// route every request-binding failure (e.g. a malformed ?forceRefresh=) through the
				// same shared ErrorResponse envelope every other error uses, so every backend
				// answers identically on the wire even for this failure mode.
				httpx.WriteError(w, httpx.NewValidationError("", err.Error()))
			},
		})

		// POST /admin/historia/backfill-snapshot: ADR-097's internal ops entry point, deliberately
		// not in docs/openapi.yaml (listed in 03-api-contract.md §Internal and operational routes).
		r.Post("/api/admin/historia/backfill-snapshot", server.BackfillSnapshot)
	})
}

// Every domain package names its handler type Handler, and an embedded field is named after its
// type, so each handler is embedded through one of these aliases (the alias is the field name).
type (
	marketDataAPI       = marketdatacore.Handler
	macroAPI            = macro.Handler
	inflationAPI        = inflation.Handler
	taxReturnsAPI       = taxreturns.Handler
	cashPositionsAPI    = cashpositions.Handler
	settingsAPI         = settingscore.Handler
	historiaAPI         = historia.Handler
	cryptoTradesAPI     = cryptotrades.Handler
	linearRegressionAPI = linearregression.Handler
	tradeChartAPI       = tradechart.Handler
	binanceTransfersAPI = binancetransfers.Handler
	newsAPI             = news.Handler
	calendarAPI         = calendar.Handler
	controlAPI          = control.Handler
	planAPI             = plan.Handler
	signalAPI           = botsignal.Handler
	tradeAPI            = trade.Handler
	statsAPI            = stats.Handler
	positionAPI         = position.Handler
	rebalancingAPI      = rebalancing.Handler
	purchaseSessionAPI  = purchasesession.Handler
	depositAPI          = deposit.Handler
	authExchangeAPI     = authexchange.Handler
	pushAPI             = push.Handler
)

// fallback answers every operation no handler serves with a plain 501 (openapi.Unimplemented). It
// is a named wrapper so apiServer embeds it one level deeper than the handlers: Go resolves a
// method at the shallowest depth, so a handler's method always beats the 501 default.
type fallback struct{ openapi.Unimplemented }

// apiServer implements openapi.ServerInterface by embedding every domain handler — the explicit,
// no-framework wiring ADR-094 §2 calls for. The handlers' operation methods (named after the
// OpenAPI operationIds) are promoted, so no per-operation forwarding exists; two handlers exporting
// the same operation name would make the selector ambiguous and fail the interface check at compile
// time. A handler whose method signature differs from the generated one keeps an explicit forwarder
// below. It is also the single parameter bundling the handlers for mountAuthenticatedRoutes.
type apiServer struct {
	fallback
	*marketDataAPI
	*macroAPI
	*inflationAPI
	*taxReturnsAPI
	*cashPositionsAPI
	*settingsAPI
	*historiaAPI
	*cryptoTradesAPI
	*linearRegressionAPI
	*tradeChartAPI
	*binanceTransfersAPI
	*newsAPI
	*calendarAPI
	*controlAPI
	*planAPI
	*signalAPI
	*tradeAPI
	*statsAPI
	*positionAPI
	*rebalancingAPI
	*purchaseSessionAPI
	*depositAPI
	*authExchangeAPI
	*pushAPI
}

// Explicit forwarders: the operations whose generated signature carries a parameter the handler's
// method does not take, so plain promotion would not match the interface — the Idempotency-Key
// header (enforced by the ADR-111 middleware, not by the handler), the unused fx-rates query
// params, and the prices tickers filter (handed to the handler as a bare *string).

func (s *apiServer) GetPrices(w http.ResponseWriter, r *http.Request, params openapi.GetPricesParams) {
	s.marketDataAPI.GetPrices(w, r, params.Tickers)
}

func (s *apiServer) GetFxRates(w http.ResponseWriter, r *http.Request, _ openapi.GetFxRatesParams) {
	s.marketDataAPI.GetFxRates(w, r)
}

func (s *apiServer) CreateCryptoTrade(w http.ResponseWriter, r *http.Request, _ openapi.CreateCryptoTradeParams) {
	s.cryptoTradesAPI.CreateCryptoTrade(w, r)
}

func (s *apiServer) CreateBinanceTransfer(w http.ResponseWriter, r *http.Request, _ openapi.CreateBinanceTransferParams) {
	s.binanceTransfersAPI.CreateBinanceTransfer(w, r)
}

func (s *apiServer) CreateRebalancingTransfer(w http.ResponseWriter, r *http.Request, _ openapi.CreateRebalancingTransferParams) {
	s.rebalancingAPI.CreateRebalancingTransfer(w, r)
}

func (s *apiServer) CreateDeposit(w http.ResponseWriter, r *http.Request, _ openapi.CreateDepositParams) {
	s.depositAPI.CreateDeposit(w, r)
}
