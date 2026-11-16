import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { toast } from "sonner";
import { idempotencyErrorMessage, useIdempotentSubmit } from "@common/app-core";
import { createCryptoTrade, type CreateCryptoTradeRequest } from "../../../api/index.js";
import { queryKeys } from "@/lib/queryKeys";
import { tradeFormSchema, type TradeFormValues } from "../schemas";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { DecimalInput } from "@/components/ui/decimal-input";
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from "@/components/ui/select";
import {
  Form,
  FormField,
  FormItem,
  FormLabel,
  FormControl,
  FormMessage,
} from "@/components/ui/form";

export function TradeEntryForm() {
  const queryClient = useQueryClient();

  const form = useForm<TradeFormValues>({
    resolver: zodResolver(tradeFormSchema),
    defaultValues: {
      tradeDate: "",
      ticker: "",
      side: "BUY",
      quantity: 0,
      unitPrice: 0,
      priceCurrency: "USD",
      totalAmount: 0,
      amountPln: 0,
      exchange: "XTB",
      accountType: "REGULAR",
      notes: "",
    },
  });

  // The form stays mounted between trades: the key is rotated after every success (ADR-111).
  const idempotentSubmit = useIdempotentSubmit(createCryptoTrade);

  const mutation = useMutation({
    mutationFn: (body: CreateCryptoTradeRequest) => idempotentSubmit.submit(body),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.cryptoTrades.all });
      toast.success("Transakcja dodana");
      form.reset();
    },
    onError: (err: unknown) => {
      toast.error(idempotencyErrorMessage(err) ?? "Błąd podczas dodawania transakcji");
    },
  });

  function onSubmit(values: TradeFormValues) {
    mutation.mutate({
      tradeDate: values.tradeDate,
      ticker: values.ticker.toUpperCase(),
      side: values.side,
      quantity: values.quantity,
      unitPrice: values.unitPrice,
      priceCurrency: values.priceCurrency,
      totalAmount: values.totalAmount,
      amountPln: values.amountPln,
      exchange: values.exchange,
      accountType: values.accountType,
      notes: values.notes ?? null,
    });
  }

  return (
    <Form {...form}>
      <form onSubmit={form.handleSubmit(onSubmit)} className="space-y-4 max-w-2xl">
        <div className="grid grid-cols-2 gap-4">
          <FormField
            control={form.control}
            name="tradeDate"
            render={({ field }) => (
              <FormItem>
                <FormLabel>Data</FormLabel>
                <FormControl>
                  <Input type="date" {...field} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name="ticker"
            render={({ field }) => (
              <FormItem>
                <FormLabel>Ticker</FormLabel>
                <FormControl>
                  <Input placeholder="np. BTC" {...field} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
        </div>

        <div className="grid grid-cols-3 gap-4">
          <FormField
            control={form.control}
            name="side"
            render={({ field }) => (
              <FormItem>
                <FormLabel>Strona</FormLabel>
                <Select onValueChange={field.onChange} value={field.value}>
                  <FormControl>
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                  </FormControl>
                  <SelectContent>
                    <SelectItem value="BUY">BUY</SelectItem>
                    <SelectItem value="SELL">SELL</SelectItem>
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
                <Select onValueChange={field.onChange} value={field.value}>
                  <FormControl>
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                  </FormControl>
                  <SelectContent>
                    <SelectItem value="XTB">XTB</SelectItem>
                    <SelectItem value="BINANCE">Binance</SelectItem>
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
                <FormLabel>Konto</FormLabel>
                <Select onValueChange={field.onChange} value={field.value}>
                  <FormControl>
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                  </FormControl>
                  <SelectContent>
                    <SelectItem value="REGULAR">Zwykłe</SelectItem>
                    <SelectItem value="IKE">IKE</SelectItem>
                    <SelectItem value="IKZE">IKZE</SelectItem>
                  </SelectContent>
                </Select>
                <FormMessage />
              </FormItem>
            )}
          />
        </div>

        <div className="grid grid-cols-3 gap-4">
          <FormField
            control={form.control}
            name="quantity"
            render={({ field }) => (
              <FormItem>
                <FormLabel>Ilość</FormLabel>
                <FormControl>
                  <DecimalInput {...field} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name="unitPrice"
            render={({ field }) => (
              <FormItem>
                <FormLabel>Cena jednostkowa</FormLabel>
                <FormControl>
                  <DecimalInput {...field} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name="priceCurrency"
            render={({ field }) => (
              <FormItem>
                <FormLabel>Waluta ceny</FormLabel>
                <Select onValueChange={field.onChange} value={field.value}>
                  <FormControl>
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                  </FormControl>
                  <SelectContent>
                    <SelectItem value="PLN">PLN</SelectItem>
                    <SelectItem value="EUR">EUR</SelectItem>
                    <SelectItem value="USD">USD</SelectItem>
                    <SelectItem value="CHF">CHF</SelectItem>
                    <SelectItem value="GBP">GBP</SelectItem>
                  </SelectContent>
                </Select>
                <FormMessage />
              </FormItem>
            )}
          />
        </div>

        <div className="grid grid-cols-2 gap-4">
          <FormField
            control={form.control}
            name="totalAmount"
            render={({ field }) => (
              <FormItem>
                <FormLabel>Kwota łączna (waluta ceny)</FormLabel>
                <FormControl>
                  <DecimalInput {...field} />
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
                <FormLabel>Kwota PLN (z brokera, do PIT-38)</FormLabel>
                <FormControl>
                  <DecimalInput {...field} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
        </div>

        <FormField
          control={form.control}
          name="notes"
          render={({ field }) => (
            <FormItem>
              <FormLabel>Notatki (opcjonalnie)</FormLabel>
              <FormControl>
                <Input {...field} />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />

        {mutation.isError && (
          <p role="alert" className="text-sm text-destructive">
            {mutation.error instanceof Error
              ? mutation.error.message
              : "Błąd podczas dodawania transakcji"}
          </p>
        )}

        <Button type="submit" disabled={mutation.isPending}>
          {mutation.isPending ? "Dodawanie..." : "Dodaj transakcję"}
        </Button>
      </form>
    </Form>
  );
}
