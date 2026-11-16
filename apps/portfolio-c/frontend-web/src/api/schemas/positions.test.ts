import { describe, expect, it } from "vitest";
import { assetSchema } from "./positions.js";

// Real-shaped `GET /positions` asset (task 161c-1 wire: a null entry-price field is OMITTED).
// `computedPositionFieldsSchema` is a plain z.object, so a key it does not name is STRIPPED — this
// pins that the entry-price fields reach the UI.
const baseAsset = {
  id: 7,
  key: "bitcoin",
  name: "Bitcoin",
  portfolio: "VARIABLE",
  exposure: "CRYPTO",
  assetClass: "CRYPTO",
  targetAllocationPct: 50,
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
};

const baseComputed = {
  unitsTotal: 0.12,
  pricePln: 480000,
  priceNative: 120000,
  valuePln: 57600,
  currentPct: 45.2,
  deviationPct: -0.1,
};

const entryPrice = {
  profitPct: -0.04,
  avgBuyPricePln: 500000,
  avgBuyPriceNative: 125000,
  equivalentUnits: 0.12,
  costlessSubassets: ["PL.ETNVIRBTCP"],
};

describe("assetSchema.computed entry-price fields (task 161c-3, portfolioC)", () => {
  it("keeps avgBuyPriceNative, equivalentUnits and costlessSubassets", () => {
    const asset = assetSchema.parse({ ...baseAsset, computed: { ...baseComputed, ...entryPrice } });

    expect(asset.computed?.avgBuyPricePln).toBe(500000);
    expect(asset.computed?.avgBuyPriceNative).toBe(125000);
    expect(asset.computed?.equivalentUnits).toBe(0.12);
    expect(asset.computed?.costlessSubassets).toEqual(["PL.ETNVIRBTCP"]);
  });

  it("defaults costlessSubassets to [] and absent entry-price numbers to null", () => {
    const asset = assetSchema.parse({ ...baseAsset, computed: baseComputed });

    expect(asset.computed?.avgBuyPricePln).toBeNull();
    expect(asset.computed?.avgBuyPriceNative).toBeNull();
    expect(asset.computed?.equivalentUnits).toBeNull();
    expect(asset.computed?.costlessSubassets).toEqual([]);
  });
});
