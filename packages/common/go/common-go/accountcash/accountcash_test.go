package accountcash

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/shopspring/decimal"

	"investing-app/common-go/httpx"
)

type fakeStore struct {
	snap  Snapshot
	err   error
	saved []Snapshot
}

func (f *fakeStore) Load(_ context.Context, _ string) (Snapshot, error) {
	if f.err != nil {
		return Snapshot{}, f.err
	}
	return f.snap, nil
}

func (f *fakeStore) Save(_ context.Context, _ string, snap Snapshot) error {
	f.saved = append(f.saved, snap)
	f.snap = snap
	return nil
}

func portfolioBService(seed map[string]decimal.Decimal) (*Service, *fakeStore) {
	st := &fakeStore{snap: Snapshot{Balances: seed}}
	svc := NewService(st, func(_ string) string { return "Account cash snapshot not found (id=1)" },
		"ikeTata", "ikeMama", "ikzeTata", "ikzeMama")
	return svc, st
}

func TestGetCurrent(t *testing.T) {
	svc, _ := portfolioBService(map[string]decimal.Decimal{
		"ikeTata": decimal.RequireFromString("1234.56"),
	})
	snap, err := svc.GetCurrent(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if got := snap.Balances["ikeTata"].StringFixed(2); got != "1234.56" {
		t.Fatalf("expected 1234.56, got %s", got)
	}

	missing := NewService(&fakeStore{err: ErrNotFound},
		func(_ string) string { return "Account cash snapshot not found (id=1)" }, "ikeTata")
	_, err = missing.GetCurrent(context.Background(), "")
	if err == nil {
		t.Fatal("expected 404 for missing row")
	}
	var appErr *httpx.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected *httpx.AppError, got %T", err)
	}
	if appErr.Status != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", appErr.Status)
	}
}

func TestUpdateManual_StampsReason(t *testing.T) {
	svc, st := portfolioBService(map[string]decimal.Decimal{})
	snap, err := svc.UpdateManual(context.Background(), "", map[string]decimal.Decimal{
		"ikeTata": decimal.RequireFromString("100"),
		"ikeMama": decimal.RequireFromString("200"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if snap.UpdateReason != ReasonManual {
		t.Fatalf("expected MANUAL, got %s", snap.UpdateReason)
	}
	if snap.Balances["ikeTata"].String() != "100" || snap.Balances["ikzeMama"].String() != "0" {
		t.Fatalf("expected overwrite + zero-fill, got %+v", snap.Balances)
	}
	if len(st.saved) != 1 {
		t.Fatal("expected one save")
	}
}

func TestDeduct_FloorsAtZero(t *testing.T) {
	svc, _ := portfolioBService(map[string]decimal.Decimal{
		"ikeTata": decimal.RequireFromString("50"),
		"ikeMama": decimal.RequireFromString("200"),
	})
	snap, err := svc.Deduct(context.Background(), "", map[string]decimal.Decimal{
		"ikeTata": decimal.RequireFromString("80"), // over-deduct → floor 0
		"ikeMama": decimal.RequireFromString("30"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if snap.UpdateReason != ReasonPostPurchase {
		t.Fatalf("expected POST_PURCHASE, got %s", snap.UpdateReason)
	}
	if !snap.Balances["ikeTata"].IsZero() {
		t.Fatalf("expected floor 0, got %s", snap.Balances["ikeTata"])
	}
	if snap.Balances["ikeMama"].String() != "170" {
		t.Fatalf("expected 170, got %s", snap.Balances["ikeMama"])
	}
}

func TestCheckNonNegative(t *testing.T) {
	if err := CheckNonNegative(map[string]decimal.Decimal{"balancePln": decimal.RequireFromString("1")}); err != nil {
		t.Fatal(err)
	}
	if err := CheckNonNegative(map[string]decimal.Decimal{"balancePln": decimal.RequireFromString("-0.01")}); err == nil {
		t.Fatal("expected 400 for negative amount")
	}
}
