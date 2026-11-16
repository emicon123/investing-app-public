import { createUsePortfolioKpis } from "@common/app-core";
import { computeXirr, getPrices, listHistoria, type PortfolioType } from "../api/index.js";
import { queryKeys } from "@/lib/queryKeys";

/** The portfolio page's KPI queries (task 170), bound to main's API client and keys. */
export const usePortfolioKpis = createUsePortfolioKpis<PortfolioType>({
  xirrKey: (portfolio) => queryKeys.xirr.portfolio(portfolio),
  fetchXirr: (portfolio) => computeXirr({ portfolio }),
  historyKey: (portfolio, from) => queryKeys.history.since(portfolio, from),
  fetchHistory: (portfolio, from) => listHistoria({ portfolio, from, size: 100 }),
  pricesKey: queryKeys.prices.all,
  fetchPrices: () => getPrices(),
});
