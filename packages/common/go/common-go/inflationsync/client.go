package inflationsync

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// MomEntry is one month-over-month CPI value — the direct port of
// SdpInflationClient.MomEntry (all three apps' GUS sync since task 178).
type MomEntry struct {
	Year  int
	Month int
	Cpi   decimal.Decimal
}

const (
	sdpBase       = "https://api-sdp.stat.gov.pl"
	sdpOldSeries  = 639
	sdpNewSeries  = 2874
	sdpCutoffYear = 2026

	gusPageURL    = "https://stat.gov.pl/obszary-tematyczne/ceny-handel/wskazniki-cen/wskazniki-cen-towarow-i-uslug-konsumpcyjnych-pot-inflacja-/miesieczne-wskazniki-cen-towarow-i-uslug-konsumpcyjnych-od-1982-roku/"
	fallbackCSV   = "https://stat.gov.pl/download/gfx/portalinformacyjny/pl/defaultstronaopisowa/4741/1/1/miesieczne_wskazniki_cen_towarow_i_uslug_konsumpcyjnych_od_1982_roku__2_2.csv"
	firstDataYear = 2000
)

var csvLinkPattern = regexp.MustCompile(`(?i)href="([^"]+\.csv)"`)

// Client fetches GUS month-over-month CPI: SDP JSON API first, stat.gov.pl CSV scrape as fallback
// (ported from the Java SdpInflationClient, ADR-094). Timeouts are connect 10s / read 30s; the
// retry (one retry after 2s per year) and the 300ms inter-year pacing are deliberate — politeness
// toward a public-stats endpoint, not a bug.
type Client struct {
	http        *http.Client
	base        string
	pageURL     string
	fallbackCSV string

	interYearDelay time.Duration
	retryDelay     time.Duration
}

// NewClient builds the production client (connect 10s, per-request read 30s).
func NewClient() *Client {
	transport := &http.Transport{
		DialContext: (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
	}
	return &Client{
		http:           &http.Client{Transport: transport},
		base:           sdpBase,
		pageURL:        gusPageURL,
		fallbackCSV:    fallbackCSV,
		interYearDelay: 300 * time.Millisecond,
		retryDelay:     2 * time.Second,
	}
}

// FetchMomInflation returns every month-over-month CPI value, SDP API first then CSV fallback.
// Fetch failures degrade to an empty result with a warn log — never an error (the caller's empty
// check is the whole failure contract). ctx cancels the multi-request sweep between years.
func (c *Client) FetchMomInflation(ctx context.Context) []MomEntry {
	if api := c.fetchFromSdp(ctx); len(api) > 0 {
		return api
	}
	slog.Warn("GUS inflation sync: SDP API empty/failed, falling back to GUS CSV scrape")
	data := c.fetchBytes(ctx, c.resolveCSVURL(ctx))
	if len(data) == 0 {
		return nil
	}
	return parseCSV(decodeCSV(data))
}

// sdpRow mirrors SdpInflationClient.SdpRow — id-okres encodes the month as 246+month.
type sdpRow struct {
	IdOkres *int             `json:"id-okres"`
	Wartosc *decimal.Decimal `json:"wartosc"`
}

func (c *Client) fetchFromSdp(ctx context.Context) []MomEntry {
	var out []MomEntry
	currentYear := warsawNow().Year()
	first := true
	for year := firstDataYear; year <= currentYear+1; year++ {
		if err := ctx.Err(); err != nil {
			slog.Warn("GUS inflation sync: context cancelled, keeping partial result", "years", len(out))
			return out
		}
		if !first {
			sleepOrDone(ctx, c.interYearDelay)
		}
		first = false
		indicator := sdpOldSeries
		if year >= sdpCutoffYear {
			indicator = sdpNewSeries
		}
		rows := c.fetchYear(ctx, indicator, year)
		if rows == nil {
			sleepOrDone(ctx, c.retryDelay)
			rows = c.fetchYear(ctx, indicator, year)
		}
		if rows == nil {
			continue
		}
		for _, r := range rows {
			// A JSON null array element decodes to the zero value (both pointers nil)
			// and is skipped by the same checks — the direct port of `r == null ||`.
			if r.Wartosc == nil || r.IdOkres == nil {
				continue
			}
			month := *r.IdOkres - 246
			if month < 1 || month > 12 {
				continue
			}
			out = append(out, MomEntry{Year: year, Month: month, Cpi: *r.Wartosc})
		}
	}
	return out
}

func (c *Client) fetchYear(ctx context.Context, indicator, year int) []sdpRow {
	url := fmt.Sprintf("%s/api/indicators/indicator-data-indicator?id-wskaznik=%d&id-rok=%d&lang=pl",
		c.base, indicator, year)
	body, status, err := c.get(ctx, url)
	if err != nil {
		// Only non-404s are worth a warning (a 404 for a future year is routine, not a fault).
		if status != http.StatusNotFound {
			slog.Warn("GUS inflation sync: SDP fetch failed", "year", year, "error", err)
		}
		return nil
	}
	var rows []sdpRow
	if err := json.Unmarshal(body, &rows); err != nil {
		slog.Warn("GUS inflation sync: SDP decode failed", "year", year, "error", err)
		return nil
	}
	return rows
}

func (c *Client) resolveCSVURL(ctx context.Context) string {
	body, _, err := c.get(ctx, c.pageURL)
	if err != nil {
		slog.Warn("GUS inflation sync: page fetch failed, using fallback CSV", "error", err)
		return c.fallbackCSV
	}
	matches := csvLinkPattern.FindAllSubmatch(body, -1)
	if len(matches) == 0 {
		return c.fallbackCSV
	}
	last := string(matches[len(matches)-1][1])
	switch {
	case strings.HasPrefix(last, "http"):
		return last
	case strings.HasPrefix(last, "/"):
		return "https://stat.gov.pl" + last
	default:
		return "https://stat.gov.pl/" + last
	}
}

func (c *Client) fetchBytes(ctx context.Context, url string) []byte {
	body, _, err := c.get(ctx, url)
	if err != nil {
		slog.Warn("GUS inflation sync: CSV fetch failed", "url", url, "error", err)
		if url != c.fallbackCSV {
			if retry, _, retryErr := c.get(ctx, c.fallbackCSV); retryErr == nil {
				return retry
			}
			slog.Warn("GUS inflation sync: fallback CSV also failed")
		}
		return nil
	}
	return body
}

// get performs one GET with the client's headers (User-Agent, Accept) and a 30s read timeout. It
// returns the status code alongside errors so callers can stay quiet on routine 404s.
func (c *Client) get(ctx context.Context, url string) ([]byte, int, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 investing-app/1.0")
	req.Header.Set("Accept", "application/json, text/html, text/csv, */*")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }() // read-side close; error not actionable
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, resp.StatusCode, fmt.Errorf("GET %s: status %d", url, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return body, resp.StatusCode, nil
}

// decodeCSV ports SdpInflationClient.decode: the GUS CSV is windows-1250-encoded; try that
// first, then UTF-8, picking the first decoding that contains the expected Polish markers.
func decodeCSV(data []byte) string {
	if s := decodeWindows1250(data); strings.Contains(s, "Poprzedni") || strings.Contains(s, "miesi") {
		return s
	}
	if s := string(data); strings.Contains(s, "Poprzedni") || strings.Contains(s, "miesi") {
		return s
	}
	return decodeWindows1250(data)
}

// parseCSV ports SdpInflationClient.parseCsv: only "Poprzedni mies..." rows carry the
// month-over-month index; year/month/value sit in columns 3/4/5 with a comma decimal
// separator.
func parseCSV(csv string) []MomEntry {
	var out []MomEntry
	for line := range strings.SplitSeq(csv, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "Nazwa") {
			continue
		}
		if !strings.Contains(line, "Poprzedni mies") {
			continue
		}
		cols := strings.Split(line, ";")
		if len(cols) < 6 {
			continue
		}
		year, err := strconv.Atoi(strings.TrimSpace(cols[3]))
		if err != nil {
			continue
		}
		month, err := strconv.Atoi(strings.TrimSpace(cols[4]))
		if err != nil {
			continue
		}
		v := strings.Replace(strings.TrimSpace(cols[5]), ",", ".", 1)
		if v == "" {
			continue
		}
		cpi, err := decimal.NewFromString(v)
		if err != nil {
			continue
		}
		if year < 2000 || year > 2100 || month < 1 || month > 12 {
			continue
		}
		out = append(out, MomEntry{Year: year, Month: month, Cpi: cpi})
	}
	return out
}

func sleepOrDone(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

// win1250 maps bytes 0x80–0xFF to runes (0x00–0x7F are identity). 0x81/0x83/0x90/0x98 are
// undefined in windows-1250 → U+FFFD, exactly as Go's own decoding would substitute.
var win1250 = [128]rune{
	0x20AC, 0xFFFD, 0x201A, 0xFFFD, 0x201E, 0x2026, 0x2020, 0x2021,
	0x02C6, 0x2030, 0x0160, 0x2039, 0x015A, 0x0164, 0x017D, 0x0179,
	0xFFFD, 0x2018, 0x2019, 0x201C, 0x201D, 0x2022, 0x2013, 0x2014,
	0xFFFD, 0x2122, 0x0161, 0x203A, 0x015B, 0x0165, 0x017E, 0x017A,
	0x00A0, 0x02C7, 0x02D8, 0x0141, 0x00A4, 0x0104, 0x00A6, 0x00A7,
	0x00A8, 0x00A9, 0x015E, 0x00AB, 0x00AC, 0x00AD, 0x00AE, 0x017B,
	0x00B0, 0x00B1, 0x02DB, 0x0142, 0x00B4, 0x00B5, 0x00B6, 0x00B7,
	0x00B8, 0x0105, 0x015F, 0x00BB, 0x013D, 0x02DD, 0x013E, 0x017C,
	0x0154, 0x00C1, 0x00C2, 0x0102, 0x00C4, 0x0139, 0x0106, 0x00C7,
	0x010C, 0x00C9, 0x0118, 0x00CB, 0x011A, 0x00CD, 0x00CE, 0x010E,
	0x0110, 0x0143, 0x0147, 0x00D3, 0x00D4, 0x0150, 0x00D6, 0x00D7,
	0x0158, 0x016E, 0x00DA, 0x0170, 0x00DC, 0x00DD, 0x0162, 0x00DF,
	0x0155, 0x00E1, 0x00E2, 0x0103, 0x00E4, 0x013A, 0x0107, 0x00E7,
	0x010D, 0x00E9, 0x0119, 0x00EB, 0x011B, 0x00ED, 0x00EE, 0x010F,
	0x0111, 0x0144, 0x0148, 0x00F3, 0x00F4, 0x0151, 0x00F6, 0x00F7,
	0x0159, 0x016F, 0x00FA, 0x0171, 0x00FC, 0x00FD, 0x0163, 0x02D9,
}

func decodeWindows1250(data []byte) string {
	var sb strings.Builder
	sb.Grow(len(data))
	for _, b := range data {
		if b < 0x80 {
			sb.WriteByte(b)
		} else {
			sb.WriteRune(win1250[b-0x80])
		}
	}
	return sb.String()
}
