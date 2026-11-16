import { z } from "zod";
import { currencySchema } from "./enums.js";
import { apiResponseSchema, metaSchema } from "@common/app-core";

export const binanceTransferSourceSchema = z.enum(["MANUAL", "SESSION"]);

export const binanceTransferSchema = z.object({
  id: z.number().int(),
  transferDate: z.string(),
  amountPln: z.number(),
  amountNative: z.number().nullable().optional(),
  nativeCurrency: currencySchema.nullable().optional(),
  comment: z.string().nullable().optional(),
  source: binanceTransferSourceSchema,
  sessionId: z.number().int().nullable().optional(),
  createdAt: z.string().datetime({ offset: true }),
});

export const createBinanceTransferRequestSchema = z
  .object({
    transferDate: z.string(),
    amountPln: z.number().positive(),
    amountNative: z.number().positive().nullable().optional(),
    nativeCurrency: currencySchema.nullable().optional(),
    comment: z.string().nullable().optional(),
  })
  .superRefine((v, ctx) => {
    if ((v.amountNative == null) !== (v.nativeCurrency == null)) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        message: "amountNative and nativeCurrency must be provided together",
        path: ["nativeCurrency"],
      });
    }
  });

export const binanceTransferYearlySumSchema = z.object({
  year: z.number().int(),
  totalPln: z.number(),
});

export const binanceTransfersListResponseSchema = z.object({
  data: z.array(binanceTransferSchema),
  yearlySums: z.array(binanceTransferYearlySumSchema),
  meta: metaSchema,
});

export const binanceTransferResponseSchema = apiResponseSchema(binanceTransferSchema);

export type BinanceTransferSource = z.infer<typeof binanceTransferSourceSchema>;
export type BinanceTransfer = z.infer<typeof binanceTransferSchema>;
export type CreateBinanceTransferRequest = z.infer<typeof createBinanceTransferRequestSchema>;
export type BinanceTransferYearlySum = z.infer<typeof binanceTransferYearlySumSchema>;
export type BinanceTransfersListResponse = z.infer<typeof binanceTransfersListResponseSchema>;
export type BinanceTransferResponse = z.infer<typeof binanceTransferResponseSchema>;
