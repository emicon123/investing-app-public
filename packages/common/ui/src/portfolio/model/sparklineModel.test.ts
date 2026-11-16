import { describe, expect, it } from "vitest";
import { buildSparkline, isoLocalDate, sparklineSeries } from "./sparklineModel.js";
import { MAIN_SNAPSHOT_LIVE_VALUE, MAIN_SNAPSHOT_SPARK } from "../__fixtures__/mainSnapshot.js";

const ascii = (s: string): string => s.replace(/ /g, " ").replace(/−/g, "-");

const TODAY = new Date(2026, 9, 24, 18, 0);
const DAY_MS = 24 * 60 * 60 * 1000;

/** The snapshot's 31 synthetic values dated today − 30 … today. */
const history = MAIN_SNAPSHOT_SPARK.map((portfolioValue, i) => ({
  snapshotDate: isoLocalDate(new Date(TODAY.getTime() - (30 - i) * DAY_MS)),
  portfolioValue,
}));

describe("buildSparkline (design doc §5.4 sparkline fixture)", () => {
  it("30 days: −15 640 zł (−11,4%)", () => {
    const spark = buildSparkline(history, MAIN_SNAPSHOT_LIVE_VALUE, 30, TODAY);
    expect(spark).not.toBeNull();
    expect(`${ascii(spark!.changeLabel)} ${spark!.rangeLabel}`).toBe("-15 640 zł (-11,4%) w 30 dni");
    expect(spark!.tone).toBe("loss");
    expect(ascii(spark!.ariaLabel)).toBe("Wycena w ostatnich 30 dniach: od 136 984 zł do 121 344 zł");
  });

  it("7 days: −7 599 zł (−5,9%)", () => {
    const spark = buildSparkline(history, MAIN_SNAPSHOT_LIVE_VALUE, 7, TODAY);
    expect(ascii(spark!.changeLabel)).toBe("-7 599 zł (-5,9%)");
    expect(spark!.rangeLabel).toBe("w 7 dni");
  });

  it("replaces today's snapshot with the live value, or appends it", () => {
    expect(sparklineSeries(history, 1, 7, TODAY).at(-1)).toBe(1);
    expect(sparklineSeries(history, 1, 7, TODAY)).toHaveLength(8);
    const withoutToday = history.slice(0, -1);
    expect(sparklineSeries(withoutToday, 1, 7, TODAY)).toHaveLength(8);
  });

  it("returns null with fewer than 2 points", () => {
    expect(buildSparkline([], 100, 30, TODAY)).toBeNull();
    expect(buildSparkline([], null, 30, TODAY)).toBeNull();
  });

  it("draws a path inside the 240 × 56 box", () => {
    const spark = buildSparkline(history, MAIN_SNAPSHOT_LIVE_VALUE, 30, TODAY)!;
    expect(spark.line.startsWith("M0.0 ")).toBe(true);
    expect(spark.area.endsWith("L240 56 L0 56 Z")).toBe(true);
    expect(spark.endYPct).toBeGreaterThan(0);
    expect(spark.endYPct).toBeLessThanOrEqual(100);
  });
});
