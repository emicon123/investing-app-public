package rebalancing

import (
	"context"
	"errors"
	"testing"

	"github.com/shopspring/decimal"

	"investing-app/common-go/httpx"
	"investing-app/common-go/rebalancecalc"
	screbalancing "investing-app/common-go/siblingcore/rebalancing"
)

func TestFunding_VariableNeedsAnAmountAndTheOverrideIsUsedVerbatim(t *testing.T) {
	_, err := depositAmount(context.Background(), nil, "VARIABLE", nil)
	var e *httpx.AppError
	if !errors.As(err, &e) || e.Status != 400 || e.Field != "depositAmountPln" {
		t.Fatalf("VARIABLE without amount: %v", err)
	}
	v := decimal.RequireFromString("123.45")
	got, err := depositAmount(context.Background(), nil, "VARIABLE", &v)
	if err != nil || !got.Equal(v) {
		t.Fatalf("override: %v %v", got, err)
	}
}

func TestFunding_CheckAndSettle(t *testing.T) {
	f := NewFunding()
	for _, amount := range []*decimal.Decimal{nil, ptr("0"), ptr("-1")} {
		if err := f.Check(screbalancing.ConfirmRequest{DepositAmountPln: amount}); err == nil {
			t.Fatalf("amount %v must be rejected", amount)
		}
	}
	req := screbalancing.ConfirmRequest{DepositAmountPln: ptr("400")}
	if err := f.Check(req); err != nil {
		t.Fatal(err)
	}
	got, err := f.Settle(context.Background(), nil, req, nil)
	if err != nil || !got.Equal(decimal.NewFromInt(400)) {
		t.Fatalf("settle records the request amount: %v %v", got, err)
	}
}

func TestIdentityAllocate_NeverSetsAccountAllocations(t *testing.T) {
	in := []rebalancecalc.BuyListItem{{Ticker: "A", AllocatedPln: decimal.NewFromInt(10)}}
	if out := identityAllocate(in, nil); len(out) != 1 || out[0].AccountAllocations != nil {
		t.Fatalf("%+v", out)
	}
}

func ptr(s string) *decimal.Decimal { d := decimal.RequireFromString(s); return &d }
