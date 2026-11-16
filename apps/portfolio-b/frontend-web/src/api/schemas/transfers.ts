import { z } from "zod";
import { accountTypeSchema } from "./enums.js";
import { metaSchema } from "@common/app-core";

export const soldLegSchema = z.object({
  ticker: z.string(),
  accountType: accountTypeSchema,
  units: z.number(),
  pricePln: z.number(),
});

export const buyLegSchema = z.object({
  ticker: z.string(),
  accountType: accountTypeSchema,
  units: z.number(),
  pricePln: z.number(),
});

export const positionTransferRequestSchema = z.object({
  sold: soldLegSchema,
  buys: z.array(buyLegSchema).min(1),
});

export const positionTransferResponseSchema = z.object({
  data: z.object({
    transferId: z.string(),
    sold: soldLegSchema,
    buys: z.array(buyLegSchema),
  }),
  meta: metaSchema,
});

export type SoldLeg = z.infer<typeof soldLegSchema>;
export type BuyLeg = z.infer<typeof buyLegSchema>;
export type PositionTransferRequest = z.infer<typeof positionTransferRequestSchema>;
export type PositionTransferResponse = z.infer<typeof positionTransferResponseSchema>;
