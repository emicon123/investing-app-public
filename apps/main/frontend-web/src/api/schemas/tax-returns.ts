import { z } from "zod";
import { pagedResponseSchema } from "@common/app-core";

export const taxReturnSchema = z.object({
  id: z.number().int(),
  year: z.number().int(),
  grossGain: z.number(),
  taxPaid: z.number(),
  effectiveRatePct: z.number(),
  createdAt: z.string().datetime({ offset: true }),
  updatedAt: z.string().datetime({ offset: true }),
});

export const taxReturnListResponseSchema = pagedResponseSchema(taxReturnSchema);

export type TaxReturn = z.infer<typeof taxReturnSchema>;
export type TaxReturnListResponse = z.infer<typeof taxReturnListResponseSchema>;
