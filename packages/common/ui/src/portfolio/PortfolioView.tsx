import { PageShell } from "../PageShell.js";
import { Skeleton } from "../skeleton.js";
import { AllocationRibbon } from "./AllocationRibbon.js";
import { GroupedPositionsTable } from "./GroupedPositionsTable.js";
import { KpiBento } from "./KpiBento.js";
import { PortfolioHeader } from "./PortfolioHeader.js";
import { PositionsFilterBar } from "./PositionsFilterBar.js";
import { usePositionsView } from "./usePositionsView.js";
import type { PortfolioAssetLike, PortfolioSubassetLike, PortfolioViewProps } from "./types.js";

/**
 * The portfolio page of all three apps (task 170, ADR-106): header, KPI bento, allocation ribbon,
 * filters + search and the positions table grouped by exposure, centred at 1400 px. Presentational:
 * each app's container (main `MainPortfolioScreen`, common `PortfolioPage` for portfolioB/portfolioC) owns
 * the data, dialogs and edit flows and passes them in; app-specific content enters only through
 * slots (`headerStatus`, `children`, `table.renderAccountCell`, the callbacks).
 */

function PositionsSkeleton() {
  return (
    <div className="flex flex-col gap-3">
      <Skeleton className="h-14 w-full rounded-[14px]" />
      {Array.from({ length: 6 }, (_, i) => (
        <Skeleton key={i} className="h-12 w-full" />
      ))}
    </div>
  );
}

function EmptyBook() {
  return (
    <p className="rounded-[14px] border border-hairline bg-surface-table px-5 py-12 text-center text-text-2">
      Brak pozycji. Dodaj pierwszą pozycję.
    </p>
  );
}

export function PortfolioView<TAsset extends PortfolioAssetLike<TSub>, TSub extends PortfolioSubassetLike>({
  title,
  bookTag,
  headerStatus,
  positions,
  kpis,
  actions,
  table,
  children,
}: PortfolioViewProps<TAsset, TSub>) {
  const view = usePositionsView(positions.data, { defaultCollapsedExposures: table.defaultCollapsedExposures });
  const tableLabel = bookTag
    ? `Pozycje portfela ${bookTag} pogrupowane według ekspozycji`
    : "Pozycje portfela pogrupowane według ekspozycji";

  return (
    <div className="mx-auto flex w-full max-w-[1400px] flex-col gap-[22px] px-[clamp(16px,3vw,32px)] pt-7 pb-[72px]">
      <PortfolioHeader
        title={title}
        bookTag={bookTag}
        pricesAsOf={kpis.pricesAsOf}
        status={headerStatus}
        actions={actions}
      />

      <KpiBento kpis={kpis} />

      <PageShell
        isPending={positions.isPending}
        isError={positions.isError}
        loadingLabel="Ładowanie danych"
        loadingSkeleton={<PositionsSkeleton />}
        errorMessage="Błąd ładowania pozycji. Spróbuj ponownie."
      >
        {positions.data.length === 0 ? (
          <EmptyBook />
        ) : (
          <>
            <AllocationRibbon segments={view.model.ribbon} hasEffectiveOverride={view.model.hasEffectiveOverride} />
            <PositionsFilterBar
              chips={view.chips}
              filter={view.filter}
              onFilter={view.setFilter}
              query={view.query}
              onQuery={view.setQuery}
            />
            <GroupedPositionsTable view={view} table={table} ariaLabel={tableLabel} />
          </>
        )}
      </PageShell>

      {children}
    </div>
  );
}
