// Package siblingcore is the service core shared by the portfolioB and portfolioC backends (ADR-113).
// It knows two per-app parameters — the portfolio set and the unit-bucket set (Config) — and
// talks to each app's database only through the ports its sub-packages declare. main never
// imports it.
package siblingcore

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"investing-app/common-go/httpx"
)

// Portfolio names one independent book. WholeBook is the single implicit book of an app that
// has no portfolio dimension (portfolioB): adapters ignore it and messages omit it.
type Portfolio string

// WholeBook is the portfolio of an app without a portfolio column.
const WholeBook Portfolio = ""

// Bucket names one units column of a subasset row: an account type in portfolioB
// (IKE_TATA … INNE), SingleBucket in portfolioC.
type Bucket string

// SingleBucket is the only bucket of an app with one units column.
const SingleBucket Bucket = ""

// BucketSpec describes one bucket of an app's units model.
type BucketSpec struct {
	Bucket Bucket
	// Field is the request field named in this bucket's validation errors
	// ("unitsIkeTata" … in portfolioB, "units" in portfolioC).
	Field string
	// TracksLots: units bought into this bucket by a rebalancing confirm also record a
	// purchase lot (portfolioB: the four IKE/IKZE buckets; INNE = false; portfolioC: true).
	TracksLots bool
}

// Config is the whole per-app parameterisation of siblingcore. Each app declares exactly one,
// in internal/book.
type Config struct {
	Portfolios []Portfolio // portfolioB {WholeBook}; portfolioC {"VARIABLE", "FIXED"}
	// PortfolioEnum qualifies the 400 text for an unknown portfolio, keeping the legacy message the
	// apps return elsewhere ("No enum constant <PortfolioEnum>.<NAME>"). Unused when Portfolios ==
	// {WholeBook}.
	PortfolioEnum string
	Buckets       []BucketSpec // portfolioB 5 account types; portfolioC {SingleBucket}
}

// Validate reports a malformed Config: empty sets, duplicates, WholeBook mixed with named
// portfolios, named portfolios without PortfolioEnum, SingleBucket mixed with named buckets, a
// bucket without its request field. Each app's internal/book test calls it.
func (c Config) Validate() error {
	if len(c.Portfolios) == 0 {
		return errors.New("siblingcore: config has no portfolios")
	}
	seenP := map[Portfolio]bool{}
	for _, p := range c.Portfolios {
		if seenP[p] {
			return fmt.Errorf("siblingcore: duplicate portfolio %q", p)
		}
		seenP[p] = true
	}
	if seenP[WholeBook] && len(c.Portfolios) > 1 {
		return errors.New("siblingcore: WholeBook cannot be mixed with named portfolios")
	}
	if !seenP[WholeBook] && c.PortfolioEnum == "" {
		return errors.New("siblingcore: named portfolios need PortfolioEnum")
	}
	if len(c.Buckets) == 0 {
		return errors.New("siblingcore: config has no buckets")
	}
	seenB := map[Bucket]bool{}
	for _, b := range c.Buckets {
		if seenB[b.Bucket] {
			return fmt.Errorf("siblingcore: duplicate bucket %q", b.Bucket)
		}
		if b.Field == "" {
			return fmt.Errorf("siblingcore: bucket %q has no request field", b.Bucket)
		}
		seenB[b.Bucket] = true
	}
	if seenB[SingleBucket] && len(c.Buckets) > 1 {
		return errors.New("siblingcore: SingleBucket cannot be mixed with named buckets")
	}
	return nil
}

// CheckPortfolio returns nil when p is one of c.Portfolios, otherwise a 400 VALIDATION_ERROR
// on field: "Required request parameter 'portfolio' is missing" when p is empty, else
// "No enum constant <PortfolioEnum>.<p>". For a WholeBook config it accepts only WholeBook.
func (c Config) CheckPortfolio(field string, p Portfolio) error {
	if slices.Contains(c.Portfolios, p) {
		return nil
	}
	if p == WholeBook {
		return httpx.NewValidationError(field, "Required request parameter 'portfolio' is missing")
	}
	return httpx.NewValidationError(field, "No enum constant "+c.PortfolioEnum+"."+string(p))
}

// ResolvePortfolio maps a raw wire value to a Portfolio: a WholeBook config ignores raw and
// returns WholeBook (portfolioB accepts and ignores a body `portfolio` today); otherwise it trims
// and upper-cases raw and applies CheckPortfolio.
func (c Config) ResolvePortfolio(field, raw string) (Portfolio, error) {
	if c.wholeBook() {
		return WholeBook, nil
	}
	p := Portfolio(strings.ToUpper(strings.TrimSpace(raw)))
	if err := c.CheckPortfolio(field, p); err != nil {
		return WholeBook, err
	}
	return p, nil
}

// CheckBucket returns nil when b is one of c.Buckets, otherwise a 400 VALIDATION_ERROR on field
// ("must be one of IKE_TATA, IKE_MAMA, IKZE_TATA, IKZE_MAMA, INNE").
func (c Config) CheckBucket(field string, b Bucket) error {
	if _, ok := c.Spec(b); ok {
		return nil
	}
	names := make([]string, 0, len(c.Buckets))
	for _, s := range c.Buckets {
		names = append(names, string(s.Bucket))
	}
	return httpx.NewValidationError(field, "must be one of "+strings.Join(names, ", "))
}

// CheckUnits applies CheckBucket to every bucket u holds an entry for (zero amounts included);
// the message lists the configured buckets, so it does not depend on which entry failed.
func (c Config) CheckUnits(field string, u Units) error {
	for b := range u.m {
		if _, ok := c.Spec(b); !ok {
			return c.CheckBucket(field, b)
		}
	}
	return nil
}

// Spec returns the BucketSpec of b.
func (c Config) Spec(b Bucket) (BucketSpec, bool) {
	for _, s := range c.Buckets {
		if s.Bucket == b {
			return s, true
		}
	}
	return BucketSpec{}, false
}

func (c Config) wholeBook() bool {
	return len(c.Portfolios) == 1 && c.Portfolios[0] == WholeBook
}
