import type { PortfolioAssetLike, PortfolioSubassetLike } from "../types.js";

/**
 * A synthetic MAIN snapshot (34 assets) for the task-170 model tests (design doc §5.4), shaped like
 * the owner-approved mockup's rows. All amounts, shares, profits and entry prices are invented;
 * only names, tickers, classes and targets follow the mockup. Keys: n name, t ticker,
 * c class label, g exposure, v value PLN, cur/eff/base current/effective/base %, dev deviation
 * fraction, pr profit fraction, pp P/L PLN, px price PLN, avg entry PLN.
 */
export interface SnapshotRow {
  n: string;
  t: string;
  c: "Krypto" | "Akcja" | "ETF";
  g: string;
  v: number;
  cur: number;
  eff: number;
  base: number;
  dev: number;
  pr: number | null;
  pp: number | null;
  px: number;
  avg: number | null;
}

export const MAIN_SNAPSHOT_ROWS: readonly SnapshotRow[] = [
  {n: "Gold", t: "ETCGLDRMAU", c: "ETF", g: "METALS", v: 40065, cur: 33.02, eff: 28.43, base: 20, dev: 0.161, pr: 0.4568, pp: 12563, px: 155.02, avg: 106.411},
  {n: "Bitcoin", t: "BTC", c: "Krypto", g: "CRYPTO", v: 30851, cur: 25.43, eff: 13.88, base: 24, dev: 0.832, pr: -0.0566, pp: -1851, px: 312148, avg: 330876},
  {n: "Ethereum", t: "ETH", c: "Krypto", g: "CRYPTO", v: 6797, cur: 5.6, eff: 2.72, base: 12, dev: 1.059, pr: 0.9183, pp: 3254, px: 9021.27, avg: 4702.74},
  {n: "Solana", t: "SOL", c: "Krypto", g: "CRYPTO", v: 5935, cur: 4.89, eff: 1.36, base: 6, dev: 2.596, pr: 1.0237, pp: 3002, px: 390.176, avg: 192.803},
  {n: "Sui", t: "SUI", c: "Krypto", g: "CRYPTO", v: 4234, cur: 3.49, eff: 0.82, base: 3.6, dev: 3.256, pr: 0.22, pp: 764, px: 3.62026, avg: 2.96743},
  {n: "NASDAQ", t: "SXRV", c: "ETF", g: "EQUITY", v: 12845, cur: 10.59, eff: 25.43, base: 14.3, dev: -0.584, pr: 0.5589, pp: 4605, px: 6580.05, avg: 4220.96},
  {n: "China", t: "ICGA", c: "ETF", g: "EQUITY", v: 3120, cur: 2.57, eff: 6.57, base: 1.6, dev: -0.609, pr: 0.8371, pp: 1422, px: 19.8666, avg: 10.8141},
  {n: "Coinbase", t: "COIN", c: "Akcja", g: "CRYPTO", v: 3100, cur: 2.55, eff: 0.82, base: 3.6, dev: 2.11, pr: 0.3299, pp: 769, px: 645.647, avg: 485.485},
  {n: "Marathon Digital Holdings", t: "MARA", c: "Akcja", g: "CRYPTO", v: 963, cur: 0.79, eff: 0.27, base: 1.2, dev: 1.926, pr: 0.0992, pp: 87, px: 27.5612, avg: 25.0739},
  {n: "Dogecoin", t: "DOGE", c: "Krypto", g: "CRYPTO", v: 1030, cur: 0.85, eff: 0.27, base: 1.2, dev: 2.148, pr: -0.3417, pp: -535, px: 0.296538, avg: 0.45046},
  {n: "Cipher Mining", t: "CIFR", c: "Akcja", g: "CRYPTO", v: 529, cur: 0.44, eff: 0.27, base: 1.2, dev: 0.63, pr: -0.1463, pp: -91, px: 31.1732, avg: 36.5154},
  {n: "Hut 8 Mining", t: "HUT", c: "Akcja", g: "CRYPTO", v: 1398, cur: 1.15, eff: 0.27, base: 1.2, dev: 3.259, pr: 0.5408, pp: 491, px: 240.395, avg: 156.02},
  {n: "Obligacje USD", t: "ZPR1", c: "ETF", g: "BONDS", v: 676, cur: 0.56, eff: 2.11, base: 0, dev: -0.735, pr: 0.0138, pp: 9, px: 477.61, avg: 471.109},
  {n: "Obligacje PLN", t: "ETFPZUCASH", c: "ETF", g: "BONDS", v: 2365, cur: 1.95, eff: 6.32, base: 0, dev: -0.691, pr: 0.5156, pp: 805, px: 100.9, avg: 66.5743},
  {n: "Chainlink", t: "LINK", c: "Krypto", g: "CRYPTO", v: 588, cur: 0.48, eff: 0.41, base: 1.8, dev: 0.171, pr: 1.0802, pp: 305, px: 44.6005, avg: 21.4405},
  {n: "Quantum", t: "WQTM", c: "ETF", g: "EXPERIMENTS", v: 252, cur: 0.21, eff: 0.7, base: 0.7, dev: -0.7, pr: -0.0491, pp: -13, px: 125.535, avg: 132.017},
  {n: "Ondo", t: "ONDO", c: "Krypto", g: "CRYPTO", v: 399, cur: 0.33, eff: 0.41, base: 1.8, dev: -0.195, pr: -0.1501, pp: -70, px: 1.56848, avg: 1.84549},
  {n: "Circle", t: "CRCL", c: "Akcja", g: "EXPERIMENTS", v: 1099, cur: 0.91, eff: 0.2, base: 0.2, dev: 3.55, pr: 0.4713, pp: 352, px: 337.291, avg: 229.247},
  {n: "Strategy", t: "MSTR", c: "Akcja", g: "EXPERIMENTS", v: 387, cur: 0.32, eff: 0.1, base: 0.1, dev: 2.2, pr: 0.5895, pp: 144, px: 573.25, avg: 360.648},
  {n: "XRP", t: "XRP", c: "Krypto", g: "EXPERIMENTS", v: 205, cur: 0.17, eff: 0.3, base: 0.3, dev: -0.433, pr: -0.162, pp: -40, px: 4.96887, avg: 5.92944},
  {n: "Hedera", t: "HBAR", c: "Krypto", g: "EXPERIMENTS", v: 233, cur: 0.19, eff: 0.2, base: 0.2, dev: -0.04, pr: 0.2035, pp: 39, px: 0.29807, avg: 0.247669},
  {n: "TeraWulf", t: "WULF", c: "Akcja", g: "CRYPTO", v: 278, cur: 0.23, eff: 0.27, base: 1.2, dev: -0.148, pr: 0.4145, pp: 81, px: 44.4435, avg: 31.4199},
  {n: "Algorand", t: "ALGO", c: "Krypto", g: "EXPERIMENTS", v: 73, cur: 0.06, eff: 0, base: 0, dev: 1, pr: 0.5512, pp: 26, px: 0.40007, avg: 0.25791},
  {n: "IREN", t: "IREN", c: "Akcja", g: "CRYPTO", v: 124, cur: 0.1, eff: 0.27, base: 1.2, dev: -0.63, pr: 0.5754, pp: 45, px: 97.7795, avg: 62.0665},
  {n: "Bittensor", t: "TAO", c: "Krypto", g: "EXPERIMENTS", v: 181, cur: 0.15, eff: 0.3, base: 0.3, dev: -0.5, pr: 0.1176, pp: 19, px: 960.324, avg: 859.273},
  {n: "Zcash", t: "ZEC", c: "Krypto", g: "EXPERIMENTS", v: 60, cur: 0.05, eff: 0.2, base: 0.2, dev: -0.75, pr: -0.3606, pp: -34, px: 4476.7, avg: 7001.41},
  {n: "Tron", t: "TRX", c: "Krypto", g: "EXPERIMENTS", v: 24, cur: 0.02, eff: 0.1, base: 0.1, dev: -0.8, pr: 0.838, pp: 11, px: 1.29169, avg: 0.702769},
  {n: "India", t: "QDV5", c: "ETF", g: "EQUITY", v: 3186, cur: 2.63, eff: 6.57, base: 1.6, dev: -0.6, pr: 0.1161, pp: 331, px: 32.1557, avg: 28.8108},
  {n: "Chainflip", t: "FLIP", c: "Krypto", g: "EXPERIMENTS", v: 25, cur: 0.02, eff: 0.1, base: 0.1, dev: -0.8, pr: -0.1555, pp: -5, px: 1.20123, avg: 1.42242},
  {n: "LoFi", t: "LOFI", c: "Krypto", g: "EXPERIMENTS", v: 133, cur: 0.11, eff: 0, base: 0, dev: 1, pr: -0.0484, pp: -7, px: 0.0363147, avg: 0.0381617},
  {n: "Zano", t: "ZANO", c: "Krypto", g: "EXPERIMENTS", v: 73, cur: 0.06, eff: 0, base: 0, dev: 1, pr: 0.029, pp: 2, px: 15.6923, avg: 15.25},
  {n: "Plasma", t: "XPL", c: "Krypto", g: "EXPERIMENTS", v: 78, cur: 0.06, eff: 0.1, base: 0.1, dev: -0.4, pr: -0.1045, pp: -9, px: 0.28213, avg: 0.315053},
  {n: "Artificial Superintelligence Alliance", t: "FET", c: "Krypto", g: "EXPERIMENTS", v: 34, cur: 0.03, eff: 0.1, base: 0.1, dev: -0.7, pr: 0.7443, pp: 15, px: 0.673326, avg: 0.386015},
  {n: "OriginTrail", t: "TRAC", c: "Krypto", g: "EXPERIMENTS", v: 0, cur: 0.0, eff: 0.1, base: 0.1, dev: -1, pr: null, pp: null, px: 1.46444, avg: null},
];

/** 31 daily valuations, today − 30 … today; the last one equals the live value. */
export const MAIN_SNAPSHOT_SPARK: readonly number[] = [136984, 136522, 135645, 134418, 132942, 131345, 129766, 128344, 127199, 126426, 126078, 126167, 126658, 127476, 128511, 129632, 130697, 131572, 132142, 132322, 132070, 131386, 130315, 128943, 127387, 125782, 124269, 122978, 122018, 121462, 121344];

export const MAIN_SNAPSHOT_LIVE_VALUE = 121_344;
/** Σ of the 34 row values; the snapshot's portfolio total. */
export const MAIN_SNAPSHOT_TOTAL_PLN = 121_340;

const ASSET_CLASS_OF: Record<SnapshotRow["c"], string> = { Krypto: "CRYPTO", Akcja: "STOCK", ETF: "ETF" };

/** One `PortfolioAssetLike` per mockup row, with a single primary subasset `ticker = t`. */
export function snapshotAssets(): PortfolioAssetLike<PortfolioSubassetLike>[] {
  return MAIN_SNAPSHOT_ROWS.map((r, i) => ({
    id: i + 1,
    key: r.t,
    name: r.n,
    assetClass: ASSET_CLASS_OF[r.c],
    exposure: r.g,
    targetAllocationPct: r.base,
    computed: {
      valuePln: r.v,
      currentPct: r.cur,
      effectiveTargetAllocationPct: r.eff,
      deviationPct: r.dev,
      profitPct: r.pr,
      pricePln: r.px,
      avgBuyPricePln: r.avg,
    },
    subassets: [{ id: 1000 + i, isPrimary: true, ticker: r.t, name: r.n, valuePln: r.v, pricePln: r.px, priceCurrency: "PLN" }],
  }));
}
