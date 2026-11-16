import { z } from "zod";
import { exchangeSchema, currencySchema, deviationStatusSchema, accountTypeSchema } from "./enums.js";
import { metaSchema } from "@common/app-core";

export const accountAllocSchema = z.object({
  accountType: accountTypeSchema,
  allocPln: z.number(),
  unitsToBuy: z.number(),
});

export const buyListItemSchema = z.object({
  ticker: z.string(),
  name: z.string(),
  exchange: exchangeSchema,
  purchaseCurrency: currencySchema,
  allocatedPln: z.number(),
  allocatedNative: z.number(),
  unitsToBuy: z.number(),
  currentPct: z.number(),
  targetPct: z.number(),
  deviationPct: z.number(),
  regressionWeight: z.number().nullable().optional().transform((v) => v ?? null),
  accountAllocations: z.array(accountAllocSchema).nullable().optional().transform((v) => v ?? null),
  availableTickers: z.array(z.string()).nullable().optional().transform((v) => v ?? null),
});

export const donutSliceSchema = z.object({
  ticker: z.string(),
  name: z.string(),
  currentPct: z.number(),
  targetPct: z.number(),
  valuePln: z.number(),
  deviationPct: z.number(),
  status: deviationStatusSchema,
});

export const rebalancingResultSchema = z.object({
  cashOnHandPln: z.number().nullable().optional().transform((v) => v ?? null),
  deployableCashPln: z.number().nullable().optional().transform((v) => v ?? null),
  totalPortfolioValuePln: z.number().nullable().optional().transform((v) => v ?? null),
  pricesRefreshedAt: z.string().datetime({ offset: true }).optional(),
  buyList: z.array(buyListItemSchema),
  donutData: z.array(donutSliceSchema),
});

export const rebalancingResponseSchema = z.object({
  data: rebalancingResultSchema,
  meta: metaSchema,
});

export type AccountAlloc = z.infer<typeof accountAllocSchema>;
export type BuyListItem = z.infer<typeof buyListItemSchema>;
export type DonutSlice = z.infer<typeof donutSliceSchema>;
export type RebalancingResult = z.infer<typeof rebalancingResultSchema>;
export type RebalancingResponse = z.infer<typeof rebalancingResponseSchema>;

export const accountUnitAllocSchema = z.object({
  accountType: accountTypeSchema,
  units: z.number().min(0),
});

export const confirmPurchaseItemSchema = z.object({
  ticker: z.string(),
  accountAllocations: z.array(accountUnitAllocSchema),
});

export const confirmPurchasesRequestSchema = z.object({
  items: z.array(confirmPurchaseItemSchema),
});

export type AccountUnitAlloc = z.infer<typeof accountUnitAllocSchema>;
export type ConfirmPurchaseItem = z.infer<typeof confirmPurchaseItemSchema>;
export type ConfirmPurchasesRequest = z.infer<typeof confirmPurchasesRequestSchema>;
