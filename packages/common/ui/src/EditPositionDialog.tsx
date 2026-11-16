import { useEffect, useMemo } from "react";
import { useForm, useWatch } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { useMutation } from "@tanstack/react-query";
import { ApiError } from "@common/app-core";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "./dialog.js";
import { Button } from "./button.js";
import { Input } from "./input.js";
import { DecimalInput } from "./decimal-input.js";
import { Label } from "./label.js";
import { Separator } from "./separator.js";
import { Switch } from "./switch.js";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "./select.js";
import { AssetSelect, type AssetOption } from "./AssetSelect.js";

/**
 * 3-way shared edit dialog (task 156, Phase B1 — biggest win: 274–368 lines
 * × 3). Each app injects its `updatePosition` call, its `unitFields` set
 * (keys are the real API field names — the adapter spreads `units` flat, so
 * no reverse mapping exists), its `assetClassOptions` (order + membership
 * differ per app), and the main-only `showCompanyBacked` flag.
 *
 * Snapshot-in, diff-out (rather than `TPosition` + accessors): one ~15-line
 * mapper per app contains all type narrowing (KISS). Form-level `assetClass`
 * stays a plain string — membership is enforced by each app's own
 * backend/Zod, exactly as today.
 *
 * Task 156 (Asset / Subasset model): in portfolioB/portfolioC the dialog edits ONE
 * SUBASSET. Targets live only on the Asset, so those adapters omit
 * `targetAllocationPct` from the snapshot and the "Cel %" input is not
 * rendered at all (asset-level target edits happen on the Asset row —
 * `PUT /assets/{id}`). main still edits targets on the position row (its
 * migration is a later wave) and keeps passing the field. A NON-primary
 * subasset can additionally be re-homed under another Asset (§13.4): pass
 * `assetId`/`isPrimary` in the snapshot and the caller-filtered `assets` list;
 * `assetId` is sent only when the selection actually changed.
 */

export interface UnitFieldDef {
  /** Real API field name, e.g. `unitsIkeTata`. Rendered as input id `edit-${key}`. */
  key: string;
  label: string;
}

export interface AssetClassOption {
  value: string;
  label: string;
}

export interface EditPositionSnapshot {
  id: number;
  ticker: string;
  name: string;
  yahooTicker: string | null;
  assetClass: string;
  companyBacked: boolean;
  /**
   * Position-level target (main only). OMIT for subassets (portfolioB/portfolioC) —
   * `undefined` hides the "Cel %" input; `null` means "editable, currently unset".
   */
  targetAllocationPct?: number | null;
  /** Owning Asset id (portfolioB/portfolioC) — the re-home select's current value. */
  assetId?: number;
  /** Re-home is offered only when this is exactly `false` (primary rows are never re-homed). */
  isPrimary?: boolean;
  /** Keys ⊆ unitFields[].key. */
  unitValues: Record<string, number | null>;
}

export interface EditPositionUpdate {
  name?: string;
  yahooTicker?: string | null;
  assetClass?: string;
  /** Only present when `showCompanyBacked` is true. */
  companyBacked?: boolean;
  targetAllocationPct?: number | null;
  /** Only present when a non-primary subasset was re-homed to another Asset (§13.2). */
  assetId?: number;
  /** Changed unit fields only; the adapter spreads this flat into its request. */
  units: Record<string, number>;
}

export interface EditPositionDialogProps {
  position: EditPositionSnapshot | null;
  onClose: () => void;
  onSuccess: () => void;
  updatePosition: (id: number, body: EditPositionUpdate) => Promise<unknown>;
  unitFields: UnitFieldDef[];
  assetClassOptions: AssetClassOption[];
  showCompanyBacked: boolean;
  /** Assets a non-primary subasset may be re-homed to (caller filters to the same portfolio). Omit => no picker. */
  assets?: AssetOption[];
}

export interface EditPositionFormValues {
  name?: string;
  yahooTicker?: string;
  assetClass?: string;
  companyBacked?: boolean;
  targetAllocationPct?: string;
  /** Selected Asset id as a string (Radix Select value); parsed back to a number on submit. */
  assetId?: string;
  [unitKey: string]: string | boolean | undefined;
}

// ── Helpers ───────────────────────────────────────────────────

function toNumberOrUndefined(val: string | undefined): number | undefined {
  if (val === undefined || val.trim() === "") return undefined;
  const n = parseFloat(val);
  return isNaN(n) ? undefined : n;
}

// ── Sub-components ────────────────────────────────────────────

type FieldRowProps = {
  label: string;
  id: string;
  children: React.ReactNode;
};

function FieldRow({ label, id, children }: FieldRowProps) {
  return (
    <div className="flex flex-col gap-1.5">
      <Label htmlFor={id} className="text-sm font-medium">
        {label}
      </Label>
      {children}
    </div>
  );
}

// ── Main component ────────────────────────────────────────────

export function EditPositionDialog({
  position,
  onClose,
  onSuccess,
  updatePosition,
  unitFields,
  assetClassOptions,
  showCompanyBacked,
  assets,
}: EditPositionDialogProps) {
  const isOpen = position !== null;
  const showTarget = position?.targetAllocationPct !== undefined;
  const canRehome = position?.isPrimary === false && assets != null && assets.length > 0;

  const formSchema = useMemo(
    () =>
      z.object({
        name: z.string().optional(),
        yahooTicker: z.string().optional(),
        assetClass: z.string().optional(),
        companyBacked: z.boolean().optional(),
        targetAllocationPct: z.string().optional(),
        assetId: z.string().optional(),
        ...Object.fromEntries(unitFields.map((f) => [f.key, z.string().optional()])),
      }),
    [unitFields],
  );

  const defaultUnitValues = useMemo(
    () => Object.fromEntries(unitFields.map((f) => [f.key, ""])),
    [unitFields],
  );

  const form = useForm<EditPositionFormValues>({
    resolver: zodResolver(formSchema),
    defaultValues: {
      name: "",
      yahooTicker: "",
      assetClass: undefined,
      companyBacked: undefined,
      targetAllocationPct: "",
      assetId: "",
      ...defaultUnitValues,
    },
  });

  const assetClassValue = useWatch({ control: form.control, name: "assetClass" });
  const assetIdValue = useWatch({ control: form.control, name: "assetId" });
  const companyBackedValue = useWatch({ control: form.control, name: "companyBacked" });

  // Re-populate form whenever the position changes (dialog opens for a new row)
  useEffect(() => {
    if (position === null) return;
    form.reset({
      name: position.name,
      yahooTicker: position.yahooTicker ?? "",
      assetClass: position.assetClass,
      companyBacked: position.companyBacked,
      targetAllocationPct:
        position.targetAllocationPct != null
          ? String(position.targetAllocationPct)
          : "",
      assetId: position.assetId != null ? String(position.assetId) : "",
      ...Object.fromEntries(
        unitFields.map((f) => {
          const v = position.unitValues[f.key];
          return [f.key, v != null ? String(v) : ""];
        }),
      ),
    });
  }, [position, form, unitFields]);

  const mutation = useMutation({
    mutationFn: (body: EditPositionUpdate) => {
      // position is guaranteed non-null when the form is submitted
      return updatePosition(position!.id, body);
    },
    onSuccess: () => {
      onSuccess();
    },
  });

  function handleSubmit(values: EditPositionFormValues) {
    if (position === null) return;

    const body: EditPositionUpdate = { units: {} };

    if (values.name !== undefined && values.name.trim() !== "" && values.name !== position.name) {
      body.name = values.name.trim();
    }

    if (values.yahooTicker !== undefined) {
      const trimmed = values.yahooTicker.trim();
      const currentTicker = position.yahooTicker ?? "";
      if (trimmed !== currentTicker) {
        body.yahooTicker = trimmed === "" ? null : trimmed;
      }
    }

    if (values.assetClass !== undefined && values.assetClass !== position.assetClass) {
      body.assetClass = values.assetClass;
    }

    if (
      showCompanyBacked &&
      values.companyBacked !== undefined &&
      values.companyBacked !== position.companyBacked
    ) {
      body.companyBacked = values.companyBacked;
    }

    if (showTarget) {
      const targetNum = toNumberOrUndefined(values.targetAllocationPct);
      if (targetNum !== undefined && targetNum !== (position.targetAllocationPct ?? undefined)) {
        body.targetAllocationPct = targetNum;
      } else if (
        (values.targetAllocationPct === "" || values.targetAllocationPct === undefined) &&
        position.targetAllocationPct !== null
      ) {
        body.targetAllocationPct = null;
      }
    }

    if (canRehome) {
      const nextAssetId = toNumberOrUndefined(values.assetId);
      if (nextAssetId !== undefined && nextAssetId !== position.assetId) {
        body.assetId = nextAssetId;
      }
    }

    for (const f of unitFields) {
      const num = toNumberOrUndefined(values[f.key] as string | undefined);
      if (num !== undefined && num !== position.unitValues[f.key]) {
        body.units[f.key] = num;
      }
    }

    mutation.mutate(body);
  }

  // 4xx bodies carry an actionable message (e.g. 409 "primary cannot be re-homed", 404 unknown
  // asset, 400 different portfolio) — show it; anything else stays generic.
  const errorMessage = (() => {
    if (!mutation.isError) return null;
    const err: unknown = mutation.error;
    if (err instanceof ApiError && err.status >= 400 && err.status < 500) return err.message;
    return "Wystąpił błąd podczas zapisu. Spróbuj ponownie.";
  })();

  function handleOpenChange(open: boolean) {
    if (!open) {
      mutation.reset();
      onClose();
    }
  }

  return (
    <Dialog open={isOpen} onOpenChange={handleOpenChange}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>
            Edytuj pozycję — {position?.ticker ?? ""}
          </DialogTitle>
        </DialogHeader>

        <form
          onSubmit={form.handleSubmit(handleSubmit)}
          className="space-y-5"
          noValidate
        >
          {/* Section: Metadane */}
          <div className="space-y-3">
            <p className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
              Metadane
            </p>

            <FieldRow label="Nazwa" id="edit-name">
              <Input
                id="edit-name"
                {...form.register("name")}
                placeholder={position?.name}
              />
            </FieldRow>

            <FieldRow label="Yahoo Ticker" id="edit-yahooTicker">
              <Input
                id="edit-yahooTicker"
                {...form.register("yahooTicker")}
                placeholder={position?.yahooTicker ?? "brak"}
                className="font-mono"
              />
            </FieldRow>

            <FieldRow label="Klasa" id="edit-assetClass">
              <Select
                value={assetClassValue ?? ""}
                onValueChange={(val) => {
                  form.setValue("assetClass", val);
                }}
              >
                <SelectTrigger id="edit-assetClass">
                  <SelectValue placeholder="Wybierz klasę..." />
                </SelectTrigger>
                <SelectContent>
                  {assetClassOptions.map((opt) => (
                    <SelectItem key={opt.value} value={opt.value}>
                      {opt.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </FieldRow>

            {canRehome && (
              <FieldRow label="Aktywo" id="edit-assetId">
                <AssetSelect
                  id="edit-assetId"
                  assets={assets}
                  value={toNumberOrUndefined(assetIdValue) ?? null}
                  onChange={(id) => form.setValue("assetId", String(id), { shouldDirty: true })}
                  disabled={mutation.isPending}
                />
              </FieldRow>
            )}

            {showCompanyBacked && (
              <div className="flex items-center justify-between gap-3 rounded-md border p-3">
                <div className="flex flex-col gap-0.5">
                  <Label htmlFor="edit-companyBacked" className="cursor-pointer">
                    Ma własny, śledzalny wątek newsowy
                  </Label>
                  <p className="text-xs text-muted-foreground">
                    Steruje zakresem nocnego researchu newsów. Wyłącz np. dla
                    szerokich ETF-ów indeksowych/surowcowych lub walut/gotówki
                    (brak własnej narracji).
                  </p>
                </div>
                <Switch
                  id="edit-companyBacked"
                  checked={companyBackedValue ?? false}
                  onCheckedChange={(checked) =>
                    form.setValue("companyBacked", checked, { shouldDirty: true })
                  }
                />
              </div>
            )}
          </div>

          <Separator />

          {/* Section: Alokacja i jednostki */}
          <div className="space-y-3">
            <p className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
              {showTarget ? "Alokacja i jednostki" : "Jednostki"}
            </p>

            {showTarget && (
              <FieldRow label="Cel %" id="edit-targetAllocationPct">
                <DecimalInput
                  id="edit-targetAllocationPct"
                  {...form.register("targetAllocationPct")}
                  className="font-mono"
                />
              </FieldRow>
            )}

            {/* Task 161 (design §2 rule 6/7): a manual unit edit never touches the recorded cost. */}
            <p className="text-xs text-muted-foreground">
              Edycja liczby jednostek nie zmienia kosztu.
            </p>

            <div className="grid grid-cols-2 gap-3 sm:grid-cols-3">
              {unitFields.map((f) => (
                <FieldRow key={f.key} label={f.label} id={`edit-${f.key}`}>
                  <DecimalInput
                    id={`edit-${f.key}`}
                    {...form.register(f.key)}
                    className="font-mono"
                  />
                </FieldRow>
              ))}
            </div>
          </div>

          {errorMessage && (
            <p role="alert" className="text-sm text-destructive">
              {errorMessage}
            </p>
          )}

          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={onClose}
              disabled={mutation.isPending}
            >
              Anuluj
            </Button>
            <Button type="submit" disabled={mutation.isPending}>
              {mutation.isPending ? "Zapisywanie..." : "Zapisz"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
