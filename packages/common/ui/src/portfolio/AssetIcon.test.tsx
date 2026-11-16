import { describe, expect, it } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { AssetIcon } from "./AssetIcon.js";

describe("AssetIcon", () => {
  it("renders the resolved logo as a decorative image", () => {
    const { container } = render(<AssetIcon ticker="BTC-USD" assetClass="CRYPTO" />);
    const img = container.querySelector("img");
    expect(img?.getAttribute("src")).toBe("https://assets.coincap.io/assets/icons/btc@2x.png");
    expect(img?.getAttribute("alt")).toBe("");
    expect(img?.className).not.toContain("bg-white");
  });

  it("pads a favicon on a white circle", () => {
    const { container } = render(<AssetIcon ticker="COIN" assetClass="STOCK" />);
    expect(container.querySelector("img")?.className).toContain("bg-white");
  });

  it("shows a monogram when nothing resolves", () => {
    render(<AssetIcon ticker="NEWCO.DE" assetClass="STOCK" />);
    expect(screen.getByTestId("asset-monogram").textContent).toBe("NE");
  });

  it("swaps a broken image for the monogram", () => {
    const { container } = render(<AssetIcon ticker="ZANO-USD" assetClass="CRYPTO" />);
    fireEvent.error(container.querySelector("img")!);
    expect(container.querySelector("img")).toBeNull();
    expect(screen.getByTestId("asset-monogram").textContent).toBe("ZA");
  });
});
