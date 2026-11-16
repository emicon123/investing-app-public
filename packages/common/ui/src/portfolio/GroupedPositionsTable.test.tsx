import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { GroupedPositionsTable } from "./GroupedPositionsTable.js";
import { PositionsFilterBar } from "./PositionsFilterBar.js";
import { AccountChips } from "./AccountChips.js";
import { usePositionsView } from "./usePositionsView.js";
import { snapshotAssets } from "./__fixtures__/mainSnapshot.js";
import type { PortfolioAssetLike, PortfolioSubassetLike, PositionsTableSlots } from "./types.js";

type Asset = PortfolioAssetLike<PortfolioSubassetLike>;
type Slots = PositionsTableSlots<Asset, PortfolioSubassetLike>;

const MAIN_COLLAPSED = ["EXPERIMENTS"] as const;

function Harness({ assets, collapsed, table }: { assets: Asset[]; collapsed?: readonly string[]; table: Slots }) {
  const view = usePositionsView(assets, { defaultCollapsedExposures: collapsed });
  return (
    <>
      <PositionsFilterBar chips={view.chips} filter={view.filter} onFilter={view.setFilter} query={view.query} onQuery={view.setQuery} />
      <GroupedPositionsTable view={view} table={table} ariaLabel="Pozycje portfela Main pogrupowane według ekspozycji" />
    </>
  );
}

function slots(overrides: Partial<Slots> = {}): Slots {
  return {
    targetEdit: { update: vi.fn(async () => undefined), onSaved: vi.fn() },
    renderAccountCell: () => <AccountChips accounts={[{ label: "IKE", value: 2 }, { label: "REG EUR", value: 1 }]} assetClass="ETF" />,
    ...overrides,
  };
}

function assetsWithGoldWrapper(): Asset[] {
  const assets = snapshotAssets();
  assets.find((a) => a.name === "Gold")!.subassets.push({
    id: 9001,
    isPrimary: false,
    ticker: "IGLN.L",
    name: "iShares Physical Gold",
    valuePln: 1000,
    pricePln: 300,
    priceNative: 80,
    priceCurrency: "USD",
  });
  return assets;
}

function renderTable(props: { collapsed?: readonly string[]; table?: Slots; assets?: Asset[] } = {}) {
  const client = new QueryClient();
  return render(
    <QueryClientProvider client={client}>
      <Harness assets={props.assets ?? snapshotAssets()} collapsed={props.collapsed} table={props.table ?? slots()} />
    </QueryClientProvider>,
  );
}

const groupToggle = (label: string) => screen.queryByRole("button", { name: label });
const groupLabels = () =>
  screen.getAllByRole("rowheader").map((h) => within(h).getByRole("button").textContent);
const chip = (name: RegExp) => screen.getByRole("button", { name });
const search = () => screen.getByRole("searchbox", { name: "Szukaj aktywa po nazwie lub tickerze" });

describe("GroupedPositionsTable", () => {
  it("groups by exposure, ordered by value, under one labelled table", () => {
    renderTable();
    expect(screen.getByRole("table", { name: "Pozycje portfela Main pogrupowane według ekspozycji" })).toBeTruthy();
    expect(groupLabels()).toEqual(["Krypto", "Metale", "Akcje", "Obligacje", "Eksperymenty"]);
    expect(screen.getAllByRole("columnheader").map((h) => h.textContent)).toEqual([
      "Aktywo",
      "Cena / śr. wejście",
      "Wartość / udział",
      "Udział wobec celu",
      "Zysk / strata",
      "Rebalansowanie",
    ]);
    expect(screen.getByText("Suma celów bazowych 100,0% ✓")).toBeTruthy();
  });

  it("starts EXPERIMENTS collapsed when configured, and toggles it open", () => {
    renderTable({ collapsed: MAIN_COLLAPSED });
    expect(groupToggle("Eksperymenty")!.getAttribute("aria-expanded")).toBe("false");
    expect(screen.queryByText("Hedera")).toBeNull();
    expect(screen.getByText("Bitcoin")).toBeTruthy();
    fireEvent.click(groupToggle("Eksperymenty")!);
    expect(groupToggle("Eksperymenty")!.getAttribute("aria-expanded")).toBe("true");
    expect(screen.getByText("Hedera")).toBeTruthy();
  });

  it("has every group open without the config, and ignores an absent key", () => {
    renderTable({ collapsed: ["COMMODITIES"] });
    expect(screen.getAllByRole("rowheader").every((h) => within(h).getByRole("button").getAttribute("aria-expanded") === "true")).toBe(true);
    expect(screen.getByText("Hedera")).toBeTruthy();
  });

  it("a group chip hides the other groups and opens the matching one", () => {
    renderTable({ collapsed: MAIN_COLLAPSED });
    const experiments = chip(/^Eksperymenty 15$/);
    fireEvent.click(experiments);
    expect(experiments.getAttribute("aria-pressed")).toBe("true");
    expect(groupLabels()).toEqual(["Eksperymenty"]);
    expect(screen.getByText("Hedera")).toBeTruthy();
  });

  it("the Sprzedaj chip keeps the SELL rows of every group", () => {
    renderTable({ collapsed: MAIN_COLLAPSED });
    fireEvent.click(chip(/^Sprzedaj 14$/));
    expect(groupLabels()).toEqual(["Krypto", "Eksperymenty"]);
    expect(screen.getByText("9 z 13 pozycji")).toBeTruthy();
    expect(screen.getByText("Algorand")).toBeTruthy();
    expect(screen.queryByText("Chainlink")).toBeNull();
  });

  it("searching IGLN finds Gold through its subasset", () => {
    renderTable({ assets: assetsWithGoldWrapper() });
    fireEvent.change(search(), { target: { value: "IGLN" } });
    expect(groupLabels()).toEqual(["Metale"]);
    expect(screen.getByText("Gold")).toBeTruthy();
  });

  it("toggling while filtering does not change the browse state", () => {
    renderTable({ collapsed: MAIN_COLLAPSED });
    fireEvent.click(chip(/^Eksperymenty 15$/));
    fireEvent.click(groupToggle("Eksperymenty")!);
    expect(groupToggle("Eksperymenty")!.getAttribute("aria-expanded")).toBe("false");
    fireEvent.click(chip(/^Wszystkie 34$/));
    expect(groupToggle("Eksperymenty")!.getAttribute("aria-expanded")).toBe("false");
    fireEvent.click(groupToggle("Krypto")!);
    fireEvent.click(chip(/^Krypto 13$/));
    expect(groupToggle("Krypto")!.getAttribute("aria-expanded")).toBe("true");
    fireEvent.click(chip(/^Wszystkie 34$/));
    expect(groupToggle("Krypto")!.getAttribute("aria-expanded")).toBe("false");
  });

  it("says when nothing matches and 'Wyczyść filtry' restores everything", () => {
    renderTable();
    fireEvent.click(chip(/^Metale 1$/));
    fireEvent.change(search(), { target: { value: "bitcoin" } });
    expect(screen.getByText("Żadna pozycja nie pasuje do „bitcoin” w wybranym filtrze.")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Wyczyść filtry" }));
    expect(groupLabels()).toHaveLength(5);
    expect((search() as HTMLInputElement).value).toBe("");
    expect(chip(/^Wszystkie 34$/).getAttribute("aria-pressed")).toBe("true");
  });

  it("expanding an asset shows its instruments with their account chips", () => {
    renderTable({ assets: assetsWithGoldWrapper() });
    fireEvent.click(screen.getByRole("button", { name: "Pokaż instrumenty: Gold" }));
    expect(screen.getByText("IGLN.L")).toBeTruthy();
    expect(screen.getByText("ETCGLDRMAU")).toBeTruthy();
    expect(screen.getAllByText("REG EUR").length).toBe(2);
  });

  it("calls onSellClick from a SELL row", () => {
    const onSellClick = vi.fn();
    renderTable({ table: slots({ onSellClick }) });
    fireEvent.click(screen.getByRole("button", { name: /^Sprzedaj Bitcoin: ~14,0/ }));
    expect(onSellClick).toHaveBeenCalledWith(expect.objectContaining({ name: "Bitcoin" }));
  });

  it("shows the actions column only in edit mode", () => {
    const { unmount } = renderTable();
    expect(screen.getAllByRole("columnheader")).toHaveLength(6);
    unmount();
    renderTable({ table: slots({ onEditClick: vi.fn(), onDeleteClick: vi.fn() }) });
    expect(screen.getAllByRole("columnheader")).toHaveLength(7);
    expect(screen.getAllByRole("button", { name: /^Usuń aktywo / }).length).toBeGreaterThan(0);
  });
});
