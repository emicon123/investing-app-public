import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { AlertTriangle, RefreshCw } from "lucide-react";
import { getMacroState, refreshMacroState, type MacroComponentStatus, type MacroState } from "../../../api/index.js";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { queryKeys } from "@/lib/queryKeys";
import { cn } from "@/lib/utils";

function formatPct(value: number | null): string {
  if (value === null) return "—";
  return `${(value * 100).toFixed(2)}%`;
}

function formatSignedPct(value: number | null): string {
  if (value === null) return "—";
  const pct = value * 100;
  const sign = pct > 0 ? "+" : "";
  return `${sign}${pct.toFixed(2)}%`;
}

function formatUsdMln(value: number | null): string {
  if (value === null) return "—";
  return `${value.toLocaleString("pl-PL", { maximumFractionDigits: 0 })} mln USD`;
}

function formatBlendFactor(value: number | null): string {
  if (value === null) return "— (bootstrap)";
  return value.toFixed(2);
}

// Exported for reuse by MacroBlendHistoryChart.tsx (ADR-069) — one place for
// this app's Polish zone terminology, so the history chart's tooltip never
// drifts into a fresh translation of the same MacroZone values.
export const ZONE_LABELS: Record<MacroState["zone"] & string, string> = {
  RISK_ON: "risk-on",
  MID_ON: "lekki risk-on",
  NEUTRAL: "neutralna",
  MID_OFF: "lekki risk-off",
  RISK_OFF: "risk-off",
};

export function formatZone(zone: MacroState["zone"]): string {
  if (zone === null) return "—";
  return ZONE_LABELS[zone];
}

function formatDateTime(iso: string): string {
  try {
    return new Date(iso).toLocaleString("pl-PL");
  } catch {
    return iso;
  }
}

/** Global M2 is stale when any manually-pasted component (China/Japan) is stale. */
function isGlobalM2Stale(components: MacroComponentStatus[]): boolean {
  return components.some((c) => c.source === "MANUAL" && c.stale);
}

function RiskBadge({ riskOff }: { riskOff: boolean }) {
  return riskOff ? (
    <Badge variant="destructive">Risk-off (aktywny)</Badge>
  ) : (
    <Badge variant="outline" className="border-neon-green/50 text-neon-green">
      Risk-on
    </Badge>
  );
}

function SignalCard({
  title,
  yoy,
  accel,
  latest,
  riskOff,
}: {
  title: string;
  yoy: number | null;
  accel: number | null;
  latest: number | null;
  riskOff: boolean | null;
}) {
  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="flex items-center justify-between text-sm font-medium">
          <span>{title}</span>
          {riskOff !== null && (
            <span
              className={cn(
                "text-xs font-normal",
                riskOff ? "text-neon-loss" : "text-neon-green"
              )}
            >
              {riskOff ? "deceleracja" : "wzrost"}
            </span>
          )}
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-1.5 text-sm">
        <div className="flex justify-between">
          <span className="text-muted-foreground">YoY</span>
          <span className="font-mono">{formatPct(yoy)}</span>
        </div>
        <div className="flex justify-between">
          <span className="text-muted-foreground">Δ3M (przyspieszenie)</span>
          <span
            className={cn(
              "font-mono",
              accel !== null && accel < 0 && "text-neon-loss"
            )}
          >
            {formatSignedPct(accel)}
          </span>
        </div>
        <div className="flex justify-between">
          <span className="text-muted-foreground">Ostatnia wartość</span>
          <span className="font-mono">{formatUsdMln(latest)}</span>
        </div>
      </CardContent>
    </Card>
  );
}

function ComponentsList({ components }: { components: MacroComponentStatus[] }) {
  if (components.length === 0) return null;
  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="text-sm font-medium text-muted-foreground">
          Źródła danych
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-1.5">
        {components.map((c) => (
          <div
            key={c.seriesCode}
            className="flex items-center justify-between text-sm"
          >
            <span className="font-mono">{c.seriesCode}</span>
            <span className="flex items-center gap-2 text-muted-foreground">
              <span className="text-xs">
                {c.source} · {c.latestObservationDate}
              </span>
              {c.stale ? (
                <Badge variant="destructive" className="text-[10px]">
                  nieaktualne
                </Badge>
              ) : (
                <Badge
                  variant="outline"
                  className="border-neon-green/50 text-[10px] text-neon-green"
                >
                  aktualne
                </Badge>
              )}
            </span>
          </div>
        ))}
      </CardContent>
    </Card>
  );
}

function MacroStateContent({ state }: { state: MacroState }) {
  const globalM2Stale = isGlobalM2Stale(state.components);

  return (
    <div className="space-y-4">
      {globalM2Stale && (
        <div
          role="alert"
          className="flex items-start gap-2 rounded-md border border-destructive/40 bg-destructive/10 p-3 text-sm text-destructive"
        >
          <AlertTriangle className="mt-0.5 size-4 shrink-0" aria-hidden="true" />
          <span>
            Dane Global M2 (Chiny/Japonia) są nieaktualne — odśwież ręczny wpis
            (m2-manual), aby sygnał był wiarygodny.
          </span>
        </div>
      )}

      <div className="grid gap-4 sm:grid-cols-2">
        <SignalCard
          title="Płynność netto (net liquidity)"
          yoy={state.netLiqYoy}
          accel={state.netLiqYoyAccel}
          latest={state.netLiqLatestUsdMln}
          riskOff={state.netLiqRiskOff}
        />
        <SignalCard
          title="Global M2"
          yoy={state.globalM2Yoy}
          accel={state.globalM2YoyAccel}
          latest={state.globalM2LatestUsdMln}
          riskOff={state.globalM2RiskOff}
        />
      </div>

      <ComponentsList components={state.components} />

      <div className="flex flex-wrap gap-x-6 gap-y-1 text-xs text-muted-foreground">
        <span>Dane na dzień: {state.asOfDate}</span>
        <span>Obliczono: {formatDateTime(state.computedAt)}</span>
        <span>
          De-risk: {state.deriskEnabled ? "włączony" : "wyłączony"} · blend{" "}
          {formatBlendFactor(state.blendFactor)} · strefa {formatZone(state.zone)}
        </span>
        <span>
          composite Z {state.compositeZ === null ? "—" : state.compositeZ.toFixed(2)} ·
          smoothed Z {state.smoothedZ === null ? "—" : state.smoothedZ.toFixed(2)}
        </span>
      </div>
    </div>
  );
}

export function MacroStatePanel() {
  const queryClient = useQueryClient();
  const { data, isPending, isError } = useQuery({
    queryKey: queryKeys.macroState.all,
    queryFn: getMacroState,
    staleTime: 5 * 60_000,
  });

  // "Odśwież" must RECOMPUTE, not just re-read. It used to call the query's
  // refetch(), which re-issued the GET and re-rendered the identical stored row —
  // so the button appeared to do nothing. The recompute is the POST below; we seed
  // the cache with its response so the panel updates without a second round-trip.
  const refreshMutation = useMutation({
    mutationFn: refreshMacroState,
    onSuccess: (fresh) => {
      queryClient.setQueryData(queryKeys.macroState.all, fresh);
      void queryClient.invalidateQueries({ queryKey: queryKeys.macroState.all });
    },
  });

  const state = data?.data;
  const isRefreshing = refreshMutation.isPending;

  return (
    <section className="space-y-4">
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold">Stan makro (de-risk)</h2>
        <div className="flex items-center gap-3">
          {state && <RiskBadge riskOff={state.riskOff} />}
          <Button
            variant="outline"
            size="sm"
            onClick={() => refreshMutation.mutate()}
            disabled={isRefreshing}
            aria-label="Przelicz stan makro ze źródeł zewnętrznych"
          >
            <RefreshCw
              className={cn("size-4", isRefreshing && "animate-spin")}
              aria-hidden="true"
            />
            {isRefreshing ? "Przeliczanie…" : "Odśwież"}
          </Button>
        </div>
      </div>

      {isPending && (
        <p className="text-muted-foreground text-sm">Ładowanie stanu makro...</p>
      )}
      {isError && (
        <p role="alert" className="text-destructive text-sm">
          Błąd ładowania stanu makro.
        </p>
      )}
      {refreshMutation.isError && (
        <p role="alert" className="text-destructive text-sm">
          Nie udało się przeliczyć stanu makro. Spróbuj ponownie.
        </p>
      )}
      {isRefreshing && (
        <p className="text-muted-foreground text-sm">
          Pobieranie danych ze źródeł zewnętrznych (FRED, ECB, BoE, BoJ,
          chinadata.live)… to może potrwać kilkanaście sekund.
        </p>
      )}

      {state && <MacroStateContent state={state} />}
    </section>
  );
}
