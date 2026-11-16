import { useState, useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { getLinearRegression } from "../../api/index.js";
import { queryKeys } from "@/lib/queryKeys";
import type { RegressionPoint } from "../../api/index.js";
import {
  ComposedChart,
  Line,
  XAxis,
  YAxis,
  Tooltip,
  type TooltipContentProps,
  Legend,
  ResponsiveContainer,
} from "recharts";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { PageHeader } from "@/components/PageHeader";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { REGRESSION_BAND_COLORS } from "@/lib/chartColors";

// ── types ──────────────────────────────────────────────────────────────

interface QueryParams {
  ticker: string;
  interval: string;
  fromDate: string;
  toDate: string;
  since: number;
}

type ChartRow = RegressionPoint & { xLabel: string };

// ── helpers ─────────────────────────────────────────────────────────────

function fmt(v: number | null | undefined, decimals = 2) {
  if (v == null) return "—";
  return v.toLocaleString("pl-PL", { minimumFractionDigits: decimals, maximumFractionDigits: decimals });
}

function buildLabel(date: string, interval: string) {
  const d = new Date(date + "T00:00:00Z");
  if (interval === "mo") return d.toLocaleDateString("pl-PL", { year: "2-digit", month: "short", timeZone: "UTC" });
  return d.toLocaleDateString("pl-PL", { year: "2-digit", month: "short", day: "numeric", timeZone: "UTC" });
}

function everyNthTick(data: ChartRow[], targetTicks = 12) {
  const step = Math.max(1, Math.floor(data.length / targetTicks));
  return (_: unknown, i: number) => (i % step === 0 ? data[i]?.xLabel ?? "" : "");
}

// ── tooltip ──────────────────────────────────────────────────────────────

type PayloadEntry = { name: string; value: number | null; color: string; dataKey: string };

function RegressionTooltip({ active, payload }: TooltipContentProps) {
  if (!active || !payload?.length) return null;
  const row = (payload[0] as PayloadEntry & { payload: ChartRow }).payload;
  return (
    <div className="rounded-md border bg-popover px-3 py-2 text-xs shadow-md text-popover-foreground space-y-0.5 min-w-[160px]">
      <p className="font-semibold text-sm mb-1">{row.xLabel}</p>
      {row.close != null && <p className="text-blue-500">Cena: {fmt(row.close)}</p>}
      <p className="text-orange-500">Trend: {fmt(row.trendline)}</p>
      <p className="text-green-500">+1SD: {fmt(row.sd1plus)} / −1SD: {fmt(row.sd1minus)}</p>
      <p className="text-yellow-500">+2SD: {fmt(row.sd2plus)} / −2SD: {fmt(row.sd2minus)}</p>
      {row.residual != null && (
        <p className="text-muted-foreground mt-1">Residual: {fmt(row.residual, 4)}</p>
      )}
    </div>
  );
}

// ── chart ────────────────────────────────────────────────────────────────

function RegressionChart({ data, interval }: { data: RegressionPoint[]; interval: string }) {
  const chartData = useMemo(
    () => data.map((p): ChartRow => ({ ...p, xLabel: buildLabel(p.date, interval) })),
    [data, interval]
  );

  return (
    <ResponsiveContainer width="100%" height={560}>
      <ComposedChart data={chartData} margin={{ top: 8, right: 24, bottom: 4, left: 16 }}>
        <XAxis
          dataKey="xLabel"
          tick={{ fontSize: 10 }}
          tickFormatter={everyNthTick(chartData)}
          interval={0}
          minTickGap={40}
        />
        <YAxis
          scale="log"
          domain={["auto", "auto"]}
          tickFormatter={(v: number) => v.toLocaleString("pl-PL", { maximumSignificantDigits: 4 })}
          tick={{ fontSize: 10 }}
          width={64}
          allowDataOverflow
        />
        <Tooltip content={RegressionTooltip} />
        <Legend wrapperStyle={{ fontSize: 11 }} />

        {/* SD bands — drawn first so price sits on top */}
        <Line dataKey="sd4plus"  name="4SD+"  stroke={REGRESSION_BAND_COLORS.sd4} strokeWidth={1} strokeDasharray="2 4" dot={false} legendType="none" />
        <Line dataKey="sd4minus" name="4SD−"  stroke={REGRESSION_BAND_COLORS.sd4} strokeWidth={1} strokeDasharray="2 4" dot={false} legendType="none" />
        <Line dataKey="sd3plus"  name="3SD+"  stroke={REGRESSION_BAND_COLORS.sd3} strokeWidth={1} strokeDasharray="2 3" dot={false} legendType="none" />
        <Line dataKey="sd3minus" name="3SD−"  stroke={REGRESSION_BAND_COLORS.sd3} strokeWidth={1} strokeDasharray="2 3" dot={false} legendType="none" />
        <Line dataKey="sd2plus"  name="2SD+"  stroke={REGRESSION_BAND_COLORS.sd2} strokeWidth={1} strokeDasharray="4 3" dot={false} legendType="none" />
        <Line dataKey="sd2minus" name="2SD−"  stroke={REGRESSION_BAND_COLORS.sd2} strokeWidth={1} strokeDasharray="4 3" dot={false} legendType="none" />
        <Line dataKey="sd1plus"  name="1SD+"  stroke={REGRESSION_BAND_COLORS.sd1} strokeWidth={1} strokeDasharray="4 2" dot={false} legendType="none" />
        <Line dataKey="sd1minus" name="1SD−"  stroke={REGRESSION_BAND_COLORS.sd1} strokeWidth={1} strokeDasharray="4 2" dot={false} legendType="none" />

        {/* Trendline */}
        <Line
          dataKey="trendline"
          name="Trend"
          stroke={REGRESSION_BAND_COLORS.trend}
          strokeWidth={1.5}
          dot={false}
          connectNulls
        />

        {/* Actual close price */}
        <Line
          dataKey="close"
          name="Cena"
          stroke={REGRESSION_BAND_COLORS.price}
          strokeWidth={2}
          dot={false}
        />
      </ComposedChart>
    </ResponsiveContainer>
  );
}

// ── controls ─────────────────────────────────────────────────────────────

const today = new Date().toISOString().slice(0, 10);
const tenYearsAgo = new Date(Date.now() - 10 * 365.25 * 86400 * 1000).toISOString().slice(0, 10);

const DEFAULT: QueryParams = {
  ticker: "BTC-USD",
  interval: "d",
  fromDate: tenYearsAgo,
  toDate: today,
  since: 30,
};

// ── page ──────────────────────────────────────────────────────────────────

export default function RegressionPage() {
  const [draft, setDraft] = useState<QueryParams>(DEFAULT);
  const [params, setParams] = useState<QueryParams>(DEFAULT);

  const { data, isFetching, isError, error } = useQuery({
    queryKey: queryKeys.linearRegression.params(params),
    queryFn: () =>
      getLinearRegression({
        ticker: params.ticker,
        interval: params.interval,
        fromDate: params.fromDate || undefined,
        toDate: params.toDate || undefined,
        since: params.since,
      }),
    staleTime: 5 * 60_000,
    enabled: !!params.ticker,
  });

  function apply() {
    setParams({ ...draft });
  }

  return (
    <div className="p-4 lg:p-6 space-y-4">
      <PageHeader eyebrow="Portfel" title="Regresja Liniowa" />

      {/* ── Controls ── */}
      <Card>
        <CardContent className="pt-4">
          <div className="flex flex-wrap gap-4 items-end">
            <div className="space-y-1">
              <Label>Ticker (Yahoo Finance)</Label>
              <Input
                className="w-32"
                value={draft.ticker}
                onChange={(e) => setDraft((d) => ({ ...d, ticker: e.target.value.toUpperCase() }))}
                onKeyDown={(e) => e.key === "Enter" && apply()}
                placeholder="np. VTI"
              />
            </div>

            <div className="space-y-1">
              <Label>Interwał</Label>
              <Select
                value={draft.interval}
                onValueChange={(v) => setDraft((d) => ({ ...d, interval: v }))}
              >
                <SelectTrigger className="w-28">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="wk">Tygodniowy</SelectItem>
                  <SelectItem value="mo">Miesięczny</SelectItem>
                  <SelectItem value="d">Dzienny</SelectItem>
                </SelectContent>
              </Select>
            </div>

            <div className="space-y-1">
              <Label>Od</Label>
              <Input
                type="date"
                className="w-36"
                value={draft.fromDate}
                onChange={(e) => setDraft((d) => ({ ...d, fromDate: e.target.value }))}
              />
            </div>

            <div className="space-y-1">
              <Label>Do</Label>
              <Input
                type="date"
                className="w-36"
                value={draft.toDate}
                onChange={(e) => setDraft((d) => ({ ...d, toDate: e.target.value }))}
              />
            </div>

            <div className="space-y-1">
              <Label>Okresy naprzód</Label>
              <Input
                type="number"
                className="w-24"
                min={0}
                max={520}
                value={draft.since}
                onChange={(e) => setDraft((d) => ({ ...d, since: Number(e.target.value) }))}
              />
            </div>

            <Button onClick={apply} disabled={isFetching}>
              {isFetching ? "Ładowanie…" : "Generuj"}
            </Button>
          </div>
        </CardContent>
      </Card>

      {/* ── Error ── */}
      {isError && (
        <p className="text-sm text-destructive">
          Błąd: {error instanceof Error ? error.message : "Nieznany błąd"}
        </p>
      )}

      {/* ── Chart ── */}
      {isFetching && <Skeleton className="h-[560px] w-full rounded-xl" />}

      {data && !isFetching && (
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-base">
              {data.ticker} — {data.interval === "wk" ? "tygodniowy" : data.interval === "mo" ? "miesięczny" : "dzienny"}
              <span className="ml-3 text-xs font-normal text-muted-foreground">
                slope={data.slope.toExponential(4)} · stdDev={data.stdDev.toFixed(4)} · {data.dataPoints.length} punktów
              </span>
            </CardTitle>
          </CardHeader>
          <CardContent className="pl-0 pr-2">
            <RegressionChart data={data.dataPoints} interval={data.interval} />
          </CardContent>
        </Card>
      )}

      {/* ── Legend note ── */}
      {data && !isFetching && (
        <p className="text-xs text-muted-foreground">
          Skala logarytmiczna · Pasma SD: zielony ±1SD, żółty ±2SD, pomarańczowy ±3SD, czerwony ±4SD · Punkty bez ceny = ekstrapolacja
        </p>
      )}
    </div>
  );
}
