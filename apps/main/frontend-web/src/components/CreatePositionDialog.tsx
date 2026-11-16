import { useEffect } from "react";
import { useForm, useWatch } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { useMutation } from "@tanstack/react-query";
import { ApiError } from "@common/app-core";
import { createPosition, exchangeSchema, assetClassSchema, exposureSchema, currencySchema } from "../api/index.js";
import type { Asset, CreatePositionRequest, PortfolioType } from "../api/index.js";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { DecimalInput } from "@/components/ui/decimal-input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { AssetSelect } from "@common/ui-primitives/AssetSelect";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

// ── Form schema ───────────────────────────────────────────────

// Task 156 (Asset / Subasset model): this dialog creates one SUBASSET (a concrete holding) and
// says which ASSET it belongs to — an existing one of the SAME portfolio (asset picker, `assetId`;
// exposure is inherited from it) or a brand-new one created inline (`newAsset`: name + exposure +
// the class chosen below + optional targets — targets live on the asset, never on the subasset
// row). Replaces the old companion toggle + "Pozycja nadrzędna" ticker dropdown, whose bare-string
// parent had no FK and could orphan a row.

/** Empty -> `null` (no target); otherwise a number within 0..100, else `invalid`. */
function parsePctField(raw: string | undefined): number | null | "invalid" {
  const trimmed = raw?.trim();
  if (!trimmed) return null;
  const n = parseFloat(trimmed);
  return isNaN(n) || n < 0 || n > 100 ? "invalid" : n;
}

const formSchema = z
  .object({
    ticker: z.string().min(1, { error: "Ticker jest wymagany" }),
    name: z.string().min(1, { error: "Nazwa jest wymagana" }),
    exchange: exchangeSchema,
    assetClass: assetClassSchema,
    exposure: exposureSchema.optional(),
    priceCurrency: currencySchema,
    yahooTicker: z.string().optional(),
    companyBacked: z.boolean(),
    attachToAsset: z.boolean(),
    assetId: z.number().int().nullable(),
    assetName: z.string().optional(),
    targetAllocationPct: z.string().optional(),
    targetAllocationPctRiskOff: z.string().optional(),
  })
  .superRefine((values, ctx) => {
    if (values.attachToAsset) {
      // Attaching means naming the asset: an unselected picker would leave the request with
      // neither `assetId` nor `newAsset` (the backend answers 400). An `assetId` always names a
      // real asset (FK + NOT NULL) — the orphan class the old parent-ticker dropdown allowed is gone.
      if (values.assetId == null) {
        ctx.addIssue({ code: z.ZodIssueCode.custom, message: "Wybierz aktywo", path: ["assetId"] });
      }
      return;
    }
    if (values.exposure == null) {
      ctx.addIssue({ code: z.ZodIssueCode.custom, message: "Wybierz ekspozycję", path: ["exposure"] });
    }
    for (const field of ["targetAllocationPct", "targetAllocationPctRiskOff"] as const) {
      if (parsePctField(values[field]) === "invalid") {
        ctx.addIssue({ code: z.ZodIssueCode.custom, message: "Podaj wartość od 0 do 100", path: [field] });
      }
    }
  });

// STOCK/CRYPTO -> true, ETF/CURRENCY -> false. Mirrors the backend's own
// assetClass-derived default (ADR-064) so the toggle starts at a sensible
// value before the user touches it.
function defaultCompanyBackedForAssetClass(
  assetClass: z.infer<typeof assetClassSchema> | undefined,
): boolean {
  return assetClass === "STOCK" || assetClass === "CRYPTO";
}

type FormValues = z.infer<typeof formSchema>;

// ── Option lists derived from actual enum values ──────────────

const EXCHANGE_OPTIONS: Array<{ value: z.infer<typeof exchangeSchema>; label: string }> = [
  { value: "XTB", label: "XTB" },
  { value: "BINANCE", label: "Binance" },
];

const ASSET_CLASS_OPTIONS: Array<{ value: z.infer<typeof assetClassSchema>; label: string }> = [
  { value: "STOCK", label: "Akcja (STOCK)" },
  { value: "ETF", label: "ETF" },
  { value: "CRYPTO", label: "Krypto (CRYPTO)" },
  { value: "CURRENCY", label: "Waluta (CURRENCY)" },
];

// CASH and QUANTUM are deliberately excluded — both are deprecated `exposure_enum` values
// with zero live rows by design (CASH's bucket was removed entirely by ADR-066; QUANTUM has
// carried zero live rows since V37), kept in the enum only because Postgres has no
// `ALTER TYPE ... DROP VALUE`. Offering them here let a real position get misclassified as
// QUANTUM (2026-09-09) since its label reads like a legitimate "quantum computing" category
// rather than the dead bucket it actually is. BONDS/EXPERIMENTS/EQUITY were missing entirely,
// making it impossible to correctly classify most new EQUITY/BONDS/EXPERIMENTS positions.
const EXPOSURE_OPTIONS: Array<{ value: z.infer<typeof exposureSchema>; label: string }> = [
  { value: "CRYPTO", label: "Krypto (CRYPTO)" },
  { value: "METALS", label: "Metale (METALS)" },
  { value: "BONDS", label: "Obligacje (BONDS)" },
  { value: "EXPERIMENTS", label: "Eksperymenty (EXPERIMENTS)" },
  { value: "EQUITY", label: "Akcje (EQUITY)" },
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

type Props = {
  portfolio: PortfolioType;
  open: boolean;
  onClose: () => void;
  onSuccess: () => void;
  /** Existing assets — reused (no extra query) to populate the attach-to-asset picker. Only those of
   *  this dialog's `portfolio` are offered (the backend rejects a cross-portfolio attach with 400). */
  assets: Asset[];
};

export function CreatePositionDialog({
  portfolio,
  open,
  onClose,
  onSuccess,
  assets,
}: Props) {
  const form = useForm<FormValues>({
    resolver: zodResolver(formSchema),
    defaultValues: {
      ticker: "",
      name: "",
      exchange: undefined,
      assetClass: undefined,
      exposure: undefined,
      priceCurrency: undefined,
      yahooTicker: "",
      companyBacked: false,
      attachToAsset: false,
      assetId: null,
      assetName: "",
      targetAllocationPct: "",
      targetAllocationPctRiskOff: "",
    },
  });

  const attachOptions = assets.filter((a) => a.portfolio === portfolio);
  // Live form values (replaces `form.watch(...)`, which the React Compiler cannot memoize).
  const watched = useWatch({ control: form.control });
  const attachToAsset = watched.attachToAsset;

  // Keep the toggle synced to the selected assetClass (STOCK/CRYPTO -> true,
  // ETF/CURRENCY -> false) until the user manually overrides it — e.g. for
  // the BTC-USD-style leaderless exception.
  const assetClass = watched.assetClass;
  useEffect(() => {
    if (form.formState.dirtyFields.companyBacked) return;
    form.setValue("companyBacked", defaultCompanyBackedForAssetClass(assetClass));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [assetClass]);

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
      ticker: values.ticker.trim().toUpperCase(),
      name,
      portfolio,
      exchange: values.exchange,
      assetClass: values.assetClass,
      priceCurrency: values.priceCurrency,
      yahooTicker: values.yahooTicker?.trim() || null,
      companyBacked: values.companyBacked,
    } satisfies Partial<CreatePositionRequest>;

    // Exactly one of `assetId` / `newAsset` is ever sent (the backend rejects both/neither with 400).
    if (values.attachToAsset && values.assetId != null) {
      mutation.mutate({ ...base, assetId: values.assetId });
      return;
    }
    if (values.exposure == null) return; // unreachable — the schema requires it when not attaching
    const target = parsePctField(values.targetAllocationPct);
    // The risk-off target is a MAIN-only concept (ADR-044) — never sent for RETIREMENT assets.
    const targetRiskOff = portfolio === "MAIN" ? parsePctField(values.targetAllocationPctRiskOff) : null;
    mutation.mutate({
      ...base,
      newAsset: {
        name: values.assetName?.trim() || name,
        exposure: values.exposure,
        assetClass: values.assetClass,
        ...(target !== null && target !== "invalid" ? { targetAllocationPct: target } : {}),
        ...(targetRiskOff !== null && targetRiskOff !== "invalid"
          ? { targetAllocationPctRiskOff: targetRiskOff }
          : {}),
      },
    });
  }

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
          <DialogTitle>Dodaj pozycję</DialogTitle>
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

          <FieldRow label="Portfel" id="new-portfolio">
            <Input
              id="new-portfolio"
              value={portfolio}
              readOnly
              className="font-mono bg-muted text-muted-foreground cursor-not-allowed"
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

          {!attachToAsset && (
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
          )}

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

          <div className="flex items-center justify-between gap-3 rounded-md border p-3">
            <div className="flex flex-col gap-0.5">
              <Label htmlFor="new-attachToAsset" className="cursor-pointer">
                Instrument istniejącego aktywa
              </Label>
              <p className="text-xs text-muted-foreground">
                Dodaj go do aktywa, które już masz w portfelu (np. fizyczne ETH obok
                ETN na ETH). Jego wartość wliczy się do aktywa, a cel należy do
                aktywa — nie do instrumentu.
              </p>
            </div>
            <Switch
              id="new-attachToAsset"
              checked={attachToAsset}
              disabled={attachOptions.length === 0}
              onCheckedChange={(checked) => {
                form.setValue("attachToAsset", checked, {
                  shouldDirty: true,
                  shouldValidate: true,
                });
                if (!checked) {
                  form.setValue("assetId", null, { shouldValidate: true });
                }
              }}
            />
          </div>

          {attachToAsset ? (
            <FieldRow label="Aktywo" id="new-assetId" required error={errors.assetId?.message}>
              <AssetSelect
                id="new-assetId"
                assets={attachOptions}
                value={watched.assetId ?? null}
                onChange={(id) => form.setValue("assetId", id, { shouldValidate: true })}
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

              <FieldRow
                label="Cel % aktywa (opcjonalny)"
                id="new-targetAllocationPct"
                error={errors.targetAllocationPct?.message}
              >
                <DecimalInput
                  id="new-targetAllocationPct"
                  {...form.register("targetAllocationPct")}
                  placeholder="0.0"
                  className="font-mono"
                />
              </FieldRow>

              {portfolio === "MAIN" && (
                <FieldRow
                  label="Cel % risk-off (opcjonalny)"
                  id="new-targetAllocationPctRiskOff"
                  error={errors.targetAllocationPctRiskOff?.message}
                >
                  <DecimalInput
                    id="new-targetAllocationPctRiskOff"
                    {...form.register("targetAllocationPctRiskOff")}
                    placeholder="puste = jak risk-on"
                    className="font-mono"
                  />
                </FieldRow>
              )}
            </>
          )}

          <div className="flex items-center justify-between gap-3 rounded-md border p-3">
            <div className="flex flex-col gap-0.5">
              <Label htmlFor="new-companyBacked" className="cursor-pointer">
                Ma własny, śledzalny wątek newsowy
              </Label>
              <p className="text-xs text-muted-foreground">
                Domyślnie wg klasy aktywów (Akcja/Krypto — tak, ETF/Waluta —
                nie). Wyłącz np. dla szerokich ETF-ów indeksowych/surowcowych
                lub walut/gotówki (brak własnej narracji).
              </p>
            </div>
            <Switch
              id="new-companyBacked"
              checked={watched.companyBacked}
              onCheckedChange={(checked) =>
                form.setValue("companyBacked", checked, { shouldDirty: true })
              }
            />
          </div>

          {mutation.isError && (
            <p role="alert" className="text-sm text-destructive">
              {mutation.error instanceof ApiError && mutation.error.status === 409
                ? "Taka pozycja już istnieje (ten sam ticker, giełda i portfel)."
                : mutation.error instanceof ApiError
                  ? mutation.error.message
                  : "Wystąpił błąd podczas tworzenia pozycji. Spróbuj ponownie."}
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
