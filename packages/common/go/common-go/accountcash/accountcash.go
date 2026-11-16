// Package accountcash is the shared account-cash kernel for the sibling apps (portfolioB + portfolioC): the
// manual-update and post-purchase-deduct business rules, parameterized only by what genuinely
// differs per app — the row shape behind the Store interface.
//
//   - portfolioB: singleton row id=1 with 4 columns (ikeTata, ikeMama, ikzeTata,
//     ikzeMama); the store ignores the key.
//   - portfolioC: PK=portfolio with 1 column (balancePln); the store keys by portfolio.
//
// Rules (ported from the Java AbstractAccountCashService, ADR-094; identical in both apps after
// package-name normalization):
//
//   - updateManual overwrites every balance, stamps updatedAt=now and
//     updateReason=MANUAL.
//   - deductPurchases subtracts per balance with a floor at zero (never negative),
//     stamps updatedAt=now and updateReason=POST_PURCHASE.
//   - nil deltas count as zero.
//
// Wire notes (binding, verified against both OpenAPI bundles):
//
//   - Cash answers WITHOUT the {data,meta} envelope — the raw AccountCashDto. The
//     per-app handler uses httpx.WriteJSON directly.
//   - Reading the current balances for rebalancing (portfolioB's funding waterfall) is
//     not part of this kernel and must not be re-introduced here.
package accountcash

import (
	"context"
	"time"

	"github.com/shopspring/decimal"

	"investing-app/common-go/httpx"
)

// Update reasons, verbatim from the original services.
const (
	ReasonManual       = "MANUAL"
	ReasonPostPurchase = "POST_PURCHASE"
)

// Snapshot is one account_cash_snapshot row in plain Go types: the ordered balance
// keys (portfolioB's 4 IKE/IKZE columns, portfolioC's single balancePln) plus the audit
// columns. Free of sqlc row types so both siblings' adapters satisfy Store
// through this one shape.
type Snapshot struct {
	Balances     map[string]decimal.Decimal
	UpdatedAt    time.Time
	UpdateReason string
}

// Store is the one per-app seam: load/save of the snapshot row. PortfolioB ignores key (singleton
// id=1); portfolioC keys by portfolio (VARIABLE/FIXED). A missing row maps to ErrNotFound so the service
// answers 404.
type Store interface {
	Load(ctx context.Context, key string) (Snapshot, error)
	Save(ctx context.Context, key string, snap Snapshot) error
}

// Service is the account-cash service shared by portfolioB and portfolioC: thin wiring hands it a Store over
// its own *sqlcgen.Queries plus its ordered balance Keys, and gets the shared behavior back.
type Service struct {
	store Store
	keys  []string
	// notFoundMsg mirrors the per-app NoSuchElementException text
	// ("Account cash snapshot not found (id=1)" / "... for portfolio X").
	notFoundMsg func(key string) string
}

// NewService wires store + ordered balance keys + the per-app not-found message
// (explicit constructor wiring, ADR-094 §2 — no DI framework).
func NewService(store Store, notFoundMsg func(key string) string, keys ...string) *Service {
	return &Service{store: store, keys: keys, notFoundMsg: notFoundMsg}
}

// GetCurrent is the direct port of getCurrent — the raw snapshot, no envelope (the
// handler writes it with httpx.WriteJSON, not httpx.Single).
func (s *Service) GetCurrent(ctx context.Context, key string) (Snapshot, error) {
	snap, err := s.store.Load(ctx, key)
	if err != nil {
		return Snapshot{}, s.mapNotFound(key, err)
	}
	return snap, nil
}

// UpdateManual is the direct port of updateManual: overwrite every balance,
// stamp MANUAL + now.
func (s *Service) UpdateManual(ctx context.Context, key string, vals map[string]decimal.Decimal) (Snapshot, error) {
	snap, err := s.store.Load(ctx, key)
	if err != nil {
		return Snapshot{}, s.mapNotFound(key, err)
	}
	if snap.Balances == nil {
		snap.Balances = map[string]decimal.Decimal{}
	}
	for _, k := range s.keys {
		if v, ok := vals[k]; ok {
			snap.Balances[k] = v
		} else {
			snap.Balances[k] = decimal.Zero
		}
	}
	snap.UpdatedAt = time.Now().UTC()
	snap.UpdateReason = ReasonManual
	if err := s.store.Save(ctx, key, snap); err != nil {
		return Snapshot{}, err
	}
	return snap, nil
}

// Deduct is the direct port of deductPurchases: subtract per balance floored at
// zero, stamp POST_PURCHASE + now. Missing deltas count as zero (nullSafe).
func (s *Service) Deduct(ctx context.Context, key string, vals map[string]decimal.Decimal) (Snapshot, error) {
	snap, err := s.store.Load(ctx, key)
	if err != nil {
		return Snapshot{}, s.mapNotFound(key, err)
	}
	if snap.Balances == nil {
		snap.Balances = map[string]decimal.Decimal{}
	}
	for _, k := range s.keys {
		cur := snap.Balances[k]
		d := vals[k] // absent = zero value = nullSafe
		floored := cur.Sub(d)
		if floored.IsNegative() {
			floored = decimal.Zero
		}
		snap.Balances[k] = floored
	}
	snap.UpdatedAt = time.Now().UTC()
	snap.UpdateReason = ReasonPostPurchase
	if err := s.store.Save(ctx, key, snap); err != nil {
		return Snapshot{}, err
	}
	return snap, nil
}

// CheckNonNegative enforces the @DecimalMin("0.0") request contract shared by both
// apps' update/deduct bodies: any negative amount answers 400 VALIDATION_ERROR.
// The handler calls this before UpdateManual/Deduct.
func CheckNonNegative(vals map[string]decimal.Decimal) error {
	for k, v := range vals {
		if v.IsNegative() {
			return httpx.NewValidationError(k, "must be greater than or equal to 0")
		}
	}
	return nil
}

func (s *Service) mapNotFound(key string, err error) error {
	if isNotFound(err) {
		msg := "account cash snapshot not found"
		if s.notFoundMsg != nil {
			msg = s.notFoundMsg(key)
		}
		return httpx.NewNotFoundError(msg)
	}
	return err
}
