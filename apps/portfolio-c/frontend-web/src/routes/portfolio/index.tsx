import type { ReactNode } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { formatAssetUnits } from "@common/app-core";
import {
  listPositions,
  deletePosition,
  deleteAsset,
  setPrimary,
  updateAsset,
  type Subasset,
} from "../../api/index.js";
import { EditPositionDialog } from "@/components/EditPositionDialog";
import { CreatePositionDialog } from "@/components/CreatePositionDialog";
import { PositionTransferDialog } from "@/components/PositionTransferDialog";
import { AllocationTreemap } from "@/components/charts/AllocationTreemap";
import { HistoriaDialog } from "@/components/charts/HistoriaDialog";
import { PortfolioPage as SharedPortfolioPage } from "@common/ui-primitives/PortfolioPage";
import { usePortfolioKpis } from "@/hooks/usePortfolioKpis";
import { queryKeys } from "@/lib/queryKeys";
import { usePortfolio } from "@/lib/portfolio";
import { PORTFOLIO_LABELS } from "@/lib/formConstants";

/** portfolioC has no account dimension: a sub-row shows the units it holds. */
function renderAccountCell(sub: Subasset, assetClass: string): ReactNode {
  return (
    <span className="font-mono text-xs text-text-soft">{formatAssetUnits(sub.effectiveUnits, assetClass)} szt.</span>
  );
}

export default function PortfolioPage() {
  const { portfolio } = usePortfolio();
  const queryClient = useQueryClient();

  function usePositionsQuery() {
    const { data, isPending, isError } = useQuery({
      queryKey: queryKeys.positions.list(portfolio),
      queryFn: () => listPositions({ portfolio, size: 100 }),
      staleTime: 60_000,
      refetchInterval: 60_000,
    });

    return { data: data?.data, isPending, isError };
  }

  function useKpis() {
    return usePortfolioKpis(portfolio);
  }

  return (
    <SharedPortfolioPage
      title="Portfolio"
      bookTag={PORTFOLIO_LABELS[portfolio]}
      api={{ deletePosition, deleteAsset, setPrimary }}
      usePositionsQuery={usePositionsQuery}
      invalidatePositions={() => {
        void queryClient.invalidateQueries({ queryKey: queryKeys.positions.all });
      }}
      useKpis={useKpis}
      updateAsset={updateAsset}
      renderAccountCell={renderAccountCell}
      EditDialog={EditPositionDialog}
      CreateDialog={CreatePositionDialog}
      TransferDialog={PositionTransferDialog}
      Treemap={AllocationTreemap}
      HistoryDialog={HistoriaDialog}
    />
  );
}
