import { z } from "zod";

export const accountTypeSchema = z.enum(["IKE_TATA", "IKE_MAMA", "IKZE_TATA", "IKZE_MAMA", "INNE"]);
export const exchangeSchema = z.enum(["XTB", "BINANCE", "OTHER"]);
export const assetClassSchema = z.enum(["ETF", "METALS", "CRYPTO", "STOCK", "CURRENCY"]);
export const exposureSchema = z.enum(["CRYPTO", "METALS", "STOCKS", "COMMODITIES", "CURRENCY"]);
export const currencySchema = z.enum(["PLN", "EUR", "USD", "CHF", "GBP"]);
export const deviationStatusSchema = z.enum(["OVER", "UNDER", "ON_TARGET"]);
export const fxRateSourceSchema = z.enum(["YAHOO_FINANCE", "NBP_API"]);

export type AccountType = z.infer<typeof accountTypeSchema>;
export type Exchange = z.infer<typeof exchangeSchema>;
export type AssetClass = z.infer<typeof assetClassSchema>;
export type Exposure = z.infer<typeof exposureSchema>;
export type Currency = z.infer<typeof currencySchema>;
export type DeviationStatus = z.infer<typeof deviationStatusSchema>;
export type FxRateSource = z.infer<typeof fxRateSourceSchema>;
