import { z } from "zod";
import { portfolioTypeSchema } from "./enums.js";
import { pagedResponseSchema } from "@common/app-core";

export const historiaEntrySchema = z.object({
  snapshotDate: z.string(),
  portfolio: portfolioTypeSchema,
  totalDeposits: z.number(),
  portfolioValue: z.number(),
  xirr: z.number().nullable().optional(),
  returnPct: z.number(),
  profitPln: z.number(),
  positionValues: z.record(z.string(), z.number()).nullable().optional(),
  inflationAdjustedDeposits: z.number().nullable().optional(),
  realReturnPct: z.number().nullable().optional(),
  annualizedInflationPct: z.number().nullable().optional(),
});

export const historiaListResponseSchema = pagedResponseSchema(historiaEntrySchema);

export type HistoriaEntry = z.infer<typeof historiaEntrySchema>;
export type HistoriaListResponse = z.infer<typeof historiaListResponseSchema>;
