package position

import (
	"context"
	"testing"

	"github.com/shopspring/decimal"

	"investing-app/common-go/httpx"
	"investing-app/common-go/siblingcore"
	"investing-app/common-go/siblingcore/internal/coretest"
)

var ctx = context.Background()

func TestService_List_GroupsByAssetPrimaryFirstSortedAndPaged(t *testing.T) {
	coretest.Configs(func(name string, cfg siblingcore.Config, p siblingcore.Portfolio) {
		f := newStore(cfg)
		aA, _ := f.addAsset(p, "ZZZ", "50", "1")
		f.Sub(f.Subs[0].ID).SortOrder = 1
		f.addSub(p, aA, "AAA-SIB", "2", false)
		aB, _ := f.addAsset(p, "BBB", "50", "3")
		f.addAsset("FIXED", "OTHER-BOOK", "100", "1")
		f.Prices = map[string]decimal.Decimal{"ZZZ.WA": coretest.D("10"), "AAA-SIB.WA": coretest.D("5"), "BBB.WA": coretest.D("10")}
		items, total, err := newSvc(f).List(ctx, p, 0, 20)
		if err != nil {
			t.Fatal(err)
		}
		if total != 2 || len(items) != 2 || items[0].ID != aB || items[1].ID != aA {
			t.Fatalf("%s: order by primary (sortOrder, ticker): total %d %+v", name, total, items)
		}
		grouped := items[1]
		if len(grouped.Subassets) != 2 || !grouped.Subassets[0].IsPrimary || grouped.Subassets[1].Ticker != "AAA-SIB" {
			t.Fatalf("%s: primary first: %+v", name, grouped.Subassets)
		}
		if grouped.Computed.ValuePln == nil || !grouped.Computed.ValuePln.Equal(coretest.D("20")) {
			t.Fatalf("%s: asset value = Σ subassets: %v", name, grouped.Computed.ValuePln)
		}
		if grouped.Computed.CurrentPct == nil || !grouped.Computed.CurrentPct.Equal(coretest.D("40")) {
			t.Fatalf("%s: currentPct over the book (20 of 50): %v", name, grouped.Computed.CurrentPct)
		}
		page, total, err := newSvc(f).List(ctx, p, 1, 1)
		if err != nil || total != 2 || len(page) != 1 || page[0].ID != aA {
			t.Fatalf("%s: page 2 of size 1: %v %+v", name, err, page)
		}
		empty, _, err := newSvc(f).List(ctx, p, 5, 20)
		if err != nil || len(empty) != 0 {
			t.Fatalf("%s: past the end is empty: %v %+v", name, err, empty)
		}
	})
}

func TestService_List_RejectsBadPaging(t *testing.T) {
	coretest.Configs(func(name string, cfg siblingcore.Config, p siblingcore.Portfolio) {
		svc := newSvc(newStore(cfg))
		_, _, err := svc.List(ctx, p, -1, 20)
		appErr(t, err, 400, "page", "page must not be negative")
		_, _, err = svc.List(ctx, p, 0, 0)
		appErr(t, err, 400, "size", "size must be at least 1")
	})
}

func TestService_ComputedFields_DegradeToUnitsOnlyWhenPriceMissing(t *testing.T) {
	coretest.Configs(func(name string, cfg siblingcore.Config, p siblingcore.Portfolio) {
		f := newStore(cfg)
		_, sub := f.addAsset(p, "NOPRICE", "100", "4")
		dto, err := newSvc(f).Get(ctx, sub)
		if err != nil {
			t.Fatal(err)
		}
		c := dto.Computed
		if c.UnitsTotal == nil || !c.UnitsTotal.Equal(coretest.D("4")) || c.PricePln != nil || c.ValuePln != nil {
			t.Fatalf("%s: %+v", name, c)
		}
		if s := dto.Subassets[0]; s.PricePln != nil || !s.EffectiveUnits.Equal(coretest.D("4")) {
			t.Fatalf("%s: subasset %+v", name, s)
		}
	})
}

func TestService_Create_NewAssetInsertsZeroUnitsInEveryBucketAndIsPrimary(t *testing.T) {
	coretest.Configs(func(name string, cfg siblingcore.Config, p siblingcore.Portfolio) {
		f := newStore(cfg)
		target := coretest.D("12.5")
		dto, err := newSvc(f).Create(ctx, CreateInput{
			Portfolio: p, Ticker: "NEW", Name: "New", Exchange: "XTB", AssetClass: "ETF", Exposure: "STOCKS", PriceCurrency: "PLN",
			NewAsset: &NewAssetInput{Name: "New asset", Exposure: "STOCKS", AssetClass: "ETF", TargetAllocationPct: &target},
		})
		if err != nil {
			t.Fatal(err)
		}
		if dto.Portfolio != p || dto.Key != "NEW" || dto.TargetAllocationPct == nil || !dto.TargetAllocationPct.Equal(target) {
			t.Fatalf("%s: asset %+v", name, dto.Asset)
		}
		if len(dto.Subassets) != 1 || !dto.Subassets[0].IsPrimary {
			t.Fatalf("%s: subassets %+v", name, dto.Subassets)
		}
		stored := f.Subs[0]
		for _, b := range cfg.Buckets {
			if v := stored.Holding.Units.Of(b.Bucket); !v.IsZero() {
				t.Fatalf("%s: bucket %s = %s", name, b.Bucket, v)
			}
		}
	})
}

func TestService_Create_Rules(t *testing.T) {
	coretest.Configs(func(name string, cfg siblingcore.Config, p siblingcore.Portfolio) {
		f := newStore(cfg)
		aid, _ := f.addAsset(p, "HAS", "50", "1")
		svc := newSvc(f)
		base := CreateInput{Portfolio: p, Ticker: "X", Name: "X", Exchange: "XTB", AssetClass: "ETF", Exposure: "STOCKS", PriceCurrency: "PLN"}
		newAsset := &NewAssetInput{Name: "X", Exposure: "STOCKS", AssetClass: "ETF"}

		in := base
		_, err := svc.Create(ctx, in)
		appErr(t, err, 400, "assetId", "either assetId or newAsset is required")
		in.AssetID, in.NewAsset = &aid, newAsset
		_, err = svc.Create(ctx, in)
		appErr(t, err, 400, "newAsset", "newAsset must be omitted when assetId is set")
		in = base
		in.NewAsset = &NewAssetInput{Name: "X"}
		_, err = svc.Create(ctx, in)
		appErr(t, err, 400, "newAsset", "name, exposure and assetClass are required")
		in.NewAsset = &NewAssetInput{Name: "X", Exposure: "STOCKS", AssetClass: "ETF", TargetAllocationPct: coretest.DP("101")}
		_, err = svc.Create(ctx, in)
		appErr(t, err, 400, "targetAllocationPct", "must be between 0 and 100")
		no := false
		in = base
		in.NewAsset, in.IsPrimary = newAsset, &no
		_, err = svc.Create(ctx, in)
		appErr(t, err, 400, "isPrimary", "a new asset's first subasset is always primary")

		unknown := int64(999)
		in = base
		in.AssetID = &unknown
		_, err = svc.Create(ctx, in)
		appErr(t, err, 404, "", "Asset with id 999 not found")
		yes := true
		in.AssetID, in.IsPrimary = &aid, &yes
		_, err = svc.Create(ctx, in)
		appErr(t, err, 409, "", "asset already has a primary subasset")
		in = base
		in.Ticker, in.NewAsset = "HAS", newAsset
		_, err = svc.Create(ctx, in)
		appErr(t, err, 409, "", "Resource already exists")

		in = base
		in.AssetID = &aid
		dto, err := svc.Create(ctx, in)
		if err != nil || len(dto.Subassets) != 2 || dto.Subassets[1].Ticker != "X" || dto.Subassets[1].IsPrimary {
			t.Fatalf("%s: attach non-primary: %v %+v", name, err, dto.Subassets)
		}
	})
}

func TestService_Create_RestoresSoftDeletedTickerKeepingIDAndUnits(t *testing.T) {
	coretest.Configs(func(name string, cfg siblingcore.Config, p siblingcore.Portfolio) {
		f := newStore(cfg)
		aid, sub := f.addAsset(p, "OLD", "100", "7")
		svc := newSvc(f)
		if err := svc.Delete(ctx, sub); err != nil {
			t.Fatal(err)
		}
		if f.asset(aid).Deleted != true {
			t.Fatalf("%s: last subasset deletes the asset", name)
		}
		dto, err := svc.Create(ctx, CreateInput{
			Portfolio: p, Ticker: "OLD", Name: "Old again", Exchange: "XTB", AssetClass: "ETF", Exposure: "STOCKS", PriceCurrency: "PLN",
			NewAsset: &NewAssetInput{Name: "Old", Exposure: "STOCKS", AssetClass: "ETF"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if dto.ID != aid || len(dto.Subassets) != 1 || dto.Subassets[0].ID != sub || !dto.Subassets[0].EffectiveUnits.Equal(coretest.D("7")) {
			t.Fatalf("%s: restore keeps ids and units: %+v", name, dto)
		}
		if dto.TargetAllocationPct == nil || !dto.TargetAllocationPct.Equal(coretest.D("100")) {
			t.Fatalf("%s: omitted target keeps the stored one: %v", name, dto.TargetAllocationPct)
		}
	})
}

func TestService_Update_PatchesOnlyGivenBucketsAndValidatesInConfigOrder(t *testing.T) {
	coretest.Configs(func(name string, cfg siblingcore.Config, p siblingcore.Portfolio) {
		f := newStore(cfg)
		_, sub := f.addAsset(p, "U", "100", "3")
		svc := newSvc(f)
		last := cfg.Buckets[len(cfg.Buckets)-1]
		first := cfg.Buckets[0]
		_, err := svc.Update(ctx, sub, UpdateInput{Units: map[siblingcore.Bucket]decimal.Decimal{last.Bucket: coretest.D("-1"), first.Bucket: coretest.D("-2")}})
		appErr(t, err, 400, first.Field, "must not be negative")
		_, err = svc.Update(ctx, sub, UpdateInput{Units: map[siblingcore.Bucket]decimal.Decimal{"NO_SUCH": coretest.D("1")}})
		if err == nil {
			t.Fatalf("%s: unknown bucket must be rejected", name)
		}
		if len(f.Writes) != 0 {
			t.Fatalf("%s: rejected edits write nothing: %v", name, f.Writes)
		}
		name2 := "Renamed"
		dto, err := svc.Update(ctx, sub, UpdateInput{Units: map[siblingcore.Bucket]decimal.Decimal{last.Bucket: coretest.D("5")}, Name: &name2, TotalSpentPln: coretest.DP("99")})
		if err != nil {
			t.Fatal(err)
		}
		got := f.Sub(sub).Holding
		if !got.Units.Of(last.Bucket).Equal(coretest.D("5")) || got.TotalSpentPln == nil || !got.TotalSpentPln.Equal(coretest.D("99")) {
			t.Fatalf("%s: holding %+v", name, got)
		}
		if len(cfg.Buckets) > 1 && !got.Units.Of(first.Bucket).Equal(coretest.D("3")) {
			t.Fatalf("%s: untouched bucket changed: %s", name, got.Units.Of(first.Bucket))
		}
		if dto.Subassets[0].Name != "Renamed" {
			t.Fatalf("%s: dto %+v", name, dto.Subassets[0])
		}
		_, err = svc.Update(ctx, 4242, UpdateInput{Name: &name2})
		appErr(t, err, 404, "", "Position with id 4242 not found")
	})
}

func TestService_Update_ReHome(t *testing.T) {
	coretest.Configs(func(name string, cfg siblingcore.Config, p siblingcore.Portfolio) {
		f := newStore(cfg)
		aA, primary := f.addAsset(p, "A", "50", "1")
		sib := f.addSub(p, aA, "A2", "1", false)
		aB, _ := f.addAsset(p, "B", "50", "1")
		svc := newSvc(f)
		_, err := svc.Update(ctx, primary, UpdateInput{AssetID: &aB})
		appErr(t, err, 409, "", "a primary subasset cannot be moved to another asset — set another primary first, or delete and re-create it under the target asset")
		unknown := int64(777)
		_, err = svc.Update(ctx, sib, UpdateInput{AssetID: &unknown})
		appErr(t, err, 404, "", "Asset with id 777 not found")
		dto, err := svc.Update(ctx, sib, UpdateInput{AssetID: &aB})
		if err != nil || dto.ID != aB || len(dto.Subassets) != 2 {
			t.Fatalf("%s: moved: %v %+v", name, err, dto)
		}
		same := aB
		if _, err := svc.Update(ctx, sib, UpdateInput{AssetID: &same}); err != nil {
			t.Fatalf("%s: same asset is a no-op: %v", name, err)
		}
	})
}

func TestService_Update_ReHomeAcrossPortfoliosIsRejected_PortfolioC(t *testing.T) {
	f := newStore(coretest.PortfolioC)
	aV, _ := f.addAsset("VARIABLE", "A", "100", "1")
	aF, _ := f.addAsset("FIXED", "F", "100", "1")
	sib := f.addSub("FIXED", aF, "F2", "1", false)
	_, err := newSvc(f).Update(ctx, sib, UpdateInput{AssetID: &aV})
	appErr(t, err, 400, "assetId", "asset belongs to a different portfolio")
	_, err = newSvc(f).Create(ctx, CreateInput{Portfolio: "FIXED", Ticker: "N", Name: "N", Exchange: "XTB", AssetClass: "ETF", Exposure: "STOCKS", PriceCurrency: "PLN", AssetID: &aV})
	appErr(t, err, 400, "portfolio", "must match the asset's portfolio")
}

func TestService_Delete(t *testing.T) {
	coretest.Configs(func(name string, cfg siblingcore.Config, p siblingcore.Portfolio) {
		f := newStore(cfg)
		aid, primary := f.addAsset(p, "D", "100", "1")
		sib := f.addSub(p, aid, "D2", "1", false)
		svc := newSvc(f)
		appErr(t, svc.Delete(ctx, primary), 409, "", "asset still has other subassets — set a new primary first")
		if err := svc.Delete(ctx, sib); err != nil || !f.Sub(sib).Deleted || f.asset(aid).Deleted {
			t.Fatalf("%s: non-primary delete: %v", name, err)
		}
		if err := svc.Delete(ctx, primary); err != nil || !f.asset(aid).Deleted {
			t.Fatalf("%s: last subasset removes the asset: %v", name, err)
		}
		appErr(t, svc.Delete(ctx, primary), 404, "", "Position with id "+itoa(primary)+" not found")
		_, err := svc.Get(ctx, primary)
		appErr(t, err, 404, "", "Position with id "+itoa(primary)+" not found")
	})
}

func TestService_DeleteAsset_CascadesAndUnknownIs404(t *testing.T) {
	coretest.Configs(func(name string, cfg siblingcore.Config, p siblingcore.Portfolio) {
		f := newStore(cfg)
		aid, sub := f.addAsset(p, "DA", "100", "1")
		svc := newSvc(f)
		if err := svc.DeleteAsset(ctx, aid); err != nil || !f.Sub(sub).Deleted {
			t.Fatalf("%s: cascade %v", name, err)
		}
		appErr(t, svc.DeleteAsset(ctx, aid), 404, "", "Asset with id "+itoa(aid)+" not found")
	})
}

func TestService_UpdateAsset_TriStateTargetsAndName(t *testing.T) {
	coretest.Configs(func(name string, cfg siblingcore.Config, p siblingcore.Portfolio) {
		f := newStore(cfg)
		aid, _ := f.addAsset(p, "T", "40", "1")
		f.asset(aid).TargetAllocationPctRiskOff = coretest.DP("30")
		svc := newSvc(f)
		renamed := "Renamed"
		dto, err := svc.UpdateAsset(ctx, aid, UpdateAssetInput{TargetAllocationPct: httpx.Optional[decimal.Decimal]{Set: true, Value: coretest.DP("0")}, Name: &renamed})
		if err != nil || dto.Name != "Renamed" || !dto.TargetAllocationPct.IsZero() || dto.TargetAllocationPctRiskOff == nil {
			t.Fatalf("%s: zero is explicit, absent keeps: %v %+v", name, err, dto.Asset)
		}
		dto, err = svc.UpdateAsset(ctx, aid, UpdateAssetInput{TargetAllocationPctRiskOff: httpx.Optional[decimal.Decimal]{Set: true}})
		if err != nil || dto.TargetAllocationPctRiskOff != nil || dto.TargetAllocationPct == nil {
			t.Fatalf("%s: explicit null clears: %v %+v", name, err, dto.Asset)
		}
		_, err = svc.UpdateAsset(ctx, aid, UpdateAssetInput{TargetAllocationPctRiskOff: httpx.Optional[decimal.Decimal]{Set: true, Value: coretest.DP("-1")}})
		appErr(t, err, 400, "targetAllocationPct", "must be between 0 and 100")
		_, err = svc.UpdateAsset(ctx, 555, UpdateAssetInput{Name: &renamed})
		appErr(t, err, 404, "", "Asset with id 555 not found")
	})
}

func TestService_SetPrimary(t *testing.T) {
	coretest.Configs(func(name string, cfg siblingcore.Config, p siblingcore.Portfolio) {
		f := newStore(cfg)
		aid, primary := f.addAsset(p, "P", "60", "1")
		sib := f.addSub(p, aid, "P2", "2", false)
		other, otherSub := f.addAsset(p, "Q", "40", "1")
		f.Sub(primary).Holding.TotalSpentPln = coretest.DP("10")
		svc := newSvc(f)
		dto, err := svc.SetPrimary(ctx, aid, sib)
		if err != nil || dto.Subassets[0].ID != sib || !dto.Subassets[0].IsPrimary || f.Sub(primary).IsPrimary {
			t.Fatalf("%s: flip %v %+v", name, err, dto.Subassets)
		}
		if c := f.Sub(primary).Holding; c.TotalSpentPln == nil || !c.TotalSpentPln.Equal(coretest.D("10")) || !c.Units.Total().Equal(coretest.D("1")) {
			t.Fatalf("%s: a flip never moves units or cost: %+v", name, c)
		}
		_, err = svc.SetPrimary(ctx, aid, otherSub)
		appErr(t, err, 400, "subassetId", "subasset "+itoa(otherSub)+" does not belong to asset "+itoa(aid))
		_, err = svc.SetPrimary(ctx, 999, sib)
		appErr(t, err, 404, "", "Asset with id 999 not found")

		f.asset(other).TargetAllocationPct = coretest.DP("35")
		writes := len(f.Writes)
		_, err = svc.SetPrimary(ctx, aid, primary)
		msg := "Invariant violated: asset targetAllocationPct would sum to 95.0000 (expected 100)"
		if p != siblingcore.WholeBook {
			msg = "Invariant violated: portfolio " + string(p) + "'s asset targetAllocationPct would sum to 95.0000 (expected 100)"
		}
		appErr(t, err, 400, "", msg)
		if len(f.Writes) != writes {
			t.Fatalf("%s: the guard runs before any write", name)
		}
	})
}

func TestService_GroupProfit_UsesEverySubassetsCostAndSurvivesAFlip(t *testing.T) {
	coretest.Configs(func(name string, cfg siblingcore.Config, p siblingcore.Portfolio) {
		f := newStore(cfg)
		aid, primary := f.addAsset(p, "G", "100", "1")
		sib := f.addSub(p, aid, "G2", "1", false)
		f.Sub(primary).Holding.TotalSpentPln = coretest.DP("50")
		f.Sub(sib).Holding.TotalSpentPln = coretest.DP("50")
		f.Prices = map[string]decimal.Decimal{"G.WA": coretest.D("60"), "G2.WA": coretest.D("90")}
		svc := newSvc(f)
		before, err := svc.Get(ctx, primary)
		if err != nil || before.Computed.ProfitPct == nil || !before.Computed.ProfitPct.Equal(coretest.D("0.5")) {
			t.Fatalf("%s: (150-100)/100: %v %+v", name, err, before.Computed)
		}
		after, err := svc.SetPrimary(ctx, aid, sib)
		if err != nil || after.Computed.ProfitPct == nil || !after.Computed.ProfitPct.Equal(*before.Computed.ProfitPct) {
			t.Fatalf("%s: flip keeps the group profit: %v %+v", name, err, after.Computed)
		}
	})
}

func TestService_UnknownPortfolioIs400_PortfolioC(t *testing.T) {
	f := newStore(coretest.PortfolioC)
	svc := newSvc(f)
	_, _, err := svc.List(ctx, "NOPE", 0, 20)
	appErr(t, err, 400, "portfolio", "No enum constant pl.shared.entity.enums.Portfolio.NOPE")
	_, err = svc.Create(ctx, CreateInput{Ticker: "X", NewAsset: &NewAssetInput{Name: "X", Exposure: "STOCKS", AssetClass: "ETF"}})
	appErr(t, err, 400, "portfolio", "Required request parameter 'portfolio' is missing")
	if len(f.Writes) != 0 {
		t.Fatal("nothing written")
	}
}

func itoa(v int64) string { return decimal.NewFromInt(v).String() }
