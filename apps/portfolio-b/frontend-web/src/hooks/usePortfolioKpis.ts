import { createUsePortfolioKpis } from "@common/app-core";
import { computeXirr, getPrices, listHistoria } from "../api/index.js";
import { queryKeys } from "@/lib/queryKeys";

/** The portfolio page's KPI queries (task 170), bound to portfolioB's API client and keys (one book). */
export const usePortfolioKpis = createUsePortfolioKpis<void>({
  xirrKey: () => queryKeys.xirr.all,
  fetchXirr: () => computeXirr(),
  historyKey: (_book, from) => queryKeys.history.since(from),
  fetchHistory: (_book, from) => listHistoria({ from, size: 100 }),
  pricesKey: queryKeys.prices.all,
  fetchPrices: () => getPrices(),
});
