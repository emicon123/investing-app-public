import { z } from "zod";

export const portfolioTypeSchema = z.enum(["MAIN", "RETIREMENT"]);
export const accountTypeSchema = z.enum(["IKE", "IKZE", "REGULAR"]);
export const exchangeSchema = z.enum(["XTB", "BINANCE"]);
export const assetClassSchema = z.enum(["STOCK", "CRYPTO", "ETF", "CURRENCY"]);
export const exposureSchema = z.enum(["CRYPTO", "METALS", "CASH", "QUANTUM", "BONDS", "EXPERIMENTS", "EQUITY"]);
export const currencySchema = z.enum(["PLN", "EUR", "USD", "CHF", "GBP"]);
export const tradeSideSchema = z.enum(["BUY", "SELL"]);
export const deviationStatusSchema = z.enum(["OVER", "UNDER", "ON_TARGET"]);
export const fxRateSourceSchema = z.enum(["YAHOO_FINANCE", "NBP_API"]);
export const cryptoTradeSourceSchema = z.enum(["MIGRATION", "MANUAL"]);
// asset_purchases.trade_type (ADR-057) — classifies *why* a trade happened, drives the
// trade-history chart's marker color/shade. Mirrors trade_type_enum / TradeType in
// docs/openapi/components/schemas/enums.yaml.
export const tradeTypeSchema = z.enum([
  "DCA_BUY",
  "MIGRATION_BUY",
  "MIGRATION_SELL",
  "REBALANCING_TRIM_SELL",
  "REBALANCING_REINVEST_BUY",
  "SEED_IMPORT",
]);

export type PortfolioType = z.infer<typeof portfolioTypeSchema>;
export type AccountType = z.infer<typeof accountTypeSchema>;
export type Exchange = z.infer<typeof exchangeSchema>;
export type AssetClass = z.infer<typeof assetClassSchema>;
export type Exposure = z.infer<typeof exposureSchema>;
export type Currency = z.infer<typeof currencySchema>;
export type TradeSide = z.infer<typeof tradeSideSchema>;
export type DeviationStatus = z.infer<typeof deviationStatusSchema>;
export type FxRateSource = z.infer<typeof fxRateSourceSchema>;
export type CryptoTradeSource = z.infer<typeof cryptoTradeSourceSchema>;
export type TradeType = z.infer<typeof tradeTypeSchema>;
