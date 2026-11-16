import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  PositionTransferDialog,
  type TransferAssetLike,
} from "./PositionTransferDialog.js";

function asset(ticker: string, extra: Partial<TransferAssetLike> = {}): TransferAssetLike {
  return {
    ticker,
    name: `${ticker} name`,
    pricePln: 100,
    units: 10,
    targetAllocationPct: 10,
    currentPct: 15,
    valuePln: 1000,
    ...extra,
  };
}

const A = asset("AAA");
const B = asset("BBB");
// A multi-subasset asset whose primary subasset is MAIN, with a secondary one.
const MULTI = asset("MAIN", {
  subassets: [
    { ticker: "MAIN", name: "Main", pricePln: 100, units: 10 },
    { ticker: "ALT", name: "Alt", pricePln: 50, units: 4 },
  ],
});

function renderDialog(position: TransferAssetLike | null) {
  const props = {
    allPositions: [A, B, MULTI],
    submitTransfer: () => Promise.resolve(),
    onClose: () => {},
    onSuccess: () => {},
  };
  const ui = (p: TransferAssetLike | null) => (
    <QueryClientProvider client={client}>
      <PositionTransferDialog position={p} {...props} />
    </QueryClientProvider>
  );
  const client = new QueryClient();
  const view = render(ui(position));
  return { rerender: (p: TransferAssetLike | null) => view.rerender(ui(p)) };
}

describe("PositionTransferDialog sold-ticker sync", () => {
  it("shows the clicked position's ticker as the sold ticker", () => {
    renderDialog(A);

    expect(screen.getByRole("heading").textContent).toContain("AAA");
  });

  it("follows the position when the dialog is reopened for another asset", () => {
    const { rerender } = renderDialog(A);
    rerender(null);
    rerender(B);

    const title = screen.getByRole("heading").textContent;
    expect(title).toContain("BBB");
    expect(title).not.toContain("AAA");
  });

  it("follows a position swapped directly, without an intermediate close", () => {
    const { rerender } = renderDialog(A);
    rerender(B);

    expect(screen.getByRole("heading").textContent).toContain("BBB");
  });

  it("keeps the selection when the parent re-renders with a new object for the same ticker", () => {
    const { rerender } = renderDialog(MULTI);
    expect(screen.getByLabelText("Sprzedawany ticker").textContent).toContain("MAIN");

    rerender({ ...MULTI });

    expect(screen.getByLabelText("Sprzedawany ticker").textContent).toContain("MAIN");
  });
});
