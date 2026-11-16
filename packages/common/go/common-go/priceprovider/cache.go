package priceprovider

import (
	"context"
	"sync"
	"time"

	"github.com/shopspring/decimal"
)

// cachedPrice holds one ticker's last-fetched price plus when it was fetched.
type cachedPrice struct {
	price     decimal.Decimal
	fetchedAt time.Time
}

// CachingDecorator wraps any PriceProvider with a simple TTL cache (the Decorator of doc 13 §1:
// "adopted, carried forward... more natural in Go than in Java, no subclassing ceremony at all").
// It is itself a PriceProvider, so callers cannot tell whether they're talking to a cached or
// uncached source.
type CachingDecorator struct {
	inner PriceProvider
	ttl   time.Duration

	mu    sync.Mutex
	cache map[string]cachedPrice
}

// NewCachingDecorator wraps inner with a cache of the given TTL.
func NewCachingDecorator(inner PriceProvider, ttl time.Duration) *CachingDecorator {
	return &CachingDecorator{
		inner: inner,
		ttl:   ttl,
		cache: make(map[string]cachedPrice),
	}
}

// GetPrice returns the cached price for ticker if it is still fresh, otherwise delegates to the
// wrapped provider and caches the result.
func (c *CachingDecorator) GetPrice(ctx context.Context, ticker string) (decimal.Decimal, error) {
	c.mu.Lock()
	if cached, ok := c.cache[ticker]; ok && time.Since(cached.fetchedAt) < c.ttl {
		c.mu.Unlock()
		return cached.price, nil
	}
	c.mu.Unlock()

	price, err := c.inner.GetPrice(ctx, ticker)
	if err != nil {
		return decimal.Zero, err
	}

	c.mu.Lock()
	c.cache[ticker] = cachedPrice{price: price, fetchedAt: time.Now()}
	c.mu.Unlock()

	return price, nil
}

// Name delegates to the wrapped provider — callers see the same name whether or not the result
// came from cache, matching the Decorator's transparency requirement (doc 13 §1).
func (c *CachingDecorator) Name() string { return c.inner.Name() }
