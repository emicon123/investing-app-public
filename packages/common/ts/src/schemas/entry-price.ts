import { z } from "zod";

// The entry-price slice of an Asset's `computed` block (task 161), identical in all three apps, so
// it lives here once and each app's `computedPositionFieldsSchema` spreads `entryPriceFieldsShape`.
// A plain `z.object` STRIPS unknown keys, so a field the server emits but the schema does not name
// never reaches the UI — that is why the shape must list every field.
//
// The servers omit a null field from the wire entirely (`omitempty`): absent key and explicit null
// both normalise to `null` ("unknown" — render "—", never 0).
const nullableNumber = () => z.number().nullish().transform((v) => v ?? null);

export const entryPriceFieldsShape = {
  /** Group profit `Σvalue / Σcost − 1` as a FRACTION (0.1 = +10 %); null when no cost is recorded. */
  profitPct: nullableNumber(),
  /** Entry price: PLN per 1 unit of the PRIMARY subasset; null whenever `profitPct` is null (any portfolio, ADR-104). */
  avgBuyPricePln: nullableNumber(),
  /**
   * The same entry price in the primary's own `priceCurrency` at the CURRENT exchange rate
   * (`avgBuyPricePln / fx`) — it drifts with the rate by design. Equals `avgBuyPricePln` for a PLN primary.
   */
  avgBuyPriceNative: nullableNumber(),
  /** The asset's holdings expressed in units of the primary (`valuePln / pricePln`). */
  equivalentUnits: nullableNumber(),
  /**
   * Tickers of priced subassets that hold units but have no recorded cost. Present only when an
   * entry price exists; the UI notes that their cost is assumed included in the group cost.
   */
  costlessSubassets: z.array(z.string()).nullish().transform((v) => v ?? []),
};

export const entryPriceFieldsSchema = z.object(entryPriceFieldsShape);

export type EntryPriceFields = z.infer<typeof entryPriceFieldsSchema>;
