package macro

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// This file holds the 8 external HTTP clients behind Service.Refresh plus the ADR-068 ChinaM2Chain
// tip assembly. Design note (doc 13 §7 row 1B): the fan-out is a slice of small FetchFunc values
// (Strategy-shaped, same spirit as PriceProvider), NOT one large method coordinating 8 clients
// inline — but a plain func, not the PriceProvider interface itself, because these return whole
// dated series, not single prices, so that interface's shape genuinely does not fit (documented
// "don't force it" case).
//
// Every fetch is fail-soft: any error — network, parse, missing FRED key — yields an empty slice,
// never an error, so one dead source can never abort a refresh.

// FetchFunc fetches one raw macro series. Empty slice means "unavailable".
type FetchFunc func(ctx context.Context) []MacroPoint

// SeriesFetch binds one raw series to its provenance code for the fan-out loop.
type SeriesFetch struct {
	Series string
	Source string
	Fetch  FetchFunc
}

// Fetcher is the full fan-out behind Service.Refresh: 8 raw-series fetches (5 live + 3
// tip-only), 4 FRED FX fetches, and the 3 tip fetches keyed by source code. Constructed once
// in main.go's composition root; stubbed wholesale in unit tests.
type Fetcher struct {
	Raw  []SeriesFetch
	FX   map[string]FetchFunc // EXUSEU, EXUSUK, EXCHUS, EXJPUS
	Tips map[string]FetchFunc // TRADINGEC, PBC, YCHARTS
}

// macroHTTPClient uses one overall 25s timeout (the Java clients' 5s connect + 20s read budget,
// collapsed; Go's http.Client.Timeout covers dial through body read).
func macroHTTPClient() *http.Client { return &http.Client{Timeout: 25 * time.Second} }

func getText(ctx context.Context, client *http.Client, url, ua string, headers map[string]string) (string, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", false
	}
	req.Header.Set("User-Agent", ua)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", false
	}
	defer func() { _ = resp.Body.Close() }() // read-side close; error not actionable
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", false
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", false
	}
	return string(body), true
}

const browserUA = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0 Safari/537.36"

// ── FRED (key-authenticated, ADR-046 §6 — no fallback) ────────────────────────────────────

// FredClient ports pl.investing.client.FredClient: key-authenticated series/observations JSON
// API. Blank key short-circuits to empty with no network call. FRED's "." gap marker is skipped.
type FredClient struct {
	http    *http.Client
	baseURL string
	apiKey  string
}

func NewFredClient(apiKey string) *FredClient {
	return &FredClient{http: macroHTTPClient(), baseURL: "https://api.stlouisfed.org", apiKey: apiKey}
}

func (c *FredClient) Fetch(seriesID string) FetchFunc {
	return func(ctx context.Context) []MacroPoint {
		if strings.TrimSpace(c.apiKey) == "" {
			return []MacroPoint{}
		}
		u := c.baseURL + "/fred/series/observations?series_id=" + url.QueryEscape(seriesID) +
			"&api_key=" + url.QueryEscape(c.apiKey) + "&file_type=json"
		body, ok := getText(ctx, c.http, u, "investing-app/1.0", nil)
		if !ok {
			slog.Warn("macro fetch: FRED failed", "series", seriesID)
			return []MacroPoint{}
		}
		return parseFredResponse(seriesID, body)
	}
}

func parseFredResponse(seriesID, body string) []MacroPoint {
	var resp struct {
		Observations []struct {
			Date  string `json:"date"`
			Value string `json:"value"`
		} `json:"observations"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		slog.Warn("macro fetch: FRED decode failed", "series", seriesID)
		return []MacroPoint{}
	}
	out := []MacroPoint{}
	for _, o := range resp.Observations {
		if o.Date == "" || o.Value == "" || strings.TrimSpace(o.Value) == "" || o.Value == "." {
			continue
		}
		date, err := time.Parse("2006-01-02", o.Date)
		if err != nil {
			continue
		}
		v, err := decimal.NewFromString(strings.TrimSpace(o.Value))
		if err != nil {
			continue
		}
		out = append(out, MacroPoint{Date: date, Value: v})
	}
	return out
}

// ── ECB (euro-area M2, SDMX csvdata) ─────────────────────────────────────────────────────

type EcbClient struct {
	http    *http.Client
	baseURL string
}

func NewEcbClient() *EcbClient {
	return &EcbClient{http: macroHTTPClient(), baseURL: "https://data-api.ecb.europa.eu"}
}

const ecbM2SeriesKey = "M.U2.Y.V.M20.X.1.U2.2300.Z01.E"

func (c *EcbClient) FetchM2() FetchFunc {
	return func(ctx context.Context) []MacroPoint {
		body, ok := getText(ctx, c.http, c.baseURL+"/service/data/BSI/"+ecbM2SeriesKey+"?format=csvdata", "Mozilla/5.0", nil)
		if !ok {
			slog.Warn("macro fetch: ECB M2 failed")
			return []MacroPoint{}
		}
		return parseEcbCSV(body)
	}
}

// parseEcbCSV ports EcbClient.parseCsv: TIME_PERIOD is column 12, OBS_VALUE column 13,
// period is YYYY-MM.
func parseEcbCSV(csv string) []MacroPoint {
	out := []MacroPoint{}
	if strings.TrimSpace(csv) == "" {
		return out
	}
	lines := strings.Split(csv, "\n")
	for i := 1; i < len(lines); i++ { // skip header
		line := strings.TrimSpace(strings.Trim(lines[i], "\r"))
		if line == "" {
			continue
		}
		f := strings.Split(line, ",")
		if len(f) < 14 {
			continue
		}
		period, valStr := strings.TrimSpace(f[12]), strings.TrimSpace(f[13])
		if len(period) != 7 || period[4] != '-' || valStr == "" {
			continue
		}
		date, err := time.Parse("2006-01-02", period+"-01")
		if err != nil {
			continue
		}
		v, err := decimal.NewFromString(valStr)
		if err != nil {
			continue
		}
		out = append(out, MacroPoint{Date: date, Value: v})
	}
	return out
}

// ── BoE (UK M2 EMU-aggregate LPMVWYH, IADB CSV) ───────────────────────────────────────────

type BoeClient struct {
	http    *http.Client
	baseURL string
}

func NewBoeClient() *BoeClient {
	return &BoeClient{http: macroHTTPClient(), baseURL: "https://www.bankofengland.co.uk"}
}

func (c *BoeClient) FetchM2() FetchFunc {
	return func(ctx context.Context) []MacroPoint {
		u := c.baseURL + "/boeapps/iadb/fromshowcolumns.asp?csv.x=yes" +
			"&Datefrom=01/Jan/2010&Dateto=now&SeriesCodes=LPMVWYH&CSVF=TT&UsingCodes=Y&VPD=Y&VFD=N"
		body, ok := getText(ctx, c.http, u, "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36", nil)
		if !ok {
			slog.Warn("macro fetch: BoE M2 failed")
			return []MacroPoint{}
		}
		return parseBoeCSV(body)
	}
}

// parseBoeCSV ports BoeClient.parseCsv: header DATE,LPMVWYH then rows "31 May 2026",value.
// Dates land on month-start, matching every other M2 component.
func parseBoeCSV(csv string) []MacroPoint {
	out := []MacroPoint{}
	if strings.TrimSpace(csv) == "" {
		return out
	}
	lines := strings.Split(csv, "\n")
	for i := 1; i < len(lines); i++ {
		line := strings.TrimSpace(strings.Trim(lines[i], "\r"))
		if line == "" {
			continue
		}
		comma := strings.LastIndex(line, ",")
		if comma <= 0 {
			continue
		}
		dateStr := strings.ReplaceAll(strings.TrimSpace(line[:comma]), `"`, "")
		valStr := strings.ReplaceAll(strings.TrimSpace(line[comma+1:]), `"`, "")
		if valStr == "" {
			continue
		}
		date, err := time.Parse("2 Jan 2006", dateStr)
		if err != nil {
			// BoE pads single-digit days (" 1 May 2026" vs "31 May 2026"); time.Parse's "2"
			// already accepts both, so anything failing here is genuinely unparseable.
			continue
		}
		v, err := decimal.NewFromString(valStr)
		if err != nil {
			continue
		}
		out = append(out, MacroPoint{Date: time.Date(date.Year(), date.Month(), 1, 0, 0, 0, 0, time.UTC), Value: v})
	}
	return out
}

// ── BoJ (Japan M2, Time-Series Data Search API, ×100 to JPY millions) ────────────────────

type BojClient struct {
	http    *http.Client
	baseURL string
}

func NewBojClient() *BojClient {
	return &BojClient{http: macroHTTPClient(), baseURL: "https://www.stat-search.boj.or.jp"}
}

func (c *BojClient) FetchM2() FetchFunc {
	return func(ctx context.Context) []MacroPoint {
		end := time.Now().AddDate(0, 1, 0).Format("200601") // buffer for month/TZ edge
		u := fmt.Sprintf("%s/api/v1/getDataCode?format=csv&lang=en&db=MD02&code=MAM1NAM2M2MO&startDate=200304&endDate=%s", c.baseURL, end)
		body, ok := getText(ctx, c.http, u, "Mozilla/5.0", nil)
		if !ok {
			slog.Warn("macro fetch: BoJ M2 failed")
			return []MacroPoint{}
		}
		return parseBojCSV(body)
	}
}

// parseBojCSV ports BojClient.parseCsv: KEY,VALUE preamble, then a SERIES_CODE, header row,
// then one row per month with SURVEY_DATES (YYYYMM) and VALUES as the last two fields
// (NAME_OF_TIME_SERIES free text may contain commas — locate via lastIndexOf). Raw unit is
// "100 million yen" → ×100 to JPY millions. Literal "null" = not-yet-published month.
func parseBojCSV(csv string) []MacroPoint {
	out := []MacroPoint{}
	if strings.TrimSpace(csv) == "" {
		return out
	}
	hundred := decimal.NewFromInt(100)
	lines := strings.Split(csv, "\n")
	headerIdx := -1
	for i, l := range lines {
		if strings.HasPrefix(strings.Trim(l, "\r"), "SERIES_CODE,") {
			headerIdx = i
			break
		}
	}
	if headerIdx < 0 {
		return out
	}
	for i := headerIdx + 1; i < len(lines); i++ {
		line := strings.TrimSpace(strings.Trim(lines[i], "\r"))
		if line == "" {
			continue
		}
		last := strings.LastIndex(line, ",")
		if last <= 0 {
			continue
		}
		secondLast := strings.LastIndex(line[:last], ",")
		if secondLast < 0 {
			continue
		}
		surveyDates := strings.TrimSpace(line[secondLast+1 : last])
		valStr := strings.TrimSpace(line[last+1:])
		if valStr == "" || strings.EqualFold(valStr, "null") {
			continue
		}
		if len(surveyDates) != 6 {
			continue
		}
		digits := true
		for _, r := range surveyDates {
			if r < '0' || r > '9' {
				digits = false
				break
			}
		}
		if !digits {
			continue
		}
		date, err := time.Parse("20060102", surveyDates+"01")
		if err != nil {
			continue
		}
		v, err := decimal.NewFromString(valStr)
		if err != nil {
			continue
		}
		out = append(out, MacroPoint{Date: date, Value: v.Mul(hundred)})
	}
	return out
}

// ── chinadata.live (China M2 backbone, raw "100 Million CNY" → ×100) ─────────────────────

type ChinaDataLiveClient struct {
	http    *http.Client
	baseURL string
}

func NewChinaDataLiveClient() *ChinaDataLiveClient {
	return &ChinaDataLiveClient{http: macroHTTPClient(), baseURL: "https://chinadata.live"}
}

func (c *ChinaDataLiveClient) FetchM2() FetchFunc {
	return func(ctx context.Context) []MacroPoint {
		body, ok := getText(ctx, c.http, c.baseURL+"/api/v2/data/china-m2-money-supply", "Mozilla/5.0", nil)
		if !ok {
			slog.Warn("macro fetch: chinadata.live M2 failed")
			return []MacroPoint{}
		}
		return parseChinaDataLive(body)
	}
}

func parseChinaDataLive(body string) []MacroPoint {
	var resp struct {
		Success bool `json:"success"`
		Data    *struct {
			Data []struct {
				Date  string `json:"date"`
				Value string `json:"value"`
			} `json:"data"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil || !resp.Success || resp.Data == nil || resp.Data.Data == nil {
		return []MacroPoint{}
	}
	hundred := decimal.NewFromInt(100)
	out := []MacroPoint{}
	for _, p := range resp.Data.Data {
		if p.Date == "" || p.Value == "" || len(p.Date) != 7 {
			continue
		}
		date, err := time.Parse("2006-01-02", p.Date+"-01")
		if err != nil {
			continue
		}
		v, err := decimal.NewFromString(p.Value)
		if err != nil {
			continue
		}
		out = append(out, MacroPoint{Date: date, Value: v.Mul(hundred)})
	}
	return out
}

// ── ADR-068 tip sources (at most ONE point each, never history) ──────────────────────────

var (
	tradingECCoverage  = regexp.MustCompile(`"temporalCoverage"\s*:\s*"\d{4}-\d{2}-\d{2}/(\d{4})-(\d{2})-(\d{2})"`)
	tradingECHeadline  = regexp.MustCompile(`(?i)Money Supply M2 in China (?:increased|decreased|rose|fell) to\s+([0-9]+(?:[0-9,]*)(?:\.[0-9]+)?)\s*CNY Billion\s+in\s+([A-Z][a-z]+)`)
	yChartsObservation = regexp.MustCompile(`"value"\s*:\s*([0-9]+(?:\.[0-9]+)?)\s*,[^}]*?"observationDate"\s*:\s*"(\d{4})-(\d{2})-\d{2}"[^}]*?"priceCurrency"\s*:\s*"CNY"`)
	pbcIndex           = regexp.MustCompile(`href="(/goutongjiaoliu/113456/113469/\d+/index\.html)"[^>]*title="(\d{4})年(\d{1,2})月金融统计数据报告"`)
	pbcM2Value         = regexp.MustCompile(`广义货币[\(（]M2[\)）]余额\s*([0-9]+(?:\.[0-9]+)?)\s*万亿元`)
)

type TradingEconomicsClient struct {
	http    *http.Client
	baseURL string
}

func NewTradingEconomicsClient() *TradingEconomicsClient {
	return &TradingEconomicsClient{http: macroHTTPClient(), baseURL: "https://tradingeconomics.com"}
}

// FetchLatestM2 ports TradingEconomicsChinaM2Client: two anchors (JSON-LD temporalCoverage +
// headline sentence) must BOTH be present and their months must agree, else empty. Page quotes
// CNY Billion → ×1000 to CNY millions.
func (c *TradingEconomicsClient) FetchLatestM2() FetchFunc {
	return func(ctx context.Context) []MacroPoint {
		body, ok := getText(ctx, c.http, c.baseURL+"/china/money-supply-m2", browserUA,
			map[string]string{"Accept-Language": "en-US,en;q=0.9"})
		if !ok {
			slog.Warn("macro fetch: tradingeconomics tip failed")
			return []MacroPoint{}
		}
		return parseTradingECHTML(body)
	}
}

func parseTradingECHTML(html string) []MacroPoint {
	if strings.TrimSpace(html) == "" {
		return []MacroPoint{}
	}
	cov := tradingECCoverage.FindStringSubmatch(html)
	if cov == nil {
		return []MacroPoint{}
	}
	head := tradingECHeadline.FindStringSubmatch(html)
	if head == nil {
		return []MacroPoint{}
	}
	monthStart, err := time.Parse("2006-01-02", cov[1]+"-"+cov[2]+"-01")
	if err != nil {
		return []MacroPoint{}
	}
	v, err := decimal.NewFromString(strings.ReplaceAll(head[1], ",", ""))
	if err != nil {
		return []MacroPoint{}
	}
	if !strings.EqualFold(monthStart.Month().String(), head[2]) {
		slog.Warn("macro fetch: tradingeconomics month mismatch, rejecting")
		return []MacroPoint{}
	}
	return []MacroPoint{{Date: monthStart, Value: v.Mul(thousand)}}
}

type PbcClient struct {
	http    *http.Client
	baseURL string
}

func NewPbcClient() *PbcClient {
	return &PbcClient{http: macroHTTPClient(), baseURL: "https://www.pbc.gov.cn"}
}

// FetchLatestM2 ports PbcChinaM2Client: index page → latest monthly report link → M2 value in
// 万亿元 (trillion CNY) ×1,000,000 to CNY millions.
func (c *PbcClient) FetchLatestM2() FetchFunc {
	return func(ctx context.Context) []MacroPoint {
		idxBody, ok := getText(ctx, c.http, c.baseURL+"/goutongjiaoliu/113456/113469/index.html", browserUA,
			map[string]string{"Accept-Language": "zh-CN,zh;q=0.9,en;q=0.8"})
		if !ok || strings.TrimSpace(idxBody) == "" {
			slog.Warn("macro fetch: pbc tip index failed")
			return []MacroPoint{}
		}
		m := pbcIndex.FindStringSubmatch(idxBody)
		if m == nil {
			return []MacroPoint{}
		}
		repBody, ok := getText(ctx, c.http, c.baseURL+m[1], browserUA,
			map[string]string{"Accept-Language": "zh-CN,zh;q=0.9,en;q=0.8"})
		if !ok {
			return []MacroPoint{}
		}
		return parsePbcReport(repBody, m[2], m[3])
	}
}

func parsePbcReport(html, yearStr, monthStr string) []MacroPoint {
	if strings.TrimSpace(html) == "" {
		return []MacroPoint{}
	}
	m := pbcM2Value.FindStringSubmatch(html)
	if m == nil {
		return []MacroPoint{}
	}
	date, err := time.Parse("2006-1-02", yearStr+"-"+monthStr+"-01")
	if err != nil {
		return []MacroPoint{}
	}
	wanYi, err := decimal.NewFromString(m[1])
	if err != nil {
		return []MacroPoint{}
	}
	return []MacroPoint{{Date: date, Value: wanYi.Mul(decimal.NewFromInt(1000000))}}
}

type YChartsClient struct {
	http    *http.Client
	baseURL string
}

func NewYChartsClient() *YChartsClient {
	return &YChartsClient{http: macroHTTPClient(), baseURL: "https://ycharts.com"}
}

// FetchLatestM2 ports YChartsChinaM2Client: single JSON-LD observation (full precision,
// preferred over the rounded prose), CNY billions → ×1000 to CNY millions, dated month-start.
func (c *YChartsClient) FetchLatestM2() FetchFunc {
	return func(ctx context.Context) []MacroPoint {
		body, ok := getText(ctx, c.http, c.baseURL+"/indicators/china_m2_money_supply", browserUA,
			map[string]string{"Accept-Language": "en-US,en;q=0.9"})
		if !ok {
			slog.Warn("macro fetch: ycharts tip failed")
			return []MacroPoint{}
		}
		return parseYChartsHTML(body)
	}
}

func parseYChartsHTML(html string) []MacroPoint {
	if strings.TrimSpace(html) == "" {
		return []MacroPoint{}
	}
	m := yChartsObservation.FindStringSubmatch(html)
	if m == nil {
		return []MacroPoint{}
	}
	v, err := decimal.NewFromString(m[1])
	if err != nil {
		return []MacroPoint{}
	}
	date, err := time.Parse("2006-01-02", m[2]+"-"+m[3]+"-01")
	if err != nil {
		return []MacroPoint{}
	}
	return []MacroPoint{{Date: date, Value: v.Mul(thousand)}}
}

// ── Composition root helper ──────────────────────────────────────────────────────────────

// NewProdFetcher wires the real 8-client fan-out: 5 live series + 4 FRED FX legs + 3 tip-only
// sources. Order of Raw mirrors MacroDataService.refresh()'s fetch order (backbone before tips
// is enforced in Service.Refresh, not here).
func NewProdFetcher(fredAPIKey string) *Fetcher {
	fred := NewFredClient(fredAPIKey)
	ecb := NewEcbClient()
	boe := NewBoeClient()
	boj := NewBojClient()
	chinaLive := NewChinaDataLiveClient()
	tradingEC := NewTradingEconomicsClient()
	pbc := NewPbcClient()
	ycharts := NewYChartsClient()
	return &Fetcher{
		Raw: []SeriesFetch{
			{seriesWALCL, SourceFred, fred.Fetch(seriesWALCL)},
			{seriesWDTGAL, SourceFred, fred.Fetch(seriesWDTGAL)},
			{seriesRRPONTSYD, SourceFred, fred.Fetch(seriesRRPONTSYD)},
			{seriesM2SL, SourceFred, fred.Fetch(seriesM2SL)},
			{seriesM2EA, SourceECB, ecb.FetchM2()},
			{seriesM2UK, SourceBOE, boe.FetchM2()},
			{SeriesM2CN, SourceChinadata, chinaLive.FetchM2()},
			{SeriesM2JP, SourceBOJ, boj.FetchM2()},
		},
		FX: map[string]FetchFunc{
			seriesEXUSEU: fred.Fetch(seriesEXUSEU),
			seriesEXUSUK: fred.Fetch(seriesEXUSUK),
			seriesEXCHUS: fred.Fetch(seriesEXCHUS),
			seriesEXJPUS: fred.Fetch(seriesEXJPUS),
		},
		Tips: map[string]FetchFunc{
			SourceTradingEC: tradingEC.FetchLatestM2(),
			SourcePBC:       pbc.FetchLatestM2(),
			SourceYCharts:   ycharts.FetchLatestM2(),
		},
	}
}
