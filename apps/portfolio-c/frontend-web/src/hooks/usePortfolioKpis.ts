import { createUsePortfolioKpis } from "@common/app-core";
import { computeXirr, getPrices, listHistoria, type Portfolio } from "../api/index.js";
import { queryKeys } from "@/lib/queryKeys";

/** The portfolio page's KPI queries (task 170), bound to portfolioC's API client and keys, per book. */
export const usePortfolioKpis = createUsePortfolioKpis<Portfolio>({
  xirrKey: (portfolio) => queryKeys.deposits.xirr(portfolio),
  fetchXirr: (portfolio) => computeXirr(portfolio),
  historyKey: (portfolio, from) => queryKeys.history.since(portfolio, from),
  fetchHistory: (portfolio, from) => listHistoria({ portfolio, from, size: 100 }),
  pricesKey: queryKeys.prices.all,
  fetchPrices: () => getPrices(),
});
