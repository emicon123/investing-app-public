import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

const api = vi.hoisted(() => ({
  getMacroStateHistory: vi.fn(),
  getBenchmarkHistory: vi.fn(),
}));
vi.mock("../../../api/index.js", () => api);

import { MacroBlendHistoryChart } from "./MacroBlendHistoryChart.js";

function renderChart() {
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <MacroBlendHistoryChart />
    </QueryClientProvider>,
  );
}

const overlay = (name: string) => screen.getByTitle(new RegExp(`^(Pokaż|Ukryj) ${name}$`));


beforeEach(() => {
  vi.clearAllMocks();
  api.getMacroStateHistory.mockResolvedValue({ data: [] });
  api.getBenchmarkHistory.mockResolvedValue({ data: [] });
});

describe("MacroBlendHistoryChart regime toggles (ADR-072 §4)", () => {
  it("starts with every regime toggle inert until its overlay is on", () => {
    renderChart();

    const btcRegimes = screen.getByTitle("Włącz BTC, aby pokazać cykle byka/niedźwiedzia");
    expect((btcRegimes as HTMLButtonElement).disabled).toBe(true);
    expect(btcRegimes.getAttribute("aria-pressed")).toBe("false");
  });

  it("drops a symbol's regime bands when its overlay is turned off, and they stay off when it returns", () => {
    renderChart();

    fireEvent.click(overlay("BTC"));
    fireEvent.click(screen.getByTitle("Pokaż Cykle BTC (byk/niedźwiedź)"));
    expect(screen.getByTitle("Ukryj Cykle BTC (byk/niedźwiedź)").getAttribute("aria-pressed")).toBe("true");

    fireEvent.click(overlay("BTC"));
    const inert = screen.getByTitle("Włącz BTC, aby pokazać cykle byka/niedźwiedzia");
    expect((inert as HTMLButtonElement).disabled).toBe(true);
    expect(inert.getAttribute("aria-pressed")).toBe("false");

    fireEvent.click(overlay("BTC"));
    expect(screen.getByTitle("Pokaż Cykle BTC (byk/niedźwiedź)").getAttribute("aria-pressed")).toBe("false");
  });

  it("leaves the other symbols' regime bands alone", () => {
    renderChart();

    fireEvent.click(overlay("BTC"));
    fireEvent.click(overlay("Nasdaq 100"));
    fireEvent.click(screen.getByTitle("Pokaż Cykle NASDAQ (byk/niedźwiedź)"));

    fireEvent.click(overlay("BTC"));

    expect(screen.getByTitle("Ukryj Cykle NASDAQ (byk/niedźwiedź)").getAttribute("aria-pressed")).toBe("true");
  });
});
