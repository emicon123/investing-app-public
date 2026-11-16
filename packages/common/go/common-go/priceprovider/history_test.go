package priceprovider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNearestClose(t *testing.T) {
	d := func(s string) time.Time {
		tm, _ := time.Parse("2006-01-02", s)
		return tm
	}
	points := []OHLCPoint{
		{Date: d("2024-01-05"), Close: 105},
		{Date: d("2024-01-02"), Close: 102}, // unsorted input, must still work
		{Date: d("2024-01-03"), Close: 103},
	}

	// Exact hit.
	if v, ok := NearestClose(points, d("2024-01-03")); !ok || v != 103 {
		t.Fatalf("expected 103, got %v,%v", v, ok)
	}
	// Weekend gap → floor (Friday).
	if v, ok := NearestClose(points, d("2024-01-06")); !ok || v != 105 {
		t.Fatalf("expected floor 105, got %v,%v", v, ok)
	}
	// Target predates the series → earliest (Java's firstEntry fallback).
	if v, ok := NearestClose(points, d("2023-12-01")); !ok || v != 102 {
		t.Fatalf("expected earliest 102, got %v,%v", v, ok)
	}
	if _, ok := NearestClose(nil, d("2024-01-01")); ok {
		t.Fatal("expected false on empty series")
	}
}

func TestYahooOHLCProvider_Fixture(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("interval") != "1d" {
			t.Errorf("expected interval=1d, got %s", r.URL.Query().Get("interval"))
		}
		_, _ = fmt.Fprint(w, `{"chart":{"result":[{"timestamp":[1704067200,1704153600,1704240000],`+
			`"indicators":{"quote":[{"close":[100.5,null,102.25]}]}}],"error":null}}`)
	}))
	defer srv.Close()

	p := &YahooOHLCProvider{client: srv.Client(), baseURL: srv.URL}
	points, err := p.FetchOHLC(context.Background(), "SXRV.DE", "1d", 1704067200, 1704240000)
	if err != nil {
		t.Fatal(err)
	}
	// The null close (market holiday) is skipped, not zero-filled.
	if len(points) != 2 {
		t.Fatalf("expected 2 points, got %+v", points)
	}
	if points[0].Close != 100.5 || points[1].Close != 102.25 {
		t.Fatalf("unexpected closes: %+v", points)
	}
	if !points[0].Date.Before(points[1].Date) {
		t.Fatalf("expected ascending dates: %+v", points)
	}
}
