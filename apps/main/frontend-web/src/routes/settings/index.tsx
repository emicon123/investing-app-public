import { useState, useEffect, useMemo, useRef } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { formatPln } from "@common/app-core";
import { getCashPositions, createCashPosition, listSettings, updateSetting, type CashPosition, type Setting } from "../../api/index.js";

import { PageHeader } from "@/components/PageHeader";
import { Button } from "@/components/ui/button";
import { DecimalInput } from "@/components/ui/decimal-input";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Separator } from "@/components/ui/separator";
import { queryKeys } from "@/lib/queryKeys";
import { MacroBlendHistoryChart } from "./_components/MacroBlendHistoryChart";
import { MacroDeriskSection } from "./_components/MacroDeriskSection";
import { MacroStatePanel } from "./_components/MacroStatePanel";
import { NotificationsSection } from "./_components/NotificationsSection";

const CASH_LOCATIONS = [
  "PKO",
  "ZEN",
  "Portfel gotówka",
  "Lokaty",
] as const;

function findSetting(settings: Setting[], key: string): Setting | undefined {
  return settings.find((s) => s.key === key);
}

function CashSection() {
  const queryClient = useQueryClient();

  const { data: cashData, isPending: cashPending } = useQuery({
    queryKey: queryKeys.cashPositions.all,
    queryFn: getCashPositions,
    staleTime: 5 * 60_000,
  });

  const latestPositions: CashPosition[] = useMemo(() => cashData?.data ?? [], [cashData]);

  const [amounts, setAmounts] = useState<Record<string, string>>(
    Object.fromEntries(CASH_LOCATIONS.map((loc) => [loc, "0"]))
  );

  const hasInitialized = useRef(false);
  useEffect(() => {
    if (!hasInitialized.current && latestPositions.length > 0) {
      hasInitialized.current = true;
      setAmounts((prev) => {
        const next = { ...prev };
        latestPositions.forEach((pos) => {
          if (pos.location in next) {
            next[pos.location] = String(pos.amountPln);
          }
        });
        return next;
      });
    }
  }, [latestPositions]);

  const mutation = useMutation({
    mutationFn: createCashPosition,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.cashPositions.all });
      toast.success("Snapshot gotówki zapisany");
    },
    onError: () => {
      toast.error("Błąd zapisu snapshotu gotówki");
    },
  });

  function handleSave() {
    const positions = CASH_LOCATIONS.map((loc) => ({
      location: loc,
      amountPln: parseFloat(amounts[loc] ?? "0") || 0,
    }));
    mutation.mutate({ positions, recordedAt: new Date().toISOString() });
  }

  function handleAmountChange(loc: string, value: string) {
    setAmounts((prev) => ({ ...prev, [loc]: value }));
  }

  return (
    <section className="space-y-4">
      <h2 className="text-lg font-semibold">Gotówka</h2>

      {cashPending && <p className="text-muted-foreground text-sm">Ładowanie...</p>}

      {latestPositions.length > 0 && (
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground">
              Ostatni snapshot
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-1">
            {latestPositions.map((pos) => (
              <div key={pos.id} className="flex justify-between text-sm">
                <span>{pos.location}</span>
                <span className="font-mono">{formatPln(pos.amountPln)}</span>
              </div>
            ))}
          </CardContent>
        </Card>
      )}

      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-sm font-medium">Nowy snapshot</CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          {CASH_LOCATIONS.map((loc) => (
            <div key={loc} className="flex items-center gap-3">
              <label className="text-sm w-40 shrink-0">{loc}</label>
              <DecimalInput
                value={amounts[loc]}
                onChange={(e) => handleAmountChange(loc, e.target.value)}
                className="w-40"
                aria-label={`Kwota PLN — ${loc}`}
              />
              <span className="text-sm text-muted-foreground">PLN</span>
            </div>
          ))}
          <Button
            onClick={handleSave}
            disabled={mutation.isPending}
            className="mt-2"
          >
            {mutation.isPending ? "Zapisywanie..." : "Zapisz snapshot"}
          </Button>
        </CardContent>
      </Card>
    </section>
  );
}

function EmeryturaContributionSection({ settings }: { settings: Setting[] }) {
  const queryClient = useQueryClient();

  const settingKey = "retirement.monthly_contribution_pln";
  const current = findSetting(settings, settingKey);

  const [amount, setAmount] = useState<string>(current?.value ?? "");

  const mutation = useMutation({
    mutationFn: () => updateSetting(settingKey, { value: amount }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.settings.all });
      toast.success("Składka emerytalna zaktualizowana");
    },
    onError: () => {
      toast.error("Błąd zapisu składki emerytalnej");
    },
  });

  return (
    <section className="space-y-4">
      <h2 className="text-lg font-semibold">Emerytura — miesięczna składka</h2>

      <Card>
        <CardContent className="pt-6 space-y-4">
          <div className="flex items-center gap-3">
            <label htmlFor="emerytura-amount" className="text-sm w-40 shrink-0">
              Składka miesięczna (PLN)
            </label>
            <DecimalInput
              id="emerytura-amount"
              value={amount}
              onChange={(e) => setAmount(e.target.value)}
              className="w-44"
            />
            <span className="text-sm text-muted-foreground">PLN</span>
          </div>
          <Button onClick={() => mutation.mutate()} disabled={mutation.isPending}>
            {mutation.isPending ? "Zapisywanie..." : "Zapisz"}
          </Button>
        </CardContent>
      </Card>
    </section>
  );
}

export default function SettingsPage() {
  const { data: settingsData, isPending, isError } = useQuery({
    queryKey: queryKeys.settings.all,
    queryFn: listSettings,
    staleTime: 10 * 60_000,
  });

  const settings: Setting[] = settingsData?.data ?? [];

  return (
    <div className="p-6 space-y-8 max-w-2xl">
      <PageHeader title="Ustawienia" />

      <CashSection />

      <Separator />

      <NotificationsSection />

      {isPending && <p className="text-muted-foreground text-sm">Ładowanie ustawień...</p>}
      {isError && <p role="alert" className="text-destructive text-sm">Błąd ładowania ustawień.</p>}

      {!isPending && !isError && (
        <>
          <MacroDeriskSection settings={settings} />
          <Separator />
          <MacroStatePanel />
          <Separator />
          <MacroBlendHistoryChart />
          <Separator />
          <EmeryturaContributionSection settings={settings} />
        </>
      )}
    </div>
  );
}
