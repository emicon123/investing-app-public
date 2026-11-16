import { useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import {
  formatPln,
  idempotencyErrorMessage,
  useIdempotentSubmit,
  type IdempotencyOptions,
} from "@common/app-core";
import { listBinanceTransfers, createBinanceTransfer, deleteBinanceTransfer, type BinanceTransfer, type CreateBinanceTransferRequest } from "../../../api/index.js";
import { format, parseISO } from "date-fns";
import { pl } from "date-fns/locale";
import { toast } from "sonner";
import { Trash2 } from "lucide-react";
import { queryKeys } from "@/lib/queryKeys";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { DecimalInput } from "@/components/ui/decimal-input";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from "@/components/ui/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
  DialogClose,
} from "@/components/ui/dialog";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import {
  Form,
  FormField,
  FormItem,
  FormLabel,
  FormControl,
  FormMessage,
} from "@/components/ui/form";

const ALL_YEARS = "ALL";
const NO_CURRENCY = "NONE";

const transferFormSchema = z
  .object({
    transferDate: z.string().min(1, "Wymagane"),
    amountPln: z.coerce.number<number>().positive("Kwota musi być dodatnia"),
    amountNative: z.string(),
    nativeCurrency: z.enum([NO_CURRENCY, "PLN", "EUR", "USD", "CHF", "GBP"]),
    comment: z.string(),
  })
  .superRefine((v, ctx) => {
    const hasAmount = v.amountNative.trim() !== "";
    const hasCurrency = v.nativeCurrency !== NO_CURRENCY;
    if (hasAmount !== hasCurrency) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        message: "Kwota natywna i waluta muszą być podane razem",
        path: hasAmount ? ["nativeCurrency"] : ["amountNative"],
      });
    }
    if (hasAmount && (!Number.isFinite(Number(v.amountNative)) || Number(v.amountNative) <= 0)) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        message: "Kwota natywna musi być dodatnia",
        path: ["amountNative"],
      });
    }
  });

type TransferFormValues = z.infer<typeof transferFormSchema>;

function toCreateRequest(values: TransferFormValues): CreateBinanceTransferRequest {
  const body: CreateBinanceTransferRequest = {
    transferDate: values.transferDate,
    amountPln: values.amountPln,
  };
  if (values.amountNative.trim() !== "" && values.nativeCurrency !== NO_CURRENCY) {
    body.amountNative = Number(values.amountNative);
    body.nativeCurrency = values.nativeCurrency;
  }
  if (values.comment.trim() !== "") {
    body.comment = values.comment.trim();
  }
  return body;
}

function TransferForm({ onClose }: { onClose: () => void }) {
  const queryClient = useQueryClient();

  const form = useForm<TransferFormValues>({
    resolver: zodResolver(transferFormSchema),
    defaultValues: {
      transferDate: "",
      amountPln: 0,
      amountNative: "",
      nativeCurrency: NO_CURRENCY,
      comment: "",
    },
  });

  // The form mounts with its dialog, so one key lives per open; a retry reuses it (ADR-111).
  const idempotentSubmit = useIdempotentSubmit((values: TransferFormValues, options: IdempotencyOptions) =>
    createBinanceTransfer(toCreateRequest(values), options),
  );

  const mutation = useMutation({
    mutationFn: (values: TransferFormValues) => idempotentSubmit.submit(values),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.binanceTransfers.all });
      toast.success("Wpłata na Binance zapisana.");
      onClose();
    },
    onError: (err: unknown) => {
      const msg = idempotencyErrorMessage(err) ?? (err instanceof Error ? err.message : "Nieznany błąd.");
      toast.error("Błąd zapisu wpłaty", { description: msg });
    },
  });

  function onSubmit(values: TransferFormValues) {
    mutation.mutate(values);
  }

  return (
    <Form {...form}>
      <form onSubmit={form.handleSubmit(onSubmit)} className="space-y-4">
        <FormField
          control={form.control}
          name="transferDate"
          render={({ field }) => (
            <FormItem>
              <FormLabel>Data przelewu</FormLabel>
              <FormControl>
                <Input type="date" {...field} />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name="amountPln"
          render={({ field }) => (
            <FormItem>
              <FormLabel>Kwota PLN</FormLabel>
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
            name="amountNative"
            render={({ field }) => (
              <FormItem>
                <FormLabel>Kwota natywna (opcjonalnie)</FormLabel>
                <FormControl>
                  <DecimalInput {...field} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name="nativeCurrency"
            render={({ field }) => (
              <FormItem>
                <FormLabel>Waluta natywna</FormLabel>
                <Select onValueChange={field.onChange} defaultValue={field.value}>
                  <FormControl>
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                  </FormControl>
                  <SelectContent>
                    <SelectItem value={NO_CURRENCY}>Brak</SelectItem>
                    {(["EUR", "GBP", "USD", "CHF", "PLN"] as const).map((c) => (
                      <SelectItem key={c} value={c}>{c}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <FormMessage />
              </FormItem>
            )}
          />
        </div>
        <FormField
          control={form.control}
          name="comment"
          render={({ field }) => (
            <FormItem>
              <FormLabel>Komentarz (opcjonalnie)</FormLabel>
              <FormControl>
                <Input type="text" {...field} />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />

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

export function BinanceTransfersTable() {
  const queryClient = useQueryClient();
  const [year, setYear] = useState<string>(ALL_YEARS);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<BinanceTransfer | null>(null);

  const yearParam = year === ALL_YEARS ? undefined : Number(year);

  const { data, isPending, isError } = useQuery({
    queryKey: queryKeys.binanceTransfers.list(yearParam),
    queryFn: () => listBinanceTransfers(yearParam != null ? { year: yearParam } : undefined),
    staleTime: 5 * 60_000,
  });

  const transfers = data?.data ?? [];
  const yearlySums = data?.yearlySums ?? [];

  const deleteMutation = useMutation({
    mutationFn: (id: number) => deleteBinanceTransfer(id),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.binanceTransfers.all });
      toast.success("Wpłata usunięta.");
    },
    onError: (err: unknown) => {
      const msg = err instanceof Error ? err.message : "Nieznany błąd.";
      toast.error("Błąd usuwania wpłaty", { description: msg });
    },
  });

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between flex-wrap gap-3">
        <Select value={year} onValueChange={setYear}>
          <SelectTrigger className="w-36">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL_YEARS}>Wszystkie lata</SelectItem>
            {yearlySums.map((s) => (
              <SelectItem key={s.year} value={String(s.year)}>{s.year}</SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Button onClick={() => setDialogOpen(true)}>Dodaj wpłatę</Button>
      </div>

      {yearlySums.length > 0 && (
        <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-5 gap-3">
          {yearlySums.map((s) => (
            <button
              key={s.year}
              type="button"
              onClick={() => setYear(year === String(s.year) ? ALL_YEARS : String(s.year))}
              aria-pressed={year === String(s.year)}
              className={cn(
                "rounded-md border p-3 text-left focus-visible:outline-2 focus-visible:outline-ring",
                year === String(s.year) && "border-primary bg-muted/50",
              )}
            >
              <div className="text-sm text-muted-foreground">PIT-38 {s.year}</div>
              <div className="font-semibold font-mono">{formatPln(s.totalPln)}</div>
            </button>
          ))}
        </div>
      )}

      {isPending && (
        <div role="status" aria-label="Ładowanie danych" className="space-y-2">
          {Array.from({ length: 4 }).map((_, i) => (
            <Skeleton key={i} className="h-10 w-full" />
          ))}
        </div>
      )}

      {isError && (
        <p role="alert" className="text-destructive text-sm">
          Błąd ładowania wpłat.
        </p>
      )}

      {!isPending && !isError && (
        <div className="rounded-md border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Data</TableHead>
                <TableHead className="text-right">Kwota PLN</TableHead>
                <TableHead className="text-right">Kwota natywna</TableHead>
                <TableHead>Komentarz</TableHead>
                <TableHead>Źródło</TableHead>
                <TableHead />
              </TableRow>
            </TableHeader>
            <TableBody>
              {transfers.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={6} className="text-center text-muted-foreground py-8">
                    {yearParam != null ? `Brak wpłat na Binance w ${yearParam}.` : "Brak wpłat na Binance."}
                  </TableCell>
                </TableRow>
              ) : (
                transfers.map((t) => (
                  <TableRow key={t.id}>
                    <TableCell>
                      {format(parseISO(t.transferDate), "d MMM yyyy", { locale: pl })}
                    </TableCell>
                    <TableCell className="text-right font-mono">{formatPln(t.amountPln)}</TableCell>
                    <TableCell className="text-right font-mono text-muted-foreground">
                      {t.amountNative != null && t.nativeCurrency != null
                        ? `${t.amountNative.toLocaleString("pl-PL", { minimumFractionDigits: 2 })} ${t.nativeCurrency}`
                        : "—"}
                    </TableCell>
                    <TableCell className="text-muted-foreground">{t.comment ?? ""}</TableCell>
                    <TableCell>
                      <Badge variant={t.source === "SESSION" ? "outline" : "secondary"}>
                        {t.source === "SESSION" ? "Sesja" : "Ręczna"}
                      </Badge>
                    </TableCell>
                    <TableCell className="text-right">
                      <Button
                        variant="ghost"
                        size="icon"
                        onClick={() => setDeleteTarget(t)}
                        disabled={deleteMutation.isPending}
                      >
                        <Trash2 className="size-4" aria-hidden="true" />
                        <span className="sr-only">Usuń wpłatę z {t.transferDate}</span>
                      </Button>
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </div>
      )}

      <p className="text-xs text-muted-foreground">
        Koszty uzyskania przychodu (PIT-38) = suma wpłat na Binance za dany rok.
        Wpłaty są wycinkiem miesięcznych depozytów — nie są dodatkowym kapitałem.
      </p>

      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Dodaj wpłatę na Binance</DialogTitle>
          </DialogHeader>
          <TransferForm onClose={() => setDialogOpen(false)} />
        </DialogContent>
      </Dialog>

      <AlertDialog open={deleteTarget != null} onOpenChange={(open) => !open && setDeleteTarget(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Usunąć wpłatę?</AlertDialogTitle>
            <AlertDialogDescription>
              {deleteTarget != null &&
                `Wpłata z ${format(parseISO(deleteTarget.transferDate), "d MMM yyyy", { locale: pl })} na ${formatPln(deleteTarget.amountPln)} zostanie trwale usunięta z ewidencji PIT-38.`}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Anuluj</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                if (deleteTarget != null) deleteMutation.mutate(deleteTarget.id);
              }}
            >
              Usuń
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
