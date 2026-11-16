import { z } from "zod";
import { apiResponseSchema } from "@common/app-core";
import { portfolioTypeSchema } from "./enums.js";

export const transferLegSchema = z.object({
  ticker: z.string().min(1),
  accountType: z.enum(["IKE", "IKZE", "REGULAR"]),
  regularCurrency: z.enum(["PLN", "EUR", "USD"]).optional(),
  units: z.number().positive(),
  pricePln: z.number().positive(),
});

// ADR-063: a transfer is always scoped to a single portfolio's book — `portfolio` is
// required at the request root (not per-leg) so the sold leg and every buy leg are
// resolved as (ticker, portfolio) server-side, never by ticker alone (tickers like HUT,
// SOL-USD, PL.ETNVIRSOL exist as separate rows in both MAIN and RETIREMENT).
export const rebalancingTransferRequestSchema = z.object({
  portfolio: portfolioTypeSchema,
  sold: transferLegSchema,
  buys: z.array(transferLegSchema).min(1),
});

export const rebalancingTransferResponseSchema = apiResponseSchema(
  z.object({
    affectedTickers: z.array(z.string()),
    message: z.string().optional(),
  })
);

export type TransferLeg = z.infer<typeof transferLegSchema>;
export type RebalancingTransferRequest = z.infer<typeof rebalancingTransferRequestSchema>;
export type RebalancingTransferResponse = z.infer<typeof rebalancingTransferResponseSchema>;
