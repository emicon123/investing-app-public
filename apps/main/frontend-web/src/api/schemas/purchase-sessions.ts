import { z } from "zod";
import { portfolioTypeSchema, accountTypeSchema } from "./enums.js";
import { apiResponseSchema, pagedResponseSchema } from "@common/app-core";

export const portfoliosIncludedSchema = z.enum(["BOTH", "MAIN", "RETIREMENT"]);
export const sessionStatusSchema = z.enum(["PLANNED", "DONE"]);

export const createPurchaseSessionRequestSchema = z.object({
  portfoliosIncluded: portfoliosIncludedSchema,
  cashOverridePln: z.number().min(0).nullable().optional().transform((v) => v ?? null),
  notes: z.string().max(500).nullable().optional().transform((v) => v ?? null),
});

// No sessionId: items only ever arrive nested inside their session, and the OpenAPI
// PurchaseSessionItemPlanned contract (Go backend) doesn't carry it. Requiring it made every
// create/GET-active response fail parse after the session was already committed.
export const purchaseSessionItemSchema = z.object({
  id: z.number().int(),
  portfolio: portfolioTypeSchema,
  ticker: z.string().max(20),
  accountType: accountTypeSchema,
  plannedUnits: z.number().nullable().optional().transform((v) => v ?? null),
  plannedValuePln: z.number().nullable().optional().transform((v) => v ?? null),
  priceCurrency: z.string().nullable().optional().transform((v) => v ?? null),
  actualUnits: z.number().nullable().optional().transform((v) => v ?? null),
  actualValuePln: z.number().nullable().optional().transform((v) => v ?? null),
  pricePlnAtPlan: z.number().nullable().optional().transform((v) => v ?? null),
  isMigrationSell: z.boolean(),
  isMigrationBuy: z.boolean(),
});

export const currencyBreakdownSchema = z.object({
  xtbPln: z.number(),
  xtbEur: z.number(),
  xtbEurPln: z.number(),
  xtbUsd: z.number(),
  xtbUsdPln: z.number(),
  binanceEur: z.number(),
  binanceEurPln: z.number(),
  usdPlnRate: z.number().nullable().optional().transform((v) => v ?? null),
  eurPlnRate: z.number().nullable().optional().transform((v) => v ?? null),
});

export const accountLimitsSchema = z.object({
  ikeUsedPln: z.number(),
  ikeRemainingPln: z.number(),
  ikzeUsedPln: z.number(),
  ikzeRemainingPln: z.number(),
  ikeAnnualLimitPln: z.number(),
  ikzeAnnualLimitPln: z.number(),
});

export const purchaseSessionDetailSchema = z.object({
  id: z.number().int(),
  status: sessionStatusSchema,
  plannedAt: z.string().datetime({ offset: true }),
  doneAt: z.string().datetime({ offset: true }).nullable().optional().transform((v) => v ?? null),
  notes: z.string().nullable().optional().transform((v) => v ?? null),
  portfoliosIncluded: portfoliosIncludedSchema,
  retirementBudgetPln: z.number().nullable().optional().transform((v) => v ?? null),
  mainBudgetPln: z.number().nullable().optional().transform((v) => v ?? null),
  items: z.array(purchaseSessionItemSchema),
  currencyBreakdown: currencyBreakdownSchema,
  retirementLimitsRemaining: accountLimitsSchema,
  mainLimitsRemaining: accountLimitsSchema,
});

export const purchaseSessionSummarySchema = z.object({
  id: z.number().int(),
  status: sessionStatusSchema,
  plannedAt: z.string().datetime({ offset: true }),
  doneAt: z.string().datetime({ offset: true }).nullable().optional().transform((v) => v ?? null),
  portfoliosIncluded: portfoliosIncludedSchema,
  retirementBudgetPln: z.number().nullable().optional().transform((v) => v ?? null),
  mainBudgetPln: z.number().nullable().optional().transform((v) => v ?? null),
  totalActualPln: z.number().nullable().optional().transform((v) => v ?? null),
});

export const updatePurchaseSessionItemEntrySchema = z.object({
  id: z.number().int(),
  actualUnits: z.number().nullable().optional().transform((v) => v ?? null),
  actualValuePln: z.number().nullable().optional().transform((v) => v ?? null),
});

export const updatePurchaseSessionItemsRequestSchema = z.object({
  items: z.array(updatePurchaseSessionItemEntrySchema).min(1),
});

export const purchaseSessionCompleteResultSchema = z.object({
  sessionId: z.number().int(),
  status: sessionStatusSchema,
  purchasesWritten: z.number().int(),
  updatedPositionIds: z.array(z.number().int()),
  depositsCreated: z.number().int(),
});

export const purchaseSessionResponseSchema = apiResponseSchema(purchaseSessionDetailSchema);
export const purchaseSessionSummaryListResponseSchema = pagedResponseSchema(purchaseSessionSummarySchema);
export const purchaseSessionCompleteResponseSchema = apiResponseSchema(purchaseSessionCompleteResultSchema);

export type PortfoliosIncluded = z.infer<typeof portfoliosIncludedSchema>;
export type SessionStatus = z.infer<typeof sessionStatusSchema>;
export type CreatePurchaseSessionRequest = z.infer<typeof createPurchaseSessionRequestSchema>;
export type PurchaseSessionItem = z.infer<typeof purchaseSessionItemSchema>;
export type CurrencyBreakdown = z.infer<typeof currencyBreakdownSchema>;
export type AccountLimits = z.infer<typeof accountLimitsSchema>;
export type PurchaseSessionDetail = z.infer<typeof purchaseSessionDetailSchema>;
export type PurchaseSessionSummary = z.infer<typeof purchaseSessionSummarySchema>;
export type UpdatePurchaseSessionItemEntry = z.infer<typeof updatePurchaseSessionItemEntrySchema>;
export type UpdatePurchaseSessionItemsRequest = z.infer<typeof updatePurchaseSessionItemsRequestSchema>;
export type PurchaseSessionCompleteResult = z.infer<typeof purchaseSessionCompleteResultSchema>;
export type PurchaseSessionResponse = z.infer<typeof purchaseSessionResponseSchema>;
export type PurchaseSessionSummaryListResponse = z.infer<typeof purchaseSessionSummaryListResponseSchema>;
export type PurchaseSessionCompleteResponse = z.infer<typeof purchaseSessionCompleteResponseSchema>;
