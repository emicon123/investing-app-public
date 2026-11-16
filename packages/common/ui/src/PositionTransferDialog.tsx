import { useEffect, useMemo, useState } from "react";
import {
  useForm,
  useFieldArray,
  useFormContext,
  useWatch,
  FormProvider,
} from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { useMutation } from "@tanstack/react-query";
import { PlusIcon, Trash2Icon } from "lucide-react";
import {
  formatPln,
  idempotencyErrorMessage,
  useIdempotentSubmit,
  type IdempotencyOptions,
} from "@common/app-core";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "./dialog.js";
import { Button } from "./button.js";
import { DecimalInput } from "./decimal-input.js";
import { Label } from "./label.js";
import { Badge } from "./badge.js";
import { Separator } from "./separator.js";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "./select.js";
import { cn } from "./lib/utils.js";
import { computeTrimExcessPln } from "./portfolio/model/rebalanceStatus.js";

/**
 * Task 136 — shared "sell + buy" transfer dialog, extracted from three near-duplicate
 * per-app components (root's RebalancingTransferDialog.tsx, portfolioB's and portfolioC's
 * PositionTransferDialog.tsx). Contract: docs/architecture/diagrams/136-shared-transfer-dialog.md.
 *
 * Written fresh, not copy-pasted from any one app — the account-dimension branching
 * (root/portfolio-b have a "Konto" selector over IKE/IKZE/regular sub-accounts, portfolioC has
 * none) and the opaque-id leg shape are new structure. Each app adapts its own
 * `Position`/`Asset` + `Subasset` types into `TransferAssetLike` and injects the real
 * submit call — see each app's `components/{RebalancingTransferDialog,PositionTransferDialog}.tsx`
 * wrapper.
 */

// ── Public type contract (see task file §2 for the frozen shape) ──────────────

/** One concrete holding (task 156: a Subasset) — a fully symmetric sell/buy candidate. */
export interface TransferSubassetLike {
  ticker: string;
  name: string;
  pricePln: number | null;
  /** account-id -> held units. Omit entirely when the app has no account dimension (portfolioC). */
  unitsByAccount?: Record<string, number>;
  /** flat held units — read only when accountDimension is undefined. */
  units?: number;
}

/**
 * The clicked sell target / one entry of the buyable list — task 156: one ASSET. Its own
 * `ticker`/`name`/`pricePln`/units are the PRIMARY subasset's (the default the dialog
 * pre-selects); `targetAllocationPct`/`currentPct`/`valuePln` are the Asset's group-level numbers
 * (valuePln = Σ subassets), which is what the room-to-target math reads.
 */
export interface TransferAssetLike extends TransferSubassetLike {
  targetAllocationPct: number | null;
  currentPct: number | null;
  valuePln: number;
  /** The asset's subassets, primary first. */
  subassets?: TransferSubassetLike[] | null;
}

export interface TransferAccountOption {
  id: string;
  label: string;
}

export interface TransferAccountDimension {
  options: TransferAccountOption[];
  /** ADR-054-style wrapper lock: given the just-sold account id, which account ids are valid buy
   *  destinations. Omit for "every account is a valid destination" (portfolioB's current behavior). */
  allowedBuyAccountIds?: (soldAccountId: string) => string[];
  /** per-account unit display formatting. Defaults to `units.toFixed(4)`. */
  formatUnits?: (units: number) => string;
}

export interface TransferDialogCopy {
  dialogTitlePrefix: string;
  soldSectionLabel: string;
  buySectionLabel: string;
  soldHelperText: string | null;
  submitLabel: string;
  submitPendingLabel: string;
}

/** `accountType` is an OPAQUE string the shared component never interprets beyond "the id
 *  selected in the Konto <Select>". A composite id like root's "REGULAR_EUR" is split back into
 *  the real `{accountType:"REGULAR", regularCurrency:"EUR"}` wire shape inside root's own
 *  `submitTransfer` closure, not here. */
export interface PositionTransferLegLike {
  ticker: string;
  accountType?: string;
  units: number;
  pricePln: number;
}

export interface PositionTransferRequestLike {
  portfolio?: string;
  sold: PositionTransferLegLike;
  buys: PositionTransferLegLike[];
}

export interface PositionTransferDialogProps {
  /** The clicked asset, or null when closed. No separate `open` prop. */
  position: TransferAssetLike | null;
  /** Every buyable/sellable asset, already scoped to the right portfolio by the caller. */
  allPositions: TransferAssetLike[];
  /** undefined => no "Konto" selector rendered on either leg (portfolioC). */
  accountDimension?: TransferAccountDimension;
  /** included as `portfolio` in the request body when present; omitted when undefined (portfolioB). */
  portfolioId?: string;
  /** the actual POST call, bound per-app. The dialog owns the mutation and so the Idempotency-Key
   *  (ADR-111): one key per open, reused on retry, rotated on success/close; the app forwards
   *  `options` to its client function. */
  submitTransfer: (body: PositionTransferRequestLike, options: IdempotencyOptions) => Promise<unknown>;
  copy?: Partial<TransferDialogCopy>;
  /** fraction of soldTotal beyond which the buy/sell imbalance is visually flagged.
   *  Default 0.01 (portfolioB/portfolioC's current 1%); root passes 0.05 (its current 5%). */
  imbalanceTolerancePct?: number;
  onClose: () => void;
  onSuccess: () => void;
}

const DEFAULT_COPY: TransferDialogCopy = {
  dialogTitlePrefix: "Sprzedaj",
  soldSectionLabel: "Sprzedane na XTB",
  buySectionLabel: "Kupione za uzyskane środki",
  soldHelperText:
    "Sugerowana ilość sprowadza pozycję do dokładnie 1.0x celu — możesz ją dowolnie zmienić. Kwota podpowiadana z ceny rynkowej wybranego instrumentu.",
  submitLabel: "Zapisz transfer",
  submitPendingLabel: "Zapisywanie...",
};

const DEFAULT_IMBALANCE_TOLERANCE_PCT = 0.01;

// ── Form schema — collects transaction totals (money received/paid); the per-unit
// pricePln the API requires is derived at submit time as amountPln / units. ──────────

const positiveNumberString = z.string().refine((v) => {
  const n = parseFloat(v);
  return v.trim() !== "" && !isNaN(n) && n > 0;
}, "Musi być liczbą dodatnią");

const buyLegFormSchema = z.object({
  ticker: z.string().min(1, "Wybierz aktywo"),
  accountType: z.string().optional(),
  units: positiveNumberString,
  amountPln: positiveNumberString,
});

const formSchema = z.object({
  soldAccountType: z.string().optional(),
  soldUnits: positiveNumberString,
  soldAmountPln: positiveNumberString,
  buys: z.array(buyLegFormSchema).min(1, "Dodaj co najmniej jedną pozycję zakupu"),
});

type FormValues = z.infer<typeof formSchema>;

// ── Internal helpers ─────────────────────────────────────────────────────────

function parseNum(v: string): number {
  const n = parseFloat(v);
  return isNaN(n) ? 0 : n;
}

// Units: crypto-friendly precision (up to 8 decimals).
function formatNumberInput(n: number): string {
  if (!isFinite(n) || n <= 0) return "";
  return String(Number(n.toFixed(8)));
}

// PLN amounts: currency-style precision (2 decimals).
function formatAmountInput(n: number): string {
  if (!isFinite(n) || n <= 0) return "";
  return String(Number(n.toFixed(2)));
}

// Every top-level entry is one Asset whose valuePln already folds in ALL its subassets, so
// summing them counts each subasset exactly once.
// Exported for the task-170 equality test: the portfolio table's `totalPln` is the same sum.
export function computeTotalPortfolioValuePln(positions: TransferAssetLike[]): number {
  return positions.reduce((sum, p) => sum + p.valuePln, 0);
}

// An asset's subassets (primary first), or null when it has none listed (a bare asset) —
// callers then fall back to the asset's own ticker.
function subassetsOf(position: TransferAssetLike): TransferSubassetLike[] | null {
  const subs = position.subassets;
  return subs && subs.length > 0 ? subs : null;
}

// ADR-063 / task 156: every subasset of the clicked asset — the primary AND every other holding
// (ETN wrapper, physical, second ETF…) — is a fully symmetric sell candidate, not just the ticker
// that was clicked to open the dialog.
function buildSellCandidates(position: TransferAssetLike): TransferSubassetLike[] {
  return subassetsOf(position) ?? [position];
}

function getUnitsForAccount(candidate: TransferSubassetLike, accountId: string): number {
  return candidate.unitsByAccount?.[accountId] ?? 0;
}

type AccountOptionWithUnits = TransferAccountOption & { units: number };

function availableAccountOptions(
  candidate: TransferSubassetLike,
  options: TransferAccountOption[],
): AccountOptionWithUnits[] {
  const withUnits = options.map((o) => ({ ...o, units: getUnitsForAccount(candidate, o.id) }));
  const held = withUnits.filter((o) => o.units > 0).sort((a, b) => b.units - a.units);
  return held.length > 0 ? held : withUnits;
}

// Room-to-target is an Asset-level quantity (target % already covers the whole asset — every
// subasset), so every ticker in an asset's subassets shares the same gap.
// excessPln floored at 0; a non-positive market price yields 0 suggested units (not a fallback
// price of 1 — see task 136 §1, this is a deliberate fix over root's old behavior).
// The excess comes from `computeTrimExcessPln`, the same function the portfolio table's SELL figure
// calls (task 170, ADR-048 §7.5), so the two cannot drift apart. Exported for that equality test.
export function computeSuggestedSellUnits(
  position: TransferAssetLike,
  totalPortfolioValuePln: number,
  candidatePricePln: number | null,
): number {
  const excessPln = computeTrimExcessPln(
    position.valuePln,
    position.targetAllocationPct ?? 0,
    totalPortfolioValuePln,
  );
  if (excessPln <= 0) return 0;
  if (candidatePricePln == null || candidatePricePln <= 0) return 0;
  const raw = excessPln / candidatePricePln;
  return Math.max(0, parseFloat(raw.toFixed(4)));
}

type TickerOption = { ticker: string; name: string; pricePln: number | null };

function buildTickerOptions(positions: TransferAssetLike[]): TickerOption[] {
  const options: TickerOption[] = [];
  for (const p of positions) {
    const subs = subassetsOf(p);
    if (subs) {
      for (const sub of subs) {
        options.push({ ticker: sub.ticker, name: sub.name, pricePln: sub.pricePln });
      }
    } else {
      options.push({ ticker: p.ticker, name: p.name, pricePln: p.pricePln });
    }
  }
  return options;
}

type TickerTargetInfo = {
  targetPct: number | null;
  currentPct: number | null;
  /** targetValuePln - currentValuePln for the whole asset. Positive = room to buy. */
  gapPln: number | null;
};

function buildTickerTargetMap(
  positions: TransferAssetLike[],
  totalPortfolioValuePln: number,
): Map<string, TickerTargetInfo> {
  const map = new Map<string, TickerTargetInfo>();
  for (const p of positions) {
    const targetPct = p.targetAllocationPct;
    const currentPct = p.currentPct;
    const gapPln = targetPct != null ? (targetPct / 100) * totalPortfolioValuePln - p.valuePln : null;
    const info: TickerTargetInfo = { targetPct, currentPct, gapPln };
    const subs = subassetsOf(p) ?? [{ ticker: p.ticker }];
    for (const sub of subs) {
      map.set(sub.ticker, info);
    }
  }
  return map;
}

// ── Sub-components ────────────────────────────────────────────

type FieldRowProps = {
  label: string;
  id: string;
  error?: string;
  children: React.ReactNode;
};

function FieldRow({ label, id, error, children }: FieldRowProps) {
  return (
    <div className="flex flex-col gap-1.5">
      <Label htmlFor={id} className="text-xs text-muted-foreground">
        {label}
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

// Pure display — derives price-per-unit from the amount the user actually typed, never an input.
function UnitPriceHint({ units, amountPln }: { units: number; amountPln: number }) {
  if (!(units > 0) || !(amountPln > 0)) return null;
  const price = amountPln / units;
  if (!isFinite(price) || isNaN(price)) return null;
  return <span className="text-xs text-muted-foreground">Cena: {formatPln(price)} / jedn.</span>;
}

type BuyLegRowProps = {
  index: number;
  tickerOptions: TickerOption[];
  tickerTargetMap: Map<string, TickerTargetInfo>;
  /** null => no Konto selector rendered on this row (portfolioC). */
  accountOptions: TransferAccountOption[] | null;
  soldTotal: number;
  buysTotal: number;
  canRemove: boolean;
  onRemove: () => void;
};

function BuyLegRow({
  index,
  tickerOptions,
  tickerTargetMap,
  accountOptions,
  soldTotal,
  buysTotal,
  canRemove,
  onRemove,
}: BuyLegRowProps) {
  const {
    register,
    setValue,
    watch,
    formState: { errors, dirtyFields },
  } = useFormContext<FormValues>();

  const ticker = watch(`buys.${index}.ticker`);
  const accountType = watch(`buys.${index}.accountType`);
  const units = watch(`buys.${index}.units`);
  const amountPln = watch(`buys.${index}.amountPln`);
  const rowErrors = errors.buys?.[index];
  const marketPricePln = tickerOptions.find((o) => o.ticker === ticker)?.pricePln ?? null;

  function handleTickerChange(value: string) {
    setValue(`buys.${index}.ticker`, value, { shouldValidate: true });
  }

  // Prefill the spent amount from the market price until the user edits it manually — never
  // clobbers a value the user already typed in (the polling-stability fix, task 136).
  useEffect(() => {
    if (marketPricePln == null || !(marketPricePln > 0)) return;
    if (dirtyFields.buys?.[index]?.amountPln) return;
    const u = parseNum(units);
    if (u > 0) {
      setValue(`buys.${index}.amountPln`, formatAmountInput(u * marketPricePln));
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ticker, units, marketPricePln, index]);

  const targetInfo = tickerTargetMap.get(ticker);
  const thisLegAmount = parseNum(amountPln);
  const otherLegsTotal = buysTotal - thisLegAmount;
  const remainingPln = Math.max(0, soldTotal - otherLegsTotal);
  const gapPln = targetInfo?.gapPln;
  const fillToTargetAmount = gapPln != null && gapPln > 0 ? Math.min(gapPln, remainingPln) : remainingPln;

  function handleFillToTarget() {
    if (!(fillToTargetAmount > 0)) return;
    setValue(`buys.${index}.amountPln`, formatAmountInput(fillToTargetAmount), {
      shouldValidate: true,
      shouldDirty: true,
    });
    if (marketPricePln != null && marketPricePln > 0) {
      setValue(`buys.${index}.units`, formatNumberInput(fillToTargetAmount / marketPricePln), {
        shouldValidate: true,
        shouldDirty: true,
      });
    }
  }

  return (
    <div className="space-y-1">
      <div
        className={cn(
          "grid gap-2 items-start",
          accountOptions ? "grid-cols-[2fr_1.2fr_1fr_1fr_auto]" : "grid-cols-[2fr_1fr_1fr_auto]",
        )}
      >
        <FieldRow label="Aktywo" id={`buy-ticker-${index}`} error={rowErrors?.ticker?.message}>
          <Select value={ticker} onValueChange={handleTickerChange}>
            <SelectTrigger id={`buy-ticker-${index}`} className="h-9">
              <SelectValue placeholder="Wybierz..." />
            </SelectTrigger>
            <SelectContent>
              {tickerOptions.map((o) => (
                <SelectItem key={o.ticker} value={o.ticker} className="font-mono text-sm">
                  {o.ticker} — {o.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </FieldRow>

        {accountOptions && (
          <FieldRow label="Konto" id={`buy-account-${index}`}>
            <Select
              value={accountType ?? ""}
              onValueChange={(v) => setValue(`buys.${index}.accountType`, v)}
            >
              <SelectTrigger id={`buy-account-${index}`} className="h-9">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {accountOptions.map((a) => (
                  <SelectItem key={a.id} value={a.id}>
                    {a.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </FieldRow>
        )}

        <FieldRow label="Jednostki" id={`buy-units-${index}`} error={rowErrors?.units?.message}>
          <DecimalInput
            id={`buy-units-${index}`}
            className="font-mono h-9"
            {...register(`buys.${index}.units`)}
          />
        </FieldRow>

        <FieldRow label="Zapłacono PLN" id={`buy-amount-${index}`} error={rowErrors?.amountPln?.message}>
          <div className="space-y-0.5">
            <DecimalInput
              id={`buy-amount-${index}`}
              className="font-mono h-9"
              {...register(`buys.${index}.amountPln`)}
            />
            <UnitPriceHint units={parseNum(units)} amountPln={parseNum(amountPln)} />
          </div>
        </FieldRow>

        <Button
          type="button"
          variant="ghost"
          size="sm"
          className="h-9 w-9 p-0 mt-5 text-destructive hover:text-destructive hover:bg-destructive/10 disabled:opacity-30"
          onClick={onRemove}
          disabled={!canRemove}
          aria-label="Usuń pozycję zakupu"
        >
          <Trash2Icon className="size-3.5" aria-hidden="true" />
        </Button>
      </div>

      {ticker && targetInfo && (
        <div className="flex items-center justify-between gap-2 text-xs text-muted-foreground pl-0.5">
          <span>
            Cel: {targetInfo.targetPct != null ? `${targetInfo.targetPct.toFixed(1)}%` : "—"}
            {" · teraz "}
            {targetInfo.currentPct != null ? `${targetInfo.currentPct.toFixed(1)}%` : "—"}
            {" · zapas do celu: "}
            <span
              className={cn(
                "font-mono",
                (targetInfo.gapPln ?? 0) > 0
                  ? "text-emerald-600 dark:text-emerald-400"
                  : "text-amber-600 dark:text-amber-400",
              )}
            >
              {targetInfo.gapPln != null ? formatPln(targetInfo.gapPln) : "—"}
            </span>
          </span>
          {fillToTargetAmount > 0 && (
            <button type="button" onClick={handleFillToTarget} className="text-primary hover:underline shrink-0">
              Wypełnij do celu ({formatPln(fillToTargetAmount)})
            </button>
          )}
        </div>
      )}
    </div>
  );
}

// ── Main component ────────────────────────────────────────────

export function PositionTransferDialog({
  position,
  allPositions,
  accountDimension,
  portfolioId,
  submitTransfer,
  copy: copyProp,
  imbalanceTolerancePct = DEFAULT_IMBALANCE_TOLERANCE_PCT,
  onClose,
  onSuccess,
}: PositionTransferDialogProps) {
  const isOpen = position !== null;
  const copy: TransferDialogCopy = { ...DEFAULT_COPY, ...copyProp };

  const totalPortfolioValuePln = useMemo(
    () => computeTotalPortfolioValuePln(allPositions),
    [allPositions],
  );

  const sellCandidates = useMemo(() => (position ? buildSellCandidates(position) : []), [position]);
  const [soldTicker, setSoldTicker] = useState<string>(position?.ticker ?? "");
  const selectedCandidate = useMemo(() => {
    if (sellCandidates.length === 0) return null;
    return sellCandidates.find((c) => c.ticker === soldTicker) ?? sellCandidates[0] ?? null;
  }, [sellCandidates, soldTicker]);

  const tickerOptions = useMemo(
    () => buildTickerOptions(allPositions).filter((o) => o.ticker !== soldTicker),
    [allPositions, soldTicker],
  );
  const tickerTargetMap = useMemo(
    () => buildTickerTargetMap(allPositions, totalPortfolioValuePln),
    [allPositions, totalPortfolioValuePln],
  );
  const underweightTargets = useMemo(
    () =>
      Array.from(tickerTargetMap.entries())
        .filter(([ticker, info]) => ticker !== soldTicker && info.gapPln != null && info.gapPln > 0)
        .sort((a, b) => (b[1].gapPln ?? 0) - (a[1].gapPln ?? 0)),
    [tickerTargetMap, soldTicker],
  );

  const soldAccountOptions = useMemo(
    () => (accountDimension && selectedCandidate ? availableAccountOptions(selectedCandidate, accountDimension.options) : []),
    [accountDimension, selectedCandidate],
  );

  const methods = useForm<FormValues>({
    resolver: zodResolver(formSchema),
    defaultValues: {
      soldAccountType: undefined,
      soldUnits: "",
      soldAmountPln: "",
      buys: [{ ticker: "", accountType: undefined, units: "", amountPln: "" }],
    },
  });
  const {
    register,
    handleSubmit,
    setValue,
    getValues,
    control,
    formState: { errors, dirtyFields },
  } = methods;

  const { fields, append, remove } = useFieldArray({ control, name: "buys" });

  const idempotentSubmit = useIdempotentSubmit(submitTransfer);
  const mutation = useMutation({
    mutationFn: (body: PositionTransferRequestLike) => idempotentSubmit.submit(body),
    onSuccess: () => {
      methods.reset();
      onSuccess();
    },
  });

  // Sync the sold-ticker selection whenever the dialog opens for a (possibly new) position.
  // Adjusted during render (React's "reset state when a prop changes" pattern) rather than in an
  // effect: same end state, without the extra commit with the stale selection.
  const positionTicker = position?.ticker;
  const [syncedPositionTicker, setSyncedPositionTicker] = useState(positionTicker);
  if (positionTicker !== syncedPositionTicker) {
    setSyncedPositionTicker(positionTicker);
    if (positionTicker !== undefined) setSoldTicker(positionTicker);
  }

  // Full reset — re-picks the default account, clears buy legs, re-suggests units/price — only
  // for a genuinely new sell target: the dialog opening for a new position, or the user
  // explicitly switching which sell-candidate ticker they're selling. Deliberately NOT keyed on
  // totalPortfolioValuePln/allPositions: those come from a query that polls in the background —
  // including them here would re-fire this effect (and its unconditional reset()) on every poll
  // tick, silently reverting the account selection and wiping out anything the user had already
  // typed. This is the polling-stability fix (task 136) — see the two effects below for how live
  // price/value refreshes are handled instead (only updating fields the user hasn't touched yet,
  // via dirtyFields).
  useEffect(() => {
    if (position === null || selectedCandidate === null) return;
    const accountOptions = accountDimension
      ? availableAccountOptions(selectedCandidate, accountDimension.options)
      : [];
    const initialAccountId = accountOptions[0]?.id;
    const heldUnits = accountDimension
      ? (initialAccountId ? getUnitsForAccount(selectedCandidate, initialAccountId) : 0)
      : (selectedCandidate.units ?? 0);
    const suggestedUnits = Math.min(
      computeSuggestedSellUnits(position, totalPortfolioValuePln, selectedCandidate.pricePln),
      heldUnits,
    );
    const pricePln = selectedCandidate.pricePln ?? 0;
    methods.reset({
      soldAccountType: initialAccountId,
      soldUnits: formatNumberInput(suggestedUnits),
      soldAmountPln: formatAmountInput(pricePln > 0 ? suggestedUnits * pricePln : 0),
      buys: [{ ticker: "", accountType: initialAccountId, units: "", amountPln: "" }],
    });
    mutation.reset();
    // Keyed on the primitive ticker, not the `position`/`selectedCandidate` object references:
    // each app's wrapper adapts its own Asset type into TransferAssetLike inline on every
    // render (unmemoized is fine — allPositions must legitimately produce a new array every poll
    // tick), which means `position` itself gets a new object identity on every parent re-render
    // even when the user's selection hasn't changed. Keying this effect on that reference would
    // silently reintroduce the exact poll-driven reset bug this extraction exists to fix one
    // layer up. `position?.ticker` only changes when the user actually picks a new sell target.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [position?.ticker, soldTicker]);

  const soldUnitsWatch = useWatch({ control, name: "soldUnits" });
  const soldAmountWatch = useWatch({ control, name: "soldAmountPln" });
  const soldAccountTypeWatch = useWatch({ control, name: "soldAccountType" });

  // Keep the suggested sell-units figure fresh as totalPortfolioValuePln updates from background
  // polling (prices moved since the dialog opened) — but only while the user hasn't touched the
  // field themselves.
  useEffect(() => {
    if (position === null || selectedCandidate === null) return;
    if (dirtyFields.soldUnits) return;
    const accountId = getValues("soldAccountType");
    const heldUnits = accountDimension
      ? (accountId ? getUnitsForAccount(selectedCandidate, accountId) : 0)
      : (selectedCandidate.units ?? 0);
    const suggestedUnits = Math.min(
      computeSuggestedSellUnits(position, totalPortfolioValuePln, selectedCandidate.pricePln),
      heldUnits,
    );
    setValue("soldUnits", formatNumberInput(suggestedUnits));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [totalPortfolioValuePln, position, selectedCandidate, dirtyFields.soldUnits]);

  // Keep the received-amount figure derived from (current soldUnits × market price) — but only
  // while the user hasn't typed their own amount.
  useEffect(() => {
    if (dirtyFields.soldAmountPln) return;
    const price = selectedCandidate?.pricePln;
    if (price == null || price <= 0) return;
    const u = parseNum(soldUnitsWatch);
    if (u > 0) {
      setValue("soldAmountPln", formatAmountInput(u * price));
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [soldUnitsWatch, selectedCandidate, dirtyFields.soldAmountPln]);

  // ADR-054-style wrapper lock: if the sold account changes to one that invalidates an
  // already-selected buy-leg account, reset those now-invalid selections rather than leaving the
  // form silently invalid. No-op when accountDimension.allowedBuyAccountIds is absent.
  useEffect(() => {
    if (!accountDimension?.allowedBuyAccountIds || !soldAccountTypeWatch) return;
    const allowed = accountDimension.allowedBuyAccountIds(soldAccountTypeWatch);
    getValues("buys").forEach((buyLeg, index) => {
      if (buyLeg.accountType && !allowed.includes(buyLeg.accountType)) {
        setValue(`buys.${index}.accountType`, allowed[0]);
      }
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [soldAccountTypeWatch]);

  function handleSoldTickerChange(ticker: string) {
    setSoldTicker(ticker);
  }

  function handleSoldAccountChange(accountId: string) {
    setValue("soldAccountType", accountId);
    if (position === null || selectedCandidate === null) return;
    if (!dirtyFields.soldUnits) {
      const heldUnits = getUnitsForAccount(selectedCandidate, accountId);
      const suggested = Math.min(
        computeSuggestedSellUnits(position, totalPortfolioValuePln, selectedCandidate.pricePln),
        heldUnits,
      );
      setValue("soldUnits", formatNumberInput(suggested));
    }
  }

  function handleAddBuyLeg() {
    append({
      ticker: "",
      accountType: accountDimension ? getValues("soldAccountType") : undefined,
      units: "",
      amountPln: "",
    });
  }

  function onSubmit(values: FormValues) {
    if (position === null || selectedCandidate === null) return;

    // Defense-in-depth: the Select gating above should already prevent this in normal use, but
    // guard against a stale selection surviving a fast account switch.
    if (accountDimension?.allowedBuyAccountIds && values.soldAccountType) {
      const allowed = accountDimension.allowedBuyAccountIds(values.soldAccountType);
      if (values.buys.some((b) => b.accountType && !allowed.includes(b.accountType))) {
        return;
      }
    }

    const soldUnitsNum = parseNum(values.soldUnits);
    const soldAmountPlnNum = parseNum(values.soldAmountPln);
    const body: PositionTransferRequestLike = {
      portfolio: portfolioId,
      sold: {
        ticker: soldTicker,
        accountType: values.soldAccountType,
        units: soldUnitsNum,
        pricePln: soldUnitsNum > 0 ? soldAmountPlnNum / soldUnitsNum : 0,
      },
      buys: values.buys.map((b) => {
        const unitsNum = parseNum(b.units);
        const amountNum = parseNum(b.amountPln);
        return {
          ticker: b.ticker,
          accountType: b.accountType,
          units: unitsNum,
          pricePln: unitsNum > 0 ? amountNum / unitsNum : 0,
        };
      }),
    };
    mutation.mutate(body);
  }

  // The dialog stays mounted in portfolioB/portfolioC, so closing it ends the user's intent explicitly:
  // the next open must not reuse the previous key (ADR-111).
  function handleClose() {
    idempotentSubmit.rotate();
    onClose();
  }

  function handleOpenChange(open: boolean) {
    if (!open) {
      mutation.reset();
      handleClose();
    }
  }

  const buysWatch = useWatch({ control, name: "buys" });
  const soldTotal = parseNum(soldAmountWatch);
  const buysTotal = buysWatch.reduce((sum, b) => sum + parseNum(b.amountPln), 0);
  const diff = buysTotal - soldTotal;
  const diffTolerancePln = Math.max(1, soldTotal * imbalanceTolerancePct);
  const isNeutral = Math.abs(diff) <= diffTolerancePln;

  const buyAccountOptions: TransferAccountOption[] | null = accountDimension
    ? accountDimension.allowedBuyAccountIds && soldAccountTypeWatch
      ? accountDimension.options.filter((o) =>
          accountDimension.allowedBuyAccountIds!(soldAccountTypeWatch).includes(o.id),
        )
      : accountDimension.options
    : null;

  const formatUnits = accountDimension?.formatUnits ?? ((u: number) => u.toFixed(4));
  const availableUnitsForSelectedAccount =
    accountDimension && selectedCandidate && soldAccountTypeWatch
      ? getUnitsForAccount(selectedCandidate, soldAccountTypeWatch)
      : null;

  return (
    <Dialog open={isOpen} onOpenChange={handleOpenChange}>
      <DialogContent className="max-w-2xl max-h-[90vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            {copy.dialogTitlePrefix} —
            {sellCandidates.length > 1 ? (
              <Select value={soldTicker} onValueChange={handleSoldTickerChange}>
                <SelectTrigger
                  aria-label="Sprzedawany ticker"
                  className="h-7 w-auto text-sm gap-1 border-destructive/40 text-destructive font-mono"
                >
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {sellCandidates.map((c) => (
                    <SelectItem key={c.ticker} value={c.ticker} className="text-sm font-mono">
                      {c.ticker}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            ) : (
              <Badge variant="destructive" className="text-xs">
                {soldTicker || position?.ticker}
              </Badge>
            )}
            {selectedCandidate && (
              <span className="text-xs font-normal text-muted-foreground">{selectedCandidate.name}</span>
            )}
          </DialogTitle>
        </DialogHeader>

        <FormProvider {...methods}>
          <form onSubmit={handleSubmit(onSubmit)} className="space-y-5" noValidate>
            {/* Sold side */}
            <div className="space-y-3">
              <p className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
                {copy.soldSectionLabel}
              </p>
              <div className={cn("grid gap-3", accountDimension ? "grid-cols-3" : "grid-cols-2")}>
                {accountDimension && (
                  <FieldRow label="Konto" id="sold-account">
                    <Select value={soldAccountTypeWatch ?? ""} onValueChange={handleSoldAccountChange}>
                      <SelectTrigger id="sold-account" className="h-9">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        {soldAccountOptions.map((a) => (
                          <SelectItem key={a.id} value={a.id}>
                            {a.label}
                            <span className="ml-1 text-muted-foreground font-mono">({formatUnits(a.units)})</span>
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                    {availableUnitsForSelectedAccount != null && (
                      <span className="text-xs text-muted-foreground">
                        Dostępne: {formatUnits(availableUnitsForSelectedAccount)}
                      </span>
                    )}
                  </FieldRow>
                )}

                <FieldRow label="Jednostki" id="sold-units" error={errors.soldUnits?.message}>
                  <DecimalInput id="sold-units" className="font-mono h-9" {...register("soldUnits")} />
                  {!accountDimension && selectedCandidate && (
                    <span className="text-xs text-muted-foreground">
                      Dostępne: {(selectedCandidate.units ?? 0).toFixed(4)}
                    </span>
                  )}
                </FieldRow>

                <FieldRow label="Otrzymano PLN" id="sold-amount" error={errors.soldAmountPln?.message}>
                  <div className="space-y-0.5">
                    <DecimalInput id="sold-amount" className="font-mono h-9" {...register("soldAmountPln")} />
                    <UnitPriceHint units={parseNum(soldUnitsWatch)} amountPln={parseNum(soldAmountWatch)} />
                  </div>
                </FieldRow>
              </div>
              {copy.soldHelperText && <p className="text-xs text-muted-foreground">{copy.soldHelperText}</p>}
            </div>

            <Separator />

            {/* Buy side */}
            <div className="space-y-3">
              <div className="flex items-center justify-between">
                <p className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
                  {copy.buySectionLabel}
                </p>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  className="h-7 text-xs gap-1"
                  onClick={handleAddBuyLeg}
                >
                  <PlusIcon className="size-3" aria-hidden="true" />
                  Dodaj pozycję
                </Button>
              </div>

              {underweightTargets.length > 0 && (
                <div className="rounded-md border p-2 max-h-40 overflow-y-auto space-y-1">
                  <p className="text-xs font-medium text-muted-foreground">Niedoważone — zapas do celu</p>
                  <div className="space-y-0.5">
                    {underweightTargets.map(([ticker, info]) => (
                      <div key={ticker} className="flex items-center justify-between gap-2 text-xs">
                        <span className="font-mono">{ticker}</span>
                        <span className="text-muted-foreground">
                          {info.currentPct != null ? info.currentPct.toFixed(1) : "?"}%
                          {" → "}
                          {info.targetPct != null ? info.targetPct.toFixed(1) : "?"}%
                        </span>
                        <span className="font-mono text-emerald-600 dark:text-emerald-400">
                          {formatPln(info.gapPln ?? 0)}
                        </span>
                      </div>
                    ))}
                  </div>
                </div>
              )}

              <div className="space-y-3">
                {fields.map((field, index) => (
                  <BuyLegRow
                    key={field.id}
                    index={index}
                    tickerOptions={tickerOptions}
                    tickerTargetMap={tickerTargetMap}
                    accountOptions={buyAccountOptions}
                    soldTotal={soldTotal}
                    buysTotal={buysTotal}
                    canRemove={fields.length > 1}
                    onRemove={() => remove(index)}
                  />
                ))}
              </div>
              {errors.buys?.root && (
                <p role="alert" className="text-xs text-destructive">
                  {errors.buys.root.message}
                </p>
              )}
            </div>

            <Separator />

            {/* Running total — soft visual aid, not blocking */}
            <div className="flex items-center justify-between text-sm rounded-md border px-3 py-2">
              <span className="text-muted-foreground">
                Sprzedaż: <span className="font-mono text-foreground">{formatPln(soldTotal)}</span>
                {" · "}
                Zakupy: <span className="font-mono text-foreground">{formatPln(buysTotal)}</span>
              </span>
              <span className={cn("font-mono font-medium", isNeutral ? "text-green-600" : "text-amber-600")}>
                {diff >= 0 ? "+" : ""}
                {formatPln(diff)}
              </span>
            </div>

            {mutation.isError && (
              <p role="alert" className="text-sm text-destructive">
                {idempotencyErrorMessage(mutation.error) ??
                  (mutation.error as Error)?.message ??
                  "Wystąpił błąd podczas zapisu. Spróbuj ponownie."}
              </p>
            )}

            <DialogFooter>
              <Button type="button" variant="outline" onClick={handleClose} disabled={mutation.isPending}>
                Anuluj
              </Button>
              <Button type="submit" disabled={mutation.isPending}>
                {mutation.isPending ? copy.submitPendingLabel : copy.submitLabel}
              </Button>
            </DialogFooter>
          </form>
        </FormProvider>
      </DialogContent>
    </Dialog>
  );
}
