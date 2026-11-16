import { useMemo } from "react";
import {
  PositionTransferDialog as SharedPositionTransferDialog,
  type TransferAssetLike,
  type TransferAccountDimension,
  type TransferDialogCopy,
  type PositionTransferLegLike,
  type PositionTransferRequestLike,
} from "@common/ui-primitives/PositionTransferDialog";
import type { IdempotencyOptions } from "@common/app-core";
import type { Asset } from "../api/index.js";
import { createRebalancingTransfer, type RebalancingTransferRequest, type TransferLeg } from "../api/index.js";

// Root is the most complex of the three wrappers (migrated last per task 136 §4): it adds the
// ADR-054 wrapper-lock rule, a composite "REGULAR_EUR"-shaped account id that must be split back
// into the real {accountType, regularCurrency} wire shape, and posts to its own
// /rebalancing-transfers endpoint/DTO (never unified with portfolioB/portfolioC's shared
// /position-transfers — see docs/architecture/diagrams/136-shared-transfer-dialog.md §3).

const ROOT_ACCOUNT_OPTIONS = [
  { id: "IKE", label: "IKE" },
  { id: "IKZE", label: "IKZE" },
  { id: "REGULAR_PLN", label: "Zwykłe PLN" },
  { id: "REGULAR_EUR", label: "Zwykłe EUR" },
  { id: "REGULAR_USD", label: "Zwykłe USD" },
];

// ADR-054 (docs/architecture/05-rebalancing-algorithm.md §7.7): money already inside IKE or
// IKZE may only be reinvested within that same wrapper — only a REGULAR-sold leg may fund any
// buy-leg destination, including IKE/IKZE (a genuine new contribution).
function allowedBuyAccountIds(soldAccountId: string): string[] {
  if (soldAccountId === "IKE") return ["IKE"];
  if (soldAccountId === "IKZE") return ["IKZE"];
  return ["IKE", "IKZE", "REGULAR_PLN", "REGULAR_EUR", "REGULAR_USD"];
}

const ROOT_ACCOUNT_DIMENSION: TransferAccountDimension = {
  options: ROOT_ACCOUNT_OPTIONS,
  allowedBuyAccountIds,
};

const ROOT_COPY: Partial<TransferDialogCopy> = {
  dialogTitlePrefix: "Przenieś wartość",
  soldSectionLabel: "Sprzedaj",
  buySectionLabel: "Kup",
  soldHelperText: null,
  submitLabel: "Przenieś wartość",
  submitPendingLabel: "Przenieś wartość",
};

type Props = {
  /** Kept for route-file compat (routes still gate rendering on `transferAsset != null` and
   *  pass `open={transferAsset != null}`) — structurally redundant with `asset !== null`,
   *  but not worth a route-file edit for this cosmetic simplification (task 136 §3). */
  open: boolean;
  /** The clicked (over-target) asset. */
  asset: Asset;
  /** Every asset of the portfolio — the buyable list. */
  assets: Asset[];
  onClose: () => void;
  onSuccess: () => void;
};

function unitsByAccountOf(entity: {
  unitsIke: number;
  unitsIkze: number;
  unitsRegularPln: number;
  unitsRegularEur: number;
  unitsRegularUsd: number;
}): Record<string, number> {
  return {
    IKE: entity.unitsIke,
    IKZE: entity.unitsIkze,
    REGULAR_PLN: entity.unitsRegularPln,
    REGULAR_EUR: entity.unitsRegularEur,
    REGULAR_USD: entity.unitsRegularUsd,
  };
}

// Task 156: an Asset adapts to the shared dialog as one sell target whose own ticker/name/price/
// units are its PRIMARY subasset's (the default it pre-selects), while the group-level numbers
// (target, current %, valuePln = Σ subassets) come from the Asset; `subassets` are the symmetric
// sell candidates. Sell-assist thresholds and the suggested-sell math live in the shared dialog
// and read those Asset numbers unchanged (ADR-045/053/063).
function toTransferAsset(asset: Asset): TransferAssetLike {
  const primary = asset.subassets.find((s) => s.isPrimary) ?? asset.subassets[0];
  return {
    ticker: primary?.ticker ?? asset.key,
    name: primary?.name ?? asset.name,
    pricePln: primary?.pricePln ?? null,
    unitsByAccount: primary ? unitsByAccountOf(primary) : {},
    // ADR-063: the effective (blended) target percentage, falling back to the raw target when the
    // computed field is absent — matches the pre-extraction behavior exactly.
    targetAllocationPct: asset.computed?.effectiveTargetAllocationPct ?? asset.targetAllocationPct,
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

// Splits an opaque "REGULAR_EUR"-shaped account id (only ever produced by the shared dialog's
// own Konto <Select>, never sent over the wire as-is) back into the real
// {accountType:"REGULAR", regularCurrency:"EUR"} shape root's /rebalancing-transfers endpoint
// expects. IKE/IKZE ids need no splitting.
function toWireLeg(leg: PositionTransferLegLike): TransferLeg {
  const accountId = leg.accountType ?? "REGULAR_PLN";
  if (accountId === "IKE" || accountId === "IKZE") {
    return { ticker: leg.ticker, accountType: accountId, units: leg.units, pricePln: leg.pricePln };
  }
  const currency = accountId.split("_")[1] as "PLN" | "EUR" | "USD";
  return {
    ticker: leg.ticker,
    accountType: "REGULAR",
    regularCurrency: currency,
    units: leg.units,
    pricePln: leg.pricePln,
  };
}

export function RebalancingTransferDialog({ open, asset, assets, onClose, onSuccess }: Props) {
  // Memoized so the adapted object keeps a stable reference across the background positions
  // poll (`assets` legitimately gets a new array every refetch tick, but the single
  // currently-selected `asset` shouldn't spuriously change identity just because its
  // sibling rows refreshed) — the shared dialog's polling-stability fix relies on this.
  const transferPosition = useMemo(
    () => (open && asset ? toTransferAsset(asset) : null),
    [open, asset],
  );
  const transferAllPositions = useMemo(() => assets.map(toTransferAsset), [assets]);

  // Legs still key on (ticker, portfolio) — never on asset ids or companion/primary status.
  function submitTransfer(body: PositionTransferRequestLike, options: IdempotencyOptions) {
    return createRebalancingTransfer(
      {
        portfolio: asset.portfolio,
        sold: toWireLeg(body.sold),
        buys: body.buys.map(toWireLeg),
      } satisfies RebalancingTransferRequest,
      options,
    );
  }

  return (
    <SharedPositionTransferDialog
      position={transferPosition}
      allPositions={transferAllPositions}
      accountDimension={ROOT_ACCOUNT_DIMENSION}
      portfolioId={asset.portfolio}
      submitTransfer={submitTransfer}
      copy={ROOT_COPY}
      imbalanceTolerancePct={0.05}
      onClose={onClose}
      onSuccess={onSuccess}
    />
  );
}
