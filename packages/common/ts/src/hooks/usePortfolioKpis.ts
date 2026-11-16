import { useMemo } from "react";
import { useQuery, type QueryKey } from "@tanstack/react-query";

/**
 * The portfolio page's KPI data (task 170, design doc §2.4, §3): XIRR numbers, the last ~31
 * history snapshots for the sparkline, and the prices' timestamp for "Ceny z …". All three apps
 * would write the same three `useQuery` calls, differing only in API client and query key, so each
 * app binds this factory once (the `createUseRefreshPrices` precedent).
 */

export interface XirrDataLike {
  totalInvestedPln?: number | null;
  currentValuePln?: number | null;
  simpleReturnPct?: number | null;
  xirrPct?: number | null;
  yearsElapsed?: number | null;
}

export interface HistoryPointLike {
  snapshotDate: string;
  portfolioValue: number;
}

export interface PriceEntryLike {
  fetchedAt: string;
}

export interface PortfolioKpis {
  xirr: XirrDataLike | null;
  /** Snapshots from today − 31 days, ascending by date. */
  history: HistoryPointLike[];
  /** ISO timestamp of the newest cached price, null when none. */
  pricesAsOf: string | null;
  /** The XIRR query is pending (the KPI cards show skeletons). */
  isPending: boolean;
}

export interface PortfolioKpiSources<S> {
  xirrKey: (scope: S) => QueryKey;
  fetchXirr: (scope: S) => Promise<{ data: XirrDataLike }>;
  historyKey: (scope: S, from: string) => QueryKey;
  fetchHistory: (scope: S, from: string) => Promise<{ data: HistoryPointLike[] }>;
  pricesKey: QueryKey;
  /** `GET /market-data/prices`: cache-only, never triggers an upstream fetch. */
  fetchPrices: () => Promise<{ data: PriceEntryLike[] }>;
}

/** Same cadence as the positions list and the old summary bars. */
const POLLING = { staleTime: 60_000, refetchInterval: 60_000 } as const;
const HISTORY_WINDOW_DAYS = 31;
const DAY_MS = 24 * 60 * 60 * 1000;

const pad2 = (n: number): string => String(n).padStart(2, "0");

/** Local calendar date `days` ago, "YYYY-MM-DD". */
export function isoDateDaysAgo(days: number, now: Date = new Date()): string {
  const d = new Date(now.getTime() - days * DAY_MS);
  return `${d.getFullYear()}-${pad2(d.getMonth() + 1)}-${pad2(d.getDate())}`;
}

/** The newest `fetchedAt`, or null for an empty cache. */
export function latestFetchedAt(prices: readonly PriceEntryLike[] | undefined): string | null {
  let latest: string | null = null;
  let latestMs = -Infinity;
  for (const p of prices ?? []) {
    const ms = Date.parse(p.fetchedAt);
    if (ms > latestMs) {
      latestMs = ms;
      latest = p.fetchedAt;
    }
  }
  return latest;
}

export function createUsePortfolioKpis<S>(sources: PortfolioKpiSources<S>): (scope: S) => PortfolioKpis {
  return function usePortfolioKpis(scope: S): PortfolioKpis {
    // Computed once per mount so the history query key stays stable across renders.
    const from = useMemo(() => isoDateDaysAgo(HISTORY_WINDOW_DAYS), []);

    const xirr = useQuery({ queryKey: sources.xirrKey(scope), queryFn: () => sources.fetchXirr(scope), ...POLLING });
    const history = useQuery({
      queryKey: sources.historyKey(scope, from),
      queryFn: () => sources.fetchHistory(scope, from),
      ...POLLING,
    });
    const prices = useQuery({ queryKey: sources.pricesKey, queryFn: sources.fetchPrices, ...POLLING });

    const historyData = history.data?.data;
    const sortedHistory = useMemo(
      () => [...(historyData ?? [])].sort((a, b) => a.snapshotDate.localeCompare(b.snapshotDate)),
      [historyData],
    );

    return {
      xirr: xirr.data?.data ?? null,
      history: sortedHistory,
      pricesAsOf: latestFetchedAt(prices.data?.data),
      isPending: xirr.isPending,
    };
  };
}
