import { useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { addDays } from "date-fns";
import { listBotCalendar, getBotPlanLatest, listBotTrades, getBotStats, getBotOpenPositions, listBotSignals, getBotControl, updateBotControl } from "../../api/index.js";
import { PageHeader } from "@/components/PageHeader";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { queryKeys } from "@/lib/queryKeys";

function impactBadge(impact: string) {
  const v = impact === "HIGH" ? "destructive" : impact === "MEDIUM" ? "secondary" : "outline";
  return <Badge variant={v as never}>{impact}</Badge>;
}

function BotControlPanel() {
  const queryClient = useQueryClient();
  const [reason, setReason] = useState("");

  const { data: controlData, isPending, isError } = useQuery({
    queryKey: queryKeys.bot.control(),
    queryFn: getBotControl,
    staleTime: 15_000,
  });

  const mutation = useMutation({
    mutationFn: updateBotControl,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.bot.control() });
      setReason("");
      toast.success("Status tradingu zaktualizowany");
    },
    onError: () => {
      toast.error("Błąd zapisu statusu tradingu");
    },
  });

  const control = controlData?.data;
  const tradingEnabled = control?.tradingEnabled ?? true;

  function handlePause() {
    mutation.mutate({ tradingEnabled: false, pausedReason: reason.trim() || undefined });
  }

  function handleResume() {
    mutation.mutate({ tradingEnabled: true });
  }

  return (
    <Card className={tradingEnabled ? undefined : "border-muted-foreground/40"}>
      <CardHeader className="pb-2">
        <CardTitle className="text-sm font-medium">Kontrola tradingu</CardTitle>
      </CardHeader>
      <CardContent>
        {isPending ? (
          <Skeleton className="h-9 w-64" />
        ) : isError ? (
          <p role="alert" className="text-sm text-destructive">Błąd ładowania statusu tradingu</p>
        ) : (
          <div className="flex flex-wrap items-center gap-3" aria-live="polite">
            {tradingEnabled ? (
              <>
                <Badge variant="outline" className="text-neon-gain border-neon-gain/40">Trading aktywny</Badge>
                <Input
                  value={reason}
                  onChange={(e) => setReason(e.target.value)}
                  placeholder="Powód wstrzymania (opcjonalnie)"
                  aria-label="Powód wstrzymania tradingu (opcjonalnie)"
                  className="w-64"
                  disabled={mutation.isPending}
                />
                <Button variant="destructive" onClick={handlePause} disabled={mutation.isPending}>
                  {mutation.isPending ? "Wstrzymywanie..." : "Wstrzymaj"}
                </Button>
              </>
            ) : (
              <>
                <Badge variant="secondary">Trading wstrzymany</Badge>
                {control?.pausedReason && (
                  <span className="text-sm text-muted-foreground">{control.pausedReason}</span>
                )}
                <Button variant="default" onClick={handleResume} disabled={mutation.isPending}>
                  {mutation.isPending ? "Wznawianie..." : "Wznów"}
                </Button>
              </>
            )}
            {control?.updatedAt && (
              <span className="text-xs text-muted-foreground w-full">
                Ostatnia zmiana: {new Date(control.updatedAt).toLocaleString("pl-PL")}
              </span>
            )}
          </div>
        )}
      </CardContent>
    </Card>
  );
}

export default function BotPage() {
  const today = new Date().toISOString().slice(0, 10);
  const weekTo = addDays(new Date(), 7).toISOString().slice(0, 10);
  const calParams = { from: today, to: weekTo, impact: "HIGH,MEDIUM", currency: "USD", size: 100 } as const;
  const { data: calendarData, isPending: calPending } = useQuery({ queryKey: queryKeys.bot.calendar(calParams), queryFn: () => listBotCalendar(calParams), staleTime: 60_000 });
  const { data: planData } = useQuery({ queryKey: queryKeys.bot.planLatest(), queryFn: () => getBotPlanLatest().catch(() => null), staleTime: 60_000 });
  const { data: openData } = useQuery({ queryKey: queryKeys.bot.openPositions(), queryFn: () => getBotOpenPositions(), staleTime: 10_000, refetchInterval: 15_000 });
  const { data: tradesData } = useQuery({ queryKey: queryKeys.bot.trades({}), queryFn: () => listBotTrades({ size: 20 }), staleTime: 30_000 });
  const { data: signalsData } = useQuery({ queryKey: queryKeys.bot.signals({}), queryFn: () => listBotSignals({ size: 20 }), staleTime: 30_000 });
  const { data: statsData } = useQuery({ queryKey: queryKeys.bot.stats(), queryFn: () => getBotStats(), staleTime: 30_000 });

  const calendar = calendarData?.data ?? [];
  const plan = (planData as unknown as { data?: { status?: string; summary?: string; scenarios?: { id: number; eventName: string; action: string; leverage?: number; triggerCondition: string; reasoning: string }[] } })?.data ?? null;
  const openTrades = (openData as unknown as { data?: unknown[] })?.data ?? (openData as unknown as never as { data?: unknown[] } | undefined)?.data ?? [];
  const trades = tradesData?.data ?? [];
  const signals = signalsData?.data ?? [];
  const stats = (statsData as unknown as { data?: { totalTrades: number; winRate: number | null; totalPnlPln: number; openTrades: number } })?.data ?? null;

  const openList = (Array.isArray(openTrades) ? openTrades : []) as { id: number; side: string; entryPrice: number; currentPrice: number | null; unrealizedPnlPln: number | null; unrealizedPnlPct: number | null; leverage: number; heldSeconds: number | null; timeToHardCloseSeconds: number | null }[];

  return (
    <div className="p-6 space-y-6">
      <PageHeader eyebrow="Bot · OKX BTC" title="Bot Trading" description="Kalendarz makro, rekomendacje Claude 06:00, sygnały i pozycje" />

      <BotControlPanel />

      {stats && (
        <Card>
          <CardHeader><CardTitle>Statystyki</CardTitle></CardHeader>
          <CardContent className="flex flex-wrap gap-4 text-sm">
            <span>Trades: <b>{stats.totalTrades}</b></span>
            <span>Otwarte: <b>{stats.openTrades}</b></span>
            <span>Winrate: <b>{stats.winRate ?? "-"}%</b></span>
            <span>PnL: <b className={stats.totalPnlPln >= 0 ? "text-neon-gain" : "text-neon-loss"}>{stats.totalPnlPln?.toFixed(2)} PLN</b></span>
          </CardContent>
        </Card>
      )}

      <Card>
        <CardHeader><CardTitle>Otwarte pozycje</CardTitle></CardHeader>
        <CardContent>
          {openList.length === 0 ? <p className="text-sm text-muted-foreground">Brak otwartych pozycji</p> : (
            <div className="space-y-2">
              {openList.map(t => (
                <div key={t.id} className="flex flex-wrap gap-3 items-center text-sm border rounded-md p-3">
                  <Badge variant={t.side === "LONG" ? "default" : "destructive"}>{t.side} x{t.leverage}</Badge>
                  <span>Entry {t.entryPrice}</span>
                  <span>Now {t.currentPrice ?? "-"}</span>
                  <span className={ (t.unrealizedPnlPln ?? 0) >= 0 ? "text-neon-gain" : "text-neon-loss"}>{t.unrealizedPnlPln?.toFixed(2)} PLN ({t.unrealizedPnlPct?.toFixed(2)}%)</span>
                  <span className="text-muted-foreground">{t.heldSeconds != null ? `${Math.floor(t.heldSeconds/60)}m` : ""} / 15m {t.timeToHardCloseSeconds != null ? `(${Math.floor(t.timeToHardCloseSeconds/60)}m left)` : ""}</span>
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>

      <div className="grid md:grid-cols-2 gap-6">
        <Card>
          <CardHeader><CardTitle>Nadchodzące wydarzenia (7 dni — USD, wysoka/średnia ważność)</CardTitle></CardHeader>
          <CardContent className="space-y-2">
            {calPending ? <Skeleton className="h-20 w-full" /> : calendar.length === 0 ? <p className="text-sm text-muted-foreground">Brak wydarzeń</p> : calendar.map(e => (
              <div key={e.id} className="flex justify-between items-start text-sm border-b py-2 last:border-0">
                <div>
                  <div className="font-medium">{e.eventName} <span className="font-mono text-xs text-muted-foreground">{e.currency}</span></div>
                  <div className="text-xs text-muted-foreground">{new Date(e.eventTime).toLocaleString("pl-PL")} · F:{e.forecast ?? "-"} P:{e.previous ?? "-"} A:{e.actual ?? "-"}</div>
                </div>
                {impactBadge(e.impact)}
              </div>
            ))}
          </CardContent>
        </Card>

        <Card>
          <CardHeader><CardTitle>Plan na dziś (Claude 06:00)</CardTitle></CardHeader>
          <CardContent>
            {!plan ? <p className="text-sm text-muted-foreground">Brak planu — Claude jeszcze nie wygenerował</p> : plan.status === "EMPTY" ? (
              <p className="text-sm text-muted-foreground">Brak wydarzeń o wysokiej ważności (USD) dziś — Claude nie był wywoływany.</p>
            ) : (
              <div className="space-y-3">
                <p className="text-sm">{plan.summary ?? "—"}</p>
                {(plan.scenarios ?? []).map(s => (
                  <div key={s.id} className="rounded-md border p-3 space-y-1">
                    <div className="flex gap-2 items-center"><b className="text-sm">{s.eventName}</b> <Badge variant={s.action === "HOLD" ? "outline" : s.action === "LONG" ? "default" : "destructive"}>{s.action} {s.leverage ? `x${s.leverage}` : ""}</Badge> <span className="text-xs text-muted-foreground">{s.triggerCondition}</span></div>
                    <p className="text-xs text-muted-foreground">{s.reasoning}</p>
                  </div>
                ))}
              </div>
            )}
          </CardContent>
        </Card>
      </div>

      <div className="grid md:grid-cols-2 gap-6">
        <Card>
          <CardHeader><CardTitle>Sygnały</CardTitle></CardHeader>
          <CardContent className="space-y-2">
            {signals.length === 0 ? <p className="text-sm text-muted-foreground">Brak sygnałów</p> : signals.map(s => (
              <div key={s.id} className="text-sm border-b py-2 last:border-0 flex justify-between"><span>{s.eventName} · {s.actual} (F:{s.forecast ?? "-"})</span><Badge variant={s.decision === "LONG" ? "default" : s.decision === "SHORT" ? "destructive" : "outline"}>{s.decision}</Badge></div>
            ))}
          </CardContent>
        </Card>
        <Card>
          <CardHeader><CardTitle>Historia trades</CardTitle></CardHeader>
          <CardContent className="space-y-2">
            {trades.length === 0 ? <p className="text-sm text-muted-foreground">Brak trades</p> : trades.map(t => (
              <div key={t.id} className="text-sm border-b py-2 last:border-0 flex justify-between"><span>{t.side} x{t.leverage} · {t.entryPrice} → {t.exitPrice ?? "..."} </span><span className={t.status === "OPEN" ? "text-neon-cyan" : (t.pnlPln ?? 0) >= 0 ? "text-neon-gain" : "text-neon-loss"}>{t.status} {t.pnlPln != null ? `${t.pnlPln.toFixed(2)} PLN` : ""}</span></div>
            ))}
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
