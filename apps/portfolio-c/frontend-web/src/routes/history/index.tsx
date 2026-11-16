import { fetchAllPages } from "@common/app-core";
import { useQuery } from "@tanstack/react-query";
import { listHistoria, listPositions } from "../../api/index.js";
import { PortfolioValueLine } from "@/components/charts/PortfolioValueLine";
import { PageHeader } from "@common/ui-primitives/PageHeader";
import { HistoryPage as SharedHistoryPage } from "@common/ui-primitives/HistoryPage";
import { queryKeys } from "@/lib/queryKeys";
import { usePortfolio } from "@/lib/portfolio";
import { PORTFOLIO_LABELS } from "@/lib/formConstants";

export default function HistoriaPage() {
  const { portfolio } = usePortfolio();

  function useHistoryQuery() {
    const { data, isPending, isError } = useQuery({
      queryKey: queryKeys.history.list(portfolio),
      queryFn: () => fetchAllPages((page, size) => listHistoria({ portfolio, page, size })),
      staleTime: 5 * 60_000,
    });

    return { data: data?.data, isPending, isError };
  }

  const { data: positionsData } = useQuery({
    queryKey: queryKeys.positions.list(portfolio),
    queryFn: () => listPositions({ portfolio, size: 1000 }),
    staleTime: 60_000,
  });

  const positions = (positionsData as unknown as { data?: unknown[] })?.data as never[] | undefined;

  return (
    <SharedHistoryPage
      header={<PageHeader title={`Historia portfela — ${PORTFOLIO_LABELS[portfolio]}`} />}
      useHistoryQuery={useHistoryQuery}
      positions={positions}
      renderChart={(entries) => <PortfolioValueLine data={entries} positions={positions as never} />}
    />
  );
}
