import { formatNum, pluralPozycje } from "@common/ui-primitives/portfolio/model/portfolioFormat";
import type { BotTrade } from "../../api/index.js";

/** The bot badge's text (task 170, design doc §14 D1). */

function signedPln(x: number): string {
  return `${x > 0 ? "+" : ""}${formatNum(x, 2)} zł`;
}

/** "brak otwartych pozycji" · "LONG ×3 · +12,40 zł" · "2 otwarte pozycje · −4,10 zł". */
export function openPositionsSummary(trades: readonly Pick<BotTrade, "side" | "leverage" | "unrealizedPnlPln">[]): string {
  const [only] = trades;
  if (only === undefined) return "brak otwartych pozycji";
  if (trades.length === 1) {
    const pnl = only.unrealizedPnlPln;
    return `${only.side} ×${only.leverage}${pnl != null ? ` · ${signedPln(pnl)}` : ""}`;
  }
  const noun = pluralPozycje(trades.length);
  const adjective = noun === "pozycje" ? "otwarte" : "otwartych";
  const pnls = trades.map((t) => t.unrealizedPnlPln).filter((p): p is number => p != null);
  const total = pnls.length > 0 ? ` · ${signedPln(pnls.reduce((a, b) => a + b, 0))}` : "";
  return `${trades.length} ${adjective} ${noun}${total}`;
}
