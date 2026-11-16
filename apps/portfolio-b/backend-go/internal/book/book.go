// Package book is portfolioB's one siblingcore parameterisation (ADR-113): a single implicit book
// (no portfolio column) whose units live in five account-type columns.
package book

import "investing-app/common-go/siblingcore"

// The account types (account_type_enum), one units column each.
const (
	IkeTata  siblingcore.Bucket = "IKE_TATA"
	IkeMama  siblingcore.Bucket = "IKE_MAMA"
	IkzeTata siblingcore.Bucket = "IKZE_TATA"
	IkzeMama siblingcore.Bucket = "IKZE_MAMA"
	Inne     siblingcore.Bucket = "INNE"
)

// Config: the whole book, five buckets in account order; INNE (historical holdings) records no
// purchase lots.
var Config = siblingcore.Config{
	Portfolios: []siblingcore.Portfolio{siblingcore.WholeBook},
	Buckets: []siblingcore.BucketSpec{
		{Bucket: IkeTata, Field: "unitsIkeTata", TracksLots: true},
		{Bucket: IkeMama, Field: "unitsIkeMama", TracksLots: true},
		{Bucket: IkzeTata, Field: "unitsIkzeTata", TracksLots: true},
		{Bucket: IkzeMama, Field: "unitsIkzeMama", TracksLots: true},
		{Bucket: Inne, Field: "unitsInne", TracksLots: false},
	},
}
