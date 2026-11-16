import { describe, expect, it, vi } from "vitest";
import { createElement, type ReactNode } from "react";
import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createUsePortfolioKpis, isoDateDaysAgo, latestFetchedAt } from "./usePortfolioKpis.js";

function wrapper({ children }: { children: ReactNode }) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  // createElement, not JSX: this package compiles without a JSX transform.
  return createElement(QueryClientProvider, { client }, children);
}

describe("createUsePortfolioKpis", () => {
  it("binds the three queries to the app's scope, keys and clients", async () => {
    const fetchXirr = vi.fn(async (scope: string) => ({ data: { currentValuePln: scope === "MAIN" ? 100 : 0 } }));
    const fetchHistory = vi.fn(async () => ({
      data: [
        { snapshotDate: "2026-10-23", portfolioValue: 2 },
        { snapshotDate: "2026-10-01", portfolioValue: 1 },
      ],
    }));
    const fetchPrices = vi.fn(async () => ({
      data: [{ fetchedAt: "2026-10-24T15:00:00Z" }, { fetchedAt: "2026-10-24T15:54:00Z" }],
    }));
    const usePortfolioKpis = createUsePortfolioKpis<string>({
      xirrKey: (scope) => ["xirr", scope],
      fetchXirr,
      historyKey: (scope, from) => ["history", scope, from],
      fetchHistory,
      pricesKey: ["prices"],
      fetchPrices,
    });

    const { result } = renderHook(() => usePortfolioKpis("MAIN"), { wrapper });
    expect(result.current.isPending).toBe(true);

    await waitFor(() => expect(result.current.isPending).toBe(false));
    await waitFor(() => expect(result.current.pricesAsOf).not.toBeNull());
    await waitFor(() => expect(result.current.history).toHaveLength(2));

    expect(result.current.xirr).toEqual({ currentValuePln: 100 });
    expect(result.current.history.map((p) => p.snapshotDate)).toEqual(["2026-10-01", "2026-10-23"]);
    expect(result.current.pricesAsOf).toBe("2026-10-24T15:54:00Z");
    expect(fetchXirr).toHaveBeenCalledWith("MAIN");
    expect(fetchHistory).toHaveBeenCalledWith("MAIN", isoDateDaysAgo(31));
  });
});

describe("helpers", () => {
  it("formats the history window start as a local date", () => {
    expect(isoDateDaysAgo(31, new Date(2026, 9, 24, 12))).toBe("2026-09-23");
  });

  it("finds the newest price timestamp", () => {
    expect(latestFetchedAt(undefined)).toBeNull();
    expect(latestFetchedAt([])).toBeNull();
    expect(latestFetchedAt([{ fetchedAt: "2026-10-24T10:00:00+02:00" }, { fetchedAt: "2026-10-24T09:30:00Z" }])).toBe(
      "2026-10-24T09:30:00Z",
    );
  });
});
