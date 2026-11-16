package trade

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// BTCUSDollarSwap is the coin-margined BTC perp, margined/settled in BTC — the only
// perp this bot trades (no USDT, ADR-090 amendment). BTC_USDT_SWAP is deliberately
// absent (the account holds BTC+USDC only; the Java @Deprecated constant is not
// ported — dead surface stays dead).
const BTCUSDollarSwap = "BTC-USD-SWAP"

// OkxConfig carries the OKX credentials + mode. Demo defaults true (a missing env
// var can never default the backend to live trading — ADR-090 §4 hard requirement).
type OkxConfig struct {
	BaseURL    string
	APIKey     string
	APISecret  string
	Passphrase string
	Demo       bool
}

// okxHTTPTimeout bounds one OKX call. Entry + stop-loss + emergency close + verify at 10 s each
// is 40 s, which fits the 45 s commit budget (ADR-114); it was 15 s before.
const okxHTTPTimeout = 10 * time.Second

// DefaultOkxBaseURL is the EEA endpoint (EEA accounts get 50119 on www.okx.com —
// verified live 2026-09-10; override via OKX_BASE_URL).
const DefaultOkxBaseURL = "https://eea.okx.com"

// OkxClientException is the fail-closed OKX error (code + message), the direct port
// of OkxClientException.
type OkxClientException struct {
	Code    string
	Message string
}

func (e *OkxClientException) Error() string { return e.Message }

// OKXClient is the minimal signed OKX surface (ADR-090 §4): equity, leverage, entry,
// stop, verify — plus the close/verify pair for the emergency path and ctVal for
// contract conversion. The interface (not the struct) is what LiveExecutionService
// depends on, so tests assert the OFF-flag with a fake that records zero calls.
type OKXClient interface {
	GetEquityUsdt() (decimal.Decimal, error)
	SetLeverage(instID string, leverage int) error
	GetContractValue(instID string) (decimal.Decimal, error)
	PlaceMarketOrder(instID, side, szContracts string) (string, error)
	PlaceStopLossAlgo(instID, closeSide, szContracts string, triggerPx decimal.Decimal) (string, error)
	GetPositionContracts(instID string) (decimal.Decimal, error)
	ClosePositionMarket(instID, ourSide string) error
}

// OkxTradingClient is the Go port of OkxTradingClient: HMAC-SHA256-signed OKX API v5
// calls with per-request headers, a browser User-Agent (Cloudflare rejects the
// default Go UA), 10s total timeout, and fail-closed error mapping.
type OkxTradingClient struct {
	http    *http.Client
	baseURL string
	apiKey  string
	secret  string
	pass    string
	demo    bool
}

// NewOkxTradingClient wires the client. Base URL is constructor-injected (the
// ForexFactorySyncServiceIT WireMock URL-override pattern works for this client —
func NewOkxTradingClient(cfg OkxConfig) *OkxTradingClient {
	base := strings.TrimRight(cfg.BaseURL, "/")
	if base == "" {
		base = DefaultOkxBaseURL
	}
	return &OkxTradingClient{
		http:    &http.Client{Timeout: okxHTTPTimeout},
		baseURL: base, apiKey: cfg.APIKey, secret: cfg.APISecret,
		pass: cfg.Passphrase, demo: cfg.Demo,
	}
}

// OkxSign ports OkxSigner.sign: HMAC-SHA256(timestamp + method + requestPath + body),
// Base64-encoded, for the OK-ACCESS-SIGN header. Pure function, unit-testable.
func OkxSign(secret, timestamp, method, requestPath, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(timestamp + method + requestPath + body))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// okxTimestamp formats now as OKX expects (ISO-8601 UTC with millis).
func okxTimestamp(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z")
}

func (c *OkxTradingClient) signedHeaders(ts, signature string) http.Header {
	h := http.Header{}
	h.Set("OK-ACCESS-KEY", c.apiKey)
	h.Set("OK-ACCESS-SIGN", signature)
	h.Set("OK-ACCESS-TIMESTAMP", ts)
	h.Set("OK-ACCESS-PASSPHRASE", c.pass)
	h.Set("Content-Type", "application/json")
	if c.demo {
		h.Set("x-simulated-trading", "1")
	}
	h.Set("User-Agent",
		"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36")
	return h
}

func (c *OkxTradingClient) signedGet(path, query string) (map[string]any, error) {
	requestPath := path
	if strings.TrimSpace(query) != "" {
		requestPath = path + "?" + query
	}
	ts := okxTimestamp(time.Now())
	sig := OkxSign(c.secret, ts, "GET", requestPath, "")
	req, err := http.NewRequest(http.MethodGet, c.baseURL+requestPath, nil)
	if err != nil {
		return nil, &OkxClientException{Code: "TRANSPORT", Message: fmt.Sprintf("OKX GET %s failed: %v", requestPath, err)}
	}
	req.Header = c.signedHeaders(ts, sig)
	return c.do(req, requestPath)
}

func (c *OkxTradingClient) signedPost(path, jsonBody string) (map[string]any, error) {
	ts := okxTimestamp(time.Now())
	sig := OkxSign(c.secret, ts, "POST", path, jsonBody)
	req, err := http.NewRequest(http.MethodPost, c.baseURL+path, bytes.NewBufferString(jsonBody))
	if err != nil {
		return nil, &OkxClientException{Code: "TRANSPORT", Message: fmt.Sprintf("OKX POST %s failed: %v", path, err)}
	}
	req.Header = c.signedHeaders(ts, sig)
	return c.do(req, path)
}

func (c *OkxTradingClient) do(req *http.Request, requestPath string) (map[string]any, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, &OkxClientException{Code: "TRANSPORT", Message: fmt.Sprintf("OKX %s failed: %v", requestPath, err)}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, &OkxClientException{Code: "TRANSPORT", Message: fmt.Sprintf("OKX %s read failed: %v", requestPath, err)}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		slog.Warn("OKX call failed closed", "path", requestPath, "http", resp.StatusCode, "body", string(body))
		return nil, &OkxClientException{Code: fmt.Sprintf("HTTP_%d", resp.StatusCode),
			Message: fmt.Sprintf("OKX %s failed closed: http=%d", requestPath, resp.StatusCode)}
	}
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, &OkxClientException{Code: "PARSE", Message: fmt.Sprintf("OKX %s returned non-JSON body", requestPath)}
	}
	if code(root) != "0" {
		msg, _ := root["msg"].(string)
		if msg == "" {
			msg = "OKX error"
		}
		slog.Warn("OKX call rejected", "path", requestPath, "code", code(root), "msg", msg)
		return nil, &OkxClientException{Code: code(root),
			Message: fmt.Sprintf("OKX %s rejected: [%s] %s", requestPath, code(root), msg)}
	}
	return root, nil
}

func code(root map[string]any) string {
	if c, ok := root["code"].(string); ok {
		return c
	}
	return "?"
}

func firstData(root map[string]any, purpose string) (map[string]any, error) {
	data, ok := root["data"].([]any)
	if !ok || len(data) == 0 {
		return nil, &OkxClientException{Code: code(root),
			Message: fmt.Sprintf("OKX response carries no data for %s", purpose)}
	}
	m, ok := data[0].(map[string]any)
	if !ok {
		return nil, &OkxClientException{Code: code(root),
			Message: fmt.Sprintf("OKX response carries no data for %s", purpose)}
	}
	return m, nil
}

func textAsDecimal(m map[string]any, field string) *decimal.Decimal {
	raw, ok := m[field].(string)
	if !ok || strings.TrimSpace(raw) == "" {
		return nil
	}
	d, err := decimal.NewFromString(strings.TrimSpace(raw))
	if err != nil {
		return nil
	}
	return &d
}

// GetEquityUsdt reads totalEq (holding-agnostic portfolio USD value — works with
// zero USDT), falling back to the USDT detail leg.
func (c *OkxTradingClient) GetEquityUsdt() (decimal.Decimal, error) {
	root, err := c.signedGet("/api/v5/account/balance", "")
	if err != nil {
		return decimal.Zero, err
	}
	data, err := firstData(root, "balance")
	if err != nil {
		return decimal.Zero, err
	}
	if totalEq := textAsDecimal(data, "totalEq"); totalEq != nil {
		return *totalEq, nil
	}
	if details, ok := data["details"].([]any); ok {
		for _, d := range details {
			if m, ok := d.(map[string]any); ok {
				if ccy, _ := m["ccy"].(string); strings.EqualFold(ccy, "USDT") {
					if eq := textAsDecimal(m, "eq"); eq != nil {
						return *eq, nil
					}
				}
			}
		}
	}
	return decimal.Zero, &OkxClientException{Code: code(root), Message: "OKX balance response carries no USDT equity"}
}

// SetLeverage sets isolated-margin leverage pre-entry.
func (c *OkxTradingClient) SetLeverage(instID string, leverage int) error {
	_, err := c.signedPost("/api/v5/account/set-leverage",
		fmt.Sprintf(`{"instId":%q,"lever":%q,"mgnMode":"isolated"}`, instID, fmt.Sprintf("%d", leverage)))
	return err
}

// PlaceMarketOrder places an isolated-margin market entry, returning the ordId.
func (c *OkxTradingClient) PlaceMarketOrder(instID, side, szContracts string) (string, error) {
	root, err := c.signedPost("/api/v5/trade/order",
		fmt.Sprintf(`{"instId":%q,"tdMode":"isolated","side":%q,"ordType":"market","sz":%q}`,
			instID, side, szContracts))
	if err != nil {
		return "", err
	}
	data, err := firstData(root, "order")
	if err != nil {
		return "", err
	}
	ordID, _ := data["ordId"].(string)
	if strings.TrimSpace(ordID) == "" {
		return "", &OkxClientException{Code: code(root), Message: "OKX order response carries no ordId"}
	}
	return ordID, nil
}

// PlaceStopLossAlgo places the exchange-side conditional SL at entry, returning the
// algoId. Field set must be verified live on coin-margined swaps (demo gate could
// not run — key 50119); the first live fill stays minimum size with SL-fill watched.
func (c *OkxTradingClient) PlaceStopLossAlgo(instID, closeSide, szContracts string, triggerPx decimal.Decimal) (string, error) {
	root, err := c.signedPost("/api/v5/trade/order-algo",
		fmt.Sprintf(`{"instId":%q,"tdMode":"isolated","side":%q,"ordType":"conditional","sz":%q,"slTriggerPx":%q,"slOrdPx":"-1"}`,
			instID, closeSide, szContracts, triggerPx.String()))
	if err != nil {
		return "", err
	}
	data, err := firstData(root, "stop-loss")
	if err != nil {
		return "", err
	}
	algoID, _ := data["algoId"].(string)
	if strings.TrimSpace(algoID) == "" {
		return "", &OkxClientException{Code: code(root), Message: "OKX algo order response carries no algoId"}
	}
	return algoID, nil
}

// GetPositionContracts nets the open position size for the instrument.
func (c *OkxTradingClient) GetPositionContracts(instID string) (decimal.Decimal, error) {
	root, err := c.signedGet("/api/v5/account/positions", "instId="+instID)
	if err != nil {
		return decimal.Zero, err
	}
	net := decimal.Zero
	if data, ok := root["data"].([]any); ok {
		for _, d := range data {
			m, ok := d.(map[string]any)
			if !ok {
				continue
			}
			if id, _ := m["instId"].(string); id != instID {
				continue
			}
			if posStr, _ := m["pos"].(string); strings.TrimSpace(posStr) != "" {
				pos, err := decimal.NewFromString(strings.TrimSpace(posStr))
				if err != nil {
					return decimal.Zero, &OkxClientException{Code: code(root),
						Message: fmt.Sprintf("OKX position quantity not numeric: %s", posStr)}
				}
				net = net.Add(pos)
			}
		}
	}
	return net, nil
}

// ClosePositionMarket market-closes the whole open position (reduceOnly), no-op
// when already flat.
func (c *OkxTradingClient) ClosePositionMarket(instID, ourSide string) error {
	closeSide := "buy"
	if strings.EqualFold(ourSide, "LONG") {
		closeSide = "sell"
	}
	open, err := c.GetPositionContracts(instID)
	if err != nil {
		return err
	}
	if open.Abs().Cmp(decimal.Zero) == 0 {
		return nil
	}
	_, err = c.signedPost("/api/v5/trade/order",
		fmt.Sprintf(`{"instId":%q,"tdMode":"isolated","side":%q,"ordType":"market","sz":%q,"reduceOnly":true}`,
			instID, closeSide, open.Abs().String()))
	return err
}

// GetContractValue reads ctVal live from /public/instruments — never hardcoded
// (ADR-090 §4 open item).
func (c *OkxTradingClient) GetContractValue(instID string) (decimal.Decimal, error) {
	root, err := c.signedGet("/api/v5/public/instruments", "instType=SWAP&instId="+instID)
	if err != nil {
		return decimal.Zero, err
	}
	data, err := firstData(root, "instruments")
	if err != nil {
		return decimal.Zero, err
	}
	ctVal, _ := data["ctVal"].(string)
	if strings.TrimSpace(ctVal) == "" {
		return decimal.Zero, &OkxClientException{Code: code(root),
			Message: fmt.Sprintf("OKX instruments response carries no ctVal for %s", instID)}
	}
	v, err := decimal.NewFromString(strings.TrimSpace(ctVal))
	if err != nil {
		return decimal.Zero, &OkxClientException{Code: code(root),
			Message: fmt.Sprintf("OKX ctVal not numeric: %s", ctVal)}
	}
	return v, nil
}
