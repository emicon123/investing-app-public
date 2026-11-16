import { fetchAllPages } from "@common/app-core";
import { useQuery } from "@tanstack/react-query";
import { listHistoria } from "../../api/index.js";
import type { HistoriaEntry, PortfolioType } from "../../api/index.js";
import {
  HistoriaDialog as SharedHistoriaDialog,
  type HistoriaQueryResult,
} from "@common/ui-primitives/HistoriaDialog";
import { PortfolioValueLine } from "./PortfolioValueLine";
import { queryKeys } from "@/lib/queryKeys";

type Props = {
  portfolio: PortfolioType;
  open: boolean;
  onOpenChange: (v: boolean) => void;
};

export function HistoriaDialog({ portfolio, open, onOpenChange }: Props) {
  function useHistoriaQuery(open: boolean): HistoriaQueryResult<HistoriaEntry> {
    const { data, isPending, isError } = useQuery({
      queryKey: queryKeys.history.portfolio(portfolio),
      queryFn: () => fetchAllPages((page, size) => listHistoria({ portfolio, page, size })),
      enabled: open,
      staleTime: 5 * 60_000,
    });

    return { data: data?.data, isPending, isError };
  }

  return (
    <SharedHistoriaDialog
      open={open}
      onOpenChange={onOpenChange}
      useHistoriaQuery={useHistoriaQuery}
      titleSuffix={portfolio}
      renderChart={(entries) => <PortfolioValueLine data={entries} height="100%" />}
    />
  );
}
