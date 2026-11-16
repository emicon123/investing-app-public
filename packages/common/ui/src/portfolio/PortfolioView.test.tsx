import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { PortfolioKpis } from "@common/app-core";
import { PortfolioView } from "./PortfolioView.js";
import { snapshotAssets } from "./__fixtures__/mainSnapshot.js";
import type { PortfolioAssetLike, PortfolioHeaderActions, PortfolioSubassetLike, PortfolioViewProps } from "./types.js";

type Asset = PortfolioAssetLike<PortfolioSubassetLike>;
type Props = PortfolioViewProps<Asset, PortfolioSubassetLike>;

const KPIS: PortfolioKpis = {
  xirr: { currentValuePln: 121_344, totalInvestedPln: 112_500, simpleReturnPct: 0.0786, xirrPct: 0.0291, yearsElapsed: 5.17 },
  history: [],
  pricesAsOf: new Date(2026, 9, 24, 17, 54).toISOString(),
  isPending: false,
};

function actions(editMode = false): PortfolioHeaderActions {
  return { editMode, onToggleEdit: vi.fn(), onOpenHistory: vi.fn(), onOpenTreemap: vi.fn(), onAdd: vi.fn() };
}

function renderView(overrides: Partial<Props> = {}) {
  const props: Props = {
    title: "Portfolio",
    bookTag: "Main",
    positions: { data: snapshotAssets(), isPending: false, isError: false },
    kpis: KPIS,
    actions: actions(),
    table: {
      targetEdit: { update: vi.fn(async () => undefined), onSaved: vi.fn(), title: "Cel % (risk-on)" },
      renderAccountCell: () => null,
      defaultCollapsedExposures: ["EXPERIMENTS"],
    },
    ...overrides,
  };
  render(
    <QueryClientProvider client={new QueryClient()}>
      <PortfolioView {...props} />
    </QueryClientProvider>,
  );
  return props;
}

describe("PortfolioView", () => {
  it("composes header, KPIs, ribbon, filters, table and the children slot in order", () => {
    renderView({ headerStatus: <a href="/bot">Bot OKX</a>, children: <section aria-label="Reżim makro" /> });
    expect(screen.getByRole("heading", { level: 1, name: "Portfolio" })).toBeTruthy();
    expect(screen.getByText("Main")).toBeTruthy();
    expect(screen.getByText("Ceny z 24.10.2026, 17:54")).toBeTruthy();
    expect(screen.getByRole("link", { name: "Bot OKX" })).toBeTruthy();
    const order = [
      screen.getByRole("region", { name: "Podsumowanie portfela" }),
      screen.getByRole("heading", { name: "Alokacja według ekspozycji" }),
      screen.getByRole("group", { name: "Filtruj pozycje" }),
      screen.getByRole("table", { name: "Pozycje portfela Main pogrupowane według ekspozycji" }),
      screen.getByRole("region", { name: "Reżim makro" }),
    ];
    for (let i = 1; i < order.length; i++) {
      expect(order[i - 1]!.compareDocumentPosition(order[i]!) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    }
    expect(screen.getByRole("button", { name: "Eksperymenty" }).getAttribute("aria-expanded")).toBe("false");
  });

  it("wires the header actions, with no 'Rebalansuj' button", () => {
    const props = renderView();
    fireEvent.click(screen.getByRole("button", { name: "Historia" }));
    fireEvent.click(screen.getByRole("button", { name: "Mapa alokacji" }));
    fireEvent.click(screen.getByRole("button", { name: "Dodaj pozycję" }));
    const edit = screen.getByRole("button", { name: "Edytuj portfel" });
    expect(edit.getAttribute("aria-pressed")).toBe("false");
    fireEvent.click(edit);
    expect(props.actions.onOpenHistory).toHaveBeenCalledOnce();
    expect(props.actions.onOpenTreemap).toHaveBeenCalledOnce();
    expect(props.actions.onAdd).toHaveBeenCalledOnce();
    expect(props.actions.onToggleEdit).toHaveBeenCalledOnce();
    // Price refresh lives only in the app top bar, not in the portfolio header.
    expect(screen.queryByRole("button", { name: "Odśwież ceny" })).toBeNull();
    expect(screen.queryByRole("button", { name: /Rebalansuj/ })).toBeNull();
  });

  it("reads 'Wyjdź z edycji' while editing", () => {
    renderView({ actions: actions(true) });
    expect(screen.getByRole("button", { name: "Wyjdź z edycji" }).getAttribute("aria-pressed")).toBe("true");
  });

  it("shows the loading state, keeping the header", () => {
    renderView({ positions: { data: [], isPending: true, isError: false } });
    expect(screen.getByRole("status", { name: "Ładowanie danych" })).toBeTruthy();
    expect(screen.getByRole("heading", { level: 1 })).toBeTruthy();
  });

  it("shows the error state", () => {
    renderView({ positions: { data: [], isPending: false, isError: true } });
    expect(screen.getByRole("alert").textContent).toBe("Błąd ładowania pozycji. Spróbuj ponownie.");
  });

  it("shows the empty-book state", () => {
    renderView({ positions: { data: [], isPending: false, isError: false } });
    expect(screen.getByText("Brak pozycji. Dodaj pierwszą pozycję.")).toBeTruthy();
    expect(screen.queryByRole("table")).toBeNull();
  });

  it("hides 'Ceny z' when no price is cached and names a tagless book plainly", () => {
    renderView({ bookTag: undefined, kpis: { ...KPIS, pricesAsOf: null } });
    expect(screen.queryByText(/^Ceny z/)).toBeNull();
    expect(screen.getByRole("table", { name: "Pozycje portfela pogrupowane według ekspozycji" })).toBeTruthy();
  });
});
