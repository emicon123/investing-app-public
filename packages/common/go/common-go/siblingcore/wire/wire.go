// Package wire holds the JSON wire structs portfolioB and portfolioC share for the endpoints whose shapes
// differ only by the `portfolio` field (task 180 wave 6, ADR-113): deposits, the deposits XIRR,
// position transfers and the purchase history. Response numbers are float64 (the generated
// OpenAPI models were float32, too narrow for NUMERIC); request numbers decode as decimals; field
// order is each app's existing wire order, so the bytes are unchanged. Position, rebalancing-calculate and account-cash wire stay per app (their
// units, allocations and table shapes differ).
package wire

import (
	"time"

	"github.com/shopspring/decimal"

	"investing-app/common-go/decx"
	"investing-app/common-go/rebalancecalc"
	"investing-app/common-go/siblingcore"
	"investing-app/common-go/siblingcore/transfer"
	"investing-app/common-go/xirr"
)

// Deposit is one deposit; portfolio is omitted for a WholeBook app.
type Deposit struct {
	ID          int64     `json:"id"`
	Portfolio   string    `json:"portfolio,omitempty"`
	DepositDate string    `json:"depositDate"`
	Amount      float64   `json:"amount"`
	Comment     *string   `json:"comment,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}

// FromDeposit maps a siblingcore deposit (dates as YYYY-MM-DD, createdAt in UTC).
func FromDeposit(d siblingcore.Deposit) Deposit {
	return Deposit{
		ID: d.ID, Portfolio: string(d.Portfolio), DepositDate: d.DepositDate.Format("2006-01-02"),
		Amount: decx.ToFloat(d.Amount), Comment: d.Comment, CreatedAt: d.CreatedAt.UTC(),
	}
}

// XirrResult is GET /deposits/xirr's data.
type XirrResult struct {
	XirrPct          *float64  `json:"xirrPct,omitempty"`
	TotalInvestedPln float64   `json:"totalInvestedPln"`
	CurrentValuePln  float64   `json:"currentValuePln"`
	SimpleReturnPct  *float64  `json:"simpleReturnPct,omitempty"`
	YearsElapsed     *float64  `json:"yearsElapsed,omitempty"`
	ComputedAt       time.Time `json:"computedAt"`
}

// FromXirr maps the XIRR summary computed at computedAt.
func FromXirr(r xirr.Result, computedAt time.Time) XirrResult {
	return XirrResult{
		XirrPct: decx.ToFloatPtr(r.XirrPct), TotalInvestedPln: decx.ToFloat(r.TotalInvestedPln),
		CurrentValuePln: decx.ToFloat(r.CurrentValuePln), SimpleReturnPct: decx.ToFloatPtr(r.SimpleReturnPct),
		YearsElapsed: decx.ToFloatPtr(r.YearsElapsed), ComputedAt: computedAt.UTC(),
	}
}

// TransferLeg is one leg of POST /position-transfers' response; accountType is the bucket of a
// multi-bucket app (portfolioB) and absent for a single-bucket one (portfolioC).
type TransferLeg struct {
	Ticker      string  `json:"ticker"`
	AccountType *string `json:"accountType,omitempty"`
	Units       float64 `json:"units"`
	PricePln    float64 `json:"pricePln"`
}

// TransferLegInput is TransferLeg as the request carries it: units and pricePln decode as
// decimals (task 182 / F3.4), so no float64 sits on the money input path. The wire still takes
// JSON numbers; responses keep TransferLeg (decimal.Decimal would marshal as a string).
type TransferLegInput struct {
	Ticker      string          `json:"ticker"`
	AccountType *string         `json:"accountType,omitempty"`
	Units       decimal.Decimal `json:"units"`
	PricePln    decimal.Decimal `json:"pricePln"`
}

// TransferRequest is POST /position-transfers' body (a WholeBook app ignores portfolio).
type TransferRequest struct {
	Portfolio string             `json:"portfolio"`
	Sold      TransferLegInput   `json:"sold"`
	Buys      []TransferLegInput `json:"buys"`
}

// ToRequest resolves the portfolio (400 for an unknown one) and each leg's bucket: the single
// bucket of a single-bucket app, otherwise the leg's accountType (missing = the empty bucket,
// which the transfer service rejects with a 400).
func (r TransferRequest) ToRequest(cfg siblingcore.Config) (transfer.Request, error) {
	p, err := cfg.ResolvePortfolio("portfolio", r.Portfolio)
	if err != nil {
		return transfer.Request{}, err
	}
	buys := make([]transfer.Leg, 0, len(r.Buys))
	for _, b := range r.Buys {
		buys = append(buys, b.toLeg(cfg))
	}
	return transfer.Request{Portfolio: p, Sold: r.Sold.toLeg(cfg), Buys: buys}, nil
}

func (l TransferLegInput) toLeg(cfg siblingcore.Config) transfer.Leg {
	bucket := siblingcore.SingleBucket
	if !singleBucket(cfg) && l.AccountType != nil {
		bucket = siblingcore.Bucket(*l.AccountType)
	}
	return transfer.Leg{Ticker: l.Ticker, Bucket: bucket, Units: l.Units, PricePln: l.PricePln}
}

// TransferResult is POST /position-transfers' data: the recorded request plus its transfer id.
type TransferResult struct {
	TransferID string        `json:"transferId"`
	Sold       TransferLeg   `json:"sold"`
	Buys       []TransferLeg `json:"buys"`
}

// FromTransfer maps a recorded transfer (accountType echoed for a multi-bucket app only).
func FromTransfer(r transfer.Result, cfg siblingcore.Config) TransferResult {
	buys := make([]TransferLeg, 0, len(r.Buys))
	for _, b := range r.Buys {
		buys = append(buys, fromLeg(b, cfg))
	}
	return TransferResult{TransferID: r.TransferID.String(), Sold: fromLeg(r.Sold, cfg), Buys: buys}
}

func fromLeg(l transfer.Leg, cfg siblingcore.Config) TransferLeg {
	w := TransferLeg{Ticker: l.Ticker, Units: decx.ToFloat(l.Units), PricePln: decx.ToFloat(l.PricePln)}
	if !singleBucket(cfg) {
		accountType := string(l.Bucket)
		w.AccountType = &accountType
	}
	return w
}

func singleBucket(cfg siblingcore.Config) bool {
	return len(cfg.Buckets) == 1 && cfg.Buckets[0].Bucket == siblingcore.SingleBucket
}

// PurchaseLot is one lot of a purchase session, valued at today's cached price.
type PurchaseLot struct {
	Ticker          string   `json:"ticker"`
	Name            *string  `json:"name,omitempty"`
	Units           float64  `json:"units"`
	PricePln        float64  `json:"pricePln"`
	CostPln         float64  `json:"costPln"`
	CurrentPricePln *float64 `json:"currentPricePln,omitempty"`
	CurrentValuePln *float64 `json:"currentValuePln,omitempty"`
	GainPln         *float64 `json:"gainPln,omitempty"`
	GainPct         *float64 `json:"gainPct,omitempty"`
	PriceAvailable  bool     `json:"priceAvailable"`
}

// PurchaseSession is one confirmed purchase session.
type PurchaseSession struct {
	SessionID            string        `json:"sessionId"`
	PurchasedAt          string        `json:"purchasedAt"`
	TotalCostPln         float64       `json:"totalCostPln"`
	TotalCurrentValuePln float64       `json:"totalCurrentValuePln"`
	TotalGainPln         float64       `json:"totalGainPln"`
	TotalGainPct         *float64      `json:"totalGainPct,omitempty"`
	Lots                 []PurchaseLot `json:"lots"`
}

// PurchaseHistory is GET /purchases/history's data.
type PurchaseHistory struct {
	Sessions []PurchaseSession `json:"sessions"`
}

// FromSessions maps the history (nil = an empty list on the wire).
func FromSessions(sessions []rebalancecalc.PurchaseSessionResult) []PurchaseSession {
	out := make([]PurchaseSession, 0, len(sessions))
	for _, s := range sessions {
		lots := make([]PurchaseLot, 0, len(s.Lots))
		for _, l := range s.Lots {
			lots = append(lots, PurchaseLot{
				Ticker: l.Ticker, Name: l.Name, Units: decx.ToFloat(l.Units), PricePln: decx.ToFloat(l.PricePln),
				CostPln: decx.ToFloat(l.CostPln), CurrentPricePln: decx.ToFloatPtr(l.CurrentPricePln),
				CurrentValuePln: decx.ToFloatPtr(l.CurrentValuePln), GainPln: decx.ToFloatPtr(l.GainPln),
				GainPct: decx.ToFloatPtr(l.GainPct), PriceAvailable: l.PriceAvailable,
			})
		}
		out = append(out, PurchaseSession{
			SessionID: s.SessionID.String(), PurchasedAt: s.PurchasedAt.Format("2006-01-02"),
			TotalCostPln: decx.ToFloat(s.TotalCostPln), TotalCurrentValuePln: decx.ToFloat(s.TotalCurrentValuePln),
			TotalGainPln: decx.ToFloat(s.TotalGainPln), TotalGainPct: decx.ToFloatPtr(s.TotalGainPct), Lots: lots,
		})
	}
	return out
}
