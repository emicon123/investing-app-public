/// <reference types="vite/client" />
// ↑ types the `*.svg` imports below for every program that compiles this file (the apps, common/ui).
import goldSvg from "./asset-icons/gold.svg";
import silverSvg from "./asset-icons/silver.svg";
import flagCnSvg from "./asset-icons/flag-cn.svg";
import flagInSvg from "./asset-icons/flag-in.svg";
import flagUsSvg from "./asset-icons/flag-us.svg";
import flagPlSvg from "./asset-icons/flag-pl.svg";
import flagChSvg from "./asset-icons/flag-ch.svg";
import quantumSvg from "./asset-icons/quantum.svg";

/**
 * One asset-icon map for all three apps (task 170, design doc §9), keyed by SUBASSET ticker; the
 * caller passes the asset's primary ticker. It replaces main's `STOCK_ICONS`/`ETF_ICONS`, the
 * portfolioB/portfolioC map and the `createAssetIconResolver` factory.
 *
 * Owner rule "only reliable icons, no name-guessing": nothing is derived from an asset NAME or a
 * guessed domain. Every non-crypto icon is an explicit entry. Crypto keeps the CoinCap derivation
 * from the exchange symbol (live in all three apps, every live coin checked), with explicit
 * CoinGecko entries where CoinCap 404s. A miss or a broken image renders a monogram (`AssetIcon`).
 *
 * The bundled SVGs are under Vite's inline limit, so they ship as data URIs. They must be used as
 * `<img src>`, never inlined into the DOM: several reuse `clipPath id="c"`.
 */

/** `logo`: drawn as is. `favicon`: a site favicon, drawn padded on a white circle. */
export type AssetIconKind = "logo" | "favicon";

export interface AssetIconSpec {
  src: string;
  kind: AssetIconKind;
}

/** Google's favicon service: free, no API key, returns the site's real icon. */
export function gfav(domain: string, size = 64): string {
  return `https://www.google.com/s2/favicons?domain=${domain}&sz=${size}`;
}

const coincap = (symbol: string): string => `https://assets.coincap.io/assets/icons/${symbol}@2x.png`;
const logo = (src: string): AssetIconSpec => ({ src, kind: "logo" });
const favicon = (domain: string, size?: number): AssetIconSpec => ({ src: gfav(domain, size), kind: "favicon" });

function entries(spec: AssetIconSpec, tickers: readonly string[]): [string, AssetIconSpec][] {
  return tickers.map((ticker) => [ticker, spec]);
}

export const ASSET_ICONS: Readonly<Record<string, AssetIconSpec>> = Object.fromEntries([
  // CoinGecko: CoinCap answers 404 for these symbols.
  ["FLIP-USD", logo("https://coin-images.coingecko.com/coins/images/25576/small/kdt_AgmT_400x400.png?1696524709")],
  ["LOFI-USDT", logo("https://coin-images.coingecko.com/coins/images/52058/small/LOFI.png?1732454544")],
  ["XPL-USD", logo("https://coin-images.coingecko.com/coins/images/66489/small/Plasma-symbol-green-1.png?1755142558")],

  // Crypto ETN/ETF wrappers and bare coin subassets: the coin's own logo.
  ...entries(logo(coincap("btc")), ["ETNVIRBTCP", "PL.ETNVIRBTCP", "ETNVIRBTCP.WA", "ETFBTCPL", "IB1T", "BTC"]),
  ...entries(logo(coincap("eth")), ["ETNVIRETH", "PL.ETNVIRETH", "ETNVIRETH.WA", "XETH", "ETH"]),
  ...entries(logo(coincap("sol")), ["ETNVIRSOL", "PL.ETNVIRSOL", "SOL"]),
  ...entries(logo(coincap("xrp")), ["ETNVIRXRP", "PL.ETNVIRXRP", "XRP"]),
  ...entries(logo(coincap("link")), ["ETNVIRLINK.PL"]),
  ...entries(logo(coincap("sui")), ["VRTU.DE", "SUI"]),

  // Metals: bars, not the PAXG token logo.
  ...entries(logo(goldSvg), ["ETCGLDRMAU", "ETCGLDRMAU.PL", "ETCGLDRMAU.WA", "4GLD.DE", "IGLN.L", "ZLOTO_FIZ"]),
  ...entries(logo(silverSvg), ["XAD6", "XAD6.DE", "ISLN.L", "SREBRO_FIZ"]),

  // Country flags: China / India equity, USD / PLN bond ETFs, the Swiss franc.
  ...entries(logo(flagCnSvg), ["ICGA.DE", "ICHN.AS"]),
  ...entries(logo(flagInSvg), ["QDV5.DE"]),
  ...entries(logo(flagUsSvg), ["ZPR1.DE"]),
  ...entries(logo(flagPlSvg), ["ETFPZUCASH.PL"]),
  ...entries(logo(flagChSvg), ["CHF"]),

  // Quantum-computing ETFs: an atom.
  ...entries(logo(quantumSvg), ["WQTM.DE", "QUTM.DE", "QUTM"]),

  // Nasdaq-100 trackers: the index's own favicon.
  ...entries(favicon("nasdaq.com", 48), ["SXRV.DE", "CNDX.L", "ETFBNDXPL", "NQSE"]),

  // Companies.
  ["COIN", favicon("coinbase.com")],
  ["MSTR", favicon("strategy.com")],
  ["MARA", favicon("mara.com")],
  ["CIFR", favicon("ciphermining.com")],
  ["HUT", favicon("hut8.com")],
  ["CRCL", favicon("circle.com")],
  ["IREN", favicon("iren.com")],
  ["WULF", favicon("terawulf.com")],
  ["FISV", favicon("fiserv.com")],
  ["GME", favicon("gamestop.com")],

  // Issuers of portfolioB's thematic ETFs.
  ...entries(favicon("ishares.com", 32), ["CBUV", "DPYE", "EUNL", "IS3N", "IOGP"]),
  ["XDWT", favicon("xtrackers.com")],
  ...entries(favicon("landg.com", 32), ["XMLD", "HTWO"]),
  ["URNU", favicon("globalxetfs.com", 64)],
  ["VVMX", favicon("vaneck.com")],
]);

/** The CoinCap symbol of an exchange ticker: "SUI-USDT" → "sui", "BTC-USD" → "btc". */
function coincapSymbol(ticker: string): string {
  return ticker.replace(/-USDT?$/, "").replace(/\d+$/, "").toLowerCase();
}

/** `ASSET_ICONS[ticker]` whatever the class; else the CoinCap logo for CRYPTO; else null. */
export function resolveAssetIcon(ticker: string, assetClass: string): AssetIconSpec | null {
  const mapped = ASSET_ICONS[ticker];
  if (mapped !== undefined) return mapped;
  if (assetClass === "CRYPTO") return logo(coincap(coincapSymbol(ticker)));
  return null;
}

/** Compatibility wrapper for the 20 px icons (main's MacroRegimePanel, AssetActionDialogs). */
export function getAssetIconUrl(ticker: string, assetClass: string): string | null {
  return resolveAssetIcon(ticker, assetClass)?.src ?? null;
}
