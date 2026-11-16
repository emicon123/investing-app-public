import { useState } from "react";
import type { ReactNode } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation } from "@tanstack/react-query";
import {
  depositFormSchema,
  idempotencyErrorMessage,
  useIdempotentSubmit,
  type DepositFormValues,
  type IdempotencyOptions,
} from "@common/app-core";
import { Button } from "./button.js";
import { Input } from "./input.js";
import { DecimalInput } from "./decimal-input.js";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
  DialogClose,
} from "./dialog.js";
import {
  Form,
  FormField,
  FormItem,
  FormLabel,
  FormControl,
  FormMessage,
} from "./form.js";
import { PageShell } from "./PageShell.js";
import { PageHeader } from "./PageHeader.js";

/**
 * portfolioB/portfolioC only — main's 455-line deposits page (cap bars, dual
 * sections) is untouched. `depositFormSchema`/`DepositFormValues` are already
 * in `@common/app-core`, so the shared page imports them directly. Generic
 * over `TDeposit` (no app Zod type imported); the table stays injected
 * because each app renders its own (already shared) `DepositsTable` wrapper.
 */
export interface DepositsPageProps<TDeposit> {
  title: string;
  dialogTitle: string;
  /** Adapter unwraps the `{data}` envelope. */
  useDepositsQuery: () => {
    data: TDeposit[] | undefined;
    isPending: boolean;
    isError: boolean;
  };
  renderTable: (deposits: TDeposit[]) => ReactNode;
  /** portfolioB passes its client fn straight through; portfolioC's adapter adds `portfolio`. `options`
   *  carries the Idempotency-Key the form holds for this open (ADR-111) — forward it. */
  createDeposit: (values: DepositFormValues, options: IdempotencyOptions) => Promise<unknown>;
  /** Adapter invalidates its own deposits key — keys stay per-app. */
  onDepositSaved: () => void;
}

function DepositForm({
  createDeposit,
  onSaved,
  onClose,
}: {
  createDeposit: (values: DepositFormValues, options: IdempotencyOptions) => Promise<unknown>;
  onSaved: () => void;
  onClose: () => void;
}) {
  const form = useForm<DepositFormValues>({
    resolver: zodResolver(depositFormSchema),
    defaultValues: {
      depositDate: "",
      amount: 0,
      comment: "",
    },
  });

  // The form mounts with its dialog, so one key lives per open; a retry reuses it (ADR-111).
  const idempotentSubmit = useIdempotentSubmit((values: DepositFormValues, options: IdempotencyOptions) => {
    const depositDate = values.depositDate.length === 7
      ? `${values.depositDate}-01`
      : values.depositDate;
    return createDeposit(
      {
        depositDate,
        amount: values.amount,
        comment: values.comment || undefined,
      },
      options,
    );
  });

  const mutation = useMutation({
    mutationFn: (values: DepositFormValues) => idempotentSubmit.submit(values),
    onSuccess: () => {
      onSaved();
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
        <FormField
          control={form.control}
          name="comment"
          render={({ field }) => (
            <FormItem>
              <FormLabel>Komentarz</FormLabel>
              <FormControl>
                <Input {...field} />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />

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

export function DepositsPage<TDeposit>({
  title,
  dialogTitle,
  useDepositsQuery,
  renderTable,
  createDeposit,
  onDepositSaved,
}: DepositsPageProps<TDeposit>) {
  const [dialogOpen, setDialogOpen] = useState(false);

  const { data, isPending, isError } = useDepositsQuery();

  const deposits = data ?? [];

  return (
    <div className="p-6 space-y-6">
      <PageHeader
        title={title}
        actions={<Button onClick={() => setDialogOpen(true)}>Dodaj depozyt</Button>}
      />

      <PageShell isPending={isPending} isError={isError} loadingLabel="Ładowanie danych">
        {renderTable(deposits)}
      </PageShell>

      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{dialogTitle}</DialogTitle>
          </DialogHeader>
          <DepositForm
            createDeposit={createDeposit}
            onSaved={onDepositSaved}
            onClose={() => setDialogOpen(false)}
          />
        </DialogContent>
      </Dialog>
    </div>
  );
}
