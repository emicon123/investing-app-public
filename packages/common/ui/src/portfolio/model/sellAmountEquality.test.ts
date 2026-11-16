import { describe, expect, it } from "vitest";
import {
  computeSuggestedSellUnits,
  computeTotalPortfolioValuePln,
  type TransferAssetLike,
} from "../../PositionTransferDialog.js";
import { computeTrimExcessPln, effectiveTargetPct } from "./rebalanceStatus.js";
import { buildPositionsModel } from "./positionsModel.js";
import { formatKiloZl } from "./portfolioFormat.js";
import { MAIN_SNAPSHOT_TOTAL_PLN, snapshotAssets } from "../__fixtures__/mainSnapshot.js";

/**
 * Task 170 / ADR-048 §7.5: the SELL figure in the table and the transfer dialog's pre-filled
 * amount come from one formula (`computeTrimExcessPln`) and must agree, up to the dialog's
 * 4-decimal unit rounding (half a 0.0001-unit step × price). Design doc §5.4 "Equality test".
 */

const PLENTY_OF_UNITS = 1e9;

/** main's `RebalancingTransferDialog.toTransferAsset` target mapping: effective ?? raw. */
function toTransferAsset(asset: ReturnType<typeof snapshotAssets>[number]): TransferAssetLike {
  return {
    ticker: asset.key,
    name: asset.name,
    pricePln: asset.computed?.pricePln ?? null,
    units: PLENTY_OF_UNITS,
    targetAllocationPct: effectiveTargetPct(asset),
    currentPct: asset.computed?.currentPct ?? null,
    valuePln: asset.computed?.valuePln ?? 0,
  };
}

/** What the dialog pre-fills in "Kwota" (PLN): suggested units × the candidate's price. */
function dialogPrefillPln(position: TransferAssetLike, totalPln: number): number {
  const price = position.pricePln ?? 0;
  return computeSuggestedSellUnits(position, totalPln, price) * price;
}

describe("SELL figure equals the transfer dialog's pre-fill", () => {
  const assets = snapshotAssets();
  const positions = assets.map(toTransferAsset);
  const model = buildPositionsModel(assets);

  it("both sides use the same portfolio total", () => {
    expect(computeTotalPortfolioValuePln(positions)).toBe(model.totalPln);
    expect(model.totalPln).toBe(MAIN_SNAPSHOT_TOTAL_PLN);
  });

  it("Bitcoin: 14 009,0 zł on both sides, shown as ~14,0 tys. zł", () => {
    const bitcoin = positions.find((p) => p.name === "Bitcoin")!;
    const table = computeTrimExcessPln(30_851, 13.88, MAIN_SNAPSHOT_TOTAL_PLN);
    const dialog = dialogPrefillPln(bitcoin, MAIN_SNAPSHOT_TOTAL_PLN);
    expect(table).toBeCloseTo(14_009.0, 1);
    expect(Math.abs(dialog - table)).toBeLessThanOrEqual(0.00005 * 312_148);
    expect(formatKiloZl(dialog)).toBe(formatKiloZl(table));
    expect(model.rows.find((r) => r.name === "Bitcoin")!.figure).toBe(`~${formatKiloZl(dialog)}`);
  });

  it("holds for every SELL row of the snapshot", () => {
    const sellRows = model.rows.filter((r) => r.status === "SELL" && r.effectivePct !== 0);
    expect(sellRows.length).toBeGreaterThan(0);
    for (const row of sellRows) {
      const position = positions.find((p) => p.name === row.name)!;
      const table = computeTrimExcessPln(row.valuePln, row.effectivePct!, model.totalPln);
      const dialog = dialogPrefillPln(position, model.totalPln);
      expect(Math.abs(dialog - table)).toBeLessThanOrEqual(0.00005 * (position.pricePln ?? 0) + 1e-9);
    }
  });

  it("a zero target trims the whole position (\"całość\")", () => {
    const algorand = positions.find((p) => p.name === "Algorand")!;
    expect(dialogPrefillPln(algorand, MAIN_SNAPSHOT_TOTAL_PLN)).toBeCloseTo(73, 0);
    expect(model.rows.find((r) => r.name === "Algorand")!.figure.replace(/ /g, " ")).toBe("całość, 73 zł");
  });
});
