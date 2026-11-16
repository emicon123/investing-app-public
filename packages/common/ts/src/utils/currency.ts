const plnFormatter = new Intl.NumberFormat("pl-PL", {
  style: "currency",
  currency: "PLN",
  minimumFractionDigits: 2,
  maximumFractionDigits: 2,
});

export function formatPln(value: number, decimals = 2): string {
  if (decimals === 2) return plnFormatter.format(value);
  return new Intl.NumberFormat("pl-PL", {
    style: "currency",
    currency: "PLN",
    minimumFractionDigits: decimals,
    maximumFractionDigits: decimals,
  }).format(value);
}

export function formatCurrency(value: number, currency: string, decimals = 2): string {
  return new Intl.NumberFormat("pl-PL", {
    style: "currency",
    currency,
    minimumFractionDigits: decimals,
    maximumFractionDigits: decimals,
  }).format(value);
}

export function formatPct(value: number, decimals = 2): string {
  return new Intl.NumberFormat("pl-PL", {
    style: "percent",
    minimumFractionDigits: decimals,
    maximumFractionDigits: decimals,
  }).format(value / 100);
}

export function getAssetUnitsPrecision(assetClass: string): number {
  switch (assetClass) {
    case "CRYPTO":
      return 9;
    case "ETF":
    case "CURRENCY":
      return 6;
    case "STOCK":
    default:
      return 4;
  }
}

export function formatAssetUnits(value: number, assetClass: string): string {
  if (value === 0) return "—";
  const precision = getAssetUnitsPrecision(assetClass);
  return value.toFixed(precision);
}
