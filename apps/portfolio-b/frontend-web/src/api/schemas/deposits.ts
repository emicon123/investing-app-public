import { z } from "zod";
import { apiResponseSchema, pagedResponseSchema } from "@common/app-core";

export const depositSchema = z.object({
  id: z.number().int(),
  depositDate: z.string(),
  amount: z.number(),
  comment: z.string().nullable().optional(),
  createdAt: z.string().datetime({ offset: true }),
});

export const createDepositRequestSchema = z.object({
  depositDate: z.string(),
  amount: z.number(),
  comment: z.string().nullable().optional(),
});

export const depositResponseSchema = apiResponseSchema(depositSchema);
export const depositListResponseSchema = pagedResponseSchema(depositSchema);

export const xirrResultSchema = z.object({
  xirrPct: z.number().nullish(),
  totalInvestedPln: z.number().nullish(),
  currentValuePln: z.number().nullish(),
  simpleReturnPct: z.number().nullish(),
  yearsElapsed: z.number().nullish(),
  computedAt: z.string().datetime({ offset: true }).optional(),
});

export const xirrResponseSchema = apiResponseSchema(xirrResultSchema);

export type Deposit = z.infer<typeof depositSchema>;
export type CreateDepositRequest = z.infer<typeof createDepositRequestSchema>;
export type DepositResponse = z.infer<typeof depositResponseSchema>;
export type DepositListResponse = z.infer<typeof depositListResponseSchema>;
export type XirrResult = z.infer<typeof xirrResultSchema>;
export type XirrResponse = z.infer<typeof xirrResponseSchema>;
