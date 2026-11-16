import { describe, expect, it } from "vitest";
import { z } from "zod";
import { entryPriceFieldsShape } from "./entry-price.js";

// The `computed` block of an Asset as the servers emit it (task 161c-1): a null is OMITTED on the
// wire, so absent and explicit-null both have to normalise to "unknown".
const computedSchema = z.object(entryPriceFieldsShape);

describe("entryPriceFieldsShape (task 161c-3)", () => {
  it("keeps the entry-price fields of a real-shaped USD-primary payload", () => {
    const parsed = computedSchema.parse({
      profitPct: -0.04,
      avgBuyPricePln: 500000,
      avgBuyPriceNative: 125000,
      equivalentUnits: 0.12,
      costlessSubassets: ["PL.ETNVIRBTCP"],
    });

    expect(parsed).toEqual({
      profitPct: -0.04,
      avgBuyPricePln: 500000,
      avgBuyPriceNative: 125000,
      equivalentUnits: 0.12,
      costlessSubassets: ["PL.ETNVIRBTCP"],
    });
  });

  it("defaults costlessSubassets to [] and every absent number to null", () => {
    const parsed = computedSchema.parse({});

    expect(parsed.costlessSubassets).toEqual([]);
    expect(parsed.avgBuyPricePln).toBeNull();
    expect(parsed.avgBuyPriceNative).toBeNull();
    expect(parsed.equivalentUnits).toBeNull();
    expect(parsed.profitPct).toBeNull();
  });

  it("treats explicit nulls like absent keys (RETIREMENT / unknown entry price)", () => {
    const parsed = computedSchema.parse({
      profitPct: null,
      avgBuyPricePln: null,
      avgBuyPriceNative: null,
      equivalentUnits: null,
      costlessSubassets: null,
    });

    expect(parsed.avgBuyPriceNative).toBeNull();
    expect(parsed.costlessSubassets).toEqual([]);
  });

  it("does not turn an absent entry price into 0", () => {
    expect(computedSchema.parse({}).avgBuyPricePln).not.toBe(0);
  });

  it("rejects a non-string ticker in costlessSubassets", () => {
    expect(computedSchema.safeParse({ costlessSubassets: [1] }).success).toBe(false);
  });
});
