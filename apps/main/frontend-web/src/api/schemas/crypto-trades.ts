import { z } from "zod";
import { exchangeSchema, accountTypeSchema, currencySchema, tradeSideSchema, cryptoTradeSourceSchema } from "./enums.js";
import { apiResponseSchema, pagedResponseSchema } from "@common/app-core";

export const cryptoTradeSchema = z.object({
  id: z.number().int(),
  tradeDate: z.string(),
  ticker: z.string(),
  exchange: exchangeSchema,
  accountType: accountTypeSchema,
  side: tradeSideSchema,
  quantity: z.number(),
  unitPrice: z.number(),
  priceCurrency: currencySchema,
  totalAmount: z.number(),
  amountPln: z.number(),
  source: cryptoTradeSourceSchema,
  notes: z.string().nullable().optional(),
  createdAt: z.string().datetime({ offset: true }),
});

export const createCryptoTradeRequestSchema = z.object({
  tradeDate: z.string(),
  ticker: z.string(),
  exchange: exchangeSchema,
  accountType: accountTypeSchema,
  side: tradeSideSchema,
  quantity: z.number(),
  unitPrice: z.number(),
  priceCurrency: currencySchema,
  totalAmount: z.number(),
  amountPln: z.number(),
  notes: z.string().nullable().optional(),
});

export const cryptoTradeResponseSchema = apiResponseSchema(cryptoTradeSchema);
export const cryptoTradeListResponseSchema = pagedResponseSchema(cryptoTradeSchema);

export const cryptoTaxTickerSummarySchema = z.object({
  ticker: z.string().optional(),
  totalBuysPln: z.number().optional(),
  totalSellsPln: z.number().optional(),
  netGainPln: z.number().optional(),
  estimatedBelkaTaxPln: z.number().optional(),
});

export const cryptoTaxSummarySchema = z.object({
  year: z.number().int(),
  totalBuysPln: z.number(),
  totalSellsPln: z.number(),
  netGainPln: z.number(),
  estimatedBelkaTaxPln: z.number(),
  byTicker: z.array(cryptoTaxTickerSummarySchema),
});

export const cryptoTaxSummaryResponseSchema = apiResponseSchema(cryptoTaxSummarySchema);

export type CryptoTrade = z.infer<typeof cryptoTradeSchema>;
export type CreateCryptoTradeRequest = z.infer<typeof createCryptoTradeRequestSchema>;
export type CryptoTradeResponse = z.infer<typeof cryptoTradeResponseSchema>;
export type CryptoTradeListResponse = z.infer<typeof cryptoTradeListResponseSchema>;
export type CryptoTaxTickerSummary = z.infer<typeof cryptoTaxTickerSummarySchema>;
export type CryptoTaxSummary = z.infer<typeof cryptoTaxSummarySchema>;
export type CryptoTaxSummaryResponse = z.infer<typeof cryptoTaxSummaryResponseSchema>;
