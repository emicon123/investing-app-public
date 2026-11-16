package rebalancecalc

import "time"

// PricesRefreshedAt is the earliest price fetch time (UTC) over the non-companion inputs that
// have a Yahoo ticker and an entry in fetchedAt, capped at now (UTC). It answers now.UTC() when
// no input qualifies. Pure: the caller passes now (time.Now()).
//
// The .UTC() normalisation is load-bearing: fetchedAt values loaded from Postgres carry the
// server's local zone, and an offset other than "Z" once broke every calculate response on both
// siblings (portfolioB e68ca04, portfolioC 268a4e8) — the reason this lives here once instead of per app.
func PricesRefreshedAt(inputs []PositionInput, fetchedAt map[string]time.Time, now time.Time) time.Time {
	earliest := now.UTC()
	for _, p := range inputs {
		if p.IsCompanionRow || p.YahooTicker == nil || *p.YahooTicker == "" {
			continue
		}
		if t, ok := fetchedAt[*p.YahooTicker]; ok {
			if t = t.UTC(); t.Before(earliest) {
				earliest = t
			}
		}
	}
	return earliest
}
