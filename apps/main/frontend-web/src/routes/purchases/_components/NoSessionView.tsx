import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { toast } from "sonner";
import type { z } from "zod";
import { createPurchaseSession } from "../../../api/index.js";
import type { PortfoliosIncluded } from "../../../api/index.js";
import { queryKeys } from "@/lib/queryKeys";
import { createSessionSchema, type CreateSessionFormValues } from "../schemas";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { DecimalInput } from "@/components/ui/decimal-input";
import { Label } from "@/components/ui/label";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import {
  Form,
  FormField,
  FormItem,
  FormLabel,
  FormControl,
  FormMessage,
} from "@/components/ui/form";
import { HistorySection } from "./HistorySection";

export function NoSessionView() {
  const queryClient = useQueryClient();

  // The schema preprocesses `cashOverridePln` (raw input string -> number), so its input type is
  // wider than its output type: the form holds the input, `onSubmit` receives the output.
  const form = useForm<z.input<typeof createSessionSchema>, unknown, CreateSessionFormValues>({
    resolver: zodResolver(createSessionSchema),
    defaultValues: {
      portfoliosIncluded: "BOTH" as PortfoliosIncluded,
      cashOverridePln: undefined,
      notes: "",
    },
  });

  const createMutation = useMutation({
    mutationFn: (values: CreateSessionFormValues) =>
      createPurchaseSession({
        portfoliosIncluded: values.portfoliosIncluded,
        cashOverridePln: values.cashOverridePln ?? null,
        notes: values.notes || null,
      }),
    onSuccess: (res) => {
      queryClient.setQueryData(queryKeys.purchaseSessions.active(), res);
      toast.success("Plan zakupów wygenerowany.");
    },
    onError: (err: unknown) => {
      const msg = err instanceof Error ? err.message : "Nieznany błąd.";
      toast.error("Błąd generowania planu", { description: msg });
    },
  });

  function onSubmit(values: CreateSessionFormValues) {
    createMutation.mutate(values);
  }

  return (
    <div className="space-y-8">
      <Card className="max-w-lg">
        <CardHeader>
          <CardTitle className="text-base">Nowa sesja zakupów</CardTitle>
        </CardHeader>
        <CardContent>
          <Form {...form}>
            <form onSubmit={form.handleSubmit(onSubmit)} className="space-y-4">
              <FormField
                control={form.control}
                name="portfoliosIncluded"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>Portfel</FormLabel>
                    <FormControl>
                      <RadioGroup
                        onValueChange={field.onChange}
                        defaultValue={field.value}
                        className="flex gap-4"
                      >
                        <div className="flex items-center gap-2">
                          <RadioGroupItem value="BOTH" id="both" />
                          <Label htmlFor="both">Oba portfele</Label>
                        </div>
                        <div className="flex items-center gap-2">
                          <RadioGroupItem value="MAIN" id="main" />
                          <Label htmlFor="main">Inwestycje</Label>
                        </div>
                        <div className="flex items-center gap-2">
                          <RadioGroupItem value="RETIREMENT" id="retirement" />
                          <Label htmlFor="retirement">Emerytura</Label>
                        </div>
                      </RadioGroup>
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name="cashOverridePln"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>Gotówka PLN (override)</FormLabel>
                    <FormControl>
                      <DecimalInput
                        placeholder="Zostaw puste — użyj ostatniego snapshotu"
                        {...field}
                        value={field.value == null ? "" : String(field.value)}
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name="notes"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>Notatka</FormLabel>
                    <FormControl>
                      <Input
                        placeholder="np. Czerwiec 2026"
                        {...field}
                        value={field.value ?? ""}
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <Button type="submit" disabled={createMutation.isPending}>
                {createMutation.isPending ? "Generuję plan..." : "Generuj plan"}
              </Button>
            </form>
          </Form>
          {createMutation.isError && (
            <p role="alert" className="mt-3 text-sm text-destructive">
              {createMutation.error instanceof Error
                ? createMutation.error.message
                : "Błąd generowania planu zakupów."}
            </p>
          )}
        </CardContent>
      </Card>

      <HistorySection />
    </div>
  );
}
