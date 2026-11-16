import { describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiError } from "@common/app-core";
import { TargetMeter } from "./TargetMeter.js";
import { StatusCell } from "./StatusCell.js";
import { TargetEditPopover } from "./TargetEditPopover.js";
import { AccountChips } from "./AccountChips.js";
import { AssetRow } from "./AssetRow.js";
import { SubassetRow } from "./SubassetRow.js";
import { GroupHeaderRow } from "./GroupHeaderRow.js";
import { applyPositionsFilter, buildPositionsModel } from "./model/positionsModel.js";
import { computeTargetMeter, targetMeterLabels } from "./model/targetMeter.js";
import { snapshotAssets } from "./__fixtures__/mainSnapshot.js";
import type { PortfolioAssetLike, PortfolioSubassetLike, PositionsTableSlots } from "./types.js";

type Asset = PortfolioAssetLike<PortfolioSubassetLike>;

function withQueryClient(children: ReactNode) {
  const client = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

function slots(overrides: Partial<PositionsTableSlots<Asset, PortfolioSubassetLike>> = {}) {
  return {
    targetEdit: { update: vi.fn(async () => undefined), onSaved: vi.fn() },
    renderAccountCell: (sub: PortfolioSubassetLike) => <span>konta {sub.ticker}</span>,
    ...overrides,
  };
}

const model = buildPositionsModel(snapshotAssets());
const bitcoin = model.rows.find((r) => r.name === "Bitcoin")!;
const nasdaq = model.rows.find((r) => r.name === "NASDAQ")!;

describe("TargetMeter", () => {
  it("draws the labels, the effective marker, the dashed base tick and a coral excess (Bitcoin)", () => {
    const { container } = render(<TargetMeter geometry={bitcoin.meter} labels={bitcoin.meterLabels} />);
    expect(container.textContent).toBe("25,4%cel 13,9%");
    expect(screen.getByTestId("target-meter-marker").style.left).toBe("46.26%");
    expect(screen.getByTestId("target-meter-base").style.left).toBe("79.98%");
    expect(container.querySelector("[data-tone='sell']")).not.toBeNull();
    expect(container.firstElementChild?.getAttribute("title")).toBe(
      "Udział 25,4%, cel efektywny 13,9%, cel bazowy 24,0%",
    );
  });

  it("shows the blue gap for UNDER (NASDAQ)", () => {
    const { container } = render(<TargetMeter geometry={nasdaq.meter} labels={nasdaq.meterLabels} />);
    const gap = container.querySelector<HTMLElement>("[data-tone='under']");
    expect(gap?.style.left).toBe("35.29%");
    expect(gap?.style.width).toBe("49.45%");
  });

  it("has no marker without a target", () => {
    const geometry = computeTargetMeter({ currentPct: 5, effectivePct: null, basePct: null, status: "OK" });
    const { container } = render(<TargetMeter geometry={geometry} labels={targetMeterLabels(5, null, null)} />);
    expect(screen.queryByTestId("target-meter-marker")).toBeNull();
    expect(container.textContent).toBe("5,0%cel —");
  });
});

describe("StatusCell", () => {
  it("is a button with the amount in its name when SELL is clickable", () => {
    const onSell = vi.fn();
    render(<StatusCell status="SELL" figure="~14,0 tys. zł" assetName="Bitcoin" onSell={onSell} />);
    fireEvent.click(screen.getByRole("button", { name: "Sprzedaj Bitcoin: ~14,0 tys. zł" }));
    expect(onSell).toHaveBeenCalledOnce();
  });

  it("is plain text when SELL is not clickable, or for any other status", () => {
    const { container, rerender } = render(<StatusCell status="SELL" figure="~14,0 tys. zł" assetName="Bitcoin" />);
    expect(screen.queryByRole("button")).toBeNull();
    expect(container.textContent).toBe("Sprzedaj~14,0 tys. zł");
    rerender(<StatusCell status="UNDER" figure="−14,8 p.p." assetName="NASDAQ" onSell={vi.fn()} />);
    expect(screen.queryByRole("button")).toBeNull();
    expect(container.textContent).toBe("Za mało−14,8 p.p.");
  });
});

describe("TargetEditPopover", () => {
  const asset = { id: 7, name: "Gold", targetAllocationPct: 20 };

  function open() {
    fireEvent.click(screen.getByRole("button", { name: "Edytuj cel % dla Gold" }));
    return screen.getByRole("textbox", { name: "Cel % (risk-on) dla Gold" }) as HTMLInputElement;
  }

  it("saves on Enter and calls onSaved", async () => {
    const update = vi.fn(async () => undefined);
    const onSaved = vi.fn();
    render(withQueryClient(<TargetEditPopover asset={asset} update={update} onSaved={onSaved} title="Cel % (risk-on)" />));
    const input = open();
    expect(input.value).toBe("20");
    fireEvent.change(input, { target: { value: "22,5" } });
    fireEvent.keyDown(input, { key: "Enter" });
    await waitFor(() => expect(onSaved).toHaveBeenCalledOnce());
    expect(update).toHaveBeenCalledWith(7, { targetAllocationPct: 22.5 });
  });

  it("clears the target with an empty input", async () => {
    const update = vi.fn(async () => undefined);
    render(withQueryClient(<TargetEditPopover asset={asset} update={update} onSaved={vi.fn()} title="Cel % (risk-on)" />));
    const input = open();
    fireEvent.change(input, { target: { value: "" } });
    fireEvent.click(screen.getByRole("button", { name: "Zapisz" }));
    await waitFor(() => expect(update).toHaveBeenCalledWith(7, { targetAllocationPct: null }));
  });

  it("cancels on Escape without saving", () => {
    const update = vi.fn(async () => undefined);
    render(withQueryClient(<TargetEditPopover asset={asset} update={update} onSaved={vi.fn()} title="Cel % (risk-on)" />));
    const input = open();
    fireEvent.keyDown(input, { key: "Escape" });
    expect(screen.queryByRole("textbox")).toBeNull();
    expect(update).not.toHaveBeenCalled();
  });

  it("shows the backend's 4xx reason", async () => {
    const update = vi.fn(async () => {
      throw new ApiError(400, [{ code: "RANGE", message: "Cel musi być w zakresie 0–100" }], "r1");
    });
    render(withQueryClient(<TargetEditPopover asset={asset} update={update} onSaved={vi.fn()} title="Cel % (risk-on)" />));
    const input = open();
    fireEvent.change(input, { target: { value: "50" } });
    await act(async () => {
      fireEvent.keyDown(input, { key: "Enter" });
    });
    expect((await screen.findByRole("alert")).textContent).toBe("Cel musi być w zakresie 0–100");
  });

  it("defaults the title to 'Cel %'", () => {
    render(withQueryClient(<TargetEditPopover asset={asset} update={vi.fn()} onSaved={vi.fn()} />));
    fireEvent.click(screen.getByRole("button", { name: "Edytuj cel % dla Gold" }));
    expect(screen.getByText("Cel % — Gold")).toBeTruthy();
  });
});

describe("AccountChips", () => {
  it("drops empty accounts", () => {
    const { container } = render(
      <AccountChips
        accounts={[
          { label: "IKE", value: 0 },
          { label: "IKZE", value: 1.5 },
          { label: "BINANCE", value: 0.25 },
        ]}
        assetClass="STOCK"
      />,
    );
    expect(container.textContent).toBe("IKZE1.5000BINANCE0.2500");
  });

  it("shows a dash when nothing is held", () => {
    const { container } = render(<AccountChips accounts={[{ label: "IKE", value: 0 }]} assetClass="ETF" />);
    expect(container.textContent).toBe("—");
  });
});

describe("AssetRow", () => {
  function renderRow(extra: Partial<PositionsTableSlots<Asset, PortfolioSubassetLike>>, editMode: boolean, onToggleExpand = vi.fn()) {
    return render(
      withQueryClient(
        <AssetRow row={bitcoin} editMode={editMode} expanded={false} onToggleExpand={onToggleExpand} subRowIds="x" slots={slots(extra)} />,
      ),
    );
  }

  it("renders the merged two-line cells", () => {
    const { container } = renderRow({}, false);
    const text = (container.textContent ?? "").replace(/ /g, " ").replace(/−/g, "-");
    expect(text).toContain("BitcoinBTC • Krypto");
    expect(text).toContain("312 148 złwejście 330 876 zł");
    expect(text).toContain("30 851 zł25,4% portfela");
    expect(text).toContain("-5,7%-1 851 zł");
    expect(text).toContain("Sprzedaj~14,0 tys. zł");
  });

  it("has no edit buttons outside edit mode", () => {
    renderRow({}, false);
    expect(screen.queryByRole("button", { name: /Edytuj instrument|Usuń aktywo/ })).toBeNull();
  });

  it("calls the edit, delete and sell callbacks in edit mode", () => {
    const onEditClick = vi.fn();
    const onDeleteClick = vi.fn();
    const onSellClick = vi.fn();
    renderRow({ onEditClick, onDeleteClick, onSellClick }, true);
    fireEvent.click(screen.getByRole("button", { name: "Edytuj instrument Bitcoin" }));
    fireEvent.click(screen.getByRole("button", { name: "Usuń aktywo Bitcoin" }));
    fireEvent.click(screen.getByRole("button", { name: /^Sprzedaj Bitcoin/ }));
    expect(onEditClick).toHaveBeenCalledWith(bitcoin.asset);
    expect(onDeleteClick).toHaveBeenCalledWith(bitcoin.asset);
    expect(onSellClick).toHaveBeenCalledWith(bitcoin.asset);
  });

  it("toggles its instruments with an aria-expanded chevron", () => {
    const onToggleExpand = vi.fn();
    renderRow({}, false, onToggleExpand);
    const chevron = screen.getByRole("button", { name: "Pokaż instrumenty: Bitcoin" });
    expect(chevron.getAttribute("aria-expanded")).toBe("false");
    fireEvent.click(chevron);
    expect(onToggleExpand).toHaveBeenCalledWith(bitcoin.id);
  });
});

describe("SubassetRow", () => {
  const primary: PortfolioSubassetLike = { id: 1, isPrimary: true, ticker: "BTC-USD", name: "Bitcoin", valuePln: 23_000, pricePln: 312_148, priceNative: 85_000, priceCurrency: "USD" };
  const wrapper: PortfolioSubassetLike = { id: 2, isPrimary: false, ticker: "PL.ETNVIRBTCP", name: "Bitcoin ETN", valuePln: 7_851, pricePln: 120, priceNative: 120, priceCurrency: "PLN" };

  it("shows ticker, Główna, price, native price, value, share and the account slot", () => {
    const { container } = render(
      <SubassetRow id="s1" sub={primary} assetClass="CRYPTO" assetValuePln={30_851} editMode={false} slots={slots()} />,
    );
    const text = (container.textContent ?? "").replace(/ /g, " ");
    expect(text).toContain("BTC-USDGłówna");
    expect(text).toContain("312 148 zł85 000,00 USD");
    expect(text).toContain("23 000 zł74,6% aktywa");
    expect(text).toContain("konta BTC-USD");
  });

  it("offers 'Ustaw jako główną' only on non-primary rows, disabled while in flight", () => {
    const onSetPrimaryClick = vi.fn();
    const props = { assetClass: "CRYPTO", assetValuePln: 30_851, editMode: true } as const;
    const { rerender } = render(
      <SubassetRow id="s1" sub={primary} {...props} slots={slots({ onSetPrimaryClick })} />,
    );
    expect(screen.queryByRole("button", { name: /jako główną/ })).toBeNull();

    rerender(<SubassetRow id="s2" sub={wrapper} {...props} slots={slots({ onSetPrimaryClick, settingPrimaryId: 2 })} />);
    expect((screen.getByRole("button", { name: "Ustaw PL.ETNVIRBTCP jako główną" }) as HTMLButtonElement).disabled).toBe(true);

    rerender(<SubassetRow id="s2" sub={wrapper} {...props} slots={slots({ onSetPrimaryClick, settingPrimaryId: null })} />);
    fireEvent.click(screen.getByRole("button", { name: "Ustaw PL.ETNVIRBTCP jako główną" }));
    expect(onSetPrimaryClick).toHaveBeenCalledWith(wrapper);
  });

  it("calls main's per-row edit pencil and delete", () => {
    const onEditSubClick = vi.fn();
    const onDeleteSubClick = vi.fn();
    render(
      <SubassetRow id="s2" sub={wrapper} assetClass="CRYPTO" assetValuePln={30_851} editMode slots={slots({ onEditSubClick, onDeleteSubClick })} />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Edytuj instrument PL.ETNVIRBTCP" }));
    fireEvent.click(screen.getByRole("button", { name: "Usuń instrument PL.ETNVIRBTCP" }));
    expect(onEditSubClick).toHaveBeenCalledWith(wrapper);
    expect(onDeleteSubClick).toHaveBeenCalledWith(wrapper);
  });
});

describe("GroupHeaderRow", () => {
  it("is one aria-expanded toggle with the whole group's figures", () => {
    const onToggle = vi.fn();
    const view = applyPositionsFilter(model, "all", "").groups[0]!;
    const { container } = render(<GroupHeaderRow view={view} open onToggle={onToggle} rowsId="g-crypto" editMode={false} />);
    const toggle = screen.getByRole("button", { name: "Krypto" });
    expect(toggle.getAttribute("aria-expanded")).toBe("true");
    expect(toggle.getAttribute("aria-controls")).toBe("g-crypto");
    fireEvent.click(toggle);
    expect(onToggle).toHaveBeenCalledOnce();
    const text = (container.textContent ?? "").replace(/ /g, " ");
    expect(text).toContain("13 pozycji");
    expect(text).toContain("56 226 zł46,3% portfela");
    expect(text).toContain("Sprzedaj×9");
  });
});
