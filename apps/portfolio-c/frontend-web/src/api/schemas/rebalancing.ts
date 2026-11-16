import { z } from "zod";
import { exchangeSchema, currencySchema, deviationStatusSchema, portfolioSchema } from "./enums.js";
import { metaSchema } from "@common/app-core";

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
  portfolio: portfolioSchema,
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

export type BuyListItem = z.infer<typeof buyListItemSchema>;
export type DonutSlice = z.infer<typeof donutSliceSchema>;
export type RebalancingResult = z.infer<typeof rebalancingResultSchema>;
export type RebalancingResponse = z.infer<typeof rebalancingResponseSchema>;

/**
 * Body for POST /purchases/calculate. depositAmountPln is optional for FIXED
 * (falls back server-side to the stored fixed_monthly_deposit_default_pln setting)
 * and required for VARIABLE (no stored default — see docs/adr/001-portfolio-dimension.md §2).
 */
export const calculateRequestSchema = z.object({
  portfolio: portfolioSchema,
  depositAmountPln: z.number().positive().optional(),
});

export const confirmPurchaseItemSchema = z.object({
  ticker: z.string(),
  units: z.number().min(0),
});

export const confirmPurchasesRequestSchema = z.object({
  portfolio: portfolioSchema,
  depositAmountPln: z.number().positive(),
  items: z.array(confirmPurchaseItemSchema),
});

export type CalculateRequest = z.infer<typeof calculateRequestSchema>;
export type ConfirmPurchaseItem = z.infer<typeof confirmPurchaseItemSchema>;
export type ConfirmPurchasesRequest = z.infer<typeof confirmPurchasesRequestSchema>;
