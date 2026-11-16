import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { EditPositionDialog, type EditPositionSnapshot } from "./EditPositionDialog.js";

const position: EditPositionSnapshot = {
  id: 1,
  ticker: "BTC-USD",
  name: "Bitcoin",
  yahooTicker: "BTC-USD",
  assetClass: "CRYPTO",
  companyBacked: false,
  unitValues: { units: 0.5 },
};

function renderDialog(snapshot: EditPositionSnapshot | null) {
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <EditPositionDialog
        position={snapshot}
        onClose={() => {}}
        onSuccess={() => {}}
        updatePosition={() => Promise.resolve()}
        unitFields={[{ key: "units", label: "Jednostki" }]}
        assetClassOptions={[{ value: "CRYPTO", label: "Krypto" }]}
        showCompanyBacked={false}
      />
    </QueryClientProvider>,
  );
}

describe("EditPositionDialog", () => {
  it("tells the user that editing units does not change the cost (task 161c-2)", () => {
    renderDialog(position);

    expect(screen.getByText("Edycja liczby jednostek nie zmienia kosztu.")).toBeTruthy();
  });

  it("renders nothing while no position is being edited", () => {
    renderDialog(null);

    expect(screen.queryByText("Edycja liczby jednostek nie zmienia kosztu.")).toBeNull();
  });

  it("pre-selects the position's class once the form is populated", () => {
    renderDialog(position);

    expect(screen.getByRole("combobox", { name: "Klasa" }).textContent).toContain("Krypto");
  });
});
