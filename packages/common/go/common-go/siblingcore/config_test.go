package siblingcore_test

import (
	"errors"
	"strings"
	"testing"

	"investing-app/common-go/httpx"
	"investing-app/common-go/siblingcore"
	"investing-app/common-go/siblingcore/internal/coretest"
)

func TestConfig_BothAppConfigsValidate(t *testing.T) {
	for name, cfg := range map[string]siblingcore.Config{"portfolioB": coretest.PortfolioB, "portfolioC": coretest.PortfolioC} {
		if err := cfg.Validate(); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestConfig_ValidateRejectsMalformed(t *testing.T) {
	one := []siblingcore.BucketSpec{{Bucket: siblingcore.SingleBucket, Field: "units", TracksLots: true}}
	cases := map[string]siblingcore.Config{
		"no portfolios":          {Buckets: one},
		"duplicate portfolio":    {Portfolios: []siblingcore.Portfolio{"A", "A"}, PortfolioEnum: "x.P", Buckets: one},
		"whole book mixed":       {Portfolios: []siblingcore.Portfolio{siblingcore.WholeBook, "A"}, PortfolioEnum: "x.P", Buckets: one},
		"named without enum":     {Portfolios: []siblingcore.Portfolio{"A"}, Buckets: one},
		"no buckets":             {Portfolios: []siblingcore.Portfolio{siblingcore.WholeBook}},
		"duplicate bucket":       {Portfolios: []siblingcore.Portfolio{siblingcore.WholeBook}, Buckets: []siblingcore.BucketSpec{{Bucket: "A", Field: "a"}, {Bucket: "A", Field: "b"}}},
		"single bucket mixed":    {Portfolios: []siblingcore.Portfolio{siblingcore.WholeBook}, Buckets: []siblingcore.BucketSpec{{Bucket: siblingcore.SingleBucket, Field: "u"}, {Bucket: "A", Field: "a"}}},
		"bucket without a field": {Portfolios: []siblingcore.Portfolio{siblingcore.WholeBook}, Buckets: []siblingcore.BucketSpec{{Bucket: "A"}}},
	}
	for name, cfg := range cases {
		if err := cfg.Validate(); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func assertValidation(t *testing.T, err error, field, message string) {
	t.Helper()
	var appErr *httpx.AppError
	if !errors.As(err, &appErr) || appErr.Status != 400 || appErr.Field != field || appErr.Message != message {
		t.Fatalf("expected 400 %s %q, got %#v", field, message, err)
	}
}

func TestConfig_WholeBookResolvesAnyRawPortfolioAndRejectsANamedOne(t *testing.T) {
	for _, raw := range []string{"", "VARIABLE", "anything"} {
		p, err := coretest.PortfolioB.ResolvePortfolio("portfolio", raw)
		if err != nil || p != siblingcore.WholeBook {
			t.Fatalf("raw %q: got %q, %v", raw, p, err)
		}
	}
	if err := coretest.PortfolioB.CheckPortfolio("portfolio", siblingcore.WholeBook); err != nil {
		t.Fatal(err)
	}
	if err := coretest.PortfolioB.CheckPortfolio("portfolio", "VARIABLE"); err == nil {
		t.Fatal("a whole-book config must reject a named portfolio")
	}
}

func TestConfig_NamedPortfolios(t *testing.T) {
	p, err := coretest.PortfolioC.ResolvePortfolio("portfolio", " fixed ")
	if err != nil || p != "FIXED" {
		t.Fatalf("got %q, %v", p, err)
	}
	_, err = coretest.PortfolioC.ResolvePortfolio("portfolio", "  ")
	assertValidation(t, err, "portfolio", "Required request parameter 'portfolio' is missing")
	_, err = coretest.PortfolioC.ResolvePortfolio("portfolio", "x")
	assertValidation(t, err, "portfolio", "No enum constant pl.shared.entity.enums.Portfolio.X")
	assertValidation(t, coretest.PortfolioC.CheckPortfolio("portfolio", siblingcore.WholeBook), "portfolio", "Required request parameter 'portfolio' is missing")
	if err := coretest.PortfolioC.CheckPortfolio("portfolio", "VARIABLE"); err != nil {
		t.Fatal(err)
	}
}

func TestConfig_CheckBucketListsBucketsInConfigOrder(t *testing.T) {
	if err := coretest.PortfolioB.CheckBucket("sold.accountType", "IKZE_MAMA"); err != nil {
		t.Fatal(err)
	}
	assertValidation(t, coretest.PortfolioB.CheckBucket("sold.accountType", ""), "sold.accountType",
		"must be one of IKE_TATA, IKE_MAMA, IKZE_TATA, IKZE_MAMA, INNE")
	if err := coretest.PortfolioC.CheckBucket("sold.accountType", siblingcore.SingleBucket); err != nil {
		t.Fatal(err)
	}
	if err := coretest.PortfolioC.CheckBucket("x", "IKE_TATA"); err == nil || !strings.Contains(err.Error(), "must be one of") {
		t.Fatalf("portfolioC has no IKE_TATA bucket: %v", err)
	}
}

func TestConfig_CheckUnitsRejectsAnUnconfiguredBucket(t *testing.T) {
	ok := siblingcore.Units{}.Add("IKE_TATA", one).Add("INNE", one)
	if err := coretest.PortfolioB.CheckUnits("items.accountAllocations.accountType", ok); err != nil {
		t.Fatal(err)
	}
	bad := ok.Add("IKE_BABCIA", one)
	assertValidation(t, coretest.PortfolioB.CheckUnits("items.accountAllocations.accountType", bad),
		"items.accountAllocations.accountType", "must be one of IKE_TATA, IKE_MAMA, IKZE_TATA, IKZE_MAMA, INNE")
}

func TestConfig_Spec(t *testing.T) {
	spec, ok := coretest.PortfolioB.Spec("INNE")
	if !ok || spec.TracksLots || spec.Field != "unitsInne" {
		t.Fatalf("INNE: %+v %v", spec, ok)
	}
	spec, ok = coretest.PortfolioC.Spec(siblingcore.SingleBucket)
	if !ok || !spec.TracksLots || spec.Field != "units" {
		t.Fatalf("single: %+v %v", spec, ok)
	}
	if _, ok := coretest.PortfolioC.Spec("INNE"); ok {
		t.Fatal("portfolioC has no INNE")
	}
}
