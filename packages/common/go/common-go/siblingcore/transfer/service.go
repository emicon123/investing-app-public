package transfer

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"investing-app/common-go/costbasis"
	"investing-app/common-go/costmember"
	"investing-app/common-go/httpx"
	"investing-app/common-go/pgerr"
	"investing-app/common-go/siblingcore"
)

// cashNeutralTolerancePln: a transfer whose buys differ from the sale by more than this is logged
// (never rejected — the user records what actually happened).
var cashNeutralTolerancePln = decimal.RequireFromString("0.05")

// Service is the transfer service.
type Service struct {
	cfg siblingcore.Config
	tx  func(ctx context.Context, fn func(Store) error) error
}

// NewService: tx must be the app's serialised money-write runner (ADR-111).
func NewService[S Store](cfg siblingcore.Config, tx siblingcore.TxFunc[S]) *Service {
	return &Service{cfg: cfg, tx: func(ctx context.Context, fn func(Store) error) error {
		return tx(ctx, func(q S) error { return fn(q) })
	}}
}

// touched is one subasset row the transfer changes, in first-touch order.
type touched struct {
	id, assetID int64
	holding     siblingcore.Holding
}

// ExecuteTransfer records exactly what the user submitted — the sold leg comes from req, never
// from a target or deviation (manual-sell-only rule).
func (s *Service) ExecuteTransfer(ctx context.Context, req Request) (Result, error) {
	if err := s.validate(req); err != nil {
		return Result{}, err
	}
	transferID := uuid.New()
	now := time.Now().UTC()

	var result Result
	err := s.tx(ctx, func(q Store) error {
		rows := map[string]*touched{}
		var order []*touched
		load := func(ticker string) (*touched, error) {
			if t, ok := rows[ticker]; ok {
				return t, nil
			}
			sub, err := q.GetSubassetByTicker(ctx, req.Portfolio, ticker)
			if err != nil {
				return nil, pgerr.NotFoundOr(err, "Position not found: "+ticker, "positiontransfer: read position "+ticker)
			}
			t := &touched{id: sub.ID, assetID: sub.AssetID, holding: sub.Holding}
			rows[ticker] = t
			order = append(order, t)
			return t, nil
		}
		record := func(leg Leg, delta, total decimal.Decimal) LegRecord {
			return LegRecord{TransferID: transferID, Portfolio: req.Portfolio, Ticker: leg.Ticker, Bucket: leg.Bucket,
				UnitsDelta: delta, PricePln: leg.PricePln, TotalPln: total, TransferredAt: now}
		}

		sold, err := load(req.Sold.Ticker)
		if err != nil {
			return err
		}
		if held := sold.holding.Units.Of(req.Sold.Bucket); held.Cmp(req.Sold.Units) < 0 {
			return httpx.NewValidationError("", fmt.Sprintf(
				"Insufficient units: %s holds %s units, cannot sell %s", req.Sold.Ticker, held, req.Sold.Units))
		}
		sold.holding.Units = sold.holding.Units.Add(req.Sold.Bucket, req.Sold.Units.Neg())
		soldTotal := req.Sold.Units.Mul(req.Sold.PricePln).Round(4).Neg()
		legs := []LegRecord{record(req.Sold, req.Sold.Units.Neg(), soldTotal)}

		soldValue := soldTotal.Neg()
		sale := costbasis.Sale{PositionID: sold.id, Units: req.Sold.Units, PricePln: req.Sold.PricePln, ValuePln: &soldValue}
		boughtTotal := decimal.Zero
		var costLegs []costbasis.Leg
		for _, leg := range req.Buys {
			buy, err := load(leg.Ticker)
			if err != nil {
				return err
			}
			buy.holding.Units = buy.holding.Units.Add(leg.Bucket, leg.Units)
			legTotal := leg.Units.Mul(leg.PricePln).Round(4)
			boughtTotal = boughtTotal.Add(legTotal)
			costLegs = append(costLegs, costbasis.Leg{PositionID: buy.id, AssetID: buy.assetID, Units: leg.Units, PricePln: leg.PricePln, ValuePln: &legTotal})
			legs = append(legs, record(leg, leg.Units, legTotal))
		}
		if imbalance := boughtTotal.Add(soldTotal); imbalance.Abs().Cmp(cashNeutralTolerancePln) > 0 {
			slog.Warn("position transfer is not cash-neutral", "transferId", transferID,
				"soldPln", soldTotal.Abs(), "boughtPln", boughtTotal, "imbalancePln", imbalance)
		}

		changed, err := planCosts(ctx, q, req.Portfolio, sold.assetID, costbasis.Transfer{Sold: sale, Buys: costLegs})
		if err != nil {
			return err
		}
		newCost := make(map[int64]*decimal.Decimal, len(changed))
		for _, m := range changed {
			newCost[m.ID] = m.Units.TotalSpentPln
		}
		writtenByDelta := make([]int64, 0, len(order))
		for _, t := range order {
			if cost, ok := newCost[t.id]; ok {
				t.holding.TotalSpentPln = cost // persisted with the units below
			}
			writtenByDelta = append(writtenByDelta, t.id)
			if err := q.WriteHolding(ctx, t.id, t.holding); err != nil {
				return fmt.Errorf("positiontransfer: apply delta for position %d: %w", t.id, err)
			}
		}
		if err := siblingcore.PersistCosts(ctx, q, changed, writtenByDelta...); err != nil {
			return fmt.Errorf("positiontransfer: %w", err)
		}
		for _, r := range legs {
			if err := q.InsertTransferLeg(ctx, r); err != nil {
				return fmt.Errorf("positiontransfer: insert transfer row for %s: %w", r.Ticker, err)
			}
		}
		result = Result{TransferID: transferID, Sold: req.Sold, Buys: req.Buys}
		return nil
	})
	return result, err
}

// planCosts runs the cost kernel over the portfolio's rows as they stood before this transfer
// (DB prices only — never a network call inside a money transaction).
func planCosts(ctx context.Context, q Store, p siblingcore.Portfolio, soldAssetID int64, t costbasis.Transfer) ([]*siblingcore.CostMember, error) {
	subs, err := q.ListSubassets(ctx, p)
	if err != nil {
		return nil, fmt.Errorf("positiontransfer: list positions for cost: %w", err)
	}
	prices, err := q.PricePlnByYahooTicker(ctx)
	if err != nil {
		return nil, fmt.Errorf("positiontransfer: read price cache: %w", err)
	}
	return costmember.PlanTransfer(siblingcore.MembersByAsset(subs), prices, soldAssetID, t), nil
}

// validate: the leg amounts first (today's order), then the portfolio and every leg's bucket —
// all before the transaction opens.
func (s *Service) validate(req Request) error {
	if len(req.Buys) == 0 {
		return httpx.NewValidationError("buys", "at least one buy leg is required")
	}
	if req.Sold.Units.Sign() <= 0 {
		return httpx.NewValidationError("sold.units", "units must be positive")
	}
	if req.Sold.PricePln.Sign() <= 0 {
		return httpx.NewValidationError("sold.pricePln", "pricePln must be positive")
	}
	for _, buy := range req.Buys {
		if buy.Units.Sign() <= 0 {
			return httpx.NewValidationError("buys.units", "units must be positive")
		}
		if buy.PricePln.Sign() <= 0 {
			return httpx.NewValidationError("buys.pricePln", "pricePln must be positive")
		}
	}
	if err := s.cfg.CheckPortfolio("portfolio", req.Portfolio); err != nil {
		return err
	}
	if err := s.cfg.CheckBucket("sold.accountType", req.Sold.Bucket); err != nil {
		return err
	}
	for _, buy := range req.Buys {
		if err := s.cfg.CheckBucket("buys.accountType", buy.Bucket); err != nil {
			return err
		}
	}
	return nil
}
