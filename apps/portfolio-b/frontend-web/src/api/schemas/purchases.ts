import { z } from "zod";
import { metaSchema } from "@common/app-core";

export const purchaseLotSchema = z.object({
  ticker: z.string(),
  name: z.string(),
  units: z.number(),
  pricePln: z.number(),
  costPln: z.number(),
  currentPricePln: z.number().nullable().optional().transform((v) => v ?? null),
  currentValuePln: z.number().nullable().optional().transform((v) => v ?? null),
  gainPln: z.number().nullable().optional().transform((v) => v ?? null),
  gainPct: z.number().nullable().optional().transform((v) => v ?? null),
  priceAvailable: z.boolean(),
});

export const purchaseSessionSchema = z.object({
  sessionId: z.string(),
  purchasedAt: z.string(),
  totalCostPln: z.number(),
  totalCurrentValuePln: z.number(),
  totalGainPln: z.number(),
  totalGainPct: z.number().nullable().optional().transform((v) => v ?? null),
  lots: z.array(purchaseLotSchema),
});

export const purchaseHistoryResponseSchema = z.object({
  data: z.object({
    sessions: z.array(purchaseSessionSchema),
  }),
  meta: metaSchema,
});

export type PurchaseLot = z.infer<typeof purchaseLotSchema>;
export type PurchaseSession = z.infer<typeof purchaseSessionSchema>;
export type PurchaseHistoryResponse = z.infer<typeof purchaseHistoryResponseSchema>;
