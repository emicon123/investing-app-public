import { z } from "zod";

export const accountCashSchema = z.object({
  ikeTata: z.number(),
  ikeMama: z.number(),
  ikzeTata: z.number(),
  ikzeMama: z.number(),
  updatedAt: z.string().datetime({ offset: true }).nullable().optional(),
  updateReason: z.string().nullable().optional(),
});

export const accountCashUpdateSchema = z.object({
  ikeTata: z.number().min(0),
  ikeMama: z.number().min(0),
  ikzeTata: z.number().min(0),
  ikzeMama: z.number().min(0),
});

export type AccountCash = z.infer<typeof accountCashSchema>;
export type AccountCashUpdate = z.infer<typeof accountCashUpdateSchema>;
