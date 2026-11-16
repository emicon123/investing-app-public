import { useMemo } from "react";
import { z } from "zod";
import { updatePosition, assetClassSchema } from "../api/index.js";
import type { Asset, Subasset } from "../api/index.js";
import {
  EditPositionDialog as SharedEditPositionDialog,
  type EditPositionSnapshot,
  type EditPositionUpdate,
} from "@common/ui-primitives/EditPositionDialog";

const UNIT_FIELDS = [
  { key: "unitsIkeTata", label: "IKE tata" },
  { key: "unitsIkeMama", label: "IKE mama" },
  { key: "unitsIkzeTata", label: "IKZE tata" },
  { key: "unitsIkzeMama", label: "IKZE mama" },
  { key: "unitsInne", label: "Inne" },
];

const ASSET_CLASS_OPTIONS: Array<{ value: z.infer<typeof assetClassSchema>; label: string }> = [
  { value: "ETF", label: "ETF" },
  { value: "METALS", label: "METALS" },
  { value: "CRYPTO", label: "CRYPTO" },
  { value: "STOCK", label: "STOCK" },
  { value: "CURRENCY", label: "CURRENCY" },
];

type EditTarget = { asset: Asset; subasset: Subasset };

/**
 * Snapshot of ONE subasset. No `targetAllocationPct` — targets live on the Asset only, so the
 * shared dialog renders no "Cel %" input (the Asset row's inline editor owns it). `assetClass` is
 * the parent asset's: `SubassetDto` carries no class of its own.
 */
function toSnapshot({ asset, subasset }: EditTarget): EditPositionSnapshot {
  return {
    id: subasset.id,
    ticker: subasset.ticker,
    name: subasset.name,
    yahooTicker: subasset.yahooTicker,
    assetClass: asset.assetClass,
    companyBacked: false,
    assetId: asset.id,
    isPrimary: subasset.isPrimary,
    unitValues: {
      unitsIkeTata: subasset.unitsIkeTata,
      unitsIkeMama: subasset.unitsIkeMama,
      unitsIkzeTata: subasset.unitsIkzeTata,
      unitsIkzeMama: subasset.unitsIkzeMama,
      unitsInne: subasset.unitsInne,
    },
  };
}

function handleUpdate(id: number, body: EditPositionUpdate) {
  const { units, ...rest } = body;
  return updatePosition(id, { ...rest, ...units } as Parameters<typeof updatePosition>[1]);
}

type Props = {
  target: EditTarget | null;
  /** Every asset of the list — a NON-primary subasset can be re-homed under any of them. */
  assets: Asset[];
  onClose: () => void;
  onSuccess: () => void;
};

export function EditPositionDialog({ target, assets, onClose, onSuccess }: Props) {
  // Memoized on `target` (stable React state) — a fresh snapshot object every render would make the
  // shared dialog's reset-on-`position`-change effect wipe in-progress edits on every background poll.
  const snapshot = useMemo(() => (target ? toSnapshot(target) : null), [target]);

  return (
    <SharedEditPositionDialog
      position={snapshot}
      onClose={onClose}
      onSuccess={onSuccess}
      updatePosition={handleUpdate}
      unitFields={UNIT_FIELDS}
      assetClassOptions={ASSET_CLASS_OPTIONS}
      showCompanyBacked={false}
      assets={assets}
    />
  );
}
