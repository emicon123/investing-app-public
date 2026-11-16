import { formatPln } from "@common/app-core";
import type { PortfoliosIncluded, PurchaseSessionItem } from "../../../api/index.js";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { DecimalInput } from "@/components/ui/decimal-input";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

const fmtUnits = (v: number | null) =>
  v == null ? "—" : new Intl.NumberFormat("pl-PL", { maximumFractionDigits: 8 }).format(v);

const fmtNative =(pln: number | null, rate: number, currency: string) => {
  if (pln == null || rate <= 0) return null;
  const native = pln / rate;
  const formatted = new Intl.NumberFormat("en-US", {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  }).format(native);
  return `${formatted} ${currency}`;
};

export type ItemEdits = Record<number, { actualUnits: string; actualValuePln: string }>;

type FxRates = {
  USD?: number | null;
  EUR?: number | null;
  CHF?: number | null;
};

type Props = {
  title: string;
  items: PurchaseSessionItem[];
  edits: ItemEdits;
  onEdit: (id: number, field: "actualUnits" | "actualValuePln", value: string) => void;
  fxRates?: FxRates;
  // Session-level portfolio selection, used only to flag rows that exist purely because
  // of cross-portfolio donor activity (ADR-065) — see crossPortfolioNote below. Optional
  // so existing/other call sites aren't forced to thread it through.
  portfoliosIncluded?: PortfoliosIncluded;
};

// ADR-065: a purchase session can now legitimately contain items tagged with a portfolio
// the user didn't select (a donor sell in the other portfolio, or the compensating buy
// booked back to it). Detected client-side, without a dedicated API field, by comparing
// the row's own portfolio against what portfoliosIncluded implies — "BOTH" never flags a
// row (every portfolio is implied), and a "MAIN"/"RETIREMENT" session flags any row tagged
// with the *other* portfolio. This heuristic can't distinguish a cross-portfolio row from
// an ordinary same-portfolio row when portfoliosIncluded is "BOTH" (both portfolios are
// implied either way) — that gap would need a dedicated backend-provided boolean.
function crossPortfolioNote(
  itemPortfolio: PurchaseSessionItem["portfolio"],
  portfoliosIncluded: PortfoliosIncluded | undefined
): string | null {
  if (!portfoliosIncluded || portfoliosIncluded === "BOTH") return null;
  if (itemPortfolio === portfoliosIncluded) return null;
  return itemPortfolio === "RETIREMENT"
    ? "Emerytura (przeniesienie z zakupów Inwestycje)"
    : "Inwestycje (przeniesienie z zakupów Emerytura)";
}

export function PortfolioItemsTable({ title, items, edits, onEdit, fxRates, portfoliosIncluded }: Props) {
  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">{title}</CardTitle>
      </CardHeader>
      <CardContent className="p-0">
        <div className="overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Aktywo</TableHead>
                <TableHead>Konto</TableHead>
                <TableHead className="text-right">Plan PLN</TableHead>
                <TableHead className="text-right">Plan jedn.</TableHead>
                <TableHead className="text-right">Rzeczywiste jedn.</TableHead>
                <TableHead className="text-right">Rzeczywiste PLN</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((item) => {
                const edit = edits[item.id];
                const currency = item.priceCurrency;
                const rate = currency && fxRates ? fxRates[currency as keyof FxRates] : null;
                const nativeLabel =
                  currency && currency !== "PLN" && rate != null && rate > 0
                    ? fmtNative(item.plannedValuePln, rate, currency)
                    : null;
                const crossPortfolio = crossPortfolioNote(item.portfolio, portfoliosIncluded);

                return (
                  <TableRow key={item.id}>
                    <TableCell>
                      <span className="font-mono text-xs">{item.ticker}</span>
                      {item.isMigrationSell && (
                        <Badge variant="destructive" className="ml-2 text-xs">
                          SPRZEDAŻ
                        </Badge>
                      )}
                      {item.isMigrationBuy && (
                        <Badge className="ml-2 text-xs bg-amber-500 hover:bg-amber-600">
                          ZASTĘP
                        </Badge>
                      )}
                      {crossPortfolio && (
                        <Badge variant="secondary" className="ml-2 text-xs font-normal">
                          {crossPortfolio}
                        </Badge>
                      )}
                    </TableCell>
                    <TableCell>
                      <Badge variant="outline" className="text-xs">
                        {item.accountType}
                      </Badge>
                    </TableCell>
                    <TableCell className="text-right text-sm">
                      {item.plannedValuePln != null ? formatPln(item.plannedValuePln) : "—"}
                      {nativeLabel && (
                        <div className="text-xs text-muted-foreground">
                          ({nativeLabel})
                        </div>
                      )}
                    </TableCell>
                    <TableCell className="text-right font-mono text-xs">
                      {fmtUnits(item.plannedUnits)}
                    </TableCell>
                    <TableCell className="text-right">
                      <DecimalInput
                        className="w-28 text-right text-sm h-8"
                        value={edit?.actualUnits ?? ""}
                        onChange={(e) => onEdit(item.id, "actualUnits", e.target.value)}
                        aria-label={`Rzeczywiste jednostki ${item.ticker}`}
                      />
                    </TableCell>
                    <TableCell className="text-right">
                      <DecimalInput
                        className="w-32 text-right text-sm h-8"
                        value={edit?.actualValuePln ?? ""}
                        onChange={(e) => onEdit(item.id, "actualValuePln", e.target.value)}
                        aria-label={`Rzeczywista wartość PLN ${item.ticker}`}
                      />
                    </TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        </div>
      </CardContent>
    </Card>
  );
}
