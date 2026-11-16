package rebalancingcalc

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shopspring/decimal"

	"investing-app/common-go/httpx"
)

// Task 182 B4 (F1.5 option A): the pure calculation returns its own *UnavailableError (no
// httpx import), with the messages the wire has always carried.
func TestUnavailableErrors_TypeAndMessage(t *testing.T) {
	_, missingRate := divByRate(decimal.NewFromInt(100), map[string]decimal.Decimal{}, "EUR")
	_, zeroRate := divByRate(decimal.NewFromInt(100), map[string]decimal.Decimal{"USD": decimal.Zero}, "USD")
	cases := []struct {
		name, want string
		err        error
	}{
		{"divByRate without a rate", "FX rate unavailable for currency: EUR", missingRate},
		{"divByRate with a zero rate", "FX rate unavailable for currency: USD", zeroRate},
		{"priceUnavailable, yahoo ticker", "Price unavailable for ticker: EUNL.DE",
			priceUnavailable(PositionRow{Ticker: "EUNL", YahooTicker: new("EUNL.DE")})},
		{"priceUnavailable, no yahoo ticker", "Price unavailable for ticker: GOLD", priceUnavailable(PositionRow{Ticker: "GOLD"})},
		{"priceUnavailable, blank yahoo ticker", "Price unavailable for ticker: GOLD",
			priceUnavailable(PositionRow{Ticker: "GOLD", YahooTicker: new("")})},
	}
	for _, c := range cases {
		var u *UnavailableError
		if !errors.As(c.err, &u) {
			t.Fatalf("%s: %T is not a *UnavailableError", c.name, c.err)
		}
		if !u.Unavailable() {
			t.Errorf("%s: Unavailable() = false", c.name)
		}
		if c.err.Error() != c.want {
			t.Errorf("%s: message %q, want %q", c.name, c.err.Error(), c.want)
		}
	}
}

// A handler surfacing the calculation's error (wrapped or not) still answers 503
// SERVICE_UNAVAILABLE with the calculation's own message.
func TestUnavailableError_AnswersServiceUnavailable(t *testing.T) {
	calcErr := priceUnavailable(PositionRow{Ticker: "BTC", YahooTicker: new("BTC-USD")})
	for _, err := range []error{calcErr, fmt.Errorf("purchasesession: plan: %w", calcErr)} {
		rec := httptest.NewRecorder()
		httpx.WriteError(rec, err)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("%v: status %d, want 503", err, rec.Code)
		}
		var body httpx.ErrorResponse
		if jerr := json.Unmarshal(rec.Body.Bytes(), &body); jerr != nil || len(body.Errors) != 1 {
			t.Fatalf("%v: bad envelope %s (%v)", err, rec.Body.String(), jerr)
		}
		got := body.Errors[0]
		if got.Code != httpx.CodeServiceUnavailable || got.Message != "Price unavailable for ticker: BTC-USD" || got.Field != "" {
			t.Errorf("%v: error detail %+v, want SERVICE_UNAVAILABLE / the calc message", err, got)
		}
	}
}
