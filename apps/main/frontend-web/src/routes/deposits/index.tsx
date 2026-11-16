import { useRef, useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import {
  formatPln,
  idempotencyErrorMessage,
  useIdempotentSubmit,
  type IdempotencyOptions,
} from "@common/app-core";
import { listDeposits, createDeposit, getAccountCapUsage, updateAccountCapAdjustment } from "../../api/index.js";
import { depositFormSchema, type DepositFormValues } from "./schemas";
import { queryKeys } from "@/lib/queryKeys";
import { CURRENT_YEAR } from "@/lib/dateConstants";
import { CURRENCY_OPTIONS, EXCHANGE_OPTIONS, ACCOUNT_TYPE_OPTIONS, PORTFOLIO_OPTIONS } from "@/lib/formConstants";
import type { AccountCapUsageItem } from "../../api/index.js";
import { DepositsTable } from "@/components/tables/DepositsTable";
import { PageHeader } from "@/components/PageHeader";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { DecimalInput } from "@/components/ui/decimal-input";
import { Progress } from "@/components/ui/progress";
import { Popover, PopoverTrigger, PopoverContent } from "@/components/ui/popover";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
  DialogClose,
} from "@/components/ui/dialog";
import {
  Form,
  FormField,
  FormItem,
  FormLabel,
  FormControl,
  FormMessage,
} from "@/components/ui/form";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { PageShell } from "@common/ui-primitives/PageShell";
import { PencilIcon } from "lucide-react";

// ── Inline "edit remaining limit" control (ADR-058) ────────────────────────
// A small, unobtrusive pencil affordance next to "Pozostało" on each IKE/IKZE cap card —
// opens an inline popover with a single PLN input prefilled with the current remaining
// figure. The user just types the real remaining amount (e.g. read off a broker statement);
// the backend works out the correction needed to make it read back exactly that. Mirrors the
// portfolio table's `TargetEditPopover` interaction pattern (pencil -> Popover -> save).

function RemainingLimitControl({ item }: { item: AccountCapUsageItem }) {
  const queryClient = useQueryClient();
  const [open, setOpen] = useState(false);
  const [amountInput, setAmountInput] = useState("");
  const amountRef = useRef<HTMLInputElement | null>(null);

  const accountType = item.accountType;
  const year = item.year ?? CURRENT_YEAR;
  const remaining = item.remainingPln ?? (item.annualCapPln ?? 0) - (item.totalDepositedPln ?? 0);

  const mutation = useMutation({
    mutationFn: (remainingPln: number) => {
      if (accountType !== "IKE" && accountType !== "IKZE") {
        return Promise.reject(new Error("Edycja dostępna tylko dla kont IKE/IKZE"));
      }
      return updateAccountCapAdjustment({ accountType, year, remainingPln });
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.deposits.capUsage(year) });
      void queryClient.invalidateQueries({ queryKey: queryKeys.purchaseSessions.active() });
      setOpen(false);
    },
  });

  if (accountType !== "IKE" && accountType !== "IKZE") return null;

  function handleOpen() {
    setAmountInput(remaining ? String(remaining) : "");
    mutation.reset();
    setOpen(true);
    requestAnimationFrame(() => amountRef.current?.focus());
  }

  function handleSave() {
    const trimmed = amountInput.trim();
    const n = parseFloat(trimmed);
    if (trimmed === "" || isNaN(n)) return;
    mutation.mutate(n);
  }

  return (
    <Popover open={open} onOpenChange={(o) => { if (!o) setOpen(false); }}>
      <PopoverTrigger asChild>
        <button
          onClick={handleOpen}
          className="opacity-0 group-hover:opacity-100 focus-visible:opacity-100 transition-opacity flex items-center justify-center size-5 rounded hover:bg-accent shrink-0"
          aria-label={`Edytuj pozostały limit ${accountType} ${year}`}
        >
          <PencilIcon className="size-3 text-muted-foreground" aria-hidden="true" />
        </button>
      </PopoverTrigger>
      <PopoverContent className="w-56 p-3" align="start">
        <div className="space-y-2">
          <p className="text-xs font-medium text-muted-foreground">
            Pozostały limit {accountType} {year} (PLN)
          </p>
          <DecimalInput
            ref={amountRef}
            value={amountInput}
            onChange={(e) => setAmountInput(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") handleSave();
              if (e.key === "Escape") setOpen(false);
            }}
            className="font-mono h-8 text-sm"
            placeholder="0.00"
            aria-label="Pozostały limit w PLN"
          />
          {mutation.isError && (
            <p role="alert" className="text-xs text-destructive">
              Błąd zapisu. Spróbuj ponownie.
            </p>
          )}
          <div className="flex gap-2 justify-end">
            <Button
              type="button"
              variant="outline"
              size="sm"
              className="h-7 text-xs"
              onClick={() => setOpen(false)}
              disabled={mutation.isPending}
            >
              Anuluj
            </Button>
            <Button
              type="button"
              size="sm"
              className="h-7 text-xs"
              onClick={handleSave}
              disabled={mutation.isPending}
            >
              {mutation.isPending ? "..." : "Zapisz"}
            </Button>
          </div>
        </div>
      </PopoverContent>
    </Popover>
  );
}

function CapBar({ item }: { item: AccountCapUsageItem }) {
  const pct = item.utilizationPct ?? 0;
  const indicatorClass = pct >= 100 ? "bg-green-500" : pct >= 80 ? "bg-amber-500" : undefined;
  const remaining = item.remainingPln ?? (item.annualCapPln ?? 0) - (item.totalDepositedPln ?? 0);

  return (
    <div className="group space-y-1">
      <div className="flex items-center justify-between text-sm">
        <span className="font-medium">
          {item.accountType} {item.year}: {formatPln(item.totalDepositedPln ?? 0)} /{" "}
          {formatPln(item.annualCapPln ?? 0)} PLN
        </span>
        <span className="text-muted-foreground">{pct.toFixed(1)}%</span>
      </div>
      <Progress value={Math.min(Math.max(pct, 0), 100)} indicatorClassName={indicatorClass} />
      <div className="flex items-center gap-1 text-xs text-muted-foreground font-mono">
        <span>
          Pozostało: {formatPln(remaining)}
          {remaining < 0 && " (przekroczono)"}
        </span>
        <RemainingLimitControl item={item} />
      </div>
    </div>
  );
}

function DepositForm({ onClose }: { onClose: () => void }) {
  const queryClient = useQueryClient();

  const form = useForm<DepositFormValues>({
    resolver: zodResolver(depositFormSchema),
    defaultValues: {
      depositDate: "",
      amount: 0,
      currency: "PLN",
      exchange: "XTB",
      accountType: "IKE",
      portfolio: "MAIN",
    },
  });

  // The form mounts with its dialog, so one key lives per open; a retry reuses it (ADR-111).
  const idempotentSubmit = useIdempotentSubmit((values: DepositFormValues, options: IdempotencyOptions) => {
    const depositDate = values.depositDate.length === 7
      ? `${values.depositDate}-01`
      : values.depositDate;
    return createDeposit({ ...values, depositDate }, options);
  });

  const mutation = useMutation({
    mutationFn: (values: DepositFormValues) => idempotentSubmit.submit(values),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.deposits.all });
      void queryClient.invalidateQueries({ queryKey: queryKeys.deposits.capUsage(CURRENT_YEAR) });
      onClose();
    },
  });

  function onSubmit(values: DepositFormValues) {
    mutation.mutate(values);
  }

  return (
    <Form {...form}>
      <form onSubmit={form.handleSubmit(onSubmit)} className="space-y-4">
        <FormField
          control={form.control}
          name="depositDate"
          render={({ field }) => (
            <FormItem>
              <FormLabel>Data (rok-miesiąc)</FormLabel>
              <FormControl>
                <Input type="month" {...field} />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name="amount"
          render={({ field }) => (
            <FormItem>
              <FormLabel>Kwota</FormLabel>
              <FormControl>
                <DecimalInput {...field} />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        <div className="grid grid-cols-2 gap-4">
          <FormField
            control={form.control}
            name="currency"
            render={({ field }) => (
              <FormItem>
                <FormLabel>Waluta</FormLabel>
                <Select onValueChange={field.onChange} defaultValue={field.value}>
                  <FormControl>
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                  </FormControl>
                  <SelectContent>
                    {CURRENCY_OPTIONS.map((c) => (
                      <SelectItem key={c} value={c}>{c}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name="exchange"
            render={({ field }) => (
              <FormItem>
                <FormLabel>Giełda</FormLabel>
                <Select onValueChange={field.onChange} defaultValue={field.value}>
                  <FormControl>
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                  </FormControl>
                  <SelectContent>
                    {EXCHANGE_OPTIONS.map((e) => (
                      <SelectItem key={e} value={e}>{e}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name="accountType"
            render={({ field }) => (
              <FormItem>
                <FormLabel>Typ konta</FormLabel>
                <Select onValueChange={field.onChange} defaultValue={field.value}>
                  <FormControl>
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                  </FormControl>
                  <SelectContent>
                    {ACCOUNT_TYPE_OPTIONS.map((a) => (
                      <SelectItem key={a} value={a}>{a}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name="portfolio"
            render={({ field }) => (
              <FormItem>
                <FormLabel>Portfel</FormLabel>
                <Select onValueChange={field.onChange} defaultValue={field.value}>
                  <FormControl>
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                  </FormControl>
                  <SelectContent>
                    {PORTFOLIO_OPTIONS.map((p) => (
                      <SelectItem key={p} value={p}>{p}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <FormMessage />
              </FormItem>
            )}
          />
        </div>

        {mutation.isError && (
          <p role="alert" className="text-sm text-destructive">
            {idempotencyErrorMessage(mutation.error) ?? "Błąd zapisu. Spróbuj ponownie."}
          </p>
        )}

        <DialogFooter>
          <DialogClose asChild>
            <Button type="button" variant="outline" onClick={onClose}>
              Anuluj
            </Button>
          </DialogClose>
          <Button type="submit" disabled={mutation.isPending}>
            {mutation.isPending ? "Zapisuję..." : "Zapisz"}
          </Button>
        </DialogFooter>
      </form>
    </Form>
  );
}

function CapBarsSection() {
  const { data, isPending } = useQuery({
    queryKey: queryKeys.deposits.capUsage(CURRENT_YEAR),
    queryFn: () => getAccountCapUsage(CURRENT_YEAR),
    staleTime: 5 * 60_000,
  });

  const items: AccountCapUsageItem[] = data?.data ?? [];
  const capItems = items.filter(
    (i) => i.accountType === "IKE" || i.accountType === "IKZE"
  );

  if (isPending) {
    return (
      <div role="status" aria-label="Ładowanie limitów rocznych" className="space-y-3">
        <Skeleton className="h-8 w-full" />
        <Skeleton className="h-8 w-full" />
      </div>
    );
  }

  if (capItems.length === 0) return null;

  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="text-base">Limity roczne {CURRENT_YEAR}</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        {capItems.map((item) => (
          <CapBar key={item.accountType} item={item} />
        ))}
      </CardContent>
    </Card>
  );
}

function PortfolioDepositsSection({ portfolio, title }: { portfolio: "MAIN" | "RETIREMENT"; title: string }) {
  const { data, isPending, isError } = useQuery({
    queryKey: queryKeys.deposits.list(portfolio),
    queryFn: () => listDeposits({ size: 200, portfolio }),
    staleTime: 5 * 60_000,
  });

  const deposits = data?.data ?? [];

  return (
    <div className="space-y-3">
      <h2 className="text-lg font-medium">{title}</h2>
      <PageShell
        isPending={isPending}
        isError={isError}
        loadingLabel="Ładowanie danych"
        loadingSkeleton={
          <>
            {Array.from({ length: 4 }).map((_, i) => (
              <Skeleton key={i} className="h-10 w-full" />
            ))}
          </>
        }
      >
        <DepositsTable data={deposits} />
      </PageShell>
    </div>
  );
}

export default function DepositsPage() {
  const [dialogOpen, setDialogOpen] = useState(false);

  return (
    <div className="p-6 space-y-6">
      <PageHeader
        eyebrow="Dochód"
        title="Wpłaty"
        actions={<Button onClick={() => setDialogOpen(true)}>Dodaj depozyt</Button>}
      />

      <CapBarsSection />

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6 items-start">
        <PortfolioDepositsSection portfolio="MAIN" title="Portfel główny" />
        <PortfolioDepositsSection portfolio="RETIREMENT" title="Emerytura" />
      </div>

      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Dodaj depozyt</DialogTitle>
          </DialogHeader>
          <DepositForm onClose={() => setDialogOpen(false)} />
        </DialogContent>
      </Dialog>
    </div>
  );
}
