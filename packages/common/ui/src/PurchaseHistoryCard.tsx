import { formatPln } from "@common/app-core";
import { Card, CardContent, CardHeader, CardTitle } from "./card.js";

/**
 * portfolioB/portfolioC only — investing-app's purchase/trade concept lives under a different
 * name and file structure (routes/trade-history/) and is not part of this extraction.
 */
export interface PurchaseLotLike {
  ticker: string;
  name: string;
  units: number;
  costPln: number;
  currentValuePln: number | null;
  gainPct: number | null;
  priceAvailable: boolean;
}

export interface PurchaseSessionLike {
  purchasedAt: string;
  totalCostPln: number;
  totalCurrentValuePln: number;
  totalGainPct: number | null;
  lots: PurchaseLotLike[];
}

function formatDate(iso: string): string {
  // iso is an ISO date string (YYYY-MM-DD); render in pl-PL, fall back to raw on parse failure.
  const parsed = new Date(`${iso}T00:00:00`);
  if (Number.isNaN(parsed.getTime())) return iso;
  return parsed.toLocaleDateString("pl-PL", {
    year: "numeric",
    month: "long",
    day: "numeric",
  });
}

function formatPct(value: number): string {
  const sign = value > 0 ? "+" : "";
  return `${sign}${value.toFixed(1)}%`;
}

function gainColor(value: number): string {
  if (value > 0) return "text-green-600";
  if (value < 0) return "text-red-600";
  return "text-muted-foreground";
}

function LotRow({ lot }: { lot: PurchaseLotLike }) {
  return (
    <div className="flex items-baseline justify-between gap-3 py-1.5 text-sm">
      <div className="flex min-w-0 items-baseline gap-2">
        <span className="font-mono font-medium">{lot.ticker}</span>
        <span className="truncate text-xs text-muted-foreground">{lot.name}</span>
      </div>
      <div className="flex shrink-0 items-baseline gap-3 tabular-nums">
        <span className="text-muted-foreground">{lot.units.toFixed(2)} szt.</span>
        {lot.priceAvailable && lot.currentValuePln !== null && lot.gainPct !== null ? (
          <>
            <span>
              {formatPln(lot.costPln)} <span className="text-muted-foreground">→</span>{" "}
              {formatPln(lot.currentValuePln)}
            </span>
            <span className={`w-16 text-right font-medium ${gainColor(lot.gainPct)}`}>
              {formatPct(lot.gainPct)}
            </span>
          </>
        ) : (
          <>
            <span>
              {formatPln(lot.costPln)} <span className="text-muted-foreground">→ —</span>
            </span>
            <span className="w-16 text-right text-xs text-muted-foreground">brak ceny</span>
          </>
        )}
      </div>
    </div>
  );
}

export function PurchaseHistoryCard({ session }: { session: PurchaseSessionLike }) {
  const hasGain = session.totalGainPct !== null;
  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1 text-base">
          <span>Zakupy {formatDate(session.purchasedAt)}</span>
          <span className="flex items-baseline gap-2 text-sm font-normal tabular-nums">
            <span>
              {formatPln(session.totalCostPln)}{" "}
              <span className="text-muted-foreground">→</span>{" "}
              {formatPln(session.totalCurrentValuePln)}
            </span>
            {hasGain && session.totalGainPct !== null && (
              <span className={`font-semibold ${gainColor(session.totalGainPct)}`}>
                {formatPct(session.totalGainPct)}
              </span>
            )}
          </span>
        </CardTitle>
      </CardHeader>
      <CardContent className="divide-y pt-0">
        {session.lots.map((lot) => (
          <LotRow key={lot.ticker} lot={lot} />
        ))}
      </CardContent>
    </Card>
  );
}
