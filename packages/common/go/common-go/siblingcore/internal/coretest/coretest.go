// Package coretest holds the test fixtures every siblingcore package's tests share: the two
// production configurations (mirroring portfolioB's and portfolioC's internal/book) and an in-memory book
// that implements the read/write methods several ports have in common.
package coretest

import (
	"investing-app/common-go/siblingcore"
)

// PortfolioB mirrors portfolio-b/backend-go/internal/book.Config.
var PortfolioB = siblingcore.Config{
	Portfolios: []siblingcore.Portfolio{siblingcore.WholeBook},
	Buckets: []siblingcore.BucketSpec{
		{Bucket: "IKE_TATA", Field: "unitsIkeTata", TracksLots: true},
		{Bucket: "IKE_MAMA", Field: "unitsIkeMama", TracksLots: true},
		{Bucket: "IKZE_TATA", Field: "unitsIkzeTata", TracksLots: true},
		{Bucket: "IKZE_MAMA", Field: "unitsIkzeMama", TracksLots: true},
		{Bucket: "INNE", Field: "unitsInne", TracksLots: false},
	},
}

// PortfolioC mirrors portfolio-c/backend-go/internal/book.Config.
var PortfolioC = siblingcore.Config{
	Portfolios:    []siblingcore.Portfolio{"VARIABLE", "FIXED"},
	PortfolioEnum: "pl.shared.entity.enums.Portfolio",
	Buckets:       []siblingcore.BucketSpec{{Bucket: siblingcore.SingleBucket, Field: "units", TracksLots: true}},
}
