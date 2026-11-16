import { fetchAllPages } from "@common/app-core";
import { useQuery } from "@tanstack/react-query";
import { listHistoria } from "../../api/index.js";
import { PortfolioValueLine } from "@/components/charts/PortfolioValueLine";
import { PageHeader } from "@/components/PageHeader";
import { HistoryPage as SharedHistoryPage } from "@common/ui-primitives/HistoryPage";
import { queryKeys } from "@/lib/queryKeys";

function useHistoryQuery() {
  const { data, isPending, isError } = useQuery({
    queryKey: queryKeys.history.portfolio("MAIN"),
    queryFn: () => fetchAllPages((page, size) => listHistoria({ portfolio: "MAIN", page, size })),
    staleTime: 5 * 60_000,
  });

  return { data: data?.data, isPending, isError };
}

export default function HistoriaPage() {
  return (
    <SharedHistoryPage
      header={<PageHeader eyebrow="Portfel · Main" title="Historia portfela" />}
      useHistoryQuery={useHistoryQuery}
      positions={undefined}
      renderChart={(entries) => <PortfolioValueLine data={entries} />}
    />
  );
}
