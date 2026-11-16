import { useId } from "react";
import { SearchIcon } from "lucide-react";
import { cn } from "../lib/utils.js";
import type { FilterChip, PositionsFilter } from "./model/positionsModel.js";
import { FOCUS_RING } from "./tableLayout.js";

/**
 * Filter chips (Wszystkie / one per exposure group / Sprzedaj) and the search box. The selected
 * chip uses the app's accent. Search matches the asset's name and key and every subasset's ticker
 * and name ("IGLN" finds Gold).
 */

export interface PositionsFilterBarProps {
  chips: readonly FilterChip[];
  filter: PositionsFilter;
  onFilter: (filter: PositionsFilter) => void;
  query: string;
  onQuery: (query: string) => void;
}

export function PositionsFilterBar({ chips, filter, onFilter, query, onQuery }: PositionsFilterBarProps) {
  const searchId = useId();
  return (
    <div className="flex flex-wrap items-center justify-between gap-3">
      <div role="group" aria-label="Filtruj pozycje" className="flex flex-wrap gap-1.5">
        {chips.map((chip) => {
          const pressed = filter === chip.id;
          return (
            <button
              key={chip.id}
              type="button"
              aria-pressed={pressed}
              onClick={() => onFilter(chip.id)}
              className={cn(
                "inline-flex min-h-9 items-center gap-2 rounded-lg border px-3 text-[13px] font-medium transition-colors motion-reduce:transition-none pointer-coarse:min-h-11",
                FOCUS_RING,
                pressed
                  ? "border-primary bg-primary text-primary-foreground"
                  : "border-hairline-strong text-text-soft hover:border-white/25",
              )}
            >
              {chip.color && (
                <span
                  aria-hidden="true"
                  className={cn("size-2", chip.round ? "rounded-full" : "rounded-[2px]")}
                  // Group colours are literal data (EXPOSURE_GROUPS), not tokens.
                  style={{ background: chip.color }}
                />
              )}
              {chip.label}{" "}
              {/* The space keeps the accessible name "Krypto 13"; whitespace between flex items is not rendered. */}
              <span className={cn("font-normal", pressed ? "text-primary-foreground/70" : "text-text-caption")}>
                {chip.count}
              </span>
            </button>
          );
        })}
      </div>
      <div className="relative min-w-[220px] flex-[0_1_300px]">
        <label htmlFor={searchId} className="sr-only">
          Szukaj aktywa po nazwie lub tickerze
        </label>
        <SearchIcon
          aria-hidden="true"
          className="pointer-events-none absolute top-1/2 left-[11px] size-4 -translate-y-1/2 text-text-caption"
        />
        <input
          id={searchId}
          type="search"
          value={query}
          onChange={(e) => onQuery(e.target.value)}
          placeholder="Szukaj: nazwa lub ticker"
          className={cn(
            "min-h-9 w-full rounded-lg border border-hairline-strong bg-surface-input pr-3 pl-[34px] text-[13px] text-text-1 placeholder:text-text-caption pointer-coarse:min-h-11",
            FOCUS_RING,
          )}
        />
      </div>
    </div>
  );
}
