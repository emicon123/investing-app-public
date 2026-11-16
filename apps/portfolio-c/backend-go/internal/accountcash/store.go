package accountcash

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"investing-app/common-go/accountcash"
	"investing-app/common-go/pgnum"
	"portfolio-c/backend-go/db/sqlcgen"
	"portfolio-c/backend-go/internal/book"
)

// BalancePln is the single balance key of the PK=portfolio row.
const BalancePln = "balancePln"

// Keys is the ordered key list handed to the kernel.
var Keys = []string{BalancePln}

// NotFoundMessage mirrors AccountCashService.loadSnapshot's NoSuchElementException.
func NotFoundMessage(key string) string {
	return "Account cash snapshot not found for portfolio " + key
}

// ValidPortfolio normalizes the {portfolio} path param (case-insensitive, as the original enum
// conversion was) and rejects anything else with a 400 — callers map the error through
// httpx.WriteError.
func ValidPortfolio(raw string) (string, error) {
	upper := strings.ToUpper(strings.TrimSpace(raw))
	if slices.Contains(book.PortfolioNames(), upper) {
		return upper, nil
	}
	return "", &portfolioError{raw: raw}
}

type portfolioError struct{ raw string }

func (e *portfolioError) Error() string {
	return "No enum constant pl.shared.entity.enums.Portfolio." + strings.ToUpper(strings.TrimSpace(e.raw))
}

// Store satisfies accountcash.Store over the PK=portfolio row, keyed by portfolio.
type Store struct{ q *sqlcgen.Queries }

// NewStore wraps a *sqlcgen.Queries.
func NewStore(q *sqlcgen.Queries) *Store { return &Store{q: q} }

// NewService wires the kernel over this app's Store.
func NewService(store *Store) *accountcash.Service {
	return accountcash.NewService(store, NotFoundMessage, Keys...)
}

// Load reads one portfolio's row, mapping a missing row to accountcash.ErrNotFound.
func (s *Store) Load(ctx context.Context, key string) (accountcash.Snapshot, error) {
	row, err := s.q.GetAccountCash(ctx, sqlcgen.PortfolioEnum(key))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return accountcash.Snapshot{}, accountcash.ErrNotFound
		}
		return accountcash.Snapshot{}, err
	}
	balance, err := pgnum.ToDecimal(row.BalancePln)
	if err != nil {
		return accountcash.Snapshot{}, err
	}
	var updatedAt time.Time
	if row.UpdatedAt.Valid {
		updatedAt = row.UpdatedAt.Time
	}
	return accountcash.Snapshot{
		Balances:     map[string]decimal.Decimal{BalancePln: balance},
		UpdatedAt:    updatedAt,
		UpdateReason: row.UpdateReason,
	}, nil
}

// Save writes one portfolio's row back.
func (s *Store) Save(ctx context.Context, key string, snap accountcash.Snapshot) error {
	_, err := s.q.UpdateAccountCash(ctx, sqlcgen.UpdateAccountCashParams{
		Portfolio:    sqlcgen.PortfolioEnum(key),
		BalancePln:   pgnum.FromDecimal(snap.Balances[BalancePln]),
		UpdatedAt:    pgtype.Timestamptz{Time: snap.UpdatedAt.UTC(), Valid: true},
		UpdateReason: snap.UpdateReason,
	})
	return err
}
