package cryptotrades

import (
	"sort"

	"github.com/shopspring/decimal"
)

// belkaRate is the 19% Polish capital-gains tax rate (CryptoTradeService.BELKA_RATE).
var belkaRate = decimal.NewFromFloat(0.19)

// TaxSummary computes the annual PIT-38 summary over an already-fetched year slice, as a pure,
// allocation-light function with no DB access (the sqlc data-fetch and this computation are kept
// clearly separated).
//
// Rules, ported 1:1: group by ticker (sorted ascending), totalBuysPln = SUM(amountPln)
// over BUY rows, totalSellsPln over SELL rows, netGainPln = sells - buys,
// estimatedBelkaTaxPln = 19% of a positive net gain at scale 4 HALF_UP (zero at scale 4
// otherwise). No FIFO/LIFO — raw PLN amounts (see the endpoint's spec description).
func TaxSummary(year int, trades []TradeInput) Summary {
	byTicker := map[string][]TradeInput{}
	for _, t := range trades {
		byTicker[t.Ticker] = append(byTicker[t.Ticker], t)
	}

	tickers := make([]string, 0, len(byTicker))
	for ticker := range byTicker {
		tickers = append(tickers, ticker)
	}
	sort.Strings(tickers)

	summaries := make([]TickerSummary, 0, len(tickers))
	yearBuys := decimal.Zero
	yearSells := decimal.Zero
	for _, ticker := range tickers {
		s := tickerSummary(ticker, byTicker[ticker])
		summaries = append(summaries, s)
		yearBuys = yearBuys.Add(s.TotalBuysPln)
		yearSells = yearSells.Add(s.TotalSellsPln)
	}

	yearNet := yearSells.Sub(yearBuys)
	return Summary{
		Year:                 year,
		TotalBuysPln:         yearBuys,
		TotalSellsPln:        yearSells,
		NetGainPln:           yearNet,
		EstimatedBelkaTaxPln: belkaTax(yearNet),
		ByTicker:             summaries,
	}
}

func tickerSummary(ticker string, trades []TradeInput) TickerSummary {
	buys := decimal.Zero
	sells := decimal.Zero
	for _, t := range trades {
		if t.Side == "SELL" {
			sells = sells.Add(t.AmountPln)
		} else {
			buys = buys.Add(t.AmountPln)
		}
	}
	net := sells.Sub(buys)
	return TickerSummary{
		Ticker:               ticker,
		TotalBuysPln:         buys,
		TotalSellsPln:        sells,
		NetGainPln:           net,
		EstimatedBelkaTaxPln: belkaTax(net),
	}
}

// belkaTax is 19% of a positive net gain at scale 4 HALF_UP, else zero at scale 4. shopspring's
// Round is half-away-from-zero, which coincides with HALF_UP on the positive gains this branch ever
// sees.
func belkaTax(netGain decimal.Decimal) decimal.Decimal {
	if netGain.Cmp(decimal.Zero) > 0 {
		return netGain.Mul(belkaRate).Round(4)
	}
	return decimal.Zero.Round(4)
}
