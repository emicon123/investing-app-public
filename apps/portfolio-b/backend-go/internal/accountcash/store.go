package accountcash

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"investing-app/common-go/accountcash"
	"investing-app/common-go/pgnum"
	"portfolio-b/backend-go/db/sqlcgen"
)

// Balance keys in DTO order — the four singleton-row columns.
const (
	KeyIkeTata  = "ikeTata"
	KeyIkeMama  = "ikeMama"
	KeyIkzeTata = "ikzeTata"
	KeyIkzeMama = "ikzeMama"
)

// Keys is the ordered key list handed to the kernel (DTO order, stable).
var Keys = []string{KeyIkeTata, KeyIkeMama, KeyIkzeTata, KeyIkzeMama}

// NotFoundMessage mirrors AccountCashService.loadSnapshot's NoSuchElementException.
func NotFoundMessage(_ string) string { return "Account cash snapshot not found (id=1)" }

// Store satisfies accountcash.Store over the singleton id=1 row. The key is
// ignored — this app has exactly one cash row, not one per portfolio.
type Store struct{ q *sqlcgen.Queries }

// NewStore wraps a *sqlcgen.Queries.
func NewStore(q *sqlcgen.Queries) *Store { return &Store{q: q} }

// NewService wires the kernel over this app's Store.
func NewService(store *Store) *accountcash.Service {
	return accountcash.NewService(store, NotFoundMessage, Keys...)
}

// Load reads the singleton row, mapping a missing row to accountcash.ErrNotFound.
func (s *Store) Load(ctx context.Context, _ string) (accountcash.Snapshot, error) {
	row, err := s.q.GetAccountCash(ctx)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return accountcash.Snapshot{}, accountcash.ErrNotFound
		}
		return accountcash.Snapshot{}, err
	}
	return toSnapshot(row)
}

// Save writes the singleton row back.
func (s *Store) Save(ctx context.Context, _ string, snap accountcash.Snapshot) error {
	_, err := s.q.UpdateAccountCash(ctx, sqlcgen.UpdateAccountCashParams{
		IkeTata:      pgnum.FromDecimal(snap.Balances[KeyIkeTata]),
		IkeMama:      pgnum.FromDecimal(snap.Balances[KeyIkeMama]),
		IkzeTata:     pgnum.FromDecimal(snap.Balances[KeyIkzeTata]),
		IkzeMama:     pgnum.FromDecimal(snap.Balances[KeyIkzeMama]),
		UpdatedAt:    pgTime(snap.UpdatedAt),
		UpdateReason: snap.UpdateReason,
	})
	return err
}

func toSnapshot(row sqlcgen.AccountCashSnapshot) (accountcash.Snapshot, error) {
	balances := map[string]decimal.Decimal{}
	for k, col := range map[string]pgtype.Numeric{
		KeyIkeTata: row.IkeTata, KeyIkeMama: row.IkeMama,
		KeyIkzeTata: row.IkzeTata, KeyIkzeMama: row.IkzeMama,
	} {
		v, err := pgnum.ToDecimal(col)
		if err != nil {
			return accountcash.Snapshot{}, err
		}
		balances[k] = v
	}
	var updatedAt time.Time
	if row.UpdatedAt.Valid {
		updatedAt = row.UpdatedAt.Time
	}
	return accountcash.Snapshot{
		Balances:     balances,
		UpdatedAt:    updatedAt,
		UpdateReason: row.UpdateReason,
	}, nil
}

func pgTime(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t.UTC(), Valid: true}
}
