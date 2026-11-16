import { describe, expect, it } from "vitest";
import {
  assetClassLabel,
  displayTicker,
  formatKiloZl,
  formatNum,
  formatPp,
  formatPp1,
  formatPricePln,
  formatPricesAsOf,
  formatShare,
  formatSignedPct,
  formatSignedZl,
  formatYearsMonths,
  formatZl,
  pluralPozycje,
} from "./portfolioFormat.js";

/** Plain-ASCII view of a formatted figure: U+00A0 → " ", U+2212 → "-". */
const ascii = (s: string): string => s.replace(/ /g, " ").replace(/−/g, "-");

describe("formatNum", () => {
  it("groups 4-digit numbers with U+00A0 and uses the U+2212 minus", () => {
    expect(formatNum(9021, 0)).toBe("9 021");
    expect(formatNum(-12596, 0)).toBe("−12 596");
  });

  it("never signs a value that rounds to zero", () => {
    expect(formatNum(-0, 0)).toBe("0");
    expect(formatNum(-0.001, 2)).toBe("0,00");
  });
});

describe("design doc §7 examples", () => {
  it.each([
    [formatZl(30851), "30 851 zł"],
    [formatSignedZl(-1851), "-1 851 zł"],
    [formatSignedZl(12563), "+12 563 zł"],
    [formatShare(25.43), "25,4%"],
    [formatShare(0.19), "0,19%"],
    [formatShare(0), "0,0%"],
    [formatPricePln(312148), "312 148 zł"],
    [formatPricePln(155.02), "155,02 zł"],
    [formatPricePln(0.29807), "0,2981 zł"],
    [formatSignedPct(-0.0566, 1), "-5,7%"],
    [formatSignedPct(0.0291, 2), "+2,91%"],
    [formatPp(-14.84), "-14,8 p.p."],
    [formatPp(4.59), "+4,6 p.p."],
    [formatPp(-0.01), "-0,01 p.p."],
    [formatPp1(24.29), "+24,3 p.p."],
    [formatKiloZl(14009.0), "14,0 tys. zł"],
    [formatKiloZl(306.0), "310 zł"],
    [formatYearsMonths(5.17), "5 lat i 2 miesięcy"],
    [formatYearsMonths(1.0), "1 roku"],
    [formatYearsMonths(0.6), "7 miesięcy"],
    [formatYearsMonths(0.01), "mniej niż miesiąca"],
    [formatYearsMonths(1.99), "2 lat"],
  ])("%s", (actual, expected) => {
    expect(ascii(actual)).toBe(expected);
  });

  it("pluralises pozycja", () => {
    expect([1, 2, 5, 12, 22].map(pluralPozycje)).toEqual(["pozycja", "pozycje", "pozycji", "pozycji", "pozycje"]);
  });

  it("formats the prices timestamp in local time", () => {
    expect(formatPricesAsOf(new Date(2026, 9, 24, 17, 54).toISOString())).toBe("Ceny z 24.10.2026, 17:54");
  });

  it.each([
    ["BTC-USD", "BTC"],
    ["SUI-USDT", "SUI"],
    ["SXRV.DE", "SXRV"],
    ["PL.ETNVIRBTCP", "ETNVIRBTCP"],
    ["ETCGLDRMAU.PL", "ETCGLDRMAU"],
    ["ZLOTO_FIZ", "ZLOTO_FIZ"],
  ])("displayTicker(%s) = %s", (ticker, expected) => {
    expect(displayTicker(ticker)).toBe(expected);
  });

  it("labels asset classes in Polish, unknown ones raw", () => {
    expect(["CRYPTO", "STOCK", "ETF", "METALS", "CURRENCY", "BOND"].map(assetClassLabel)).toEqual([
      "Krypto",
      "Akcja",
      "ETF",
      "Metal",
      "Waluta",
      "BOND",
    ]);
  });
});
