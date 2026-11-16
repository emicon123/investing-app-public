import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  formatPln,
  idempotencyErrorMessage,
  useIdempotentSubmit,
} from "@common/app-core";
import { calculateRebalancing, confirmPurchases, listSettings } from "../api/index.js";
import type { BuyListItem, ConfirmPurchasesRequest, Portfolio } from "../api/index.js";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { DecimalInput } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
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
import { usePortfolio } from "@/lib/portfolio";
import { queryKeys } from "@/lib/queryKeys";

const FIXED_DEFAULT_SETTING_KEY = "fixed_monthly_deposit_default_pln";

function buildConfirmRequest(
  portfolio: Portfolio,
  depositAmountPln: number,
  buyList: BuyListItem[],
  actualUnits: Record<string, string>,
  selectedTicker: Record<string, string>,
): ConfirmPurchasesRequest {
  return {
    portfolio,
    depositAmountPln,
    items: buyList
      .map((item) => ({
        ticker: selectedTicker[item.ticker] ?? item.ticker,
        units: parseFloat((Number(actualUnits[item.ticker]) || 0).toFixed(8)),
      }))
      .filter((item) => item.units > 0),
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
  actualUnits: Record<string, string>;
  selectedTicker: Record<string, string>;
  onUnitsChange: (ticker: string, value: string) => void;
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
            <TableHead className="text-right">Kwota PLN</TableHead>
            <TableHead className="text-right">Jednostki (plan)</TableHead>
            <TableHead>Instrument</TableHead>
            <TableHead className="text-right">Faktycznie kupiłem</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {items.map((item) => {
            const tickers = item.availableTickers ?? [item.ticker];
            const chosen = selectedTicker[item.ticker] ?? item.ticker;
            return (
              <TableRow key={item.ticker}>
                <TableCell className="font-mono font-medium">{item.ticker}</TableCell>
                <TableCell className="text-sm">{item.name}</TableCell>
                <TableCell className="text-right font-semibold tabular-nums">
                  {formatPln(item.allocatedPln)}
                </TableCell>
                <TableCell className="text-right font-mono text-xs text-muted-foreground">
                  {item.unitsToBuy.toFixed(4)}
                </TableCell>
                <TableCell>
                  {tickers.length > 1 ? (
                    <Select value={chosen} onValueChange={(v) => onTickerChange(item.ticker, v)}>
                      <SelectTrigger className="h-8 w-36 text-sm font-mono">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        {tickers.map((t) => (
                          <SelectItem key={t} value={t} className="font-mono text-sm">{t}</SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  ) : (
                    <span className="font-mono text-sm">{item.ticker}</span>
                  )}
                </TableCell>
                <TableCell className="text-right">
                  <DecimalInput
                    value={actualUnits[item.ticker] ?? ""}
                    onChange={(e) => onUnitsChange(item.ticker, e.target.value)}
                    className="h-8 text-xs w-28 text-right tabular-nums ml-auto"
                  />
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
  const { portfolio } = usePortfolio();
  const queryClient = useQueryClient();
  const [actualUnits, setActualUnits] = useState<Record<string, string>>({});
  const [selectedTicker, setSelectedTicker] = useState<Record<string, string>>({});

  const { data: settingsData } = useQuery({
    queryKey: queryKeys.settings.all,
    queryFn: listSettings,
    staleTime: 10 * 60_000,
  });

  const fixedDefault = settingsData?.data.find((s) => s.key === FIXED_DEFAULT_SETTING_KEY)?.value;
  const isVariable = portfolio === "VARIABLE";

  // VARIABLE has no stored default (starts empty every time); FIXED prefills
  // from the stored setting so the user can accept-or-override it. Reset
  // whenever the selected portfolio changes — not on background setting
  // refetches, so we don't clobber input the user is mid-edit on. Adjusted
  // during render (React's "reset state when a prop changes" pattern) rather
  // than in an effect: same end state without an extra commit.
  const defaultDepositFor = (p: Portfolio) => (p === "FIXED" ? fixedDefault ?? "" : "");
  const [depositAmount, setDepositAmount] = useState(() => defaultDepositFor(portfolio));
  const [depositPortfolio, setDepositPortfolio] = useState(portfolio);
  if (portfolio !== depositPortfolio) {
    setDepositPortfolio(portfolio);
    setDepositAmount(defaultDepositFor(portfolio));
  }

  // One key per calculated plan: a retry of the same confirm replays, a new plan starts a new
  // intent, success rotates it (ADR-111).
  const confirmSubmit = useIdempotentSubmit(confirmPurchases);

  const calcMutation = useMutation({
    mutationFn: () => {
      const amount = parseFloat(depositAmount);
      return calculateRebalancing({
        portfolio,
        depositAmountPln: isNaN(amount) ? undefined : amount,
      });
    },
    onSuccess: (data) => {
      confirmSubmit.rotate();
      const prefillUnits: Record<string, string> = {};
      const prefillTickers: Record<string, string> = {};
      for (const item of data.data.buyList) {
        prefillTickers[item.ticker] = item.ticker;
        prefillUnits[item.ticker] = String(item.unitsToBuy);
      }
      setActualUnits(prefillUnits);
      setSelectedTicker(prefillTickers);
    },
  });

  const confirmMutation = useMutation({
    mutationFn: (req: ConfirmPurchasesRequest) => confirmSubmit.submit(req),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.positions.all });
      void queryClient.invalidateQueries({ queryKey: queryKeys.purchaseHistory.all });
      calcMutation.reset();
      setActualUnits({});
      setSelectedTicker({});
    },
  });

  const result = calcMutation.data?.data ?? null;
  const depositAmountNum = parseFloat(depositAmount);
  const hasValidDepositAmount = !isNaN(depositAmountNum) && depositAmountNum > 0;

  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="text-base">Plan zakupów DCA</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="flex flex-wrap items-end gap-3">
          <div className="flex flex-col gap-1">
            <Label htmlFor="deposit-amount" className="text-xs text-muted-foreground">
              Kwota wpłaty (PLN){isVariable && <span className="text-destructive ml-1">*</span>}
            </Label>
            <DecimalInput
              id="deposit-amount"
              value={depositAmount}
              placeholder={isVariable ? "np. 1500" : fixedDefault ?? "400"}
              onChange={(e) => setDepositAmount(e.target.value)}
              className="h-9 w-40 font-mono"
            />
          </div>
          <Button
            onClick={() => calcMutation.mutate()}
            disabled={calcMutation.isPending || !hasValidDepositAmount}
          >
            {calcMutation.isPending ? "Obliczam..." : "Oblicz zakupy"}
          </Button>
          {result && (
            <Badge variant="outline" className="text-xs">
              {result.buyList.length} pozycji do kupna
            </Badge>
          )}
        </div>

        {isVariable && (
          <p className="text-xs text-muted-foreground">
            Portfel zmienny nie ma domyślnej kwoty wpłaty — podaj ją ręcznie przy każdym obliczeniu.
          </p>
        )}

        {calcMutation.isError && (
          <p role="alert" className="text-sm text-destructive">
            Błąd obliczania. Sprawdź czy ceny są dostępne oraz czy podano kwotę wpłaty.
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
              onUnitsChange={(ticker, value) =>
                setActualUnits((prev) => ({ ...prev, [ticker]: value }))
              }
              onTickerChange={(planTicker, chosenTicker) =>
                setSelectedTicker((prev) => ({ ...prev, [planTicker]: chosenTicker }))
              }
            />

            {result.buyList.length > 0 && (
              <div className="flex items-center gap-3 pt-1 border-t">
                <Button
                  onClick={() => {
                    // Record exactly the amount that was used for calculate() above
                    // (this input's current value), per docs/adr/001-portfolio-dimension.md §2 —
                    // confirm() doesn't re-derive it, it just persists what we send.
                    const amount = parseFloat(depositAmount);
                    confirmMutation.mutate(
                      buildConfirmRequest(
                        portfolio,
                        isNaN(amount) ? 0 : amount,
                        result.buyList,
                        actualUnits,
                        selectedTicker,
                      ),
                    );
                  }}
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
