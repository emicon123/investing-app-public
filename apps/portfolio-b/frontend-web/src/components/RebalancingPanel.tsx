import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import {
  formatPln,
  idempotencyErrorMessage,
  useIdempotentSubmit,
} from "@common/app-core";
import { calculateRebalancing, confirmPurchases } from "../api/index.js";
import type { BuyListItem, ConfirmPurchasesRequest } from "../api/index.js";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { DecimalInput } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { queryKeys } from "@/lib/queryKeys";

function buildConfirmRequest(
  buyList: BuyListItem[],
  actualUnits: Record<string, Record<string, string>>,
  selectedTicker: Record<string, string>
): ConfirmPurchasesRequest {
  return {
    items: buyList
      .map(item => {
        const perAccount = actualUnits[item.ticker] ?? {};
        return {
          ticker: selectedTicker[item.ticker] ?? item.ticker,
          accountAllocations: (item.accountAllocations ?? [])
            .map(a => ({
              accountType: a.accountType,
              units: parseFloat((Number(perAccount[a.accountType]) || 0).toFixed(8)),
            }))
            .filter(a => a.units > 0),
        };
      })
      .filter(item => item.accountAllocations.length > 0),
  };
}

function BuyListTable({
  items,
  actualUnits,
  selectedTicker,
  onUnitsChange,
  onTickerChange,
}: {
  items: BuyListItem[];
  actualUnits: Record<string, Record<string, string>>;
  selectedTicker: Record<string, string>;
  onUnitsChange: (ticker: string, accountType: string, value: string) => void;
  onTickerChange: (planTicker: string, chosenTicker: string) => void;
}) {
  if (items.length === 0) {
    return <p className="text-sm text-muted-foreground">Wszystkie pozycje są dobrze wyważone.</p>;
  }
  return (
    <div className="rounded-md border overflow-x-auto">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Ticker</TableHead>
            <TableHead>Nazwa</TableHead>
            <TableHead className="text-right">Łącznie PLN</TableHead>
            <TableHead>Konta</TableHead>
            <TableHead>Instrument</TableHead>
            <TableHead className="text-right">Faktycznie kupiłem</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {items.map((item) => {
            const allocs = item.accountAllocations ?? [];
            const tickers = item.availableTickers ?? [item.ticker];
            const chosen = selectedTicker[item.ticker] ?? item.ticker;
            return (
              <TableRow key={item.ticker}>
                <TableCell className="font-mono font-medium">{item.ticker}</TableCell>
                <TableCell className="text-sm">{item.name}</TableCell>
                <TableCell className="text-right font-semibold tabular-nums">
                  {formatPln(item.allocatedPln)}
                </TableCell>
                <TableCell>
                  {allocs.length === 0 ? (
                    <span className="text-muted-foreground text-xs">—</span>
                  ) : (
                    <div className="flex flex-wrap gap-1.5">
                      {allocs.map((a) => (
                        <span key={a.accountType} className="flex items-center gap-1 text-xs">
                          <Badge variant="outline" className="px-1.5 py-0 text-[10px] font-normal">
                            {a.accountType.replace("_", " ").toLowerCase().replace(/^\w/, c => c.toUpperCase())}
                          </Badge>
                          <span className="font-mono">{formatPln(a.allocPln)}</span>
                          <span className="text-muted-foreground">({a.unitsToBuy.toFixed(4)} szt.)</span>
                        </span>
                      ))}
                    </div>
                  )}
                </TableCell>
                <TableCell>
                  {tickers.length > 1 ? (
                    <Select value={chosen} onValueChange={v => onTickerChange(item.ticker, v)}>
                      <SelectTrigger className="h-8 w-36 text-sm font-mono">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        {tickers.map(t => (
                          <SelectItem key={t} value={t} className="font-mono text-sm">{t}</SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  ) : (
                    <span className="font-mono text-sm">{item.ticker}</span>
                  )}
                </TableCell>
                <TableCell className="text-right">
                  {allocs.length === 0 ? (
                    <span className="text-muted-foreground text-xs">—</span>
                  ) : (
                    <div className="flex flex-col gap-1 items-end">
                      {allocs.map(a => (
                        <div key={a.accountType} className="flex items-center gap-1.5">
                          <span className="text-[10px] text-muted-foreground font-mono">
                            {a.accountType.replace("_", " ").toLowerCase()}
                          </span>
                          <DecimalInput
                            value={actualUnits[item.ticker]?.[a.accountType] ?? ""}
                            onChange={e => onUnitsChange(item.ticker, a.accountType, e.target.value)}
                            className="h-7 text-xs w-24 text-right tabular-nums"
                          />
                        </div>
                      ))}
                    </div>
                  )}
                </TableCell>
              </TableRow>
            );
          })}
        </TableBody>
      </Table>
    </div>
  );
}

export function RebalancingPanel() {
  const queryClient = useQueryClient();
  const [actualUnits, setActualUnits] = useState<Record<string, Record<string, string>>>({});
  const [selectedTicker, setSelectedTicker] = useState<Record<string, string>>({});

  // One key per calculated plan: a retry of the same confirm replays, a new plan starts a new
  // intent, success rotates it (ADR-111).
  const confirmSubmit = useIdempotentSubmit(confirmPurchases);

  const calcMutation = useMutation({
    mutationFn: calculateRebalancing,
    onSuccess: (data) => {
      confirmSubmit.rotate();
      const prefillUnits: Record<string, Record<string, string>> = {};
      const prefillTickers: Record<string, string> = {};
      for (const item of data.data.buyList) {
        prefillTickers[item.ticker] = item.ticker;
        const perAccount: Record<string, string> = {};
        for (const a of item.accountAllocations ?? []) {
          perAccount[a.accountType] = String(a.unitsToBuy);
        }
        prefillUnits[item.ticker] = perAccount;
      }
      setActualUnits(prefillUnits);
      setSelectedTicker(prefillTickers);
    },
  });

  const confirmMutation = useMutation({
    mutationFn: (req: ConfirmPurchasesRequest) => confirmSubmit.submit(req),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["positions"] });
      void queryClient.invalidateQueries({ queryKey: ["accountCash"] });
      void queryClient.invalidateQueries({ queryKey: queryKeys.purchaseHistory.all });
      calcMutation.reset();
      setActualUnits({});
      setSelectedTicker({});
    },
  });

  const result = calcMutation.data?.data ?? null;

  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="text-base">Plan zakupów DCA</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="flex items-center gap-3">
          <Button
            onClick={() => calcMutation.mutate()}
            disabled={calcMutation.isPending}
          >
            {calcMutation.isPending ? "Obliczam..." : "Oblicz zakupy"}
          </Button>
          {result && (
            <Badge variant="outline" className="text-xs">
              {result.buyList.length} pozycji do kupna
            </Badge>
          )}
        </div>

        {calcMutation.isError && (
          <p role="alert" className="text-sm text-destructive">
            Błąd obliczania. Sprawdź czy ceny są dostępne.
          </p>
        )}

        {result && (
          <div className="space-y-4">
            <div className="flex flex-wrap gap-4 text-sm">
              <div className="flex flex-col gap-0.5">
                <span className="text-xs text-muted-foreground uppercase tracking-wide">Wartość portfela</span>
                <span className="font-semibold">{formatPln(result.totalPortfolioValuePln ?? 0)}</span>
              </div>
              <div className="flex flex-col gap-0.5">
                <span className="text-xs text-muted-foreground uppercase tracking-wide">Do zainwestowania</span>
                <span className="font-semibold text-blue-600">{formatPln(result.deployableCashPln ?? 0)}</span>
              </div>
            </div>

            <BuyListTable
              items={result.buyList}
              actualUnits={actualUnits}
              selectedTicker={selectedTicker}
              onUnitsChange={(ticker, accountType, value) =>
                setActualUnits(prev => ({
                  ...prev,
                  [ticker]: { ...(prev[ticker] ?? {}), [accountType]: value },
                }))
              }
              onTickerChange={(planTicker, chosenTicker) =>
                setSelectedTicker(prev => ({ ...prev, [planTicker]: chosenTicker }))
              }
            />

            {result.buyList.length > 0 && (
              <div className="flex items-center gap-3 pt-1 border-t">
                <Button
                  onClick={() =>
                    confirmMutation.mutate(buildConfirmRequest(result.buyList, actualUnits, selectedTicker))
                  }
                  disabled={confirmMutation.isPending}
                >
                  {confirmMutation.isPending ? "Zapisuję..." : "Zatwierdź zakupy i zaktualizuj portfel"}
                </Button>
                {confirmMutation.isSuccess && (
                  <span className="text-xs text-green-600">Portfel zaktualizowany</span>
                )}
                {confirmMutation.isError && (
                  <span className="text-xs text-destructive">
                    {idempotencyErrorMessage(confirmMutation.error) ?? "Błąd zapisu"}
                  </span>
                )}
              </div>
            )}
          </div>
        )}
      </CardContent>
    </Card>
  );
}
