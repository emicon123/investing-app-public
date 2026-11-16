package priceprovider

import (
	"context"
	"errors"
	"testing"

	"github.com/shopspring/decimal"
)

type fixedProvider struct {
	name  string
	price decimal.Decimal
	err   error
}

func (f fixedProvider) GetPrice(_ context.Context, _ string) (decimal.Decimal, error) {
	return f.price, f.err
}
func (f fixedProvider) Name() string { return f.name }

func TestFallbackProvider_UsesPrimaryWhenItSucceeds(t *testing.T) {
	primary := fixedProvider{name: "PRIMARY", price: decimal.NewFromInt(1)}
	fallback := fixedProvider{name: "FALLBACK", price: decimal.NewFromInt(2)}
	f := NewFallbackProvider(primary, fallback)

	price, err := f.GetPrice(context.Background(), "X")
	if err != nil {
		t.Fatalf("GetPrice: %v", err)
	}
	if !price.Equal(decimal.NewFromInt(1)) {
		t.Fatalf("expected primary's price 1, got %s", price)
	}
}

func TestFallbackProvider_FallsBackWhenPrimaryErrors(t *testing.T) {
	primary := fixedProvider{name: "PRIMARY", err: errors.New("primary down")}
	fallback := fixedProvider{name: "FALLBACK", price: decimal.NewFromInt(2)}
	f := NewFallbackProvider(primary, fallback)

	price, err := f.GetPrice(context.Background(), "X")
	if err != nil {
		t.Fatalf("GetPrice: %v", err)
	}
	if !price.Equal(decimal.NewFromInt(2)) {
		t.Fatalf("expected fallback's price 2, got %s", price)
	}
}

func TestFallbackProvider_ReturnsErrorWhenBothFail(t *testing.T) {
	primary := fixedProvider{name: "PRIMARY", err: errors.New("primary down")}
	fallback := fixedProvider{name: "FALLBACK", err: errors.New("fallback down")}
	f := NewFallbackProvider(primary, fallback)

	if _, err := f.GetPrice(context.Background(), "X"); err == nil {
		t.Fatal("expected an error when both providers fail")
	}
}

func TestFallbackProvider_Name(t *testing.T) {
	f := NewFallbackProvider(fixedProvider{name: "BINANCE"}, fixedProvider{name: "YAHOO"})
	if got := f.Name(); got != "BINANCE→YAHOO" {
		t.Fatalf("expected BINANCE→YAHOO, got %s", got)
	}
}
