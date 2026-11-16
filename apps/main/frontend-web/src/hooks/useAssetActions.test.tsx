import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { act, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiError } from "@common/app-core";
import { assetSchema } from "../api/schemas/positions.js";

const api = vi.hoisted(() => ({
  deleteAsset: vi.fn(),
  deletePosition: vi.fn(),
  setPrimary: vi.fn(),
}));

vi.mock("../api/index.js", () => api);

import { useAssetActions } from "./useAssetActions.js";
import { queryKeys } from "@/lib/queryKeys";

/**
 * Task 173 (asset soft delete): a delete now hides the row server-side (`deleted_at`) instead of
 * removing it, so the only thing that makes the portfolio drop it is the positions refetch this hook
 * triggers after a 2xx. A failed delete (409 "set a new primary first") must not refetch.
 */

const ASSET = assetSchema.parse({
  id: 7,
  key: "gold",
  name: "Gold",
  portfolio: "MAIN",
  exposure: "METALS",
  assetClass: "ETF",
  targetAllocationPct: 20,
  targetAllocationPctRiskOff: null,
  createdAt: "2026-01-01T00:00:00Z",
  subassets: [
    {
      id: 70,
      assetId: 7,
      isPrimary: true,
      ticker: "ETCGLDRMAU",
      name: "Gold ETC",
      portfolio: "MAIN",
      exchange: "XTB",
      assetClass: "ETF",
      priceCurrency: "PLN",
      unitsIke: 0,
      unitsIkze: 0,
      unitsRegularEur: 0,
      unitsRegularUsd: 0,
      unitsRegularPln: 0,
      effectiveUnits: 0,
      companyBacked: false,
    },
  ],
});
const SUBASSET = ASSET.subassets[0]!;

function setup() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  const invalidate = vi.spyOn(client, "invalidateQueries");
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
  const hook = renderHook(() => useAssetActions([ASSET]), { wrapper });
  return { ...hook, invalidate };
}

describe("useAssetActions delete (task 173)", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    api.deleteAsset.mockResolvedValue(undefined);
    api.deletePosition.mockResolvedValue(undefined);
  });

  it("refetches every positions query after a successful asset delete", async () => {
    const { result, invalidate } = setup();

    act(() => result.current.requestDeleteAsset(ASSET));
    act(() => result.current.confirmDelete());

    await waitFor(() => expect(result.current.deleteTarget).toBeNull());
    expect(api.deleteAsset).toHaveBeenCalledWith(7);
    expect(invalidate).toHaveBeenCalledWith({ queryKey: queryKeys.positions.all });
  });

  it("words the cascade note as hiding the instruments, not erasing their holdings", () => {
    const { result } = setup();

    act(() => result.current.requestDeleteAsset(ASSET));

    expect(result.current.deleteTarget?.note).toBe(
      "Wraz z aktywem znikną też wszystkie jego instrumenty (ETCGLDRMAU). Jego cel (20%) zniknie z sumy celów portfela.",
    );
  });

  it("refetches every positions query after a successful subasset delete", async () => {
    const { result, invalidate } = setup();

    act(() => result.current.requestDeleteSubasset(SUBASSET));
    act(() => result.current.confirmDelete());

    await waitFor(() => expect(result.current.deleteTarget).toBeNull());
    expect(api.deletePosition).toHaveBeenCalledWith(70);
    expect(invalidate).toHaveBeenCalledWith({ queryKey: queryKeys.positions.all });
  });

  it("keeps the dialog open with the server's reason and does not refetch when the delete is refused", async () => {
    api.deletePosition.mockRejectedValue(
      new ApiError(409, [{ code: "CONFLICT", message: "set a new primary first" }], "req-1"),
    );
    const { result, invalidate } = setup();

    act(() => result.current.requestDeleteSubasset(SUBASSET));
    act(() => result.current.confirmDelete());

    await waitFor(() => expect(result.current.deleteErrorMessage).toBe("set a new primary first"));
    expect(result.current.deleteTarget).not.toBeNull();
    expect(invalidate).not.toHaveBeenCalled();
  });
});
