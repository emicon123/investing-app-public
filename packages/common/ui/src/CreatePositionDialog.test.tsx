import { describe, expect, it } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { CreatePositionDialog } from "./CreatePositionDialog.js";

function renderDialog() {
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <CreatePositionDialog
        open
        onClose={() => {}}
        onSuccess={() => {}}
        createPosition={() => Promise.resolve()}
        assets={[{ id: 7, key: "gold", name: "Gold", exposure: "METALS", assetClass: "METALS" }]}
      />
    </QueryClientProvider>,
  );
}

describe("CreatePositionDialog live form values", () => {
  it("mirrors the typed name into the new-asset-name placeholder", () => {
    renderDialog();
    expect(screen.getByLabelText(/Nazwa aktywa/).getAttribute("placeholder")).toBe(
      "domyślnie nazwa instrumentu",
    );

    fireEvent.change(document.getElementById("new-name") as HTMLElement, { target: { value: "Bitcoin" } });

    expect(screen.getByLabelText(/Nazwa aktywa/).getAttribute("placeholder")).toBe("Bitcoin");
  });

  it("swaps the new-asset fields for the asset picker when attaching to an existing asset", () => {
    renderDialog();
    expect(screen.queryByLabelText(/Cel % aktywa/)).not.toBeNull();

    fireEvent.click(screen.getByLabelText(/Dodaj jako instrument istniejącego aktywa/));

    expect(screen.queryByLabelText(/Cel % aktywa/)).toBeNull();
    expect(screen.queryByLabelText(/Nazwa aktywa/)).toBeNull();
  });
});
