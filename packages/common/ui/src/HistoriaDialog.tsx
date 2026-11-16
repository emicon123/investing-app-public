import type { ReactNode } from "react";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "./dialog";
import { Skeleton } from "./skeleton";
import type { HistoriaEntryLike } from "./PortfolioValueLine";

/**
 * Result shape each app's `useHistoriaQuery` hook must return. `data` is
 * already unwrapped to the entry array (not the raw paginated envelope) so
 * this shell never needs to know about each app's `listHistoria` response
 * type.
 *
 * Generic over `T` (defaulting to the lean `HistoriaEntryLike`) because
 * investing-app's real `HistoriaEntry` carries an extra required
 * `portfolio` field that its own local `PortfolioValueLine` needs — the
 * shell only requires `T` to be at least `HistoriaEntryLike`-shaped.
 */
export interface HistoriaQueryResult<T extends HistoriaEntryLike = HistoriaEntryLike> {
  data: T[] | undefined;
  isPending: boolean;
  isError: boolean;
}

export interface HistoriaDialogProps<T extends HistoriaEntryLike = HistoriaEntryLike> {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /**
   * Called as `useHistoriaQuery(open)` on every render — a real hook, not a
   * one-shot result object — so each app can wire its own portfolio-scoped
   * (or unscoped) `useQuery` underneath, including the `enabled: open`
   * re-fetch-on-open behavior.
   */
  useHistoriaQuery: (open: boolean) => HistoriaQueryResult<T>;
  /**
   * Renders the chart for the populated state. Injected rather than
   * hardcoded because investing-app uses its own richer local
   * `PortfolioValueLine` (regression bands, macro overlays) while
   * portfolioB/portfolioC use the shared one from `./PortfolioValueLine`.
   */
  renderChart: (entries: T[]) => ReactNode;
  /** Appended as ` — {titleSuffix}` when present (investing-app's per-portfolio title). */
  titleSuffix?: string;
}

export function HistoriaDialog<T extends HistoriaEntryLike = HistoriaEntryLike>({
  open,
  onOpenChange,
  useHistoriaQuery,
  renderChart,
  titleSuffix,
}: HistoriaDialogProps<T>) {
  const { data, isPending, isError } = useHistoriaQuery(open);
  const entries = data ?? [];

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-[95vw] h-[92vh] flex flex-col">
        <DialogHeader>
          <DialogTitle>
            Historia portfela{titleSuffix ? ` — ${titleSuffix}` : ""}
          </DialogTitle>
        </DialogHeader>
        {isPending && (
          <div role="status" aria-label="Ładowanie historii portfela">
            <Skeleton className="h-80 w-full" />
          </div>
        )}
        {isError && (
          <p role="alert" className="text-destructive text-sm">
            Błąd ładowania danych historycznych.
          </p>
        )}
        {!isPending && !isError && (
          <div className="flex-1 min-h-0">
            {entries.length > 0 ? (
              renderChart(entries)
            ) : (
              <p className="text-muted-foreground text-sm">Brak danych historycznych.</p>
            )}
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}
