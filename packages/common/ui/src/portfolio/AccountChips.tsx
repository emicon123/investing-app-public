import { formatAssetUnits } from "@common/app-core";

/**
 * Units held per account, one chip each (zero buckets are dropped). The apps' `renderAccountCell`
 * slots build the account list (main: IKE/IKZE/REG, crypto folding regular into "BINANCE";
 * portfolioB: IKE/IKZE tata/mama + Inne) and render it through this one component.
 */

export interface AccountUnits {
  label: string;
  value: number;
}

export function AccountChips({ accounts, assetClass }: { accounts: readonly AccountUnits[]; assetClass: string }) {
  const held = accounts.filter((a) => a.value > 0);
  if (held.length === 0) return <span className="text-text-caption">—</span>;
  return (
    <div className="flex flex-wrap gap-x-2 gap-y-1">
      {held.map((a) => (
        <span key={a.label} className="inline-flex items-center gap-1 text-xs">
          <span className="rounded border border-hairline-strong px-1 text-[10px] leading-4 text-text-2">{a.label}</span>
          <span className="font-mono text-text-soft">{formatAssetUnits(a.value, assetClass)}</span>
        </span>
      ))}
    </div>
  );
}
