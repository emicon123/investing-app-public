import { describe, expect, it } from "vitest";
import { computeTargetMeter, targetMeterLabels, type TargetMeterGeometry } from "./targetMeter.js";
import { deviationStatus, formatRebalanceFigure, groupMeterStatus } from "./rebalanceStatus.js";
import { MAIN_SNAPSHOT_TOTAL_PLN } from "../__fixtures__/mainSnapshot.js";

const ascii = (s: string): string => s.replace(/ /g, " ").replace(/−/g, "-");

interface MeterCase {
  name: string;
  cur: number;
  eff: number | null;
  base: number | null;
  dev: number | null;
  value: number;
  status: string;
  fill: number;
  excess: [number, number, string] | null;
  gap: [number, number, string] | null;
  marker: number | null;
  baseTick: number | null;
  labels: [string, string];
  figure: string;
}

// Design doc §5.4 shapes, synthetic values (the __fixtures__/mainSnapshot.ts rows); totalPln = 121 340.
const CASES: MeterCase[] = [
  { name: "Bitcoin", cur: 25.43, eff: 13.88, base: 24, dev: 0.832, value: 30851, status: "SELL", fill: 46.26, excess: [46.26, 38.49, "sell"], gap: null, marker: 46.26, baseTick: 79.98, labels: ["25,4%", "cel 13,9%"], figure: "~14,0 tys. zł" },
  { name: "Gold", cur: 33.02, eff: 28.43, base: 20, dev: 0.161, value: 40065, status: "OVER", fill: 72.97, excess: [72.97, 11.78, "over"], gap: null, marker: 72.97, baseTick: 51.33, labels: ["33,0%", "cel 28,4%"], figure: "+4,6 p.p." },
  { name: "NASDAQ", cur: 10.59, eff: 25.43, base: 14.3, dev: -0.584, value: 12845, status: "UNDER", fill: 35.29, excess: null, gap: [35.29, 49.45, "under"], marker: 84.75, baseTick: 47.65, labels: ["10,6%", "cel 25,4%"], figure: "-14,8 p.p." },
  { name: "Hedera", cur: 0.19, eff: 0.2, base: 0.2, dev: -0.04, value: 233, status: "OK", fill: 80.51, excess: null, gap: [80.51, 4.24, "faint"], marker: 84.75, baseTick: null, labels: ["0,19%", "cel 0,20%"], figure: "-0,01 p.p." },
  { name: "Algorand", cur: 0.06, eff: 0, base: 0, dev: 1.0, value: 73, status: "SELL", fill: 0, excess: [0, 84.75, "sell"], gap: null, marker: 0, baseTick: null, labels: ["0,06%", "cel 0,0%"], figure: "całość, 73 zł" },
  { name: "Sui", cur: 3.49, eff: 0.82, base: 3.6, dev: 3.256, value: 4234, status: "SELL", fill: 19.91, excess: [19.91, 64.83, "sell"], gap: null, marker: 19.91, baseTick: 87.42, labels: ["3,5%", "cel 0,82%"], figure: "~3,2 tys. zł" },
  { name: "Ethereum", cur: 5.6, eff: 2.72, base: 12, dev: 1.059, value: 6797, status: "SELL", fill: 41.16, excess: [41.16, 43.58, "sell"], gap: null, marker: 41.16, baseTick: null, labels: ["5,6%", "cel 2,7%"], figure: "~3,5 tys. zł" },
  { name: "synthetic, no target", cur: 5, eff: null, base: null, dev: null, value: 1000, status: "OK", fill: 84.75, excess: null, gap: null, marker: null, baseTick: null, labels: ["5,0%", "cel —"], figure: "brak celu" },
];

function expectSegment(
  actual: TargetMeterGeometry["excess"] | TargetMeterGeometry["gap"],
  expected: [number, number, string] | null,
) {
  if (expected === null) {
    expect(actual).toBeNull();
    return;
  }
  expect(actual).not.toBeNull();
  expect(actual?.leftPct).toBeCloseTo(expected[0], 2);
  expect(actual?.widthPct).toBeCloseTo(expected[1], 2);
  expect(actual?.tone).toBe(expected[2]);
}

describe.each(CASES)("TargetMeter + status: $name", (c) => {
  const status = deviationStatus(c.dev);
  const geometry = computeTargetMeter({ currentPct: c.cur, effectivePct: c.eff, basePct: c.base, status });

  it("derives the status from the unchanged thresholds", () => {
    expect(status).toBe(c.status);
  });

  it("computes fill, excess and gap", () => {
    expect(geometry.fillPct).toBeCloseTo(c.fill, 2);
    expectSegment(geometry.excess, c.excess);
    expectSegment(geometry.gap, c.gap);
  });

  it("places the effective marker and the base tick", () => {
    if (c.marker === null) expect(geometry.markerPct).toBeNull();
    else expect(geometry.markerPct).toBeCloseTo(c.marker, 2);
    if (c.baseTick === null) expect(geometry.baseTickPct).toBeNull();
    else expect(geometry.baseTickPct).toBeCloseTo(c.baseTick, 2);
  });

  it("labels the bar", () => {
    const labels = targetMeterLabels(c.cur, c.eff, c.base);
    expect([labels.current, labels.target]).toEqual(c.labels);
  });

  it("formats the Rebalansowanie figure", () => {
    const figure = formatRebalanceFigure({
      status,
      valuePln: c.value,
      currentPct: c.cur,
      effectivePct: c.eff,
      totalPln: MAIN_SNAPSHOT_TOTAL_PLN,
    });
    expect(ascii(figure)).toBe(c.figure);
  });
});

describe("rebalance figure edge cases", () => {
  it("rounds a sub-1000 SELL amount to tens (Circle)", () => {
    const figure = formatRebalanceFigure({
      status: deviationStatus(3.55),
      valuePln: 1099,
      currentPct: 0.91,
      effectivePct: 0.2,
      totalPln: MAIN_SNAPSHOT_TOTAL_PLN,
    });
    expect(figure).toBe("~860 zł");
  });

  it("never shows a percentage in the status column", () => {
    for (const c of CASES) {
      const figure = formatRebalanceFigure({
        status: deviationStatus(c.dev),
        valuePln: c.value,
        currentPct: c.cur,
        effectivePct: c.eff,
        totalPln: MAIN_SNAPSHOT_TOTAL_PLN,
      });
      expect(figure).not.toMatch(/%/);
    }
  });
});

describe("meter tooltip", () => {
  it("names the base target only when it differs from the effective one", () => {
    expect(targetMeterLabels(25.43, 13.88, 24).title).toBe("Udział 25,4%, cel efektywny 13,9%, cel bazowy 24,0%");
    expect(targetMeterLabels(0.19, 0.2, 0.2).title).toBe("Udział 0,19%, cel 0,20%");
  });
});

describe("groupMeterStatus", () => {
  it("is OVER / UNDER / OK around ±5 % of the group target, never SELL", () => {
    expect(groupMeterStatus(57.56, 22.04)).toBe("OVER");
    expect(groupMeterStatus(7.58, 38.57)).toBe("UNDER");
    expect(groupMeterStatus(2.19, 2.25)).toBe("OK");
    expect(groupMeterStatus(1, 0)).toBe("OVER");
  });

  it("colours a group excess amber, never coral", () => {
    const geometry = computeTargetMeter({ currentPct: 57.56, effectivePct: 22.04, basePct: 60, status: "OVER" });
    expect(geometry.excess?.tone).toBe("over");
  });
});
