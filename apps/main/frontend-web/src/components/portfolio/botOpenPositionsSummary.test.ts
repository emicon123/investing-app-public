import { describe, expect, it } from "vitest";
import { openPositionsSummary } from "./botOpenPositionsSummary";

const ascii = (s: string): string => s.replace(/ /g, " ").replace(/−/g, "-");

describe("openPositionsSummary (bot badge, design doc §14 D1)", () => {
  it("says when nothing is open", () => {
    expect(openPositionsSummary([])).toBe("brak otwartych pozycji");
  });

  it("describes one position with its unrealised P/L", () => {
    expect(openPositionsSummary([{ side: "LONG", leverage: 3, unrealizedPnlPln: 12.4 }])).toBe("LONG ×3 · +12,40 zł");
    expect(openPositionsSummary([{ side: "SHORT", leverage: 5, unrealizedPnlPln: null }])).toBe("SHORT ×5");
  });

  it("counts several positions with Polish plurals and sums the P/L", () => {
    const two = [
      { side: "LONG", leverage: 3, unrealizedPnlPln: -6.1 },
      { side: "SHORT", leverage: 2, unrealizedPnlPln: 2 },
    ];
    expect(ascii(openPositionsSummary(two))).toBe("2 otwarte pozycje · -4,10 zł");
    const five = Array.from({ length: 5 }, () => ({ side: "LONG", leverage: 1, unrealizedPnlPln: null }));
    expect(openPositionsSummary(five)).toBe("5 otwartych pozycji");
  });
});
