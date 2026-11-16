import { describe, expect, it } from "vitest";
import { EXPOSURE_FALLBACK_COLOR, exposureGroupOf } from "./exposure-groups.js";

describe("exposureGroupOf (design doc §4.2)", () => {
  it.each([
    ["CRYPTO", "Krypto", "#9D8CF8"],
    ["METALS", "Metale", "#D8B45C"],
    ["EQUITY", "Akcje", "#4DB6C3"],
    ["STOCKS", "Akcje", "#4DB6C3"],
    ["BONDS", "Obligacje", "#8297BE"],
    ["EXPERIMENTS", "Eksperymenty", "#6B7588"],
    ["COMMODITIES", "Surowce", "#956725"],
    ["CURRENCY", "Waluta", "#9B46B4"],
    ["CASH", "Gotówka", "#9B46B4"],
    ["QUANTUM", "Quantum", "#956725"],
  ])("%s → %s %s", (exposure, label, color) => {
    expect(exposureGroupOf(exposure)).toEqual({ exposure, label, color });
  });

  it("falls back to the raw value and the neutral colour", () => {
    expect(exposureGroupOf("NEW")).toEqual({ exposure: "NEW", label: "NEW", color: EXPOSURE_FALLBACK_COLOR });
    expect(exposureGroupOf(undefined).color).toBe(EXPOSURE_FALLBACK_COLOR);
  });
});
