import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { formatPln } from "@common/app-core";
import { getCryptoTaxSummary } from "../../api/index.js";
import { CURRENT_YEAR } from "@/lib/dateConstants";

import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from "@/components/ui/select";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Accordion,
  AccordionItem,
  AccordionTrigger,
  AccordionContent,
} from "@/components/ui/accordion";
import { PageHeader } from "@/components/PageHeader";
import { queryKeys } from "@/lib/queryKeys";

const previousYear = CURRENT_YEAR - 1;
const availableYears = Array.from({ length: 5 }, (_, i) => previousYear - i + 1);

function TaxRow({ label, value }: { label: string; value: number }) {
  return (
    <div className="flex justify-between py-2 border-b last:border-0">
      <span className="text-muted-foreground">{label}</span>
      <span className="font-mono font-semibold">{formatPln(value)}</span>
    </div>
  );
}

export default function CryptoTaxPage() {
  const [year, setYear] = useState<number>(previousYear);

  const { data, isPending, isError } = useQuery({
    queryKey: queryKeys.cryptoTrades.taxSummary(year),
    queryFn: () => getCryptoTaxSummary(year),
    staleTime: 5 * 60_000,
  });

  const summary = data?.data;

  return (
    <div className="p-6 space-y-6 max-w-2xl">
      <PageHeader eyebrow="Krypto" title="PIT-38" />
      <div className="flex items-center gap-4">
        <Select
          value={String(year)}
          onValueChange={(v) => setYear(Number(v))}
        >
          <SelectTrigger className="w-32">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {availableYears.map((y) => (
              <SelectItem key={y} value={String(y)}>
                {y}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      {isPending && (
        <p className="text-muted-foreground">Ładowanie...</p>
      )}

      {isError && (
        <p role="alert" className="text-destructive">
          Błąd ładowania podsumowania podatkowego.
        </p>
      )}

      {summary && (
        <>
          <Card>
            <CardHeader>
              <CardTitle className="text-lg">PIT-38 — Rok {year}</CardTitle>
            </CardHeader>
            <CardContent className="space-y-0">
              <TaxRow label="Przychód (8C)" value={summary.totalSellsPln} />
              <TaxRow label="Koszty (9C)" value={summary.totalBuysPln} />
              <TaxRow label="Dochód (10)" value={summary.netGainPln} />
              <TaxRow label="Podatek 19% (27)" value={summary.estimatedBelkaTaxPln} />
            </CardContent>
          </Card>

          <p className="text-sm text-muted-foreground italic">
            Wartości pomocnicze — skonsultuj z doradcą podatkowym.
          </p>

          {summary.byTicker.length > 0 && (
            <div>
              <h2 className="text-base font-semibold mb-2">Szczegóły według tickera</h2>
              <Accordion type="multiple" className="rounded-md border">
                {summary.byTicker.map((ticker, idx) => (
                  <AccordionItem key={ticker.ticker ?? idx} value={ticker.ticker ?? String(idx)}>
                    <AccordionTrigger className="px-4">
                      <span className="font-mono font-semibold">{ticker.ticker ?? "—"}</span>
                    </AccordionTrigger>
                    <AccordionContent className="px-4">
                      <div className="space-y-0">
                        <TaxRow label="Przychód" value={ticker.totalSellsPln ?? 0} />
                        <TaxRow label="Koszty" value={ticker.totalBuysPln ?? 0} />
                        <TaxRow label="Dochód" value={ticker.netGainPln ?? 0} />
                        <TaxRow label="Podatek est." value={ticker.estimatedBelkaTaxPln ?? 0} />
                      </div>
                    </AccordionContent>
                  </AccordionItem>
                ))}
              </Accordion>
            </div>
          )}
        </>
      )}
    </div>
  );
}
