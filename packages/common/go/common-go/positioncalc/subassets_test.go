package positioncalc

import "testing"

// subRow is a test-local SubassetRow.
type subRow struct {
	id      int
	assetID int64
	primary bool
	ticker  string
}

func (s subRow) SubassetAssetID() int64  { return s.assetID }
func (s subRow) SubassetIsPrimary() bool { return s.primary }
func (s subRow) SubassetTicker() string  { return s.ticker }

func ids(rows []subRow) []int {
	out := make([]int, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.id)
	}
	return out
}

func TestOrderSubassets(t *testing.T) {
	rows := []subRow{
		{1, 7, false, "ZZZ"},
		{2, 9, true, "AAA"}, // other asset: ignored
		{3, 7, false, "BBB"},
		{4, 7, true, "MMM"},  // primary first although its ticker sorts in the middle
		{5, 7, false, "BBB"}, // same ticker as 3: stable, keeps input order
		{6, 9, false, "CCC"},
	}
	tests := []struct {
		name    string
		assetID int64
		want    []int
	}{
		{"primary first, rest by ticker, stable", 7, []int{4, 3, 5, 1}},
		{"other asset", 9, []int{2, 6}},
		{"unknown asset", 42, []int{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ids(OrderSubassets(rows, tc.assetID))
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("got %v, want %v", got, tc.want)
				}
			}
		})
	}
	if OrderSubassets(rows, 42) != nil {
		t.Fatal("no rows for the asset must be a nil slice (today's subassetsOf)")
	}
}

func TestOrderSubassets_NoPrimaryKeepsTickerOrder(t *testing.T) {
	rows := []subRow{{1, 7, false, "B"}, {2, 7, false, "A"}}
	if got := ids(OrderSubassets(rows, 7)); len(got) != 2 || got[0] != 2 || got[1] != 1 {
		t.Fatalf("got %v, want [2 1]", got)
	}
}

func TestPrimaryOf(t *testing.T) {
	rows := []subRow{{1, 7, false, "A"}, {2, 9, true, "B"}, {3, 7, true, "C"}}
	p := PrimaryOf(rows, 7)
	if p == nil || p.id != 3 {
		t.Fatalf("PrimaryOf(7) = %v, want row 3", p)
	}
	if p != &rows[2] {
		t.Fatal("PrimaryOf must point into rows, not at a copy")
	}
	if got := PrimaryOf(rows, 42); got != nil {
		t.Fatalf("PrimaryOf(unknown) = %v, want nil", got)
	}
	if got := PrimaryOf([]subRow{{1, 7, false, "A"}}, 7); got != nil {
		t.Fatalf("primary-less asset: got %v, want nil", got)
	}
}
