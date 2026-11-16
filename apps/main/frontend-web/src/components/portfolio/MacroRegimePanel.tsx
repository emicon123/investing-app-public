import { useQuery } from "@tanstack/react-query";
import { getMacroState, type Asset, type MacroZone } from "../../api/index.js";

import {
  Accordion,
  AccordionItem,
  AccordionTrigger,
  AccordionContent,
} from "@/components/ui/accordion";
import { Badge } from "@/components/ui/badge";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { queryKeys } from "@/lib/queryKeys";
import { cn } from "@/lib/utils";
import { getAssetIconUrl } from "@/lib/assetIcons";

function formatSignedPct(value: number | null): string {
  if (value === null) return "—";
  const pct = value * 100;
  const sign = pct > 0 ? "+" : "";
  return `${sign}${pct.toFixed(2)}%`;
}

function formatBlendFactor(value: number | null): string {
  if (value === null) return "— (bootstrap)";
  return value.toFixed(2);
}

function formatTargetPct(value: number | null): string {
  if (value === null) return "—";
  return `${value.toFixed(1)}%`;
}

const ZONE_LABELS: Record<MacroZone, string> = {
  RISK_ON: "risk-on",
  MID_ON: "lekki risk-on",
  NEUTRAL: "neutralna",
  MID_OFF: "lekki risk-off",
  RISK_OFF: "risk-off",
};

const ZONE_BADGE_CLASS: Record<MacroZone, string> = {
  RISK_ON: "border-neon-green/50 text-neon-green",
  MID_ON: "border-neon-green/30 text-neon-green",
  NEUTRAL: "border-muted-foreground/30 text-muted-foreground",
  MID_OFF: "border-neon-loss/30 text-neon-loss",
  RISK_OFF: "border-neon-loss/50 text-neon-loss",
};

function RegimeTelemetry({
  blendFactor,
  zone,
  asOfDate,
  netLiqYoyAccel,
  globalM2YoyAccel,
  globalM2Stale,
  deriskEnabled,
}: {
  blendFactor: number | null;
  zone: MacroZone | null;
  asOfDate: string;
  netLiqYoyAccel: number | null;
  globalM2YoyAccel: number | null;
  globalM2Stale: boolean;
  deriskEnabled: boolean;
}) {
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-3">
        <span className="text-sm text-muted-foreground">Blend</span>
        <span className="font-mono text-sm font-semibold">
          {formatBlendFactor(blendFactor)}
        </span>
        {zone && (
          <Badge variant="outline" className={cn("text-xs", ZONE_BADGE_CLASS[zone])}>
            {ZONE_LABELS[zone]}
          </Badge>
        )}
        <span className="text-xs text-muted-foreground">na dzień {asOfDate}</span>
      </div>

      <div className="flex flex-wrap gap-x-6 gap-y-1 text-xs text-muted-foreground">
        <span>
          Δ3M płynność netto:{" "}
          <span
            className={cn(
              "font-mono",
              netLiqYoyAccel !== null && netLiqYoyAccel < 0 && "text-neon-loss",
            )}
          >
            {formatSignedPct(netLiqYoyAccel)}
          </span>
        </span>
        <span>
          Δ3M Global M2:{" "}
          <span
            className={cn(
              "font-mono",
              globalM2YoyAccel !== null && globalM2YoyAccel < 0 && "text-neon-loss",
            )}
          >
            {formatSignedPct(globalM2YoyAccel)}
          </span>
        </span>
        {globalM2Stale && (
          <Badge variant="destructive" className="text-[10px]">
            Global M2 nieaktualne
          </Badge>
        )}
        <span>De-risk: {deriskEnabled ? "włączony" : "wyłączony"}</span>
      </div>
    </div>
  );
}

// Task 156: targets (risk-on, risk-off, effective) live on the ASSET, and every list entry already
// is one — subassets carry no target, so there is nothing to filter out any more.
function MacroTargetsTable({
  assets,
}: {
  assets: Asset[];
}) {
  const rows = assets
    .slice()
    .sort((a, b) => (b.targetAllocationPct ?? 0) - (a.targetAllocationPct ?? 0));

  return (
    <div className="rounded-md border overflow-x-auto">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Aktywo</TableHead>
            <TableHead className="text-right">Cel risk-on</TableHead>
            <TableHead className="text-right">Cel risk-off</TableHead>
            <TableHead className="text-right">Cel efektywny</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.length === 0 ? (
            <TableRow>
              <TableCell colSpan={4} className="h-24 text-center text-muted-foreground">
                Brak pozycji.
              </TableCell>
            </TableRow>
          ) : (
            rows.map((asset) => {
              const primary = asset.subassets.find((s) => s.isPrimary) ?? asset.subassets[0];
              const iconUrl = getAssetIconUrl(primary?.ticker ?? asset.key, asset.assetClass);
              const effective = asset.computed?.effectiveTargetAllocationPct ?? null;
              return (
                <TableRow key={asset.id}>
                  <TableCell>
                    <div className="flex items-center gap-2">
                      {iconUrl && (
                        <img
                          src={iconUrl}
                          alt=""
                          className="size-5 rounded-full shrink-0 object-contain"
                          onError={(e) => {
                            (e.currentTarget as HTMLImageElement).style.display = "none";
                          }}
                        />
                      )}
                      <div className="flex flex-col">
                        <span className="font-mono text-xs font-medium">{asset.key}</span>
                        <span className="text-xs text-muted-foreground">{asset.name}</span>
                      </div>
                    </div>
                  </TableCell>
                  <TableCell className="text-right font-mono text-xs tabular-nums">
                    {formatTargetPct(asset.targetAllocationPct)}
                  </TableCell>
                  <TableCell className="text-right font-mono text-xs tabular-nums text-muted-foreground">
                    {asset.targetAllocationPctRiskOff === null
                      ? "= risk-on"
                      : formatTargetPct(asset.targetAllocationPctRiskOff)}
                  </TableCell>
                  <TableCell className="text-right font-mono text-xs font-semibold tabular-nums">
                    {formatTargetPct(effective)}
                  </TableCell>
                </TableRow>
              );
            })
          )}
        </TableBody>
      </Table>
    </div>
  );
}

export function MacroRegimePanel({ assets }: { assets: Asset[] }) {
  const { data, isPending, isError } = useQuery({
    queryKey: queryKeys.macroState.all,
    queryFn: getMacroState,
    staleTime: 5 * 60_000,
  });

  const state = data?.data;

  return (
    <Accordion type="single" collapsible className="rounded-md border">
      <AccordionItem value="macro-regime" className="border-0">
        <AccordionTrigger className="px-4 hover:no-underline">
          <span className="text-base font-semibold">Reżim makro</span>
        </AccordionTrigger>
        <AccordionContent className="px-4 space-y-4">
          {isPending && (
            <p className="text-sm text-muted-foreground">Ładowanie stanu makro...</p>
          )}
          {isError && (
            <p role="alert" className="text-sm text-destructive">
              Błąd ładowania stanu makro.
            </p>
          )}
          {state && (
            <>
              <RegimeTelemetry
                blendFactor={state.blendFactor}
                zone={state.zone}
                asOfDate={state.asOfDate}
                netLiqYoyAccel={state.netLiqYoyAccel}
                globalM2YoyAccel={state.globalM2YoyAccel}
                globalM2Stale={state.globalM2Stale}
                deriskEnabled={state.deriskEnabled}
              />
              <MacroTargetsTable assets={assets} />
            </>
          )}
        </AccordionContent>
      </AccordionItem>
    </Accordion>
  );
}
