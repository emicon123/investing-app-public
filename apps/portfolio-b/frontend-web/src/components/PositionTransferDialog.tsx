import { useMemo } from "react";
import { useQueryClient } from "@tanstack/react-query";
import {
  PositionTransferDialog as SharedPositionTransferDialog,
  type TransferAssetLike,
  type TransferAccountDimension,
  type PositionTransferRequestLike,
} from "@common/ui-primitives/PositionTransferDialog";
import { formatAssetUnits, type IdempotencyOptions } from "@common/app-core";
import { executePositionTransfer, type AccountType, type Asset, type PositionTransferRequest } from "../api/index.js";
import { queryKeys } from "@/lib/queryKeys";

// ── Account labels (matches AccountBreakdown in PositionsTable.tsx) ────

const ACCOUNT_TYPE_LABELS: Record<AccountType, string> = {
  IKE_TATA: "IKE tata",
  IKE_MAMA: "IKE mama",
  IKZE_TATA: "IKZE tata",
  IKZE_MAMA: "IKZE mama",
  INNE: "Inne",
};

const ACCOUNT_OPTIONS = (Object.keys(ACCOUNT_TYPE_LABELS) as AccountType[]).map((id) => ({
  id,
  label: ACCOUNT_TYPE_LABELS[id],
}));

type Props = {
  /** The clicked (over-target) asset, or null when closed. */
  asset: Asset | null;
  /** Every asset — the buyable list. */
  assets: Asset[];
  onClose: () => void;
  onSuccess: () => void;
};

function unitsByAccountOf(entity: {
  unitsIkeTata: number;
  unitsIkeMama: number;
  unitsIkzeTata: number;
  unitsIkzeMama: number;
  unitsInne: number;
}): Record<string, number> {
  return {
    IKE_TATA: entity.unitsIkeTata,
    IKE_MAMA: entity.unitsIkeMama,
    IKZE_TATA: entity.unitsIkzeTata,
    IKZE_MAMA: entity.unitsIkzeMama,
    INNE: entity.unitsInne,
  };
}

// Task 156: an Asset adapts to the shared dialog as one sell target whose own ticker/price/units
// are its PRIMARY subasset's (the default it pre-selects), group-level numbers (target, current %,
// valuePln = Σ subassets) come from the Asset, and `subassets` are the symmetric sell candidates.
function toTransferAsset(asset: Asset): TransferAssetLike {
  const primary = asset.subassets.find((s) => s.isPrimary) ?? asset.subassets[0];
  return {
    ticker: primary?.ticker ?? asset.key,
    name: primary?.name ?? asset.name,
    pricePln: primary?.pricePln ?? null,
    unitsByAccount: primary ? unitsByAccountOf(primary) : {},
    targetAllocationPct: asset.targetAllocationPct,
    currentPct: asset.computed?.currentPct ?? null,
    valuePln: asset.computed?.valuePln ?? 0,
    subassets: asset.subassets.map((sub) => ({
      ticker: sub.ticker,
      name: sub.name,
      pricePln: sub.pricePln,
      unitsByAccount: unitsByAccountOf(sub),
    })),
  };
}

export function PositionTransferDialog({ asset, assets, onClose, onSuccess }: Props) {
  const queryClient = useQueryClient();

  // Memoized so the adapted object keeps a stable reference across the background positions
  // poll (`assets` legitimately gets a new array every refetch tick, but the single
  // currently-selected `asset` shouldn't spuriously change identity just because its
  // sibling rows refreshed) — the shared dialog's polling-stability fix relies on this.
  const transferPosition = useMemo(() => (asset ? toTransferAsset(asset) : null), [asset]);
  const transferAllPositions = useMemo(() => assets.map(toTransferAsset), [assets]);

  // A fresh object every render is fine here — it's cheap config, not memoized state; no
  // allowedBuyAccountIds means every account is an unrestricted buy destination.
  const accountDimension: TransferAccountDimension = {
    options: ACCOUNT_OPTIONS,
    formatUnits: (units) => formatAssetUnits(units, asset?.assetClass ?? "ETF"),
  };

  function submitTransfer(body: PositionTransferRequestLike, options: IdempotencyOptions) {
    return executePositionTransfer(
      {
        sold: {
          ticker: body.sold.ticker,
          accountType: body.sold.accountType as AccountType,
          units: body.sold.units,
          pricePln: body.sold.pricePln,
        },
        buys: body.buys.map((b) => ({
          ticker: b.ticker,
          accountType: b.accountType as AccountType,
          units: b.units,
          pricePln: b.pricePln,
        })),
      } satisfies PositionTransferRequest,
      options,
    );
  }

  function handleSuccess() {
    void queryClient.invalidateQueries({ queryKey: queryKeys.positions.list() });
    onSuccess();
  }

  return (
    <SharedPositionTransferDialog
      position={transferPosition}
      allPositions={transferAllPositions}
      accountDimension={accountDimension}
      submitTransfer={submitTransfer}
      onClose={onClose}
      onSuccess={handleSuccess}
    />
  );
}
