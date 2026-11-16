import { fetchAllPages } from "@common/app-core";
import { useQuery } from "@tanstack/react-query";
import { listHistoria, listPositions } from "../../api/index.js";
import { PortfolioValueLine } from "@/components/charts/PortfolioValueLine";
import { PageHeader } from "@common/ui-primitives/PageHeader";
import { HistoryPage as SharedHistoryPage } from "@common/ui-primitives/HistoryPage";
import { queryKeys } from "@/lib/queryKeys";

function useHistoryQuery() {
  const { data, isPending, isError } = useQuery({
    queryKey: queryKeys.history.list(),
    queryFn: () => fetchAllPages((page, size) => listHistoria({ page, size })),
    staleTime: 5 * 60_000,
  });

  return { data: data?.data, isPending, isError };
}

export default function HistoriaPage() {
  const { data: positionsData } = useQuery({
    queryKey: queryKeys.positions.list(),
    queryFn: () => listPositions({ size: 1000 }),
    staleTime: 60_000,
  });

  const positions = (positionsData as unknown as { data?: unknown[] })?.data as never[] | undefined;

  return (
    <SharedHistoryPage
      header={<PageHeader title="Historia portfela" />}
      useHistoryQuery={useHistoryQuery}
      positions={positions}
      renderChart={(entries) => <PortfolioValueLine data={entries} positions={positions as never} />}
    />
  );
}
