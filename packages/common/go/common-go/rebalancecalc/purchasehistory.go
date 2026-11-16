package rebalancecalc

import (
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

const (
	moneyScale = 4
	pctScale   = 4
)

// PurchaseLotInput is the pre-decoded, per-app view of one `purchase_lots` row — a pure
// read-only transform input, no per-app divergence at all (portfolioB's and portfolioC's PurchaseLot
// entities differ only by an extra `portfolio` column portfolioC's own confirm/history query already
// filters on before calling in here).
type PurchaseLotInput struct {
	SessionID   uuid.UUID
	PurchasedAt time.Time // date-only; PurchaseConfirmService writes one shared date per session
	CreatedAt   time.Time
	Ticker      string
	Units       decimal.Decimal
	PricePln    decimal.Decimal
	CostPln     decimal.Decimal
}

// PositionLookup is the minimal per-ticker position info PurchaseHistoryEngine.buildLot reads —
// the direct port of the (Position::getName, Position::getYahooTicker) extractor pair.
type PositionLookup struct {
	Name        string
	YahooTicker *string
}

// PurchaseLotResult is the direct port of PurchaseLotDto.
type PurchaseLotResult struct {
	Ticker          string
	Name            *string
	Units           decimal.Decimal
	PricePln        decimal.Decimal
	CostPln         decimal.Decimal
	CurrentPricePln *decimal.Decimal
	CurrentValuePln *decimal.Decimal
	GainPln         *decimal.Decimal
	GainPct         *decimal.Decimal
	PriceAvailable  bool
}

// PurchaseSessionResult is the direct port of PurchaseSessionDto.
type PurchaseSessionResult struct {
	SessionID            uuid.UUID
	PurchasedAt          time.Time
	TotalCostPln         decimal.Decimal
	TotalCurrentValuePln decimal.Decimal
	TotalGainPln         decimal.Decimal
	TotalGainPct         *decimal.Decimal
	Lots                 []PurchaseLotResult
}

// BuildPurchaseHistory groups lots by session, computes per-lot/per-session gain figures against
// the live price cache, and orders sessions by (purchasedAt DESC, MIN(createdAt) DESC) and each
// session's own lots by ticker ASC. pricePlnByYahooTicker is a cache-only read (see
// valuecomputation.go's identical convention) — a lot whose position has no resolvable current
// price answers with priceAvailable=false and nil current-value/gain fields, never a zero standing
// in for "unknown".
func BuildPurchaseHistory(lots []PurchaseLotInput, positionsByTicker map[string]PositionLookup, pricePlnByYahooTicker map[string]decimal.Decimal) []PurchaseSessionResult {
	if len(lots) == 0 {
		return nil
	}

	bySession := map[uuid.UUID][]PurchaseLotInput{}
	for _, lot := range lots {
		bySession[lot.SessionID] = append(bySession[lot.SessionID], lot)
	}

	type keyed struct {
		purchasedAt time.Time
		minCreated  time.Time
		session     PurchaseSessionResult
	}
	built := make([]keyed, 0, len(bySession))
	for sessionID, sessionLots := range bySession {
		minCreated := sessionLots[0].CreatedAt
		for _, l := range sessionLots[1:] {
			if l.CreatedAt.Before(minCreated) {
				minCreated = l.CreatedAt
			}
		}
		session := buildSession(sessionID, sessionLots, positionsByTicker, pricePlnByYahooTicker)
		built = append(built, keyed{purchasedAt: sessionLots[0].PurchasedAt, minCreated: minCreated, session: session})
	}

	sort.SliceStable(built, func(i, j int) bool {
		if !built[i].purchasedAt.Equal(built[j].purchasedAt) {
			return built[i].purchasedAt.After(built[j].purchasedAt)
		}
		return built[i].minCreated.After(built[j].minCreated)
	})

	out := make([]PurchaseSessionResult, 0, len(built))
	for _, b := range built {
		out = append(out, b.session)
	}
	return out
}

func buildSession(sessionID uuid.UUID, lots []PurchaseLotInput, positionsByTicker map[string]PositionLookup, pricePlnByYahooTicker map[string]decimal.Decimal) PurchaseSessionResult {
	ordered := make([]PurchaseLotInput, len(lots))
	copy(ordered, lots)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Ticker < ordered[j].Ticker })

	lotDtos := make([]PurchaseLotResult, 0, len(ordered))
	totalCost := decimal.Zero
	totalCurrentValue := decimal.Zero
	for _, lot := range ordered {
		pos, hasPos := positionsByTicker[lot.Ticker]
		dto := buildLot(lot, hasPos, pos, pricePlnByYahooTicker)
		lotDtos = append(lotDtos, dto)
		totalCost = totalCost.Add(dto.CostPln)
		if dto.PriceAvailable {
			totalCurrentValue = totalCurrentValue.Add(*dto.CurrentValuePln)
		}
	}
	totalCost = totalCost.Round(moneyScale)
	totalCurrentValue = totalCurrentValue.Round(moneyScale)
	totalGain := totalCurrentValue.Sub(totalCost).Round(moneyScale)
	totalGainPct := percentOf(totalGain, totalCost)

	return PurchaseSessionResult{
		SessionID: sessionID, PurchasedAt: ordered[0].PurchasedAt,
		TotalCostPln: totalCost, TotalCurrentValuePln: totalCurrentValue,
		TotalGainPln: totalGain, TotalGainPct: totalGainPct, Lots: lotDtos,
	}
}

func buildLot(lot PurchaseLotInput, hasPos bool, pos PositionLookup, pricePlnByYahooTicker map[string]decimal.Decimal) PurchaseLotResult {
	var name *string
	if hasPos {
		n := pos.Name
		name = &n
	}
	costPln := lot.CostPln.Round(moneyScale)

	var currentPricePln *decimal.Decimal
	if hasPos && pos.YahooTicker != nil {
		if price, ok := pricePlnByYahooTicker[*pos.YahooTicker]; ok && price.Sign() > 0 {
			currentPricePln = &price
		}
	}

	if currentPricePln == nil {
		return PurchaseLotResult{
			Ticker: lot.Ticker, Name: name, Units: lot.Units, PricePln: lot.PricePln,
			CostPln: costPln, PriceAvailable: false,
		}
	}

	currentValue := lot.Units.Mul(*currentPricePln).Round(moneyScale)
	gainPln := currentValue.Sub(costPln).Round(moneyScale)
	gainPct := percentOf(gainPln, costPln)
	return PurchaseLotResult{
		Ticker: lot.Ticker, Name: name, Units: lot.Units, PricePln: lot.PricePln, CostPln: costPln,
		CurrentPricePln: currentPricePln, CurrentValuePln: &currentValue,
		GainPln: &gainPln, GainPct: gainPct, PriceAvailable: true,
	}
}

// percentOf is the direct port of PurchaseHistoryEngine.percentOf — nil when denominator is
// zero, never a divide-by-zero panic or a spurious 0%.
func percentOf(numerator, denominator decimal.Decimal) *decimal.Decimal {
	if denominator.Sign() == 0 {
		return nil
	}
	v := numerator.DivRound(denominator, pctScale+2).Mul(hundred).Round(pctScale)
	return &v
}
