import { fetchAllPages } from "@common/app-core";
import { useQuery } from "@tanstack/react-query";
import { listHistoria, listPositions } from "../../api/index.js";
import {
  HistoriaDialog as SharedHistoriaDialog,
  type HistoriaQueryResult,
} from "@common/ui-primitives/HistoriaDialog";
import { PortfolioValueLine } from "./PortfolioValueLine";
import { queryKeys } from "@/lib/queryKeys";
import { usePortfolio } from "@/lib/portfolio";

type Props = {
  open: boolean;
  onOpenChange: (v: boolean) => void;
};

function useHistoriaQuery(open: boolean): HistoriaQueryResult {
  const { portfolio } = usePortfolio();
  const { data, isPending, isError } = useQuery({
    queryKey: queryKeys.history.list(portfolio),
    queryFn: () => fetchAllPages((page, size) => listHistoria({ portfolio, page, size })),
    enabled: open,
    staleTime: 5 * 60_000,
  });

  return { data: data?.data, isPending, isError };
}

export function HistoriaDialog({ open, onOpenChange }: Props) {
  const { portfolio } = usePortfolio();
  const { data: positionsData } = useQuery({
    queryKey: queryKeys.positions.list(portfolio),
    queryFn: () => listPositions({ portfolio, size: 1000 }),
    staleTime: 60_000,
    enabled: open,
  });
  const positions = (positionsData as unknown as { data?: unknown[] })?.data as never[] | undefined;

  return (
    <SharedHistoriaDialog
      open={open}
      onOpenChange={onOpenChange}
      useHistoriaQuery={useHistoriaQuery}
      renderChart={(entries) => <PortfolioValueLine data={entries} height="100%" positions={positions as never} />}
    />
  );
}
