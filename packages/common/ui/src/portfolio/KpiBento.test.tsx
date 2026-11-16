import { describe, expect, it } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import type { PortfolioKpis } from "@common/app-core";
import { KpiBento } from "./KpiBento.js";
import { isoLocalDate } from "./model/sparklineModel.js";
import { MAIN_SNAPSHOT_LIVE_VALUE, MAIN_SNAPSHOT_SPARK } from "./__fixtures__/mainSnapshot.js";

const ascii = (s: string | null): string => (s ?? "").replace(/ /g, " ").replace(/−/g, "-");
const DAY_MS = 24 * 60 * 60 * 1000;

function kpis(overrides: Partial<PortfolioKpis> = {}): PortfolioKpis {
  const now = Date.now();
  return {
    xirr: {
      currentValuePln: MAIN_SNAPSHOT_LIVE_VALUE,
      totalInvestedPln: 112_500,
      simpleReturnPct: 0.0786,
      xirrPct: 0.0291,
      yearsElapsed: 5.17,
    },
    history: MAIN_SNAPSHOT_SPARK.map((portfolioValue, i) => ({
      snapshotDate: isoLocalDate(new Date(now - (30 - i) * DAY_MS)),
      portfolioValue,
    })),
    pricesAsOf: null,
    isPending: false,
    ...overrides,
  };
}

describe("KpiBento", () => {
  it("shows the four cards from the XIRR numbers", () => {
    const { container } = render(<KpiBento kpis={kpis()} />);
    const text = ascii(container.textContent);
    expect(text).toContain("Wycena całkowita");
    expect(text).toContain("121 344zł");
    expect(text).toContain("Zysk / strata+8 844zł+7,86%");
    expect(text).toContain("Wpłacony kapitał112 500zł");
    expect(text).toContain("Wpłaty z 5 lat i 2 miesięcy");
    expect(text).toContain("XIRR+2,91%rocznie");
  });

  it("switches the change line between 30 and 7 days (aria-pressed)", () => {
    const { container } = render(<KpiBento kpis={kpis()} />);
    const group = screen.getByRole("group", { name: "Okres zmiany wyceny" });
    const thirty = screen.getByRole("button", { name: "30 dni" });
    const seven = screen.getByRole("button", { name: "7 dni" });
    expect(group).toBeTruthy();
    expect(thirty.getAttribute("aria-pressed")).toBe("true");
    expect(ascii(container.textContent)).toContain("-15 640 zł (-11,4%) w 30 dni");

    fireEvent.click(seven);
    expect(seven.getAttribute("aria-pressed")).toBe("true");
    expect(thirty.getAttribute("aria-pressed")).toBe("false");
    expect(ascii(container.textContent)).toContain("-7 599 zł (-5,9%) w 7 dni");
    expect(screen.getByRole("img").getAttribute("aria-label")).toMatch(/^Wycena w ostatnich 7 dniach/);
  });

  it("shows '—' for an unknown XIRR and no sparkline without history", () => {
    const { container } = render(
      <KpiBento kpis={kpis({ xirr: { ...kpis().xirr, xirrPct: null }, history: [] })} />,
    );
    expect(ascii(container.textContent)).toContain("XIRR—");
    expect(screen.queryByRole("img")).toBeNull();
  });

  it("renders skeletons while the XIRR query is pending", () => {
    render(<KpiBento kpis={kpis({ isPending: true })} />);
    expect(screen.getByRole("status", { name: "Ładowanie podsumowania portfela" })).toBeTruthy();
  });
});
