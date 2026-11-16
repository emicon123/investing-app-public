import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { ApiError } from "@common/app-core";
import { deleteAsset, deletePosition, setPrimary, type Asset, type Subasset } from "../api/index.js";
import { queryKeys } from "@/lib/queryKeys";

/**
 * Edit / delete / set-primary flows of the Portfolio and Retirement pages (task 156), shared by
 * both routes — they used to carry two byte-identical copies of the promote + delete plumbing.
 * The list is ASSETS (each with its subassets); every action here targets either one whole
 * asset or one of its subassets.
 */

/** The subasset being edited plus the asset it currently belongs to. */
export interface EditTarget {
  asset: Asset;
  subasset: Subasset;
}

/** What the confirm dialog is about to delete: a whole asset, or one subasset. */
export interface DeleteTarget {
  kind: "asset" | "subasset";
  id: number;
  /** Ticker (subasset) or key (asset). */
  ticker: string;
  name: string;
  /** Extra consequence to spell out in the confirm dialog (cascade / last-subasset / 409 hint), or null. */
  note: string | null;
}

function deleteAssetNote(asset: Asset): string | null {
  const parts: string[] = [];
  if (asset.subassets.length > 0) {
    parts.push(
      `Wraz z aktywem znikną też wszystkie jego instrumenty (${asset.subassets.map((s) => s.ticker).join(", ")}).`,
    );
  }
  if (asset.targetAllocationPct != null) {
    parts.push(`Jego cel (${asset.targetAllocationPct}%) zniknie z sumy celów portfela.`);
  }
  return parts.length > 0 ? parts.join(" ") : null;
}

function deleteSubassetNote(sub: Subasset, owner: Asset | undefined): string | null {
  if (owner?.subassets.length === 1) {
    return "To ostatni instrument tego aktywa — aktywo również zostanie usunięte.";
  }
  if (sub.isPrimary) {
    return "To główny instrument aktywa — serwer odmówi usunięcia, dopóki nie ustawisz innego instrumentu jako głównego.";
  }
  return null;
}

export function useAssetActions(assets: Asset[]) {
  const queryClient = useQueryClient();
  const [editTarget, setEditTarget] = useState<EditTarget | null>(null);
  const [pickerAsset, setPickerAsset] = useState<Asset | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<DeleteTarget | null>(null);

  // Every action can change which tickers belong to which asset (and its exposure), so refresh the
  // whole positions family — both portfolios' lists and the history chart's ticker->exposure map.
  function invalidatePositions() {
    void queryClient.invalidateQueries({ queryKey: queryKeys.positions.all });
  }

  // ── Delete ──────────────────────────────────────────────────

  const deleteMutation = useMutation({
    mutationFn: (target: DeleteTarget) =>
      target.kind === "asset" ? deleteAsset(target.id) : deletePosition(target.id),
    onSuccess: () => {
      setDeleteTarget(null);
      invalidatePositions();
    },
  });

  // Surfaces the backend's reason (e.g. 409 "set a new primary first") inside the still-open
  // confirm dialog instead of failing silently.
  const deleteErrorMessage = (() => {
    if (!deleteMutation.isError) return null;
    const err: unknown = deleteMutation.error;
    if (err instanceof ApiError && err.status >= 400 && err.status < 500) return err.message;
    return "Nie udało się usunąć. Spróbuj ponownie.";
  })();

  function requestDeleteAsset(asset: Asset) {
    setDeleteTarget({
      kind: "asset",
      id: asset.id,
      ticker: asset.key,
      name: asset.name,
      note: deleteAssetNote(asset),
    });
  }

  function requestDeleteSubasset(sub: Subasset) {
    const owner = assets.find((a) => a.id === sub.assetId);
    setDeleteTarget({
      kind: "subasset",
      id: sub.id,
      ticker: sub.ticker,
      name: sub.name,
      note: deleteSubassetNote(sub, owner),
    });
  }

  function confirmDelete() {
    if (deleteTarget) deleteMutation.mutate(deleteTarget);
  }

  function closeDelete() {
    setDeleteTarget(null);
    deleteMutation.reset();
  }

  // ── Set primary ─────────────────────────────────────────────

  // "Ustaw jako główną" (task 156, replaces task 142 / ADR-088's "set as main") — a pure boolean
  // flip between a non-primary subasset and its asset's current primary. No heavy confirm dialog:
  // the operation is involutive (setting the demoted subasset primary again undoes it exactly), so
  // a success toast with a "Cofnij" (undo) action is the confirmation UX instead.
  // `fireSetPrimary` is reused for both the initial click and the undo action.
  const setPrimaryMutation = useMutation({
    mutationFn: (vars: { assetId: number; subassetId: number }) =>
      setPrimary(vars.assetId, vars.subassetId),
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

  function setSubassetPrimary(sub: Subasset) {
    const owner = assets.find((a) => a.id === sub.assetId);
    if (owner == null) return;
    const previousPrimary = owner.subassets.find((s) => s.isPrimary);
    fireSetPrimary(
      { assetId: owner.id, subassetId: sub.id, ticker: sub.ticker },
      previousPrimary != null
        ? { assetId: owner.id, subassetId: previousPrimary.id, ticker: previousPrimary.ticker }
        : undefined,
    );
  }

  // ── Edit ────────────────────────────────────────────────────

  /** Asset-row pencil: a single instrument is edited directly, several go through the picker. */
  function startEditAsset(asset: Asset) {
    const [only, ...rest] = asset.subassets;
    if (only !== undefined && rest.length === 0) {
      setEditTarget({ asset, subasset: only });
    } else {
      setPickerAsset(asset);
    }
  }

  /** Sub-row pencil. */
  function startEditSubasset(sub: Subasset) {
    const owner = assets.find((a) => a.id === sub.assetId);
    if (owner != null) setEditTarget({ asset: owner, subasset: sub });
  }

  function pickSubasset(asset: Asset, sub: Subasset) {
    setPickerAsset(null);
    setEditTarget({ asset, subasset: sub });
  }

  /** Leaving edit mode drops every open dialog target. */
  function reset() {
    setEditTarget(null);
    setPickerAsset(null);
    setDeleteTarget(null);
  }

  return {
    editTarget,
    pickerAsset,
    deleteTarget,
    deletePending: deleteMutation.isPending,
    deleteErrorMessage,
    settingPrimaryId: setPrimaryMutation.isPending ? (setPrimaryMutation.variables?.subassetId ?? null) : null,
    startEditAsset,
    startEditSubasset,
    pickSubasset,
    closeEdit: () => setEditTarget(null),
    closePicker: () => setPickerAsset(null),
    requestDeleteAsset,
    requestDeleteSubasset,
    confirmDelete,
    closeDelete,
    setSubassetPrimary,
    invalidatePositions,
    reset,
  };
}
