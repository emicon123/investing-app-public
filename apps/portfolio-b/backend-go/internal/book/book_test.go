package book

import (
	"testing"

	"investing-app/common-go/siblingcore"
)

func TestConfig_ValidAndPinned(t *testing.T) {
	if err := Config.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(Config.Portfolios) != 1 || Config.Portfolios[0] != siblingcore.WholeBook {
		t.Fatalf("portfolioB is one whole book: %v", Config.Portfolios)
	}
	want := []siblingcore.BucketSpec{
		{Bucket: "IKE_TATA", Field: "unitsIkeTata", TracksLots: true},
		{Bucket: "IKE_MAMA", Field: "unitsIkeMama", TracksLots: true},
		{Bucket: "IKZE_TATA", Field: "unitsIkzeTata", TracksLots: true},
		{Bucket: "IKZE_MAMA", Field: "unitsIkzeMama", TracksLots: true},
		{Bucket: "INNE", Field: "unitsInne", TracksLots: false},
	}
	if len(Config.Buckets) != len(want) {
		t.Fatalf("buckets: %+v", Config.Buckets)
	}
	for i := range want {
		if Config.Buckets[i] != want[i] {
			t.Fatalf("bucket %d: got %+v want %+v", i, Config.Buckets[i], want[i])
		}
	}
}
