package priceprovider

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

var errCountingProvider = errors.New("counting provider: simulated failure")

// countingProvider records how many times GetPrice was called, so tests can assert whether the
// cache actually avoided a call to the wrapped provider.
type countingProvider struct {
	calls int
	price decimal.Decimal
	err   error
}

func (p *countingProvider) GetPrice(_ context.Context, _ string) (decimal.Decimal, error) {
	p.calls++
	return p.price, p.err
}

func (p *countingProvider) Name() string { return "COUNTING" }

func TestCachingDecorator_CachesWithinTTL(t *testing.T) {
	inner := &countingProvider{price: decimal.NewFromInt(100)}
	dec := NewCachingDecorator(inner, time.Minute)

	for range 3 {
		price, err := dec.GetPrice(context.Background(), "AAPL")
		if err != nil {
			t.Fatalf("GetPrice: %v", err)
		}
		if !price.Equal(decimal.NewFromInt(100)) {
			t.Fatalf("expected 100, got %s", price)
		}
	}

	if inner.calls != 1 {
		t.Fatalf("expected exactly 1 call to the wrapped provider, got %d", inner.calls)
	}
}

func TestCachingDecorator_RefetchesAfterTTLExpires(t *testing.T) {
	inner := &countingProvider{price: decimal.NewFromInt(100)}
	dec := NewCachingDecorator(inner, time.Millisecond)

	if _, err := dec.GetPrice(context.Background(), "AAPL"); err != nil {
		t.Fatalf("GetPrice: %v", err)
	}
	time.Sleep(5 * time.Millisecond)
	if _, err := dec.GetPrice(context.Background(), "AAPL"); err != nil {
		t.Fatalf("GetPrice: %v", err)
	}

	if inner.calls != 2 {
		t.Fatalf("expected 2 calls after TTL expiry, got %d", inner.calls)
	}
}

func TestCachingDecorator_CachesPerTicker(t *testing.T) {
	inner := &countingProvider{price: decimal.NewFromInt(50)}
	dec := NewCachingDecorator(inner, time.Minute)

	if _, err := dec.GetPrice(context.Background(), "AAPL"); err != nil {
		t.Fatalf("GetPrice: %v", err)
	}
	if _, err := dec.GetPrice(context.Background(), "MSFT"); err != nil {
		t.Fatalf("GetPrice: %v", err)
	}

	if inner.calls != 2 {
		t.Fatalf("expected a separate call per distinct ticker, got %d calls", inner.calls)
	}
}

func TestCachingDecorator_DoesNotCacheErrors(t *testing.T) {
	inner := &countingProvider{err: errCountingProvider}
	dec := NewCachingDecorator(inner, time.Minute)

	if _, err := dec.GetPrice(context.Background(), "AAPL"); err == nil {
		t.Fatal("expected an error")
	}
	if _, err := dec.GetPrice(context.Background(), "AAPL"); err == nil {
		t.Fatal("expected an error on the second call too")
	}

	if inner.calls != 2 {
		t.Fatalf("expected the decorator to retry on every call after an error, got %d calls", inner.calls)
	}
}
