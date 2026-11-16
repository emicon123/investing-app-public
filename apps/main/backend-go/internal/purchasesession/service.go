package purchasesession

import (
	"context"
	"sync"

	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/backend-go/internal/deposit"
	"investing-app/backend-go/internal/rebalancingcalc"
)

// Queries is the narrow slice of *sqlcgen.Queries this package calls, declared as an interface
// so service-layer unit tests can fake the DB without a real Postgres connection. Embeds
// rebalancingcalc.Queries (Calculate/LoadMainTargetOverrides during createSession) and
// deposit.CapUsageQueries (the account-cap-usage cross-package read the diagram doc 150 §3
// calls out) — Go's structural typing means a single *sqlcgen.Queries value satisfies every
// embedded interface at once.
type Queries interface {
	rebalancingcalc.Queries
	deposit.CapUsageQueries

	ExistsPlannedSession(ctx context.Context) (bool, error)
	GetPlannedSession(ctx context.Context) (sqlcgen.PurchaseSession, error)
	GetSessionByID(ctx context.Context, id int64) (sqlcgen.PurchaseSession, error)
	GetSessionForUpdate(ctx context.Context, id int64) (sqlcgen.PurchaseSession, error)
	InsertPurchaseSession(ctx context.Context, arg sqlcgen.InsertPurchaseSessionParams) (sqlcgen.PurchaseSession, error)
	UpdateSessionStatusDone(ctx context.Context, arg sqlcgen.UpdateSessionStatusDoneParams) (sqlcgen.PurchaseSession, error)
	DeleteSessionByID(ctx context.Context, id int64) error
	ListSessionsByStatusPaged(ctx context.Context, arg sqlcgen.ListSessionsByStatusPagedParams) ([]sqlcgen.PurchaseSession, error)
	CountSessionsByStatus(ctx context.Context, status sqlcgen.SessionStatusEnum) (int64, error)
	// Task 173: buildDetail is a history read — it sees soft-deleted assets too.
	ListAllPositionsIncludingDeleted(ctx context.Context) ([]sqlcgen.Position, error)
	ListAllAssetsIncludingDeleted(ctx context.Context) ([]sqlcgen.Asset, error)

	InsertPurchaseSessionItem(ctx context.Context, arg sqlcgen.InsertPurchaseSessionItemParams) (sqlcgen.PurchaseSessionItem, error)
	ListSessionItemsBySessionID(ctx context.Context, sessionID int64) ([]sqlcgen.PurchaseSessionItem, error)
	ListSessionItemTotals(ctx context.Context, sessionIds []int64) ([]sqlcgen.ListSessionItemTotalsRow, error)
	ListSessionItemsOrderedForCompletion(ctx context.Context, sessionID int64) ([]sqlcgen.PurchaseSessionItem, error)
	UpdateSessionItemActuals(ctx context.Context, arg sqlcgen.UpdateSessionItemActualsParams) (sqlcgen.PurchaseSessionItem, error)
	DeleteSessionItemsBySessionID(ctx context.Context, sessionID int64) error

	// Write-ownership cluster (architect-output §2/§4) — the same canonical queries
	// internal/position and internal/rebalancing declare in their own narrow interfaces.
	UpdatePositionDelta(ctx context.Context, arg sqlcgen.UpdatePositionDeltaParams) (sqlcgen.Position, error)
	// Task 161a-2 asset-level cost bookkeeping: sibling rows' cost writes and the tx-scoped
	// price_cache read (DB only) the costbasis kernel values the asset with.
	UpdatePositionCost(ctx context.Context, arg sqlcgen.UpdatePositionCostParams) error
	GetAllPriceCache(ctx context.Context) ([]sqlcgen.GetAllPriceCacheRow, error)
	InsertAssetPurchase(ctx context.Context, arg sqlcgen.InsertAssetPurchaseParams) (sqlcgen.AssetPurchase, error)
	InsertDeposit(ctx context.Context, arg sqlcgen.InsertDepositParams) (sqlcgen.Deposit, error)
	// InsertBinanceTransferSession is THIS package's own single writer of SESSION-sourced
	// binance_transfers rows — never InsertBinanceTransferManual (guard_test.go enforces this
	// statically).
	InsertBinanceTransferSession(ctx context.Context, arg sqlcgen.InsertBinanceTransferSessionParams) (sqlcgen.BinanceTransfer, error)
}

// TransactFunc runs fn inside a single DB transaction. This package uses TWO separate instances
// (architect-output §4): one for lifecycle.go's session/item CRUD (cheap, session-table-only),
// one for execution.go's CompleteSession (the money-critical, FOR-UPDATE-gated one). They are
// never nested or shared — createSession and completeSession are always two separate HTTP
// requests.
type TransactFunc func(ctx context.Context, fn func(Queries) error) error

// PriceSource is the read-only cross-package dependency into internal/marketdata, reused by
// both createSession's rebalancingcalc.Calculate call and planning.go's ComputePlan (which needs
// the same getAllPricesPln-equivalent map).
type PriceSource = rebalancingcalc.PriceSource

// Service is the purchase-session top-level orchestrator: handler -> Service -> sqlc Queries.
// Composes lifecycle.go (session/item CRUD), planning.go (pure ComputePlan), and execution.go
// (completeSession's write cluster) — see this package's own doc comment for the file split.
//
// sessionCache is the direct port of PurchaseSessionService.newDepositPlnByBucketCache (ADR-065
// §5): createSession's own newDepositPlnByBucket figure, bridged to completeSession across two
// separate HTTP requests. A JVM/process restart between create and complete loses the entry —
// completeSession treats a cache miss as "no direct figure available" and falls back to the
// pre-ADR-065 group-sum-then-subtract reconstruction (see execution.go).
type Service struct {
	queries     Queries
	lifecycleTx TransactFunc
	executionTx TransactFunc
	prices      PriceSource

	cacheMu      sync.Mutex
	sessionCache map[int64]map[groupKey]decimal.Decimal
}

// NewService wires queries + the two transaction runners + price source (explicit constructor
// wiring, ADR-094 §2 — no DI framework).
func NewService(queries Queries, lifecycleTx, executionTx TransactFunc, prices PriceSource) *Service {
	if lifecycleTx == nil {
		lifecycleTx = func(ctx context.Context, fn func(Queries) error) error { return fn(queries) }
	}
	if executionTx == nil {
		executionTx = func(ctx context.Context, fn func(Queries) error) error { return fn(queries) }
	}
	return &Service{
		queries: queries, lifecycleTx: lifecycleTx, executionTx: executionTx, prices: prices,
		sessionCache: map[int64]map[groupKey]decimal.Decimal{},
	}
}

func (s *Service) cachePut(sessionID int64, v map[groupKey]decimal.Decimal) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	s.sessionCache[sessionID] = v
}

// cacheTake removes and returns the cached entry (single-use, mirrors ConcurrentHashMap.remove).
// Only call this once the DB transaction that consumes the value has actually committed — a
// rollback cannot undo an in-memory map delete, so evicting mid-transaction would strand a
// legitimate retry on the less-authoritative fallback reconstruction.
func (s *Service) cacheTake(sessionID int64) (map[groupKey]decimal.Decimal, bool) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	v, ok := s.sessionCache[sessionID]
	delete(s.sessionCache, sessionID)
	return v, ok
}

// cachePeek is the non-destructive read used from inside an open transaction (see cacheTake's
// doc comment) — the entry is only actually evicted via cacheTake/cacheEvict after that
// transaction commits.
func (s *Service) cachePeek(sessionID int64) (map[groupKey]decimal.Decimal, bool) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	v, ok := s.sessionCache[sessionID]
	return v, ok
}

func (s *Service) cacheEvict(sessionID int64) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	delete(s.sessionCache, sessionID)
}
