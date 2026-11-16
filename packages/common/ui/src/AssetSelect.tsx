import { useMemo } from "react";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "./select.js";

/**
 * Asset picker (task 156, diagram §10 / §13.4) — the single shared control for
 * "which Asset does this subasset belong to". Born in `CreatePositionDialog`
 * (attach a new subasset to an existing Asset), reused by `EditPositionDialog`
 * (re-home a NON-primary subasset). The caller filters `assets` to the right
 * portfolio; this component only sorts (by key) and renders `{key} — {name}`.
 */

export interface AssetOption {
  id: number;
  key: string;
  name: string;
}

export interface AssetSelectProps {
  /** DOM id of the trigger, so a `<Label htmlFor>` can point at it. */
  id: string;
  assets: AssetOption[];
  value: number | null;
  onChange: (assetId: number) => void;
  disabled?: boolean;
  placeholder?: string;
}

export function AssetSelect({
  id,
  assets,
  value,
  onChange,
  disabled,
  placeholder = "Wybierz aktywo...",
}: AssetSelectProps) {
  const sorted = useMemo(
    () => assets.slice().sort((a, b) => a.key.localeCompare(b.key)),
    [assets],
  );

  return (
    <Select
      value={value != null ? String(value) : ""}
      onValueChange={(v) => onChange(Number(v))}
      disabled={disabled}
    >
      <SelectTrigger id={id}>
        <SelectValue placeholder={placeholder} />
      </SelectTrigger>
      <SelectContent>
        {sorted.map((a) => (
          <SelectItem key={a.id} value={String(a.id)}>
            {a.key} — {a.name}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}
