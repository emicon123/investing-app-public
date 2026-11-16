import { z } from "zod";
import { currencySchema, fxRateSourceSchema } from "./enums.js";
import { apiResponseSchema } from "@common/app-core";

export const priceEntrySchema = z.object({
  ticker: z.string(),
  price: z.number(),
  currency: currencySchema,
  pricePln: z.number(),
  fetchedAt: z.string().datetime({ offset: true }),
});

export const fxRateEntrySchema = z.object({
  fromCurrency: currencySchema,
  rateToPln: z.number(),
  source: fxRateSourceSchema,
  fetchedAt: z.string().datetime({ offset: true }),
});

export const priceListResponseSchema = apiResponseSchema(z.array(priceEntrySchema));
export const fxRateListResponseSchema = apiResponseSchema(z.array(fxRateEntrySchema));

export type PriceEntry = z.infer<typeof priceEntrySchema>;
export type FxRateEntry = z.infer<typeof fxRateEntrySchema>;
export type PriceListResponse = z.infer<typeof priceListResponseSchema>;
export type FxRateListResponse = z.infer<typeof fxRateListResponseSchema>;
