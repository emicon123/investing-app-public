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
import { Checkbox } from "./checkbox.js";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "./select.js";
import { AssetSelect, type AssetOption } from "./AssetSelect.js";

/**
 * portfolioB/portfolioC only — investing-app's CreatePositionDialog covers a much
 * broader asset roster/field set (crypto exposure, more asset classes) and is
 * not part of this extraction (see task 107; same precedent as LoginPage.tsx
 * and PortfolioValueLine.tsx).
 *
 * The two apps' request/enum schemas live in per-app `shared-ts` packages
 * (`@common/app-core` + per-app `src/api`), so this component can't import
 * either directly — it redeclares the same enum literal sets locally (they are
 * byte-identical between the two apps today) and takes the actual
 * `createPosition` API call as an injected prop instead.
 *
 * Task 156 (Asset / Subasset model): this dialog creates one SUBASSET (a concrete holding) and
 * says which ASSET it belongs to — either an existing one (asset picker, `assetId`) or a brand-new
 * one created inline (`newAsset`: name + the exposure/class chosen below + optional target — the
 * target lives on the asset, never on the subasset row). Replaces the old companion toggle +
 * "Pozycja nadrzędna" ticker dropdown (a bare-string parent with no FK could orphan a row).
 */

// ── Enum schemas (mirrors portfolioB's/portfolio-c's shared-ts `schemas/enums.ts`) ────

const exchangeSchema = z.enum(["XTB", "BINANCE", "OTHER"]);
const assetClassSchema = z.enum(["ETF", "METALS", "CRYPTO", "STOCK", "CURRENCY"]);
const exposureSchema = z.enum(["CRYPTO", "METALS", "STOCKS", "COMMODITIES", "CURRENCY"]);
const currencySchema = z.enum(["PLN", "EUR", "USD", "CHF", "GBP"]);

// ── Injected request shape ──────────────────────────────────────────────

/** Inline asset payload sent when the subasset opens a brand-new asset (no `assetId`). */
export interface NewAssetRequestLike {
  name: string;
  exposure: z.infer<typeof exposureSchema>;
  assetClass: z.infer<typeof assetClassSchema>;
  targetAllocationPct?: number;
}

export interface CreatePositionRequestLike {
  portfolio?: string;
  ticker: string;
  name: string;
  exchange: z.infer<typeof exchangeSchema>;
  assetClass: z.infer<typeof assetClassSchema>;
  exposure: z.infer<typeof exposureSchema>;
  priceCurrency: z.infer<typeof currencySchema>;
  yahooTicker?: string | null;
  /** Attach to this existing asset. Exactly one of `assetId` / `newAsset` is ever set. */
  assetId?: number;
  /** Create a new asset with this subasset as its (primary) first member. */
  newAsset?: NewAssetRequestLike;
}

/** An asset the new subasset can be attached to — exposure/class prefill the still-empty form fields. */
export interface CreatePositionAssetOptionLike extends AssetOption {
  exposure: string;
  assetClass: string;
}

// ── Form schema ───────────────────────────────────────────────

const formSchema = z
  .object({
    ticker: z.string().min(1, "Ticker jest wymagany"),
    name: z.string().min(1, "Nazwa jest wymagana"),
    exchange: exchangeSchema,
    assetClass: assetClassSchema,
    exposure: exposureSchema,
    priceCurrency: currencySchema,
    yahooTicker: z.string().optional(),
    attachToAsset: z.boolean().default(false),
    assetId: z.number().int().nullable(),
    assetName: z.string().optional(),
    targetAllocationPct: z.string().optional(),
  })
  .superRefine((values, ctx) => {
    // Attaching means naming the asset: an unselected picker would leave the request with neither
    // `assetId` nor `newAsset` (the backend answers 400). An `assetId` always names a real asset
    // (FK + NOT NULL) — the bare-string orphan class this dialog used to allow is gone.
    if (values.attachToAsset && values.assetId == null) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        message: "Wybierz aktywo",
        path: ["assetId"],
      });
    }
  });

type FormValues = z.infer<typeof formSchema>;

// ── Option lists derived from actual enum values ──────────────

const EXCHANGE_OPTIONS: Array<{ value: z.infer<typeof exchangeSchema>; label: string }> = [
  { value: "XTB", label: "XTB" },
  { value: "BINANCE", label: "Binance" },
  { value: "OTHER", label: "Inne (np. fizyczne, fundusze)" },
];

const ASSET_CLASS_OPTIONS: Array<{ value: z.infer<typeof assetClassSchema>; label: string }> = [
  { value: "ETF", label: "ETF" },
  { value: "STOCK", label: "Akcja (STOCK)" },
  { value: "CRYPTO", label: "Krypto (CRYPTO)" },
  { value: "CURRENCY", label: "Waluta (CURRENCY)" },
  { value: "METALS", label: "Metale (METALS)" },
];

const EXPOSURE_OPTIONS: Array<{ value: z.infer<typeof exposureSchema>; label: string }> = [
  { value: "CRYPTO", label: "Krypto" },
  { value: "METALS", label: "Metale" },
  { value: "STOCKS", label: "Akcje" },
  { value: "COMMODITIES", label: "Surowce" },
  { value: "CURRENCY", label: "Waluta" },
];

const CURRENCY_OPTIONS: Array<{ value: z.infer<typeof currencySchema>; label: string }> = [
  { value: "PLN", label: "PLN" },
  { value: "USD", label: "USD" },
  { value: "EUR", label: "EUR" },
  { value: "CHF", label: "CHF" },
  { value: "GBP", label: "GBP" },
];

// ── Sub-components ────────────────────────────────────────────

type FieldRowProps = {
  label: string;
  id: string;
  required?: boolean;
  children: React.ReactNode;
  error?: string;
};

function FieldRow({ label, id, required, children, error }: FieldRowProps) {
  return (
    <div className="flex flex-col gap-1.5">
      <Label htmlFor={id} className="text-sm font-medium">
        {label}
        {required && <span className="text-destructive ml-1">*</span>}
      </Label>
      {children}
      {error && (
        <p role="alert" className="text-xs text-destructive">
          {error}
        </p>
      )}
    </div>
  );
}

// ── Main component ────────────────────────────────────────────

export interface CreatePositionDialogProps {
  open: boolean;
  onClose: () => void;
  onSuccess: () => void;
  /** Actual API call, bound per-app (e.g. `createPosition` from the app's own `src/api`). */
  createPosition: (body: CreatePositionRequestLike) => Promise<unknown>;
  /** portfolioC passes its current portfolio; portfolioB omits this and the request body omits the field. */
  portfolio?: string;
  /** portfolioC passes `PORTFOLIO_LABELS[portfolio]`, pre-formatted; portfolioB omits this. */
  titleSuffix?: string;
  /** Existing assets in this portfolio — reused (no extra query) to populate the
   *  attach-to-asset picker. */
  assets: CreatePositionAssetOptionLike[];
}

export function CreatePositionDialog({
  open,
  onClose,
  onSuccess,
  createPosition,
  portfolio,
  titleSuffix,
  assets,
}: CreatePositionDialogProps) {
  const form = useForm<z.input<typeof formSchema>, unknown, FormValues>({
    resolver: zodResolver(formSchema),
    defaultValues: {
      ticker: "",
      name: "",
      exchange: undefined,
      assetClass: undefined,
      exposure: undefined,
      priceCurrency: undefined,
      yahooTicker: "",
      attachToAsset: false,
      assetId: null,
      assetName: "",
      targetAllocationPct: "",
    },
  });

  // Live form values (replaces `form.watch(...)`, which the React Compiler cannot memoize).
  const watched = useWatch({ control: form.control });
  const attachToAsset = watched.attachToAsset;

  // Picking an asset pre-fills exposure / class from it — but only the still-empty fields, so a
  // deliberate choice is never overwritten. (Subassets of one asset share its exposure.)
  function handleAssetChange(assetId: number) {
    form.setValue("assetId", assetId, { shouldValidate: true });
    const asset = assets.find((a) => a.id === assetId);
    if (!asset) return;
    const exposure = exposureSchema.safeParse(asset.exposure);
    if (exposure.success && !form.getValues("exposure")) {
      form.setValue("exposure", exposure.data, { shouldValidate: true });
    }
    const assetClass = assetClassSchema.safeParse(asset.assetClass);
    if (assetClass.success && !form.getValues("assetClass")) {
      form.setValue("assetClass", assetClass.data, { shouldValidate: true });
    }
  }

  const mutation = useMutation({
    mutationFn: createPosition,
    onSuccess: () => {
      form.reset();
      onSuccess();
      onClose();
    },
    onError: (err: unknown) => {
      if (err instanceof ApiError && err.status === 409) {
        onSuccess();
      }
    },
  });

  function handleSubmit(values: FormValues) {
    if (mutation.isPending) return;
    const name = values.name.trim();
    const base = {
      portfolio,
      ticker: values.ticker.trim().toUpperCase(),
      name,
      exchange: values.exchange,
      assetClass: values.assetClass,
      exposure: values.exposure,
      priceCurrency: values.priceCurrency,
      yahooTicker: values.yahooTicker?.trim() || null,
    };

    if (values.attachToAsset && values.assetId != null) {
      mutation.mutate({ ...base, assetId: values.assetId });
      return;
    }

    const targetNum = values.targetAllocationPct?.trim()
      ? parseFloat(values.targetAllocationPct)
      : undefined;
    mutation.mutate({
      ...base,
      newAsset: {
        name: values.assetName?.trim() || name,
        exposure: values.exposure,
        assetClass: values.assetClass,
        ...(targetNum != null && !isNaN(targetNum) ? { targetAllocationPct: targetNum } : {}),
      },
    });
  }

  const errorMessage = (() => {
    const err = mutation.error as unknown;
    if (err instanceof ApiError) {
      if (err.status === 409 || err.errors.some((e) => e.code === "DUPLICATE_ENTRY")) {
        return "Pozycja o tym tickerze już istnieje w tym portfelu — lista została odświeżona.";
      }
      return err.errors[0]?.message ?? "Wystąpił błąd podczas tworzenia pozycji. Spróbuj ponownie.";
    }
    if (mutation.isError) return "Wystąpił błąd podczas tworzenia pozycji. Spróbuj ponownie.";
    return null;
  })();

  function handleOpenChange(o: boolean) {
    if (!o) {
      mutation.reset();
      form.reset();
      onClose();
    }
  }

  const errors = form.formState.errors;

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className="max-w-lg max-h-[90vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Dodaj pozycję{titleSuffix ? ` — ${titleSuffix}` : ""}</DialogTitle>
        </DialogHeader>

        <form
          onSubmit={form.handleSubmit(handleSubmit)}
          className="space-y-4"
          noValidate
        >
          <FieldRow label="Ticker" id="new-ticker" required error={errors.ticker?.message}>
            <Input
              id="new-ticker"
              {...form.register("ticker")}
              placeholder="np. MSFT"
              className="font-mono uppercase"
              autoComplete="off"
            />
          </FieldRow>

          <FieldRow label="Nazwa" id="new-name" required error={errors.name?.message}>
            <Input
              id="new-name"
              {...form.register("name")}
              placeholder="np. Microsoft Corp"
              autoComplete="off"
            />
          </FieldRow>

          <FieldRow label="Giełda" id="new-exchange" required error={errors.exchange?.message}>
            <Select
              value={watched.exchange ?? ""}
              onValueChange={(val) => form.setValue("exchange", val as z.infer<typeof exchangeSchema>, { shouldValidate: true })}
            >
              <SelectTrigger id="new-exchange">
                <SelectValue placeholder="Wybierz giełdę..." />
              </SelectTrigger>
              <SelectContent>
                {EXCHANGE_OPTIONS.map((opt) => (
                  <SelectItem key={opt.value} value={opt.value}>
                    {opt.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </FieldRow>

          <FieldRow label="Klasa aktywów" id="new-assetClass" required error={errors.assetClass?.message}>
            <Select
              value={watched.assetClass ?? ""}
              onValueChange={(val) => form.setValue("assetClass", val as z.infer<typeof assetClassSchema>, { shouldValidate: true })}
            >
              <SelectTrigger id="new-assetClass">
                <SelectValue placeholder="Wybierz klasę..." />
              </SelectTrigger>
              <SelectContent>
                {ASSET_CLASS_OPTIONS.map((opt) => (
                  <SelectItem key={opt.value} value={opt.value}>
                    {opt.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </FieldRow>

          <FieldRow label="Ekspozycja" id="new-exposure" required error={errors.exposure?.message}>
            <Select
              value={watched.exposure ?? ""}
              onValueChange={(val) => form.setValue("exposure", val as z.infer<typeof exposureSchema>, { shouldValidate: true })}
            >
              <SelectTrigger id="new-exposure">
                <SelectValue placeholder="Wybierz ekspozycję..." />
              </SelectTrigger>
              <SelectContent>
                {EXPOSURE_OPTIONS.map((opt) => (
                  <SelectItem key={opt.value} value={opt.value}>
                    {opt.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </FieldRow>

          <FieldRow label="Waluta ceny" id="new-priceCurrency" required error={errors.priceCurrency?.message}>
            <Select
              value={watched.priceCurrency ?? ""}
              onValueChange={(val) => form.setValue("priceCurrency", val as z.infer<typeof currencySchema>, { shouldValidate: true })}
            >
              <SelectTrigger id="new-priceCurrency">
                <SelectValue placeholder="Wybierz walutę..." />
              </SelectTrigger>
              <SelectContent>
                {CURRENCY_OPTIONS.map((opt) => (
                  <SelectItem key={opt.value} value={opt.value}>
                    {opt.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </FieldRow>

          <FieldRow label="Yahoo Ticker (opcjonalny)" id="new-yahooTicker">
            <Input
              id="new-yahooTicker"
              {...form.register("yahooTicker")}
              placeholder="np. MSFT"
              className="font-mono"
              autoComplete="off"
            />
          </FieldRow>

          <div className="flex items-center gap-2 pt-1">
            <Checkbox
              id="new-attachToAsset"
              checked={attachToAsset}
              disabled={assets.length === 0}
              onCheckedChange={(checked) => {
                form.setValue("attachToAsset", !!checked, { shouldValidate: true });
                if (!checked) {
                  form.setValue("assetId", null, { shouldValidate: true });
                }
              }}
            />
            <Label htmlFor="new-attachToAsset" className="text-sm font-medium cursor-pointer">
              Dodaj jako instrument istniejącego aktywa
            </Label>
          </div>

          {attachToAsset ? (
            <FieldRow label="Aktywo" id="new-assetId" required error={errors.assetId?.message}>
              <AssetSelect
                id="new-assetId"
                assets={assets}
                value={watched.assetId ?? null}
                onChange={handleAssetChange}
                disabled={mutation.isPending}
              />
            </FieldRow>
          ) : (
            <>
              <FieldRow label="Nazwa aktywa (opcjonalna)" id="new-assetName">
                <Input
                  id="new-assetName"
                  {...form.register("assetName")}
                  placeholder={watched.name || "domyślnie nazwa instrumentu"}
                  autoComplete="off"
                />
              </FieldRow>

              <FieldRow label="Cel % aktywa (opcjonalny)" id="new-targetAllocationPct">
                <DecimalInput
                  id="new-targetAllocationPct"
                  {...form.register("targetAllocationPct")}
                  placeholder="0.0"
                  className="font-mono"
                />
              </FieldRow>
            </>
          )}

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
              {mutation.isPending ? "Tworzenie..." : "Dodaj"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
