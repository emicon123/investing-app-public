import { z } from "zod";
import { portfolioTypeSchema, exchangeSchema, currencySchema, accountTypeSchema } from "./enums.js";
import { apiResponseSchema, pagedResponseSchema } from "@common/app-core";

export const depositSchema = z.object({
  id: z.number().int(),
  depositDate: z.string(),
  amount: z.number(),
  currency: currencySchema,
  exchange: exchangeSchema,
  accountType: accountTypeSchema,
  portfolio: portfolioTypeSchema,
  comment: z.string().nullable().optional(),
  createdAt: z.string().datetime({ offset: true }),
});

export const createDepositRequestSchema = z
  .object({
    depositDate: z.string(),
    amount: z.number(),
    currency: currencySchema,
    exchange: exchangeSchema,
    accountType: accountTypeSchema,
    portfolio: portfolioTypeSchema,
    comment: z.string().nullable().optional(),
  })
  .superRefine((v, ctx) => {
    if ((v.accountType === "IKE" || v.accountType === "IKZE") && v.currency !== "PLN") {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        message: "Depozyty IKE/IKZE muszą być w PLN",
        path: ["currency"],
      });
    }
  });

export const depositResponseSchema = apiResponseSchema(depositSchema);
export const depositListResponseSchema = pagedResponseSchema(depositSchema);

export const xirrResultSchema = z.object({
  portfolio: portfolioTypeSchema.optional(),
  xirrPct: z.number().nullish(),
  totalInvestedPln: z.number().nullish(),
  currentValuePln: z.number().nullish(),
  simpleReturnPct: z.number().nullish(),
  yearsElapsed: z.number().nullish(),
  computedAt: z.string().datetime({ offset: true }).optional(),
});

export const xirrResponseSchema = apiResponseSchema(xirrResultSchema);

export const accountCapUsageItemSchema = z.object({
  accountType: accountTypeSchema.optional(),
  year: z.number().int().optional(),
  totalDepositedPln: z.number().optional(),
  annualCapPln: z.number().optional(),
  // ADR-058: silently incorporates any manual correction the user has entered via
  // PUT /deposits/account-cap-usage/adjustment — do not re-derive it client-side.
  remainingPln: z.number().optional(),
  utilizationPct: z.number().optional(),
});

export const accountCapUsageResponseSchema = apiResponseSchema(z.array(accountCapUsageItemSchema));

// ADR-058: request for PUT /deposits/account-cap-usage/adjustment. remainingPln is the real
// remaining limit the user is asserting (e.g. read off a broker statement), not a signed delta
// — the backend solves for whatever internal correction produces exactly that remaining figure.
// accountType is restricted to IKE/IKZE server-side (REGULAR -> 400 VALIDATION_ERROR); not
// re-validated client-side since the only UI surface offering this control already restricts
// the choice to IKE/IKZE cards.
export const accountCapAdjustmentRequestSchema = z.object({
  accountType: accountTypeSchema,
  year: z.number().int(),
  remainingPln: z.number(),
});

// The response is just the freshly-recomputed AccountCapUsageItem for that (accountType, year).
export const accountCapAdjustmentResponseSchema = apiResponseSchema(accountCapUsageItemSchema);

export type Deposit = z.infer<typeof depositSchema>;
export type CreateDepositRequest = z.infer<typeof createDepositRequestSchema>;
export type DepositResponse = z.infer<typeof depositResponseSchema>;
export type DepositListResponse = z.infer<typeof depositListResponseSchema>;
export type XirrResult = z.infer<typeof xirrResultSchema>;
export type XirrResponse = z.infer<typeof xirrResponseSchema>;
export type AccountCapUsageItem = z.infer<typeof accountCapUsageItemSchema>;
export type AccountCapUsageResponse = z.infer<typeof accountCapUsageResponseSchema>;
export type AccountCapAdjustmentRequest = z.infer<typeof accountCapAdjustmentRequestSchema>;
export type AccountCapAdjustmentResponse = z.infer<typeof accountCapAdjustmentResponseSchema>;
