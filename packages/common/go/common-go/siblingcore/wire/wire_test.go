package wire

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"investing-app/common-go/httpx"
	"investing-app/common-go/rebalancecalc"
	"investing-app/common-go/siblingcore"
	"investing-app/common-go/siblingcore/internal/coretest"
	"investing-app/common-go/siblingcore/transfer"
	"investing-app/common-go/xirr"
)

func js(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

var at = time.Date(2026, 11, 5, 10, 0, 0, 0, time.FixedZone("CET", 3600))

func TestFromDeposit_PortfolioOnlyForANamedBook(t *testing.T) {
	c := "c"
	d := siblingcore.Deposit{ID: 7, DepositDate: coretest.Day(2026, 1, 10), Amount: coretest.D("150.25"), Comment: &c, CreatedAt: at}
	if got := js(t, FromDeposit(d)); got != `{"id":7,"depositDate":"2026-01-10","amount":150.25,"comment":"c","createdAt":"2026-11-05T09:00:00Z"}` {
		t.Fatalf("whole book: %s", got)
	}
	d.Portfolio, d.Comment = "FIXED", nil
	if got := js(t, FromDeposit(d)); got != `{"id":7,"portfolio":"FIXED","depositDate":"2026-01-10","amount":150.25,"createdAt":"2026-11-05T09:00:00Z"}` {
		t.Fatalf("named: %s", got)
	}
}

func TestFromXirr(t *testing.T) {
	pct := coretest.D("0.05")
	got := js(t, FromXirr(xirr.Result{XirrPct: &pct, TotalInvestedPln: coretest.D("100"), CurrentValuePln: coretest.D("110")}, at))
	if got != `{"xirrPct":0.05,"totalInvestedPln":100,"currentValuePln":110,"computedAt":"2026-11-05T09:00:00Z"}` {
		t.Fatalf("%s", got)
	}
}

func TestTransfer_RoundTripPerConfig(t *testing.T) {
	body := `{"portfolio":"variable","sold":{"ticker":"S","accountType":"IKE_TATA","units":4,"pricePln":100},"buys":[{"ticker":"B","units":8,"pricePln":25}]}`
	var req TransferRequest
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatal(err)
	}
	// portfolioB: buckets from accountType (a missing one stays empty for the service's D1 400),
	// the body portfolio is ignored.
	l, err := req.ToRequest(coretest.PortfolioB)
	if err != nil || l.Portfolio != siblingcore.WholeBook || l.Sold.Bucket != "IKE_TATA" || l.Buys[0].Bucket != "" || !l.Sold.Units.Equal(coretest.D("4")) {
		t.Fatalf("portfolioB %+v %v", l, err)
	}
	// portfolioC: the single bucket whatever accountType says, the portfolio resolved.
	m, err := req.ToRequest(coretest.PortfolioC)
	if err != nil || m.Portfolio != "VARIABLE" || m.Sold.Bucket != siblingcore.SingleBucket || m.Buys[0].Bucket != siblingcore.SingleBucket {
		t.Fatalf("portfolioC %+v %v", m, err)
	}
	req.Portfolio = "BOGUS"
	if _, err := req.ToRequest(coretest.PortfolioC); !errors.As(err, new(*httpx.AppError)) {
		t.Fatalf("D2: %v", err)
	}

	id := uuid.MustParse("6f1c2b8e-3a4d-4e5f-8a9b-0c1d2e3f4a5b")
	res := transfer.Result{TransferID: id, Sold: transfer.Leg{Ticker: "S", Bucket: "IKE_TATA", Units: coretest.D("4"), PricePln: coretest.D("100")},
		Buys: []transfer.Leg{{Ticker: "B", Bucket: "INNE", Units: coretest.D("8"), PricePln: coretest.D("25")}}}
	if got := js(t, FromTransfer(res, coretest.PortfolioB)); got != `{"transferId":"`+id.String()+`","sold":{"ticker":"S","accountType":"IKE_TATA","units":4,"pricePln":100},"buys":[{"ticker":"B","accountType":"INNE","units":8,"pricePln":25}]}` {
		t.Fatalf("portfolioB result %s", got)
	}
	res.Sold.Bucket, res.Buys[0].Bucket = siblingcore.SingleBucket, siblingcore.SingleBucket
	if got := js(t, FromTransfer(res, coretest.PortfolioC)); got != `{"transferId":"`+id.String()+`","sold":{"ticker":"S","units":4,"pricePln":100},"buys":[{"ticker":"B","units":8,"pricePln":25}]}` {
		t.Fatalf("portfolioC result %s", got)
	}
}

func TestFromSessions(t *testing.T) {
	name := "N"
	gain := decimal.NewFromInt(5)
	got := js(t, PurchaseHistory{Sessions: FromSessions([]rebalancecalc.PurchaseSessionResult{{
		SessionID: uuid.Nil, PurchasedAt: coretest.Day(2026, 3, 1), TotalCostPln: coretest.D("100"), TotalCurrentValuePln: coretest.D("105"), TotalGainPln: gain,
		Lots: []rebalancecalc.PurchaseLotResult{{Ticker: "T", Name: &name, Units: coretest.D("1"), PricePln: coretest.D("100"), CostPln: coretest.D("100"), PriceAvailable: true, GainPln: &gain}},
	}})})
	want := `{"sessions":[{"sessionId":"00000000-0000-0000-0000-000000000000","purchasedAt":"2026-03-01","totalCostPln":100,"totalCurrentValuePln":105,"totalGainPln":5,"lots":[{"ticker":"T","name":"N","units":1,"pricePln":100,"costPln":100,"gainPln":5,"priceAvailable":true}]}]}`
	if got != want {
		t.Fatalf("\n got %s\nwant %s", got, want)
	}
	if js(t, PurchaseHistory{Sessions: FromSessions(nil)}) != `{"sessions":[]}` {
		t.Fatal("no sessions is an empty list")
	}
}

// TestTransferRequest_DecodesNumbersExactly pins task 182 B5 (F3.4): the request legs decode
// JSON numbers as decimals, so 18 significant digits and typed PLN amounts reach the service
// unchanged (float64 kept ~17 digits).
func TestTransferRequest_DecodesNumbersExactly(t *testing.T) {
	body := `{"portfolio":"VARIABLE","sold":{"ticker":"S","units":0.123456789012345678,"pricePln":1234567.8912},` +
		`"buys":[{"ticker":"B","units":12345678.123456789012345678,"pricePln":0.000000012345678901}]}`
	var req TransferRequest
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatal(err)
	}
	r, err := req.ToRequest(coretest.PortfolioC)
	if err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		got  decimal.Decimal
		want string
	}{
		"sold.units":    {r.Sold.Units, "0.123456789012345678"},
		"sold.pricePln": {r.Sold.PricePln, "1234567.8912"},
		"buy.units":     {r.Buys[0].Units, "12345678.123456789012345678"},
		"buy.pricePln":  {r.Buys[0].PricePln, "0.000000012345678901"},
	} {
		if c.got.String() != c.want {
			t.Errorf("%s = %s, want exactly %s", name, c.got, c.want)
		}
	}
}
