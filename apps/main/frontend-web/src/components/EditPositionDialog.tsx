import { useMemo } from "react";
import { z } from "zod";
import { updatePosition, assetClassSchema } from "../api/index.js";
import type { Asset, UpdatePositionRequest } from "../api/index.js";
import type { EditTarget } from "@/hooks/useAssetActions";
import {
  EditPositionDialog as SharedEditPositionDialog,
  type EditPositionSnapshot,
  type EditPositionUpdate,
} from "@common/ui-primitives/EditPositionDialog";

const UNIT_FIELDS = [
  { key: "unitsIke", label: "IKE" },
  { key: "unitsIkze", label: "IKZE" },
  { key: "unitsRegularEur", label: "EUR" },
  { key: "unitsRegularUsd", label: "USD" },
  { key: "unitsRegularPln", label: "PLN" },
];

const ASSET_CLASS_OPTIONS: Array<{ value: z.infer<typeof assetClassSchema>; label: string }> = [
  { value: "STOCK", label: "STOCK" },
  { value: "CRYPTO", label: "CRYPTO" },
  { value: "ETF", label: "ETF" },
  { value: "CURRENCY", label: "CURRENCY" },
];

/**
 * Snapshot of ONE subasset (task 156). No `targetAllocationPct` — targets live on the Asset only,
 * so the shared dialog renders no "Cel %" input (the asset row's inline editor owns it). `assetId`
 * + `isPrimary` switch on the re-home picker for a NON-primary subasset (§13.4).
 */
function toSnapshot({ subasset }: EditTarget): EditPositionSnapshot {
  return {
    id: subasset.id,
    ticker: subasset.ticker,
    name: subasset.name,
    yahooTicker: subasset.yahooTicker,
    assetClass: subasset.assetClass,
    companyBacked: subasset.companyBacked,
    assetId: subasset.assetId,
    isPrimary: subasset.isPrimary,
    unitValues: {
      unitsIke: subasset.unitsIke,
      unitsIkze: subasset.unitsIkze,
      unitsRegularEur: subasset.unitsRegularEur,
      unitsRegularUsd: subasset.unitsRegularUsd,
      unitsRegularPln: subasset.unitsRegularPln,
    },
  };
}

/** Subasset-owned fields only — a top-level target key would be rejected with 400 by the backend. */
function toUpdateRequest(body: EditPositionUpdate): UpdatePositionRequest {
  const { units } = body;
  return {
    name: body.name,
    assetClass: assetClassSchema.optional().parse(body.assetClass),
    yahooTicker: body.yahooTicker,
    companyBacked: body.companyBacked,
    assetId: body.assetId,
    unitsIke: units.unitsIke,
    unitsIkze: units.unitsIkze,
    unitsRegularEur: units.unitsRegularEur,
    unitsRegularUsd: units.unitsRegularUsd,
    unitsRegularPln: units.unitsRegularPln,
  };
}

function handleUpdate(id: number, body: EditPositionUpdate) {
  return updatePosition(id, toUpdateRequest(body));
}

/** Re-home options: assets of the edited subasset's portfolio only (the backend rejects other portfolios with 400). */
function rehomeOptions(assets: Asset[], portfolio: string) {
  return assets
    .filter((a) => a.portfolio === portfolio)
    .map((a) => ({ id: a.id, key: a.key, name: a.name }));
}

type Props = {
  target: EditTarget | null;
  /** Every asset of the (portfolio-scoped) list — a non-primary subasset can be re-homed under any of them. */
  assets: Asset[];
  onClose: () => void;
  onSuccess: () => void;
};

export function EditPositionDialog({ target, assets, onClose, onSuccess }: Props) {
  // Memoized on `target` (stable React state) — a fresh snapshot object every render would make the
  // shared dialog's reset-on-`position`-change effect wipe in-progress edits on every background poll.
  const snapshot = useMemo(() => (target ? toSnapshot(target) : null), [target]);
  const assetOptions = useMemo(
    () => (target ? rehomeOptions(assets, target.subasset.portfolio) : []),
    [assets, target],
  );

  return (
    <SharedEditPositionDialog
      position={snapshot}
      onClose={onClose}
      onSuccess={onSuccess}
      updatePosition={handleUpdate}
      unitFields={UNIT_FIELDS}
      assetClassOptions={ASSET_CLASS_OPTIONS}
      showCompanyBacked
      assets={assetOptions}
    />
  );
}
