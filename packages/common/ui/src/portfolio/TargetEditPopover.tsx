import { useRef, useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { PencilIcon } from "lucide-react";
import { ApiError } from "@common/app-core";
import { Button } from "../button.js";
import { DecimalInput } from "../decimal-input.js";
import { Popover, PopoverContent, PopoverTrigger } from "../popover.js";
import { cn } from "../lib/utils.js";
import { COARSE_HIT_AREA, FOCUS_RING, ICON_BUTTON } from "./tableLayout.js";
import type { TargetEditSlot } from "./types.js";

/**
 * Inline edit of an asset's base target (`PUT /assets/{id}`), the pencil next to "cel X%" in the
 * meter cell. One component for all three apps: it replaces main's and the siblings' `TargetPctCell`.
 * Enter saves, Escape cancels, an empty input clears the target (null = no target, ADR-053); a 4xx
 * shows the backend's reason. The pencil appears on row hover or focus, always on touch screens.
 */

export interface TargetEditAssetLike {
  id: number;
  name: string;
  targetAllocationPct: number | null;
}

export interface TargetEditPopoverProps extends TargetEditSlot {
  asset: TargetEditAssetLike;
}

/** null clears the target; undefined = not a valid 0–100 number (nothing is sent). */
function parseTarget(raw: string): number | null | undefined {
  const trimmed = raw.trim();
  if (trimmed === "") return null;
  const n = Number.parseFloat(trimmed);
  return Number.isNaN(n) || n < 0 || n > 100 ? undefined : n;
}

function errorMessage(error: unknown): string {
  if (error instanceof ApiError && error.status >= 400 && error.status < 500) return error.message;
  return "Błąd zapisu. Spróbuj ponownie.";
}

export function TargetEditPopover({ asset, update, onSaved, title = "Cel %" }: TargetEditPopoverProps) {
  const [open, setOpen] = useState(false);
  const [inputValue, setInputValue] = useState("");
  const inputRef = useRef<HTMLInputElement | null>(null);

  const mutation = useMutation({
    mutationFn: (value: number | null) => update(asset.id, { targetAllocationPct: value }),
    onSuccess: () => {
      setOpen(false);
      onSaved();
    },
  });

  function handleOpenChange(next: boolean) {
    if (next) {
      setInputValue(asset.targetAllocationPct != null ? String(asset.targetAllocationPct) : "");
      mutation.reset();
    }
    setOpen(next);
  }

  function handleSave() {
    const value = parseTarget(inputValue);
    if (value !== undefined) mutation.mutate(value);
  }

  return (
    <Popover open={open} onOpenChange={handleOpenChange}>
      <PopoverTrigger asChild>
        <button
          type="button"
          aria-label={`Edytuj cel % dla ${asset.name}`}
          className={cn(
            ICON_BUTTON,
            "size-5 opacity-0 group-hover/row:opacity-100 focus-visible:opacity-100 data-[state=open]:opacity-100 pointer-coarse:opacity-100",
            COARSE_HIT_AREA,
            FOCUS_RING,
          )}
        >
          <PencilIcon className="size-3" aria-hidden="true" />
        </button>
      </PopoverTrigger>
      <PopoverContent
        className="w-56 p-3"
        align="end"
        onOpenAutoFocus={(e) => {
          e.preventDefault();
          inputRef.current?.focus();
        }}
      >
        <div className="space-y-2">
          <p className="text-xs font-medium text-text-2">
            {title} — {asset.name}
          </p>
          <DecimalInput
            ref={inputRef}
            aria-label={`${title} dla ${asset.name}`}
            value={inputValue}
            onChange={(e) => setInputValue(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") handleSave();
              if (e.key === "Escape") setOpen(false);
            }}
            className="h-8 font-mono text-sm"
            placeholder="0.0"
          />
          {mutation.isError && (
            <p role="alert" className="text-xs text-loss-text">
              {errorMessage(mutation.error)}
            </p>
          )}
          <div className="flex justify-end gap-2">
            <Button type="button" variant="outline" size="sm" className="h-7 text-xs" onClick={() => setOpen(false)} disabled={mutation.isPending}>
              Anuluj
            </Button>
            <Button type="button" size="sm" className="h-7 text-xs" onClick={handleSave} disabled={mutation.isPending}>
              {mutation.isPending ? "..." : "Zapisz"}
            </Button>
          </div>
        </div>
      </PopoverContent>
    </Popover>
  );
}
