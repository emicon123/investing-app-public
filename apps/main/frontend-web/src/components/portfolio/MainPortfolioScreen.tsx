import { useState, type ReactNode } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { PortfolioView } from "@common/ui-primitives/portfolio/PortfolioView";
import { AccountChips, type AccountUnits } from "@common/ui-primitives/portfolio/AccountChips";
import { listPositions, updateAsset, type Asset, type PortfolioType, type Subasset } from "../../api/index.js";
import { AllocationTreemap } from "@/components/charts/AllocationTreemap";
import { HistoriaDialog } from "@/components/charts/HistoriaDialog";
import { MacroRegimePanel } from "@/components/portfolio/MacroRegimePanel";
import { BotStatusBadge } from "@/components/portfolio/BotStatusBadge";
import { RebalancingTransferDialog } from "@/components/RebalancingTransferDialog";
import { EditPositionDialog } from "@/components/EditPositionDialog";
import { CreatePositionDialog } from "@/components/CreatePositionDialog";
import { DeleteAssetDialog, SubassetPickerDialog } from "@/components/AssetActionDialogs";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { useAssetActions } from "@/hooks/useAssetActions";
import { usePortfolioKpis } from "@/hooks/usePortfolioKpis";
import { queryKeys } from "@/lib/queryKeys";

/**
 * main's portfolio container (task 170, design doc §2.2) for both books: `/portfolio` (MAIN) and
 * `/retirement` (RETIREMENT), which used to be two near-identical 160-line route bodies. It owns
 * the positions query, the dialogs and the edit flows (`useAssetActions`, unchanged) and renders
 * the shared `PortfolioView`. MAIN adds the bot badge, the macro-regime panel below the table and
 * starts EXPERIMENTS collapsed. Selling stays manual: SELL is clickable only in edit mode and opens
 * the existing transfer dialog (ADR-045 / ADR-048).
 */

const BOOK_TAG: Record<PortfolioType, string> = { MAIN: "Main", RETIREMENT: "Emerytura" };
const MAIN_COLLAPSED_EXPOSURES: readonly string[] = ["EXPERIMENTS"];
const NO_COLLAPSED_EXPOSURES: readonly string[] = [];

/** Units per account of one subasset; crypto folds every regular bucket into one "BINANCE" chip. */
function accountUnitsOf(sub: Subasset, assetClass: string): AccountUnits[] {
  if (assetClass === "CRYPTO") {
    return [
      { label: "IKE", value: sub.unitsIke },
      { label: "IKZE", value: sub.unitsIkze },
      { label: "BINANCE", value: sub.unitsRegularEur + sub.unitsRegularUsd + sub.unitsRegularPln },
    ];
  }
  return [
    { label: "IKE", value: sub.unitsIke },
    { label: "IKZE", value: sub.unitsIkze },
    { label: "REG PLN", value: sub.unitsRegularPln },
    { label: "REG EUR", value: sub.unitsRegularEur },
    { label: "REG USD", value: sub.unitsRegularUsd },
  ];
}

function renderAccountCell(sub: Subasset, assetClass: string): ReactNode {
  return <AccountChips accounts={accountUnitsOf(sub, assetClass)} assetClass={assetClass} />;
}

export function MainPortfolioScreen({ portfolio }: { portfolio: PortfolioType }) {
  const queryClient = useQueryClient();
  const [treemapOpen, setTreemapOpen] = useState(false);
  const [historiaOpen, setHistoriaOpen] = useState(false);
  const [editMode, setEditMode] = useState(false);
  const [addOpen, setAddOpen] = useState(false);
  const [transferAsset, setTransferAsset] = useState<Asset | null>(null);

  const { data, isPending, isError } = useQuery({
    queryKey: queryKeys.positions.list(portfolio),
    queryFn: () => listPositions({ portfolio, size: 100 }),
    staleTime: 60_000,
    refetchInterval: 60_000,
  });
  const assets = data?.data ?? [];
  const actions = useAssetActions(assets);
  const kpis = usePortfolioKpis(portfolio);
  const isMain = portfolio === "MAIN";

  function toggleEditMode() {
    setEditMode((prev) => {
      if (prev) {
        setTransferAsset(null);
        actions.reset();
      }
      return !prev;
    });
  }

  return (
    <>
      <PortfolioView
        title="Portfolio"
        bookTag={BOOK_TAG[portfolio]}
        headerStatus={isMain ? <BotStatusBadge /> : undefined}
        positions={{ data: assets, isPending, isError }}
        kpis={kpis}
        actions={{
          editMode,
          onToggleEdit: toggleEditMode,
          onOpenHistory: () => setHistoriaOpen(true),
          onOpenTreemap: () => setTreemapOpen(true),
          onAdd: () => setAddOpen(true),
        }}
        table={{
          onSellClick: editMode ? setTransferAsset : undefined,
          onEditClick: editMode ? actions.startEditAsset : undefined,
          onEditSubClick: editMode ? actions.startEditSubasset : undefined,
          onDeleteClick: editMode ? actions.requestDeleteAsset : undefined,
          onDeleteSubClick: editMode ? actions.requestDeleteSubasset : undefined,
          onSetPrimaryClick: editMode ? actions.setSubassetPrimary : undefined,
          settingPrimaryId: actions.settingPrimaryId,
          targetEdit: {
            update: updateAsset,
            onSaved: actions.invalidatePositions,
            title: isMain ? "Cel % (risk-on)" : undefined,
          },
          renderAccountCell,
          defaultCollapsedExposures: isMain ? MAIN_COLLAPSED_EXPOSURES : NO_COLLAPSED_EXPOSURES,
        }}
      >
        {isMain && <MacroRegimePanel assets={assets} />}
      </PortfolioView>

      <Dialog open={treemapOpen} onOpenChange={setTreemapOpen}>
        <DialogContent className="max-w-screen w-screen h-screen rounded-none p-0 flex flex-col gap-0">
          <DialogHeader className="px-4 py-3 border-b shrink-0">
            <DialogTitle>Alokacja portfela — {BOOK_TAG[portfolio]}</DialogTitle>
          </DialogHeader>
          <div className="flex-1 min-h-0 p-2">
            <AllocationTreemap assets={assets} height="100%" />
          </div>
        </DialogContent>
      </Dialog>

      <HistoriaDialog portfolio={portfolio} open={historiaOpen} onOpenChange={setHistoriaOpen} />

      {transferAsset != null && (
        <RebalancingTransferDialog
          open={transferAsset != null}
          asset={transferAsset}
          assets={assets}
          onClose={() => setTransferAsset(null)}
          onSuccess={() => {
            setTransferAsset(null);
            void queryClient.invalidateQueries({ queryKey: queryKeys.positions.all });
          }}
        />
      )}

      <SubassetPickerDialog asset={actions.pickerAsset} onPick={actions.pickSubasset} onClose={actions.closePicker} />

      <EditPositionDialog
        target={actions.editTarget}
        assets={assets}
        onClose={actions.closeEdit}
        onSuccess={() => {
          actions.invalidatePositions();
          actions.closeEdit();
        }}
      />

      <CreatePositionDialog
        portfolio={portfolio}
        open={addOpen}
        onClose={() => setAddOpen(false)}
        onSuccess={actions.invalidatePositions}
        assets={assets}
      />

      <DeleteAssetDialog
        target={actions.deleteTarget}
        pending={actions.deletePending}
        errorMessage={actions.deleteErrorMessage}
        onConfirm={actions.confirmDelete}
        onClose={actions.closeDelete}
      />
    </>
  );
}
