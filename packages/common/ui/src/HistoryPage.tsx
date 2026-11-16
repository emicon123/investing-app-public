import type { ReactNode } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "./card.js";
import { Skeleton } from "./skeleton.js";
import { PageShell } from "./PageShell.js";

/**
 * 3-way shared history page shell (task 156, Phase B2 — HistoriaDialog's
 * "shell + injected query hook + render fn" precedent, plus a header slot
 * for main's richer header). Per-app key + fetcher + `staleTime` stay in the
 * adapter, so the "same keys" constraint holds trivially. Generic over
 * `<TEntry, TPosition>` (main's entry type carries `portfolio:
 * "MAIN" | "RETIREMENT"`).
 */
export interface HistoryPageProps<TEntry, TPosition> {
  /** main passes its local PageHeader (eyebrow); portfolioB/portfolioC pass the shared one. */
  header: ReactNode;
  useHistoryQuery: () => {
    data: TEntry[] | undefined;
    isPending: boolean;
    isError: boolean;
  };
  /** portfolioB/portfolioC adapters run their own positions query and pass data down; main passes `undefined`. */
  positions: TPosition[] | undefined;
  /** main renders its local chart; portfolioB/portfolioC render the shared `PortfolioValueLine` shim. */
  renderChart: (entries: TEntry[], positions: TPosition[] | undefined) => ReactNode;
}

export function HistoryPage<TEntry, TPosition>({
  header,
  useHistoryQuery,
  positions,
  renderChart,
}: HistoryPageProps<TEntry, TPosition>) {
  const { data, isPending, isError } = useHistoryQuery();

  const entries = data ?? [];

  return (
    <div className="p-6 space-y-6">
      {header}

      <PageShell
        isPending={isPending}
        isError={isError}
        loadingLabel="Ładowanie historii portfela"
        loadingSkeleton={<Skeleton className="h-96 w-full" />}
      >
        {entries.length > 0 ? (
          <Card>
            <CardHeader>
              <CardTitle className="text-base">Wartość portfela w czasie</CardTitle>
            </CardHeader>
            <CardContent>
              {renderChart(entries, positions)}
            </CardContent>
          </Card>
        ) : (
          <p className="text-muted-foreground text-sm">Brak danych historycznych.</p>
        )}
      </PageShell>
    </div>
  );
}
