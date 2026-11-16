import { useMemo } from "react";
import { useQueryClient } from "@tanstack/react-query";
import type { IdempotencyOptions } from "@common/app-core";
import {
  PositionTransferDialog as SharedPositionTransferDialog,
  type TransferAssetLike,
  type PositionTransferRequestLike,
} from "@common/ui-primitives/PositionTransferDialog";
import { executePositionTransfer, type Asset, type PositionTransferRequest } from "../api/index.js";
import { usePortfolio } from "@/lib/portfolio";
import { queryKeys } from "@/lib/queryKeys";

// portfolioC has no account dimension at all (flat single-pool units, no "Konto" selector) — this is
// the simplest of the three wrappers, migrated first per task 136 §4 to prove the shared
// component's "no accountDimension" branch before the more complex portfolio-b/root wrappers.

type Props = {
  /** The clicked (over-target) asset, or null when closed. */
  asset: Asset | null;
  /** Every asset of the current portfolio — the buyable list. */
  assets: Asset[];
  onClose: () => void;
  onSuccess: () => void;
};

// Task 156: an Asset adapts to the shared dialog as one sell target whose own ticker/price/units
// are its PRIMARY subasset's (the default it pre-selects), group-level numbers (target, current %,
// valuePln = Σ subassets) come from the Asset, and `subassets` are the symmetric sell candidates.
function toTransferAsset(asset: Asset): TransferAssetLike {
  const primary = asset.subassets.find((s) => s.isPrimary) ?? asset.subassets[0];
  return {
    ticker: primary?.ticker ?? asset.key,
    name: primary?.name ?? asset.name,
    pricePln: primary?.pricePln ?? null,
    units: primary?.units ?? 0,
    targetAllocationPct: asset.targetAllocationPct,
    currentPct: asset.computed?.currentPct ?? null,
    valuePln: asset.computed?.valuePln ?? 0,
    subassets: asset.subassets.map((sub) => ({
      ticker: sub.ticker,
      name: sub.name,
      pricePln: sub.pricePln,
      units: sub.units,
    })),
  };
}

export function PositionTransferDialog({ asset, assets, onClose, onSuccess }: Props) {
  const { portfolio } = usePortfolio();
  const queryClient = useQueryClient();

  // Memoized so the adapted object keeps a stable reference across the background positions
  // poll (`assets` legitimately gets a new array every refetch tick, but the single
  // currently-selected `asset` shouldn't spuriously change identity just because its
  // sibling rows refreshed) — the shared dialog's polling-stability fix relies on this.
  const transferPosition = useMemo(() => (asset ? toTransferAsset(asset) : null), [asset]);
  const transferAllPositions = useMemo(() => assets.map(toTransferAsset), [assets]);

  function submitTransfer(body: PositionTransferRequestLike, options: IdempotencyOptions) {
    return executePositionTransfer(
      {
        portfolio,
        sold: { ticker: body.sold.ticker, units: body.sold.units, pricePln: body.sold.pricePln },
        buys: body.buys.map((b) => ({ ticker: b.ticker, units: b.units, pricePln: b.pricePln })),
      } satisfies PositionTransferRequest,
      options,
    );
  }

  function handleSuccess() {
    void queryClient.invalidateQueries({ queryKey: queryKeys.positions.all });
    onSuccess();
  }

  return (
    <SharedPositionTransferDialog
      position={transferPosition}
      allPositions={transferAllPositions}
      portfolioId={portfolio}
      submitTransfer={submitTransfer}
      onClose={onClose}
      onSuccess={handleSuccess}
    />
  );
}
