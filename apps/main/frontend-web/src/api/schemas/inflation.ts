import { z } from "zod";
import { pagedResponseSchema } from "@common/app-core";

export const inflationEntrySchema = z.object({
  id: z.number().int(),
  year: z.number().int(),
  month: z.number().int(),
  cpiValue: z.number(),
  createdAt: z.string().datetime({ offset: true }),
});

export const inflationListResponseSchema = pagedResponseSchema(inflationEntrySchema);

export type InflationEntry = z.infer<typeof inflationEntrySchema>;
export type InflationListResponse = z.infer<typeof inflationListResponseSchema>;
