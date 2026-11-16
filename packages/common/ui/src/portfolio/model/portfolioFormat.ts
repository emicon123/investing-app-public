/// <reference lib="es2023.intl" />
// ↑ types `useGrouping: "always"` (the apps compile against lib ES2022, whose Intl typing predates it).

/**
 * Portfolio-view formatters (task 170, design doc §7). Pure, locale pinned to pl-PL.
 *
 * Every figure goes through `formatNum`: grouping is forced ("9 021", not "9021" — pl-PL does not
 * group 4-digit numbers by default), the minus is the typographic U+2212, and a value that rounds
 * to zero never shows a sign ("0,00", not "−0,00").
 */

const MINUS = "−";

const numberFormatters = new Map<number, Intl.NumberFormat>();

function numberFormatter(decimals: number): Intl.NumberFormat {
  let formatter = numberFormatters.get(decimals);
  if (formatter === undefined) {
    formatter = new Intl.NumberFormat("pl-PL", {
      minimumFractionDigits: decimals,
      maximumFractionDigits: decimals,
      useGrouping: "always",
    });
    numberFormatters.set(decimals, formatter);
  }
  return formatter;
}

export function formatNum(x: number, decimals: number): string {
  const text = numberFormatter(decimals).format(x).replace("-", MINUS);
  return /[1-9]/.test(text) ? text : text.replace(MINUS, "");
}

const sign = (x: number): string => (x > 0 ? "+" : "");

/** Whole złoty without the unit (the KPI cards set "zł" smaller): 121344.4 → "121 344". */
export function formatZlNumber(x: number): string {
  return formatNum(Math.round(x), 0);
}

/** As `formatZlNumber`, with "+" for a gain: −1851 → "−1 851"; 12563 → "+12 563". */
export function formatSignedZlNumber(x: number): string {
  return `${sign(Math.round(x))}${formatZlNumber(x)}`;
}

/** 30851 → "30 851 zł". */
export function formatZl(x: number): string {
  return `${formatZlNumber(x)} zł`;
}

/** 12563 → "+12 563 zł"; −1851 → "−1 851 zł"; a value that rounds to 0 → "0 zł". */
export function formatSignedZl(x: number): string {
  return `${formatSignedZlNumber(x)} zł`;
}

/** A portfolio share in %: 25.43 → "25,4%"; 0.19 → "0,19%"; 0 → "0,0%". */
export function formatShare(x: number): string {
  return `${formatNum(x, x >= 1 || x === 0 ? 1 : 2)}%`;
}

/** 312148 → "312 148 zł"; 155.02 → "155,02 zł"; 0.29807 → "0,2981 zł". */
export function formatPricePln(p: number): string {
  const decimals = p >= 1000 ? 0 : p >= 1 ? 2 : 4;
  return `${formatNum(p, decimals)} zł`;
}

/** A FRACTION as a signed percentage: (−0.0566, 1) → "−5,7%"; (0.4568, 1) → "+45,7%". */
export function formatSignedPct(fraction: number, decimals: number): string {
  return `${sign(fraction)}${formatNum(fraction * 100, decimals)}%`;
}

/** A deviation in percentage points: −14.84 → "−14,8 p.p."; −0.01 → "−0,01 p.p.". */
export function formatPp(x: number): string {
  return `${sign(x)}${formatNum(x, Math.abs(x) >= 1 ? 1 : 2)} p.p.`;
}

/** The ribbon's deviation, always 1 decimal: 35.51 → "+35,5 p.p.". */
export function formatPp1(x: number): string {
  return `${sign(x)}${formatNum(x, 1)} p.p.`;
}

/** The SELL amount: 14009.0 → "14,0 tys. zł"; 306 → "310 zł" (rounded to tens below 1000). */
export function formatKiloZl(x: number): string {
  if (x >= 1000) return `${formatNum(x / 1000, 1)} tys. zł`;
  return `${formatNum(Math.round(x / 10) * 10, 0)} zł`;
}

/** 1 → "pozycja"; 2–4 (not 12–14) → "pozycje"; else "pozycji". */
export function pluralPozycje(n: number): string {
  if (n === 1) return "pozycja";
  const lastDigit = n % 10;
  const lastTwo = n % 100;
  return lastDigit >= 2 && lastDigit <= 4 && (lastTwo < 12 || lastTwo > 14) ? "pozycje" : "pozycji";
}

/** "13 pozycji", "1 pozycja", "3 pozycje". */
export function formatPositionCount(n: number): string {
  return `${n} ${pluralPozycje(n)}`;
}

/**
 * Years elapsed as the genitive phrase that follows "Wpłaty z": 5.17 → "5 lat i 2 miesięcy";
 * 1.0 → "1 roku"; 0.6 → "7 miesięcy"; under half a month → "mniej niż miesiąca".
 */
export function formatYearsMonths(years: number): string {
  let wholeYears = Math.floor(years);
  let months = Math.round((years - wholeYears) * 12);
  if (months === 12) {
    wholeYears += 1;
    months = 0;
  }
  const parts: string[] = [];
  if (wholeYears > 0) parts.push(wholeYears === 1 ? "1 roku" : `${wholeYears} lat`);
  if (months > 0) parts.push(months === 1 ? "1 miesiąca" : `${months} miesięcy`);
  return parts.length > 0 ? parts.join(" i ") : "mniej niż miesiąca";
}

const pad2 = (n: number): string => String(n).padStart(2, "0");

/** "Ceny z 24.10.2026, 17:54" (local time). */
export function formatPricesAsOf(iso: string): string {
  const d = new Date(iso);
  const date = `${pad2(d.getDate())}.${pad2(d.getMonth() + 1)}.${d.getFullYear()}`;
  return `Ceny z ${date}, ${pad2(d.getHours())}:${pad2(d.getMinutes())}`;
}

/**
 * The ticker as the owner reads it: drops the "PL." prefix, the "-USD"/"-USDT" quote suffix and a
 * 1–3 letter exchange suffix. "BTC-USD" → "BTC"; "SXRV.DE" → "SXRV"; "ZLOTO_FIZ" unchanged.
 */
export function displayTicker(ticker: string): string {
  return ticker
    .replace(/^PL\./, "")
    .replace(/-USDT?$/, "")
    .replace(/\.[A-Z]{1,3}$/, "");
}

const ASSET_CLASS_LABEL_PL: Readonly<Record<string, string>> = {
  CRYPTO: "Krypto",
  STOCK: "Akcja",
  ETF: "ETF",
  METALS: "Metal",
  CURRENCY: "Waluta",
};

export function assetClassLabel(assetClass: string): string {
  return ASSET_CLASS_LABEL_PL[assetClass] ?? assetClass;
}
