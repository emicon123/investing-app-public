import { useState } from "react";
import type { ComponentType, ReactNode } from "react";
import { useMutation } from "@tanstack/react-query";
import { toast } from "sonner";
import { ApiError, type PortfolioKpis } from "@common/app-core";
import { Badge } from "./badge.js";
import { Button } from "./button.js";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "./dialog.js";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "./alert-dialog.js";
import { PortfolioView } from "./portfolio/PortfolioView.js";
import { SOFT_DELETE_CONSEQUENCE } from "./portfolio/model/deleteCopy.js";
import type { PortfolioAssetLike, PortfolioSubassetLike } from "./portfolio/types.js";

/**
 * portfolioB/portfolioC only (task 156, Phase C1 — main excluded at 55%: bot/macro
 * widgets, companion-cascade delete, fullscreen treemap, …). All seven
 * injected collaborators are the apps' existing wrappers, passed unchanged
 * as slots — this page composes the prior extractions (Phase B
 * EditPositionDialog, the shared table/treemap/dialogs).
 *
 * Task 156 (Asset / Subasset model): the page's list is ASSETS (logical
 * groups, the only holders of targets), each nested with its SUBASSETS
 * (concrete holdings, exactly one primary). Delete works at both levels
 * (`deleteAsset` cascades; `deletePosition` removes one subasset — last one
 * takes the asset along, a primary with siblings is refused with 409 whose
 * message is shown in the confirm dialog), and "Ustaw jako główną" replaces
 * the retired promote flow.
 *
 * The only key difference on this page (portfolioB invalidates
 * `positions.list()` where portfolioC invalidates `positions.all`) is absorbed in
 * the single `invalidatePositions` callback. Sell-success only closes (both
 * apps skip invalidation there today — preserved; the TransferDialog
 * wrappers invalidate internally with their own keys).
 *
 * Task 170 (ADR-106): the page body is the shared `PortfolioView` (KPI bento,
 * allocation ribbon, grouped table). This container still owns the data, the
 * dialogs and the edit flows; the apps pass their KPI hook, the
 * target-edit call and the account cell.
 */
export type SubassetLike = PortfolioSubassetLike;
export type AssetLike<TSub extends SubassetLike = SubassetLike> = PortfolioAssetLike<TSub>;

/** The subasset being edited plus the asset it currently belongs to. */
export interface EditTarget<TAsset, TSub> {
  asset: TAsset;
  subasset: TSub;
}

/** What the confirm dialog is about to delete: a whole asset, or one subasset. */
export interface DeleteTarget {
  kind: "asset" | "subasset";
  id: number;
  /** Ticker (subasset) or key (asset). */
  ticker: string;
  name: string;
  /** Extra consequence to spell out in the confirm dialog (cascade / last-subasset), or null. */
  note: string | null;
}

export interface PortfolioPageApi {
  deletePosition: (id: number) => Promise<unknown>;
  deleteAsset: (id: number) => Promise<unknown>;
  setPrimary: (assetId: number, subassetId: number) => Promise<unknown>;
}

export interface PortfolioPageProps<
  TAsset extends AssetLike<TSub>,
  TSub extends SubassetLike,
> {
  /** "Portfolio" in every app. */
  title: string;
  /** portfolioC: the book's label; portfolioB: none. */
  bookTag?: string;
  api: PortfolioPageApi;
  usePositionsQuery: () => {
    data: TAsset[] | undefined;
    isPending: boolean;
    isError: boolean;
  };
  invalidatePositions: () => void;
  /** The app's KPI queries for this book (`createUsePortfolioKpis` binding). */
  useKpis: () => PortfolioKpis;
  /** `PUT /assets/{id}` for the inline target edit. */
  updateAsset: (id: number, body: { targetAllocationPct: number | null }) => Promise<unknown>;
  /** A sub-row's account split (portfolioB: IKE/IKZE chips; portfolioC: units). */
  renderAccountCell: (sub: TSub, assetClass: string) => ReactNode;
  /** Exposure groups that start collapsed; none by default. */
  defaultCollapsedExposures?: readonly string[];
  EditDialog: ComponentType<{
    target: EditTarget<TAsset, TSub> | null;
    /** Every asset of the (portfolio-scoped) list — the re-home picker's options. */
    assets: TAsset[];
    onClose: () => void;
    onSuccess: () => void;
  }>;
  CreateDialog: ComponentType<{
    open: boolean;
    onClose: () => void;
    onSuccess: () => void;
    /** Every asset of the (portfolio-scoped) list — the attach-to-asset picker's options. */
    assets: TAsset[];
  }>;
  TransferDialog: ComponentType<{
    asset: TAsset | null;
    assets: TAsset[];
    onClose: () => void;
    onSuccess: () => void;
  }>;
  Treemap: ComponentType<{
    assets: TAsset[];
    height?: number | string;
  }>;
  HistoryDialog: ComponentType<{
    open: boolean;
    onOpenChange: (open: boolean) => void;
  }>;
}

export function PortfolioPage<
  TAsset extends AssetLike<TSub>,
  TSub extends SubassetLike,
>({
  title,
  bookTag,
  api,
  usePositionsQuery,
  invalidatePositions,
  useKpis,
  updateAsset,
  renderAccountCell,
  defaultCollapsedExposures,
  EditDialog,
  CreateDialog,
  TransferDialog,
  Treemap,
  HistoryDialog,
}: PortfolioPageProps<TAsset, TSub>) {
  const [treemapOpen, setTreemapOpen] = useState(false);
  const [historiaOpen, setHistoriaOpen] = useState(false);
  const [editMode, setEditMode] = useState(false);
  const [editTarget, setEditTarget] = useState<EditTarget<TAsset, TSub> | null>(null);
  const [sellAsset, setSellAsset] = useState<TAsset | null>(null);
  const [subPickerAsset, setSubPickerAsset] = useState<TAsset | null>(null);
  const [addOpen, setAddOpen] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<DeleteTarget | null>(null);

  const deleteMutation = useMutation({
    mutationFn: (target: DeleteTarget) =>
      target.kind === "asset" ? api.deleteAsset(target.id) : api.deletePosition(target.id),
    onSuccess: () => {
      setDeleteTarget(null);
      invalidatePositions();
    },
  });

  // Surfaces the backend's reason (e.g. 409 "asset still has other subassets — set a new primary
  // first") inside the still-open confirm dialog instead of failing silently.
  const deleteErrorMessage = (() => {
    if (!deleteMutation.isError) return null;
    const err: unknown = deleteMutation.error;
    if (err instanceof ApiError && err.status >= 400 && err.status < 500) return err.message;
    return "Nie udało się usunąć. Spróbuj ponownie.";
  })();

  function closeDeleteDialog() {
    setDeleteTarget(null);
    deleteMutation.reset();
  }

  // "Ustaw jako główną" (task 156, replaces task 142 / ADR-088's "set as main") — a pure
  // boolean swap between a non-primary subasset and its asset's current primary. No heavy confirm
  // dialog: the operation is involutive (setting the demoted row primary again undoes it
  // exactly), so a success toast with an "Cofnij" (undo) action is the confirmation UX instead.
  // `fireSetPrimary` is reused for both the initial click and the undo action.
  const setPrimaryMutation = useMutation({
    mutationFn: (vars: { assetId: number; subassetId: number }) =>
      api.setPrimary(vars.assetId, vars.subassetId),
  });

  function fireSetPrimary(
    target: { assetId: number; subassetId: number; ticker: string },
    undoTarget?: { assetId: number; subassetId: number; ticker: string },
  ) {
    setPrimaryMutation.mutate(target, {
      onSuccess: () => {
        invalidatePositions();
        toast.success(`${target.ticker} jest teraz główną pozycją aktywa.`, {
          action:
            undoTarget != null
              ? { label: "Cofnij", onClick: () => fireSetPrimary(undoTarget) }
              : undefined,
        });
      },
      onError: (err) => {
        toast.error(err instanceof ApiError ? err.message : "Nie udało się zmienić głównej pozycji.");
      },
    });
  }

  function handleSetPrimaryClick(sub: TSub) {
    const owner = assets.find((a) => a.subassets.some((s) => s.id === sub.id));
    if (owner == null) return;
    const previousPrimary = owner.subassets.find((s) => s.isPrimary);
    fireSetPrimary(
      { assetId: owner.id, subassetId: sub.id, ticker: sub.ticker },
      previousPrimary != null
        ? { assetId: owner.id, subassetId: previousPrimary.id, ticker: previousPrimary.ticker }
        : undefined,
    );
  }

  function handleDeleteSubClick(sub: TSub) {
    const owner = assets.find((a) => a.subassets.some((s) => s.id === sub.id));
    setDeleteTarget({
      kind: "subasset",
      id: sub.id,
      ticker: sub.ticker,
      name: sub.name,
      note:
        owner?.subassets.length === 1
          ? "To ostatni instrument tego aktywa — aktywo również zostanie usunięte."
          : null,
    });
  }

  function handleDeleteAssetClick(asset: TAsset) {
    setDeleteTarget({
      kind: "asset",
      id: asset.id,
      ticker: asset.key,
      name: asset.name,
      note:
        asset.subassets.length > 1
          ? `Znikną też wszystkie jego instrumenty (${asset.subassets.length}).`
          : null,
    });
  }

  function toggleEditMode() {
    setEditMode((prev) => {
      if (prev) {
        setEditTarget(null);
        setSubPickerAsset(null);
        setDeleteTarget(null);
      }
      return !prev;
    });
  }

  function handleEditClick(asset: TAsset) {
    const [only, ...rest] = asset.subassets;
    if (only !== undefined && rest.length === 0) {
      setEditTarget({ asset, subasset: only });
    } else {
      setSubPickerAsset(asset);
    }
  }

  function handleSubPick(asset: TAsset, sub: TSub) {
    setSubPickerAsset(null);
    setEditTarget({ asset, subasset: sub });
  }

  const { data, isPending, isError } = usePositionsQuery();
  const kpis = useKpis();

  const assets = data ?? [];

  return (
    <>
      <PortfolioView
        title={title}
        bookTag={bookTag}
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
          // SELL is clickable in every mode in portfolioB/portfolioC (unchanged).
          onSellClick: setSellAsset,
          onEditClick: editMode ? handleEditClick : undefined,
          onDeleteClick: editMode ? handleDeleteAssetClick : undefined,
          onDeleteSubClick: editMode ? handleDeleteSubClick : undefined,
          onSetPrimaryClick: editMode ? handleSetPrimaryClick : undefined,
          settingPrimaryId: setPrimaryMutation.isPending ? (setPrimaryMutation.variables?.subassetId ?? null) : null,
          targetEdit: { update: updateAsset, onSaved: invalidatePositions },
          renderAccountCell,
          defaultCollapsedExposures,
        }}
      />

      {subPickerAsset !== null && (
        <Dialog open onOpenChange={(o) => { if (!o) setSubPickerAsset(null); }}>
          <DialogContent className="max-w-sm">
            <DialogHeader>
              <DialogTitle>Wybierz instrument — {subPickerAsset.key}</DialogTitle>
            </DialogHeader>
            <div className="space-y-2 py-2">
              {subPickerAsset.subassets.map((sub) => (
                <Button
                  key={sub.id}
                  variant="outline"
                  className="w-full justify-start gap-3 h-auto py-3"
                  onClick={() => handleSubPick(subPickerAsset, sub)}
                >
                  <span className="font-mono text-sm">{sub.ticker}</span>
                  <span className="text-muted-foreground text-sm truncate">{sub.name}</span>
                  {sub.isPrimary && (
                    <Badge variant="secondary" className="ml-auto px-1 py-0 text-[10px] font-normal">
                      Główna
                    </Badge>
                  )}
                </Button>
              ))}
            </div>
          </DialogContent>
        </Dialog>
      )}

      <EditDialog
        target={editTarget}
        assets={assets}
        onClose={() => setEditTarget(null)}
        onSuccess={() => {
          invalidatePositions();
          setEditTarget(null);
        }}
      />

      <CreateDialog
        open={addOpen}
        onClose={() => setAddOpen(false)}
        onSuccess={() => {
          invalidatePositions();
        }}
        assets={assets}
      />

      <TransferDialog
        asset={sellAsset}
        assets={assets}
        onClose={() => setSellAsset(null)}
        onSuccess={() => setSellAsset(null)}
      />

      <Dialog open={treemapOpen} onOpenChange={setTreemapOpen}>
        <DialogContent className="max-w-[95vw] h-[92vh] flex flex-col">
          <DialogHeader>
            <DialogTitle>Alokacja portfela</DialogTitle>
          </DialogHeader>
          <div className="flex-1 min-h-0">
            <Treemap assets={assets} height="100%" />
          </div>
        </DialogContent>
      </Dialog>

      <HistoryDialog open={historiaOpen} onOpenChange={setHistoriaOpen} />

      <AlertDialog open={deleteTarget != null} onOpenChange={(o) => { if (!o) closeDeleteDialog(); }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {deleteTarget?.kind === "asset" ? "Usuń aktywo" : "Usuń instrument"}
            </AlertDialogTitle>
            <AlertDialogDescription>
              Czy na pewno chcesz usunąć <strong>{deleteTarget?.ticker}</strong> ({deleteTarget?.name})?
              {deleteTarget?.note ? ` ${deleteTarget.note}` : ""} {SOFT_DELETE_CONSEQUENCE}
            </AlertDialogDescription>
          </AlertDialogHeader>
          {deleteErrorMessage && (
            <p role="alert" className="text-sm text-destructive">
              {deleteErrorMessage}
            </p>
          )}
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleteMutation.isPending}>Anuluj</AlertDialogCancel>
            <AlertDialogAction
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
              disabled={deleteMutation.isPending}
              onClick={(e) => {
                // Keep the dialog open until the request settles so a 409/404 message can show.
                e.preventDefault();
                if (deleteTarget) deleteMutation.mutate(deleteTarget);
              }}
            >
              {deleteMutation.isPending ? "Usuwanie..." : "Usuń"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}
