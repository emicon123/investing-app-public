import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiError, type PortfolioKpis } from "@common/app-core";
import { PortfolioPage, type PortfolioPageApi } from "./PortfolioPage.js";
import { SOFT_DELETE_CONSEQUENCE } from "./portfolio/model/deleteCopy.js";
import { snapshotAssets } from "./portfolio/__fixtures__/mainSnapshot.js";

/**
 * Task 173 (asset soft delete): the delete flow must leave the overview fresh and never render a
 * deleted asset. The backend now hides the row (`deleted_at`) instead of removing it, so the only
 * thing that makes the UI drop it is the positions refetch this page triggers on a 2xx. portfolioB and
 * portfolioC both mount this page; `invalidatePositions` is their one seam to the query cache.
 */

const KPIS: PortfolioKpis = { xirr: null, history: [], pricesAsOf: null, isPending: false };

function Slot() {
  return null;
}

function renderPage(api: Partial<PortfolioPageApi>) {
  const invalidatePositions = vi.fn();
  const fullApi: PortfolioPageApi = {
    deletePosition: vi.fn(async () => undefined),
    deleteAsset: vi.fn(async () => undefined),
    setPrimary: vi.fn(async () => undefined),
    ...api,
  };
  const assets = snapshotAssets();
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { mutations: { retry: false } } })}>
      <PortfolioPage
        title="Portfolio"
        api={fullApi}
        usePositionsQuery={() => ({ data: assets, isPending: false, isError: false })}
        invalidatePositions={invalidatePositions}
        useKpis={() => KPIS}
        updateAsset={vi.fn(async () => undefined)}
        renderAccountCell={() => null}
        EditDialog={Slot}
        CreateDialog={Slot}
        TransferDialog={Slot}
        Treemap={Slot}
        HistoryDialog={Slot}
      />
    </QueryClientProvider>,
  );
  return { api: fullApi, invalidatePositions, assets };
}

function enterEditMode() {
  fireEvent.click(screen.getByRole("button", { name: "Edytuj portfel" }));
}

function confirmDelete() {
  fireEvent.click(screen.getByRole("button", { name: "Usuń" }));
}

describe("PortfolioPage delete flow (task 173)", () => {
  it("tells the owner the delete is reversible instead of calling it irreversible", () => {
    renderPage({});
    enterEditMode();
    fireEvent.click(screen.getByRole("button", { name: "Usuń aktywo Gold" }));

    const dialog = screen.getByRole("alertdialog");
    expect(dialog.textContent).toContain(SOFT_DELETE_CONSEQUENCE);
    expect(dialog.textContent).not.toContain("nieodwracalna");
  });

  it("refetches the positions list after a successful asset delete", async () => {
    const { api, invalidatePositions, assets } = renderPage({});
    enterEditMode();
    fireEvent.click(screen.getByRole("button", { name: "Usuń aktywo Gold" }));
    confirmDelete();

    await waitFor(() => expect(invalidatePositions).toHaveBeenCalledOnce());
    expect(api.deleteAsset).toHaveBeenCalledWith(assets[0]!.id);
    expect(api.deletePosition).not.toHaveBeenCalled();
  });

  it("refetches the positions list after a successful subasset delete", async () => {
    const { api, invalidatePositions, assets } = renderPage({});
    enterEditMode();
    fireEvent.click(screen.getByRole("button", { name: "Pokaż instrumenty: Gold" }));
    fireEvent.click(screen.getByRole("button", { name: "Usuń instrument ETCGLDRMAU" }));
    confirmDelete();

    await waitFor(() => expect(invalidatePositions).toHaveBeenCalledOnce());
    expect(api.deletePosition).toHaveBeenCalledWith(assets[0]!.subassets[0]!.id);
    expect(api.deleteAsset).not.toHaveBeenCalled();
  });

  it("keeps the dialog open with the server's reason and does not refetch on a 409", async () => {
    const deletePosition = vi.fn(async () => {
      throw new ApiError(409, [{ code: "CONFLICT", message: "set a new primary first" }], "req-1");
    });
    const { invalidatePositions } = renderPage({ deletePosition });
    enterEditMode();
    fireEvent.click(screen.getByRole("button", { name: "Pokaż instrumenty: Gold" }));
    fireEvent.click(screen.getByRole("button", { name: "Usuń instrument ETCGLDRMAU" }));
    confirmDelete();

    expect((await screen.findByRole("alert")).textContent).toBe("set a new primary first");
    expect(invalidatePositions).not.toHaveBeenCalled();
  });
});
