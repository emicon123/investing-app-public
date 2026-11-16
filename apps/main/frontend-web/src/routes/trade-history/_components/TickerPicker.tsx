import { useMemo, useState } from "react";
import { Search } from "lucide-react";
import type { TradeChartTickerEntry } from "../../../api/index.js";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { cn } from "@/lib/utils";

interface TickerPickerProps {
  tickers: TradeChartTickerEntry[];
  value: string | undefined;
  onChange: (ticker: string) => void;
  disabled?: boolean;
  className?: string;
}

/**
 * Ticker picker for the trade-history chart. Every asset ever traded is listed as its own
 * radio button (grid, scrollable) rather than hidden behind a dropdown/combobox trigger —
 * with dozens of tickers, a collapsed picker made scanning/re-picking an asset an extra click
 * plus a cramped scroll; a search box narrows the grid for the rare case someone knows the
 * exact ticker they want. Sorted by trade count (most-traded first) so the assets someone
 * actually trades often surface without typing.
 */
export function TickerPicker({ tickers, value, onChange, disabled, className }: TickerPickerProps) {
  const [search, setSearch] = useState("");

  const sorted = useMemo(
    () => [...tickers].sort((a, b) => b.tradeCount - a.tradeCount),
    [tickers]
  );

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    if (!q) return sorted;
    return sorted.filter(
      (t) => t.ticker.toLowerCase().includes(q) || t.name.toLowerCase().includes(q)
    );
  }, [sorted, search]);

  return (
    <div className={cn("space-y-2", className)}>
      <div className="relative max-w-sm">
        <Search
          className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground"
          aria-hidden="true"
        />
        <Input
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder="Szukaj tickera lub nazwy…"
          className="h-8 pl-8 text-sm"
          aria-label="Szukaj aktywa"
          disabled={disabled}
        />
      </div>

      <RadioGroup
        value={value}
        onValueChange={onChange}
        disabled={disabled}
        className="grid max-h-64 grid-cols-1 gap-1 overflow-y-auto rounded-md border p-1.5 sm:grid-cols-2 lg:grid-cols-3"
      >
        {filtered.length === 0 && (
          <p className="col-span-full px-2 py-4 text-center text-sm text-muted-foreground">
            Brak wyników.
          </p>
        )}
        {filtered.map((t) => {
          const id = `ticker-${t.ticker}`;
          return (
            <Label
              key={t.ticker}
              htmlFor={id}
              className={cn(
                "flex cursor-pointer items-center justify-between gap-2 rounded-sm px-2 py-1.5 text-sm font-normal transition-colors hover:bg-accent/60",
                t.ticker === value && "bg-accent/40 font-semibold"
              )}
            >
              <span className="flex min-w-0 items-center gap-2">
                <RadioGroupItem value={t.ticker} id={id} className="shrink-0" />
                <span className="flex min-w-0 flex-col">
                  <span className="truncate">{t.name}</span>
                  <span className="font-mono text-xs text-muted-foreground">{t.ticker}</span>
                </span>
              </span>
              <span className="shrink-0 text-xs tabular-nums text-muted-foreground">{t.tradeCount}×</span>
            </Label>
          );
        })}
      </RadioGroup>
    </div>
  );
}
