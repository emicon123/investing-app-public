import { z } from "zod";
import { apiResponseSchema } from "@common/app-core";

export const cashPositionSchema = z.object({
  id: z.number().int(),
  location: z.string(),
  amountPln: z.number(),
  recordedAt: z.string().datetime({ offset: true }),
});

export const upsertCashPositionRequestSchema = z.object({
  positions: z.array(
    z.object({
      location: z.string(),
      amountPln: z.number(),
    })
  ),
  recordedAt: z.string().datetime({ offset: true }),
});

export const cashPositionListResponseSchema = apiResponseSchema(z.array(cashPositionSchema));

export type CashPosition = z.infer<typeof cashPositionSchema>;
export type UpsertCashPositionRequest = z.infer<typeof upsertCashPositionRequestSchema>;
export type CashPositionListResponse = z.infer<typeof cashPositionListResponseSchema>;
