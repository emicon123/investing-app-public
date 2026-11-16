import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { getTradeChart, getTradeChartTickers, type TradeChartMarker, type TradeType } from "../../api/index.js";
import {
  ComposedChart,
  Line,
  Bar,
  Scatter,
  XAxis,
  YAxis,
  Tooltip,
  type TooltipContentProps,
  ResponsiveContainer,
} from "recharts";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { PageHeader } from "@/components/PageHeader";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { queryKeys } from "@/lib/queryKeys";
import { TRADE_TYPE_COLORS, TRADE_TYPE_LABELS, isBuy, tradeDotRadius } from "@/lib/tradeTypeColors";
import { TickerPicker } from "./_components/TickerPicker";

// ── types ──────────────────────────────────────────────────────────────────

interface QueryParams {
  interval: string;
  fromDate: string;
  toDate: string;
}

/**
 * One row of the merged chart dataset — either a price-series point (`close`/
 * `open`/`high`/`low` set) or a trade marker (`y`/`trade` set), never both by
 * construction. A single shared, ts-sorted array (rather than separate
 * per-series datasets) is what lets Recharts' built-in axis-triggered Tooltip
 * snap correctly to whichever is nearest — see buildRows below.
 */
interface ChartRow {
  ts: number;
  open?: number;
  close?: number;
  high?: number;
  low?: number;
  y?: number;
  trade?: TradeChartMarker;
}

type ChartMode = "line" | "candle";

const PORTFOLIO_LABELS: Record<string, string> = {
  MAIN: "Główny",
  RETIREMENT: "Emerytura",
};

// ── helpers ─────────────────────────────────────────────────────────────────

function toTs(iso: string): number {
  const s = iso.length <= 10 ? `${iso}T00:00:00Z` : iso;
  return new Date(s).getTime();
}

function fmt(v: number | null | undefined, decimals = 2): string {
  if (v == null || Number.isNaN(v)) return "—";
  return v.toLocaleString("pl-PL", { minimumFractionDigits: decimals, maximumFractionDigits: decimals });
}

function fmtUnits(v: number): string {
  return v.toLocaleString("pl-PL", { minimumFractionDigits: 0, maximumFractionDigits: 6 });
}

function formatAxisTick(ts: number, interval: string): string {
  const d = new Date(ts);
  if (interval === "mo") return d.toLocaleDateString("pl-PL", { year: "2-digit", month: "short", timeZone: "UTC" });
  return d.toLocaleDateString("pl-PL", { year: "2-digit", month: "short", day: "numeric", timeZone: "UTC" });
}

function formatDateOnly(ts: number): string {
  return new Date(ts).toLocaleDateString("pl-PL", { year: "numeric", month: "short", day: "numeric", timeZone: "UTC" });
}

function formatDateTime(iso: string): string {
  return new Date(iso).toLocaleString("pl-PL", {
    year: "numeric",
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

function buildRows(
  priceSeries: { date: string; open: number; close: number; high: number; low: number }[],
  trades: TradeChartMarker[]
): ChartRow[] {
  const priceRows: ChartRow[] = priceSeries.map((p) => ({
    ts: toTs(p.date),
    open: p.open,
    close: p.close,
    high: p.high,
    low: p.low,
  }));
  const tradeRows: ChartRow[] = trades.map((t) => ({ ts: toTs(t.purchasedAt), y: t.priceNative, trade: t }));
  return [...priceRows, ...tradeRows].sort((a, b) => a.ts - b.ts);
}

function everyNthTick(rows: ChartRow[], interval: string, targetTicks = 12) {
  const step = Math.max(1, Math.floor(rows.length / targetTicks));
  return (ts: number, i: number) => (i % step === 0 ? formatAxisTick(ts, interval) : "");
}

// ── tooltip ──────────────────────────────────────────────────────────────────

type PayloadEntry = { dataKey: string; value: number | null; payload: ChartRow };

function TradeHistoryTooltip({
  active,
  payload,
  priceCurrency,
}: TooltipContentProps & { priceCurrency?: string }) {
  if (!active || !payload?.length) return null;
  const entries = payload as unknown as PayloadEntry[];
  // Matched by payload shape, not dataKey — the price series renders as either a Line
  // (dataKey "close") or, in candle mode, a Bar with a computed range dataKey, so dataKey
  // alone isn't a stable discriminator across both chart modes.
  const tradeEntry = entries.find((p) => p.payload?.trade);
  const priceEntry = entries.find((p) => !p.payload?.trade && p.payload?.close != null);

  if (tradeEntry?.payload.trade) {
    const row = tradeEntry.payload.trade;
    const buy = isBuy(row.unitsDelta);
    return (
      <div className="min-w-[220px] space-y-1 rounded-md border bg-popover px-3 py-2 text-xs text-popover-foreground shadow-md">
        <div className="flex items-center gap-1.5">
          <span
            className="inline-block h-2.5 w-2.5 shrink-0 rounded-full"
            style={{ background: TRADE_TYPE_COLORS[row.tradeType] }}
            aria-hidden="true"
          />
          <p className="text-sm font-semibold">{TRADE_TYPE_LABELS[row.tradeType]}</p>
        </div>
        <p className="text-muted-foreground">{formatDateTime(row.purchasedAt)}</p>
        <p className="font-mono text-[11px] text-muted-foreground">
          {PORTFOLIO_LABELS[row.portfolio] ?? row.portfolio} · {row.accountType}
        </p>
        <p className={buy ? "text-neon-gain" : "text-neon-loss"}>
          {buy ? "+" : ""}
          {fmtUnits(row.unitsDelta)} jedn. · {fmt(row.totalPln)} PLN
        </p>
        <p className="text-muted-foreground">
          Cena: {fmt(row.pricePln)} PLN
          {priceCurrency && priceCurrency !== "PLN" ? ` · ${fmt(row.priceNative)} ${priceCurrency}` : ""}
        </p>
      </div>
    );
  }

  if (priceEntry) {
    const { open, close, high, low } = priceEntry.payload;
    return (
      <div className="min-w-[140px] space-y-0.5 rounded-md border bg-popover px-3 py-2 text-xs text-popover-foreground shadow-md">
        <p className="text-sm font-semibold">{formatDateOnly(priceEntry.payload.ts)}</p>
        <p className="text-blue-500">
          Zamknięcie: {fmt(close)} {priceCurrency}
        </p>
        {open != null && high != null && low != null && (
          <p className="text-muted-foreground">
            O {fmt(open)} · H {fmt(high)} · L {fmt(low)}
          </p>
        )}
      </div>
    );
  }

  return null;
}

// ── chart ────────────────────────────────────────────────────────────────────

// Module-level factory (not an inline component) — Recharts' custom `shape`
// render prop needs the current dataset's max |totalPln| to scale dot radius
// by area (sqrt), so it's parameterized via closure rather than a constant.
function makeTradeShape(maxAbsTotalPln: number) {
  return function TradeDotShape(props: unknown) {
    // Recharts' Scatter shape props (cx/cy/payload) aren't exported from the
    // package's public types — see node_modules/recharts/types/cartesian/Scatter.d.ts.
    const { cx, cy, payload } = props as { cx?: number; cy?: number; payload?: ChartRow };
    const trade = payload?.trade;
    if (cx == null || cy == null || !trade) return <g />;
    const radius = tradeDotRadius(Math.abs(trade.totalPln), maxAbsTotalPln);
    const color = TRADE_TYPE_COLORS[trade.tradeType];
    return (
      <circle
        cx={cx}
        cy={cy}
        r={radius}
        fill={color}
        fillOpacity={trade.tradeType === "SEED_IMPORT" ? 0.5 : 0.85}
        stroke="var(--background)"
        strokeWidth={1}
      />
    );
  };
}

/**
 * Recharts has no built-in candlestick mark. Standard trick: a `Bar` whose `dataKey` resolves
 * to a [low, high] range (Recharts treats an array dataKey value as a range and sizes the bar's
 * y/height to span exactly that range in pixels — see recharts/es6/util/ChartUtils.js's
 * `Array.isArray(entryValue)` handling, confirmed against the installed version before relying
 * on it) — then a custom `shape` derives the open/close body's pixel position from that same
 * y/height, since they represent exactly the high→low span: pxPerUnit = height / (high - low).
 * No separate y-scale needs threading through — the bar's own geometry IS the scale.
 */
function CandleShape(props: unknown) {
  const { x, y, width, height, payload } = props as {
    x?: number;
    y?: number;
    width?: number;
    height?: number;
    payload?: ChartRow;
  };
  const { open, close, high, low } = payload ?? {};
  if (x == null || y == null || width == null || height == null) return <g />;
  if (open == null || close == null || high == null || low == null || high <= low) return <g />;

  const pxPerUnit = height / (high - low);
  const yFor = (v: number) => y + (high - v) * pxPerUnit;
  const bodyTop = yFor(Math.max(open, close));
  const bodyBottom = yFor(Math.min(open, close));
  const bodyHeight = Math.max(1, bodyBottom - bodyTop);
  const up = close >= open;
  const color = up ? "var(--neon-gain)" : "var(--neon-loss)";
  const cx = x + width / 2;
  const bodyWidth = Math.max(2, width * 0.6);

  return (
    <g>
      <line x1={cx} x2={cx} y1={y} y2={y + height} stroke={color} strokeWidth={1} />
      <rect x={cx - bodyWidth / 2} y={bodyTop} width={bodyWidth} height={bodyHeight} fill={color} />
    </g>
  );
}

function TradeHistoryChart({
  rows,
  interval,
  priceCurrency,
  logScale,
  chartMode,
  maxAbsTotalPln,
}: {
  rows: ChartRow[];
  interval: string;
  priceCurrency: string;
  logScale: boolean;
  chartMode: ChartMode;
  maxAbsTotalPln: number;
}) {
  const tradeShape = useMemo(() => makeTradeShape(maxAbsTotalPln), [maxAbsTotalPln]);

  // Y-domain is derived from the price series ONLY (open/high/low/close, so candle wicks are
  // never clipped), never from trade dots. A SEED_IMPORT marker's price is a synthetic
  // 2024-01-01 placeholder (often literally 1 PLN — a fallback baked into the original
  // migration for positions with no recorded avg_buy_price) that can sit an order of magnitude
  // away from the asset's real price range. Letting "auto" domain include it stretches the axis
  // and crushes the real price line — reported as "dots don't lie on the chart". Pinning the
  // domain to the price series (with `allowDataOverflow` below) instead clips any such outlier
  // dot rather than distorting the whole chart for every other point.
  const yDomain = useMemo((): [number, number] | ["auto", "auto"] => {
    const values = rows.flatMap((r) => [r.close, r.high, r.low]).filter((v): v is number => v != null);
    if (values.length === 0) return ["auto", "auto"];
    const min = Math.min(...values);
    const max = Math.max(...values);
    const pad = (max - min) * 0.08 || max * 0.08 || 1;
    const lower = logScale ? Math.max(min * 0.5, 0.01) : Math.max(0, min - pad);
    return [lower, max + pad];
  }, [rows, logScale]);

  // Bar's dataKey as a value-accessor function, resolving to [low, high] so Recharts sizes the
  // rect to span that range — only meaningful for price rows (trade/undefined rows resolve to
  // [0, 0], invisible since CandleShape bails out when open/close/high/low aren't all present).
  const candleRange = (row: ChartRow): [number, number] =>
    row.low != null && row.high != null ? [row.low, row.high] : [0, 0];

  return (
    <ResponsiveContainer width="100%" height={560}>
      <ComposedChart data={rows} margin={{ top: 8, right: 24, bottom: 4, left: 16 }}>
        <XAxis
          type="number"
          dataKey="ts"
          domain={["dataMin", "dataMax"]}
          tickFormatter={everyNthTick(rows, interval)}
          tick={{ fontSize: 10 }}
          interval={0}
          minTickGap={40}
        />
        <YAxis
          type="number"
          scale={logScale ? "log" : "linear"}
          domain={yDomain}
          tickFormatter={(v: number) => v.toLocaleString("pl-PL", { maximumSignificantDigits: 4 })}
          tick={{ fontSize: 10 }}
          width={64}
          allowDataOverflow
        />
        <Tooltip
          content={(props: TooltipContentProps) => (
            <TradeHistoryTooltip {...props} priceCurrency={priceCurrency} />
          )}
        />

        {chartMode === "candle" ? (
          <Bar dataKey={candleRange} name="Cena" shape={CandleShape} isAnimationActive={false} />
        ) : (
          <Line
            dataKey="close"
            name="Cena"
            stroke="#3b82f6"
            strokeWidth={2}
            dot={false}
            connectNulls
            isAnimationActive={false}
          />
        )}
        <Scatter dataKey="y" name="Transakcje" shape={tradeShape} isAnimationActive={false} />
      </ComposedChart>
    </ResponsiveContainer>
  );
}

// ── legend ───────────────────────────────────────────────────────────────────

const LEGEND_ORDER: TradeType[] = [
  "DCA_BUY",
  "MIGRATION_BUY",
  "REBALANCING_REINVEST_BUY",
  "MIGRATION_SELL",
  "REBALANCING_TRIM_SELL",
  "SEED_IMPORT",
];

function TradeTypeLegend({ showSeed }: { showSeed: boolean }) {
  return (
    <div className="flex flex-wrap items-center gap-x-4 gap-y-1.5 text-xs text-muted-foreground">
      {LEGEND_ORDER.filter((t) => showSeed || t !== "SEED_IMPORT").map((t) => (
        <span key={t} className="flex items-center gap-1.5">
          <span
            className="inline-block h-2.5 w-2.5 rounded-full"
            style={{ background: TRADE_TYPE_COLORS[t] }}
            aria-hidden="true"
          />
          {TRADE_TYPE_LABELS[t]}
        </span>
      ))}
      <span>· rozmiar kropki ∝ √(kwota transakcji)</span>
      {showSeed && (
        <span>
          · import początkowy ma syntetyczną cenę z 2024-01-01 (czasem znacznie odbiega od
          realnej) — kropki poza zakresem cen nie są rysowane, ale są widoczne w liście poniżej
        </span>
      )}
    </div>
  );
}

// ── trades table (keyboard/screen-reader accessible fallback to hover) ──────

function TradesTable({ trades, priceCurrency }: { trades: TradeChartMarker[]; priceCurrency: string }) {
  const sorted = useMemo(
    () => [...trades].sort((a, b) => b.purchasedAt.localeCompare(a.purchasedAt)),
    [trades]
  );

  if (sorted.length === 0) {
    return <p className="text-sm text-muted-foreground">Brak transakcji w wybranym zakresie.</p>;
  }

  return (
    <div className="max-h-96 overflow-y-auto">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Data</TableHead>
            <TableHead>Typ</TableHead>
            <TableHead>Portfel / konto</TableHead>
            <TableHead className="text-right">Jednostki</TableHead>
            <TableHead className="text-right">Kwota (PLN)</TableHead>
            <TableHead className="text-right">Cena</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {sorted.map((row) => {
            const buy = isBuy(row.unitsDelta);
            return (
              <TableRow key={row.id}>
                <TableCell className="whitespace-nowrap text-xs">{formatDateTime(row.purchasedAt)}</TableCell>
                <TableCell>
                  <span className="flex items-center gap-1.5 text-xs">
                    <span
                      className="inline-block h-2 w-2 shrink-0 rounded-full"
                      style={{ background: TRADE_TYPE_COLORS[row.tradeType] }}
                      aria-hidden="true"
                    />
                    {TRADE_TYPE_LABELS[row.tradeType]}
                  </span>
                </TableCell>
                <TableCell className="font-mono text-xs text-muted-foreground">
                  {(PORTFOLIO_LABELS[row.portfolio] ?? row.portfolio)} · {row.accountType}
                </TableCell>
                <TableCell className={`text-right tabular-nums ${buy ? "text-neon-gain" : "text-neon-loss"}`}>
                  {buy ? "+" : ""}
                  {fmtUnits(row.unitsDelta)}
                </TableCell>
                <TableCell className={`text-right tabular-nums ${buy ? "text-neon-gain" : "text-neon-loss"}`}>
                  {fmt(row.totalPln)}
                </TableCell>
                <TableCell className="text-right tabular-nums text-muted-foreground">
                  {fmt(row.pricePln)} PLN
                  {priceCurrency !== "PLN" ? ` / ${fmt(row.priceNative)} ${priceCurrency}` : ""}
                </TableCell>
              </TableRow>
            );
          })}
        </TableBody>
      </Table>
    </div>
  );
}

// ── controls defaults ─────────────────────────────────────────────────────────

const today = new Date().toISOString().slice(0, 10);
const historyStart = "2026-01-01";

const DEFAULT_PARAMS: QueryParams = {
  interval: "d",
  fromDate: historyStart,
  toDate: today,
};

// ── page ────────────────────────────────────────────────────────────────────

export default function TradeHistoryPage() {
  const [ticker, setTicker] = useState<string | undefined>(undefined);
  const [draft, setDraft] = useState<QueryParams>(DEFAULT_PARAMS);
  const [params, setParams] = useState<QueryParams>(DEFAULT_PARAMS);
  const [showSeed, setShowSeed] = useState(false);
  const [logScale, setLogScale] = useState(false);
  const [chartMode, setChartMode] = useState<ChartMode>("candle");

  const tickersQuery = useQuery({
    queryKey: queryKeys.tradeChart.tickers(),
    queryFn: () => getTradeChartTickers(),
    staleTime: 5 * 60_000,
  });

  const tickerEntries = tickersQuery.data?.data ?? [];
  // Derived during render, not via useEffect: falls back to the first ticker
  // once the picker list loads, until the user makes an explicit choice.
  const effectiveTicker = ticker ?? tickerEntries[0]?.ticker;

  const chartQuery = useQuery({
    queryKey: queryKeys.tradeChart.chart({ ticker: effectiveTicker, ...params }),
    queryFn: () =>
      getTradeChart({
        ticker: effectiveTicker!,
        interval: params.interval,
        fromDate: params.fromDate || undefined,
        toDate: params.toDate || undefined,
      }),
    enabled: !!effectiveTicker,
    staleTime: 5 * 60_000,
  });

  function apply() {
    setParams({ ...draft });
  }

  const chartData = chartQuery.data?.data;
  const allTrades = useMemo(() => chartData?.trades ?? [], [chartData]);
  const trades = useMemo(
    () => (showSeed ? allTrades : allTrades.filter((t) => t.tradeType !== "SEED_IMPORT")),
    [allTrades, showSeed]
  );
  const maxAbsTotalPln = useMemo(
    () => trades.reduce((max, t) => Math.max(max, Math.abs(t.totalPln)), 0),
    [trades]
  );
  const rows = useMemo(
    () => buildRows(chartData?.priceSeries ?? [], trades),
    [chartData, trades]
  );

  const isLoading = tickersQuery.isPending || (chartQuery.isFetching && !chartQuery.data);

  return (
    <div className="p-4 lg:p-6 space-y-4">
      <PageHeader
        eyebrow="Portfel"
        title="Historia Transakcji"
        description="Cena aktywa na tle każdej transakcji — zielone kropki to zakupy, czerwone sprzedaże, rozmiar odpowiada kwocie."
      />

      {/* ── Controls ── */}
      <Card>
        <CardContent className="pt-4 space-y-4">
          <div className="space-y-1">
            <Label>Aktywo</Label>
            {tickersQuery.isError ? (
              <p role="alert" className="text-sm text-destructive">
                Błąd ładowania listy tickerów
              </p>
            ) : (
              <TickerPicker
                tickers={tickerEntries}
                value={effectiveTicker}
                onChange={setTicker}
                disabled={tickersQuery.isPending}
              />
            )}
          </div>

          <div className="flex flex-wrap gap-4 items-end">
            <div className="space-y-1">
              <Label>Interwał</Label>
              <Select value={draft.interval} onValueChange={(v) => setDraft((d) => ({ ...d, interval: v }))}>
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

            <Button onClick={apply} disabled={chartQuery.isFetching}>
              {chartQuery.isFetching ? "Ładowanie…" : "Generuj"}
            </Button>

            <div className="space-y-1">
              <Label>Wykres</Label>
              <Select value={chartMode} onValueChange={(v) => setChartMode(v as ChartMode)}>
                <SelectTrigger className="w-28">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="line">Linia</SelectItem>
                  <SelectItem value="candle">Świece</SelectItem>
                </SelectContent>
              </Select>
            </div>

            <div className="flex items-center gap-2 pb-1.5">
              <Switch id="log-scale" checked={logScale} onCheckedChange={setLogScale} />
              <Label htmlFor="log-scale" className="text-sm font-normal">Skala logarytmiczna</Label>
            </div>

            <div className="flex items-center gap-2 pb-1.5">
              <Switch id="show-seed" checked={showSeed} onCheckedChange={setShowSeed} />
              <Label htmlFor="show-seed" className="text-sm font-normal">Pokaż import początkowy</Label>
            </div>
          </div>
        </CardContent>
      </Card>

      {/* ── Error ── */}
      {chartQuery.isError && (
        <p role="alert" className="text-sm text-destructive">
          Błąd: {chartQuery.error instanceof Error ? chartQuery.error.message : "Nieznany błąd"}
        </p>
      )}

      {!tickersQuery.isPending && !tickersQuery.isError && tickerEntries.length === 0 && (
        <p className="text-sm text-muted-foreground">Brak dostępnych aktywów do wyświetlenia.</p>
      )}

      {/* ── Chart ── */}
      {isLoading && <Skeleton className="h-[560px] w-full rounded-xl" />}

      {chartData && !isLoading && (
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-base">
              {chartData.ticker} ({chartData.yahooTicker})
              <span className="ml-3 text-xs font-normal text-muted-foreground">
                {chartData.priceCurrency} · {trades.length} transakcji widocznych
              </span>
            </CardTitle>
          </CardHeader>
          <CardContent className="pl-0 pr-2 space-y-3">
            <TradeHistoryChart
              rows={rows}
              interval={params.interval}
              priceCurrency={chartData.priceCurrency}
              logScale={logScale}
              chartMode={chartMode}
              maxAbsTotalPln={maxAbsTotalPln}
            />
            <div className="px-4">
              <TradeTypeLegend showSeed={showSeed} />
            </div>
          </CardContent>
        </Card>
      )}

      {/* ── Trades table (accessible detail view, mirrors hover tooltip data) ── */}
      {chartData && !isLoading && (
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-base">Lista transakcji</CardTitle>
          </CardHeader>
          <CardContent>
            <TradesTable trades={trades} priceCurrency={chartData.priceCurrency} />
          </CardContent>
        </Card>
      )}
    </div>
  );
}
