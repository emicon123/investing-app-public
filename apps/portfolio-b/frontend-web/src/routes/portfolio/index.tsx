import type { ReactNode } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
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
import { AccountChips } from "@common/ui-primitives/portfolio/AccountChips";
import { usePortfolioKpis } from "@/hooks/usePortfolioKpis";
import { queryKeys } from "@/lib/queryKeys";

function usePositionsQuery() {
  const { data, isPending, isError } = useQuery({
    queryKey: queryKeys.positions.list(),
    queryFn: () => listPositions({ size: 100 }),
    staleTime: 60_000,
    refetchInterval: 60_000,
  });

  return { data: data?.data, isPending, isError };
}

function useKpis() {
  return usePortfolioKpis();
}

/** portfolioB's 5-way account split (IKE/IKZE for each parent + Inne) under an expanded asset. */
function renderAccountCell(sub: Subasset, assetClass: string): ReactNode {
  return (
    <AccountChips
      accounts={[
        { label: "IKE tata", value: sub.unitsIkeTata },
        { label: "IKE mama", value: sub.unitsIkeMama },
        { label: "IKZE tata", value: sub.unitsIkzeTata },
        { label: "IKZE mama", value: sub.unitsIkzeMama },
        { label: "Inne", value: sub.unitsInne },
      ]}
      assetClass={assetClass}
    />
  );
}

export default function PortfolioPage() {
  const queryClient = useQueryClient();

  return (
    <SharedPortfolioPage
      title="Portfolio"
      api={{ deletePosition, deleteAsset, setPrimary }}
      usePositionsQuery={usePositionsQuery}
      invalidatePositions={() => {
        void queryClient.invalidateQueries({ queryKey: queryKeys.positions.list() });
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
