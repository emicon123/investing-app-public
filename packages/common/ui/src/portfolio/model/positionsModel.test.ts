import { describe, expect, it } from "vitest";
import {
  FILTER_ALL,
  FILTER_SELL,
  applyPositionsFilter,
  buildFilterChips,
  buildPositionsModel,
  matchesAssetQuery,
} from "./positionsModel.js";
import { snapshotAssets } from "../__fixtures__/mainSnapshot.js";
import type { PortfolioAssetLike, PortfolioSubassetLike } from "../types.js";

const ascii = (s: string): string => s.replace(/ /g, " ").replace(/−/g, "-");

const model = buildPositionsModel(snapshotAssets());

describe("grouping the mockup snapshot (design doc §5.4)", () => {
  it("builds the filter chips", () => {
    expect(buildFilterChips(model).map((c) => `${c.label} ${c.count}`)).toEqual([
      "Wszystkie 34",
      "Krypto 13",
      "Metale 1",
      "Akcje 3",
      "Obligacje 2",
      "Eksperymenty 15",
      "Sprzedaj 14",
    ]);
  });

  it("orders groups by value, descending", () => {
    expect(model.groups.map((g) => `${g.label} ${ascii(g.value)}`)).toEqual([
      "Krypto 56 226 zł",
      "Metale 40 065 zł",
      "Akcje 19 151 zł",
      "Obligacje 3 041 zł",
      "Eksperymenty 2 857 zł",
    ]);
  });

  it("orders rows inside a group by value, descending", () => {
    const crypto = model.groups[0]!;
    expect(crypto.rows.slice(0, 3).map((r) => r.name)).toEqual(["Bitcoin", "Ethereum", "Solana"]);
  });

  it("summarises each group by its most severe status", () => {
    expect(model.groups.map((g) => `${g.summary.label}${g.summary.countLabel ? ` ${g.summary.countLabel}` : ""}`)).toEqual([
      "Sprzedaj ×9",
      "Za dużo ×1",
      "Za mało ×3",
      "Za mało ×2",
      "Sprzedaj ×5",
    ]);
  });

  it("counts positions with Polish plurals", () => {
    const filtered = applyPositionsFilter(model, FILTER_ALL, "");
    expect(filtered.groups.map((g) => g.countLabel)).toEqual([
      "13 pozycji",
      "1 pozycja",
      "3 pozycje",
      "2 pozycje",
      "15 pozycji",
    ]);
  });

  it("builds the ribbon legend", () => {
    expect(model.ribbon.map((s) => [s.label, s.currentLabel, s.targetLabel, ascii(s.delta)])).toEqual([
      ["Krypto", "46,3%", "cel 22,0%", "+24,3 p.p."],
      ["Metale", "33,0%", "cel 28,4%", "+4,6 p.p."],
      ["Akcje", "15,8%", "cel 38,6%", "-22,8 p.p."],
      ["Obligacje", "2,5%", "cel 8,4%", "-5,9 p.p."],
      ["Eksperymenty", "2,4%", "cel 2,5%", "-0,1 p.p."],
    ]);
    expect(model.ribbon.map((s) => s.deltaTone)).toEqual(["over", "over", "under", "under", "flat"]);
  });

  it("normalises the target bar so it always fills", () => {
    const total = model.ribbon.reduce((a, s) => a + s.targetWidthPct, 0);
    expect(total).toBeCloseTo(100, 6);
  });

  it("detects the macro override and sums the base targets of the whole book", () => {
    expect(model.hasEffectiveOverride).toBe(true);
    expect(model.targetSum.value).toBeCloseTo(100, 1);
    expect(model.targetSum.label).toBe("Suma celów bazowych 100,0% ✓");
    expect(model.targetSum.tone).toBe("ok");
  });

  it("formats a row's cells", () => {
    const bitcoin = model.rows.find((r) => r.name === "Bitcoin")!;
    expect(bitcoin.caption).toBe("BTC • Krypto");
    expect(ascii(bitcoin.price)).toBe("312 148 zł");
    expect(ascii(bitcoin.entry)).toBe("wejście 330 876 zł");
    expect(bitcoin.shareLabel).toBe("25,4% portfela");
    expect(ascii(bitcoin.profit.profitLabel)).toBe("-5,7%");
    // P/L is derived from value and the (4-decimal) profit fraction: 30 851 − 30 851 / 0.9434.
    expect(ascii(bitcoin.profit.plLabel)).toBe("-1 851 zł");
    expect(bitcoin.profit.tone).toBe("loss");
  });

  it("says why a row has no profit", () => {
    const trac = model.rows.find((r) => r.name === "OriginTrail")!;
    expect(trac.profit.profitLabel).toBe("—");
    expect(trac.profit.plLabel).toBe("brak jednostek");
    expect(trac.entry).toBe("brak wejścia");
  });
});

describe("entry price and native currency (carries over EntryPriceCell, design doc §14 D11)", () => {
  const usdAsset: PortfolioAssetLike<PortfolioSubassetLike> = {
    id: 1,
    key: "bitcoin",
    name: "Bitcoin",
    assetClass: "CRYPTO",
    exposure: "CRYPTO",
    targetAllocationPct: 50,
    computed: {
      valuePln: 57_600,
      currentPct: 45,
      pricePln: 480_000,
      priceNative: 120_000,
      profitPct: -0.04,
      avgBuyPricePln: 500_000,
      avgBuyPriceNative: 125_000,
    },
    subassets: [{ id: 1, isPrimary: true, ticker: "BTC-USD", name: "Bitcoin", priceCurrency: "USD" }],
  };

  it("shows the PLN entry under the price and the native figures in the cell's tooltip", () => {
    const [row] = buildPositionsModel([usdAsset]).rows;
    expect(ascii(row!.price)).toBe("480 000 zł");
    expect(ascii(row!.entry)).toBe("wejście 500 000 zł");
    expect(ascii(row!.priceTitle ?? "")).toBe("Cena 120 000,00 USD · wejście ≈ 125 000,00 USD (po dzisiejszym kursie)");
  });

  it("has no tooltip for a PLN primary and says when the entry is unknown", () => {
    const pln = {
      ...usdAsset,
      computed: { ...usdAsset.computed, avgBuyPricePln: null, avgBuyPriceNative: null, profitPct: null },
      subassets: [{ ...usdAsset.subassets[0]!, priceCurrency: "PLN" }],
    };
    const [row] = buildPositionsModel([pln]).rows;
    expect(row!.priceTitle).toBeUndefined();
    expect(row!.entry).toBe("brak wejścia");
    expect(row!.profit.plLabel).toBe("brak kosztu");
  });
});

describe("filtering and search (design doc §6)", () => {
  it("a group chip keeps only that group", () => {
    const filtered = applyPositionsFilter(model, "METALS", "");
    expect(filtered.isFiltering).toBe(true);
    expect(filtered.groups.map((g) => g.group.label)).toEqual(["Metale"]);
  });

  it("the Sprzedaj chip keeps SELL rows and says how many of the group match", () => {
    const filtered = applyPositionsFilter(model, FILTER_SELL, "");
    expect(filtered.groups.map((g) => [g.group.label, g.rows.length, g.countLabel])).toEqual([
      ["Krypto", 9, "9 z 13 pozycji"],
      ["Eksperymenty", 5, "5 z 15 pozycji"],
    ]);
  });

  it("search matches a subasset ticker (IGLN finds Gold)", () => {
    const assets = snapshotAssets();
    const gold = assets.find((a) => a.name === "Gold")!;
    gold.subassets.push({ id: 9999, isPrimary: false, ticker: "IGLN.L", name: "iShares Physical Gold", priceCurrency: "USD" });
    const filtered = applyPositionsFilter(buildPositionsModel(assets), FILTER_ALL, "igln");
    expect(filtered.groups.flatMap((g) => g.rows.map((r) => r.name))).toEqual(["Gold"]);
  });

  it("chip AND query combine; nothing matching leaves no groups", () => {
    expect(applyPositionsFilter(model, "METALS", "bitcoin").groups).toEqual([]);
    expect(applyPositionsFilter(model, "CRYPTO", "bitcoin").groups.map((g) => g.rows.length)).toEqual([1]);
  });

  it("matches on key, name and every subasset ticker and name, ignoring case and blanks", () => {
    const asset: PortfolioAssetLike<PortfolioSubassetLike> = {
      id: 1,
      key: "gold",
      name: "Złoto",
      assetClass: "ETF",
      exposure: "METALS",
      targetAllocationPct: 20,
      subassets: [{ id: 1, isPrimary: true, ticker: "4GLD.DE", name: "Xetra-Gold", priceCurrency: "EUR" }],
    };
    expect(["GOLD", "złoto", "4gld", "xetra", "  "].map((q) => matchesAssetQuery(asset, q))).toEqual([
      true,
      true,
      true,
      true,
      true,
    ]);
    expect(matchesAssetQuery(asset, "btc")).toBe(false);
  });
});

describe("a book without macro override (portfolioB / portfolioC shape)", () => {
  it("compares status against the raw target and drops the 'bazowych' wording", () => {
    const assets = snapshotAssets().map((a) => ({
      ...a,
      targetAllocationPct: a.computed!.effectiveTargetAllocationPct ?? null,
      computed: { ...a.computed, effectiveTargetAllocationPct: undefined },
    }));
    const plain = buildPositionsModel(assets);
    expect(plain.hasEffectiveOverride).toBe(false);
    expect(plain.targetSum.label.startsWith("Suma celów ")).toBe(true);
    expect(plain.targetSum.label).not.toContain("bazowych");
    expect(plain.rows.every((r) => r.meter.baseTickPct === null)).toBe(true);
  });

  it("shows '—' and 'brak kosztu' for a group with no known cost", () => {
    const assets = snapshotAssets().map((a) => ({ ...a, computed: { ...a.computed, profitPct: null } }));
    const noCost = buildPositionsModel(assets);
    expect(noCost.groups[0]!.profit.profitLabel).toBe("—");
    expect(noCost.groups[0]!.profit.plLabel).toBe("brak kosztu");
  });
});
