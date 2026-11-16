package book

import (
	"slices"
	"testing"

	"investing-app/common-go/siblingcore"
)

func TestConfig_ValidAndPinned(t *testing.T) {
	if err := Config.Validate(); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(PortfolioNames(), []string{"VARIABLE", "FIXED"}) {
		t.Fatalf("portfolios %v", PortfolioNames())
	}
	if Config.PortfolioEnum != "pl.shared.entity.enums.Portfolio" {
		t.Fatalf("enum %q", Config.PortfolioEnum)
	}
	want := siblingcore.BucketSpec{Bucket: siblingcore.SingleBucket, Field: "units", TracksLots: true}
	if len(Config.Buckets) != 1 || Config.Buckets[0] != want {
		t.Fatalf("buckets %+v", Config.Buckets)
	}
}
