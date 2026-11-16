package inflationsync

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shopspring/decimal"
)

func testClient(serverURL string) *Client {
	c := NewClient()
	c.base = serverURL
	c.pageURL = serverURL + "/page"
	c.fallbackCSV = serverURL + "/fallback.csv"
	c.interYearDelay = 0
	c.retryDelay = 0
	return c
}

func TestParseCSV_PortsJavaSemantics(t *testing.T) {
	csv := "Nazwa;X;Y;Rok;Miesiac;Wartosc\n" +
		"Other line;without the marker;1;2;3;4\n" +
		"Wskaznik;Poprzedni miesiac = 100;z;2024;3;102,50\n" +
		"Wskaznik;Poprzedni miesiac = 100;z;2024;13;100,00\n" +
		"Wskaznik;Poprzedni miesiac = 100;z;1999;5;100,00\n" +
		"Wskaznik;Poprzedni miesiac = 100;z;2024;6;\n" +
		"Wskaznik;Poprzedni miesiac = 100;z;2024\n" +
		"\n"
	entries := parseCSV(csv)
	if len(entries) != 1 {
		t.Fatalf("expected exactly 1 entry (header/non-marker/bad-month/bad-year/empty/short lines skipped), got %+v", entries)
	}
	e := entries[0]
	if e.Year != 2024 || e.Month != 3 || !e.Cpi.Equal(decimal.RequireFromString("102.50")) {
		t.Fatalf("unexpected entry: %+v", e)
	}
}

func TestDecodeCSV_Windows1250Markers(t *testing.T) {
	// "Poprzedni miesiąc" with ó as windows-1250 0xF3 (in UTF-8 ó is 0xC3 0xB3).
	raw := []byte{'P', 'o', 'p', 'r', 'z', 'e', 'd', 'n', 'i', ' ', 'm', 'i', 'e', 's', 'i', 0xB9, 0x63, '\n'}
	got := decodeCSV(raw)
	if !strings.Contains(got, "Poprzedni") || !strings.Contains(got, "miesi") {
		t.Fatalf("expected windows-1250 markers decoded, got %q", got)
	}
	if !strings.Contains(got, "miesi\u0105c") {
		t.Fatalf("expected ą (0xB9) decoded to U+0105, got %q", got)
	}

	plain := "nothing special here"
	if decodeCSV([]byte(plain)) != plain {
		t.Fatal("plain ASCII must pass through unchanged")
	}
}

func TestClient_FetchYear_FailureIsNil(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := testClient(srv.URL)
	if rows := c.fetchYear(context.Background(), sdpOldSeries, 2024); rows != nil {
		t.Fatalf("expected nil on 500, got %+v", rows)
	}
	if calls != 1 {
		t.Fatalf("fetchYear itself never retries (the retry lives in fetchFromSdp), got %d calls", calls)
	}
}

func TestClient_FetchYear_ParsesRows(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// id-okres 249 = March (246+3).
		_, _ = fmt.Fprint(w, `[{"id-okres":249,"wartosc":102.5},null,{"id-okres":null,"wartosc":1}]`)
	}))
	defer srv.Close()

	c := testClient(srv.URL)
	rows := c.fetchYear(context.Background(), sdpOldSeries, 2024)
	if len(rows) != 3 {
		t.Fatalf("expected 3 decoded rows (filtering happens in fetchFromSdp), got %d", len(rows))
	}
}

func TestClient_FetchFromSdp_RetriesOncePerYear(t *testing.T) {
	calls2024 := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("id-rok") == "2024" {
			calls2024++
			if calls2024 == 1 {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			_, _ = fmt.Fprint(w, `[{"id-okres":249,"wartosc":102.5},{"id-okres":259,"wartosc":1.5}]`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := testClient(srv.URL)
	entries := c.fetchFromSdp(context.Background())
	if calls2024 != 2 {
		t.Fatalf("expected 1 retry for 2024 (2 calls), got %d", calls2024)
	}
	if len(entries) != 1 || entries[0].Year != 2024 || entries[0].Month != 3 {
		t.Fatalf("unexpected entries: %+v", entries)
	}
}

func TestClient_FetchYear_404IsQuietNil(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := testClient(srv.URL)
	if rows := c.fetchYear(context.Background(), sdpOldSeries, 2030); rows != nil {
		t.Fatalf("expected nil for 404, got %+v", rows)
	}
}

func TestClient_FetchFromSdp_UsesCutoffSeries(t *testing.T) {
	var gotIndicators []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotIndicators = append(gotIndicators, r.URL.Query().Get("id-wskaznik"))
		// Only answer 2026-02 (id-okres 248); everything else 404 (routine, quiet).
		if r.URL.Query().Get("id-rok") == "2026" {
			_, _ = fmt.Fprint(w, `[{"id-okres":248,"wartosc":101.25}]`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := testClient(srv.URL)
	entries := c.fetchFromSdp(context.Background())
	if len(entries) != 1 || entries[0].Year != 2026 || entries[0].Month != 2 {
		t.Fatalf("unexpected entries: %+v", entries)
	}
	if !entries[0].Cpi.Equal(decimal.RequireFromString("101.25")) {
		t.Fatalf("unexpected cpi: %s", entries[0].Cpi)
	}
	seenOld, seenNew := false, false
	for _, ind := range gotIndicators {
		if ind == "639" {
			seenOld = true
		}
		if ind == "2874" {
			seenNew = true
		}
	}
	if !seenOld || !seenNew {
		t.Fatalf("expected both series indicators across the cutoff (639 pre-2026, 2874 from 2026), got %v", gotIndicators)
	}
}

func TestClient_ResolveCSVURL_LastLinkWins(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `<a href="/old/file.csv">x</a><a HREF="%s/new/file.csv">y</a>`, srv.URL)
	}))
	defer srv.Close()

	c := testClient(srv.URL)
	if got := c.resolveCSVURL(context.Background()); got != srv.URL+"/new/file.csv" {
		t.Fatalf("expected last absolute CSV link, got %q", got)
	}
}

func TestClient_FetchMomInflation_FallsBackToCSV(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "indicator-data-indicator"):
			w.WriteHeader(http.StatusInternalServerError) // SDP dead (twice: initial + retry)
		case strings.HasSuffix(r.URL.Path, "/page"):
			w.WriteHeader(http.StatusNotFound) // page dead → fallback CSV constant
		case strings.HasSuffix(r.URL.Path, "/fallback.csv"):
			_, _ = fmt.Fprint(w, "Wskaznik;Poprzedni miesiac = 100;z;2025;1;100,70\n")
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := testClient(srv.URL)
	entries := c.FetchMomInflation(context.Background())
	if len(entries) != 1 || entries[0].Year != 2025 || entries[0].Month != 1 {
		t.Fatalf("expected 1 CSV-fallback entry, got %+v", entries)
	}
	if !entries[0].Cpi.Equal(decimal.RequireFromString("100.70")) {
		t.Fatalf("unexpected cpi: %s", entries[0].Cpi)
	}
}
