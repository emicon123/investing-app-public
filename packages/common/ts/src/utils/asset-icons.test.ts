import { describe, expect, it } from "vitest";
import { ASSET_ICONS, getAssetIconUrl, resolveAssetIcon } from "./asset-icons.js";

/** Every live primary/subasset ticker in the three databases on 2026-10-24 (design doc §9.2). */
const LIVE: Record<string, [ticker: string, assetClass: string][]> = {
  main: [
    ...["CIFR", "COIN", "CRCL", "HUT", "IREN", "MARA", "MSTR", "WULF"].map((t): [string, string] => [t, "STOCK"]),
    ...[
      "ALGO-USD", "BTC-USD", "DOGE-USD", "ETH-USD", "ETNVIRLINK.PL", "FET-USD", "FLIP-USD", "HBAR-USD",
      "LINK-USD", "LOFI-USDT", "ONDO-USD", "PL.ETNVIRBTCP", "PL.ETNVIRETH", "PL.ETNVIRSOL", "PL.ETNVIRXRP",
      "SOL-USD", "SUI-USDT", "TAO-USDT", "TRAC-USD", "TRX-USD", "VRTU.DE", "XPL-USD", "XRP-USD", "ZANO-USD",
      "ZEC-USD",
    ].map((t): [string, string] => [t, "CRYPTO"]),
    ...[
      "4GLD.DE", "ETCGLDRMAU.PL", "ETFPZUCASH.PL", "ICGA.DE", "IGLN.L", "ISLN.L", "QDV5.DE", "QUTM.DE",
      "SXRV.DE", "WQTM.DE", "XAD6", "ZPR1.DE",
    ].map((t): [string, string] => [t, "ETF"]),
  ],
  portfolioB: [
    ...[
      "BTC", "CBUV", "DPYE", "ETCGLDRMAU", "ETFBNDXPL", "ETFBTCPL", "ETH", "ETNVIRBTCP", "ETNVIRETH",
      "ETNVIRSOL", "ETNVIRXRP", "EUNL", "HTWO", "IOGP", "IS3N", "NQSE", "QUTM", "SOL", "SREBRO_FIZ", "SUI",
      "SXRV.DE", "URNU", "VRTU.DE", "VVMX", "XAD6", "XDWT", "XETH", "XMLD", "XRP", "ZLOTO_FIZ",
    ].map((t): [string, string] => [t, "ETF"]),
    ["ETNVIRLINK.PL", "CRYPTO"],
    ...["CIFR", "COIN", "HUT", "MARA"].map((t): [string, string] => [t, "STOCK"]),
    ["CHF", "CURRENCY"],
  ],
  portfolioC: [
    ...["CNDX.L", "ETNVIRBTCP", "ICGA.DE", "ICHN.AS", "QDV5.DE", "SXRV.DE"].map((t): [string, string] => [t, "ETF"]),
    ["4GLD.DE", "METALS"],
  ],
};

describe("resolveAssetIcon", () => {
  it.each(Object.entries(LIVE))("resolves every live %s ticker", (_app, tickers) => {
    const missing = tickers.filter(([t, c]) => resolveAssetIcon(t, c) === null);
    expect(missing).toEqual([]);
  });

  it.each(["FLIP-USD", "LOFI-USDT", "XPL-USD"])("%s uses a CoinGecko image (CoinCap 404s)", (ticker) => {
    expect(resolveAssetIcon(ticker, "CRYPTO")?.src).toMatch(/^https:\/\/coin-images\.coingecko\.com\//);
  });

  it("derives other coins from the CoinCap symbol", () => {
    expect(resolveAssetIcon("SUI-USDT", "CRYPTO")).toEqual({
      src: "https://assets.coincap.io/assets/icons/sui@2x.png",
      kind: "logo",
    });
  });

  it("looks a ticker up whatever its class (portfolioB's ETF-class Bitcoin)", () => {
    expect(resolveAssetIcon("ETNVIRBTCP", "ETF")?.src).toContain("/btc@2x.png");
  });

  it("uses bundled bars for gold, not the PAXG token", () => {
    const gold = resolveAssetIcon("ETCGLDRMAU.PL", "ETF");
    expect(gold?.kind).toBe("logo");
    expect(gold?.src).not.toContain("paxg");
    // Bundled under Vite's inline limit: a data URI carrying the bars' gold fill.
    expect(gold?.src.startsWith("data:image/svg+xml")).toBe(true);
    expect(decodeURIComponent(gold!.src).toLowerCase()).toContain("#c99a35");
  });

  it("draws favicons padded (kind favicon)", () => {
    expect(resolveAssetIcon("SXRV.DE", "ETF")?.kind).toBe("favicon");
    expect(resolveAssetIcon("COIN", "STOCK")?.src).toContain("domain=coinbase.com");
  });

  it("never guesses an unknown stock or ETF", () => {
    expect(resolveAssetIcon("NEWCO", "STOCK")).toBeNull();
    expect(resolveAssetIcon("NEWETF.DE", "ETF")).toBeNull();
    expect(getAssetIconUrl("NEWCO", "STOCK")).toBeNull();
  });

  it("keeps the compat wrapper in step with the resolver", () => {
    for (const ticker of Object.keys(ASSET_ICONS)) {
      expect(getAssetIconUrl(ticker, "ETF")).toBe(ASSET_ICONS[ticker]!.src);
    }
  });
});
