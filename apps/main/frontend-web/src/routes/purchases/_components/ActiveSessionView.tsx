import { useState, startTransition } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { formatPln } from "@common/app-core";
import { updatePurchaseSessionItems, completePurchaseSession, deletePurchaseSession } from "../../../api/index.js";
import type { PurchaseSessionDetail, AccountLimits, CurrencyBreakdown } from "../../../api/index.js";
import { queryKeys } from "@/lib/queryKeys";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Progress } from "@/components/ui/progress";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog";
import { PortfolioItemsTable, type ItemEdits } from "./PortfolioItemsTable";

const fmtCur = (v: number, cur: string) =>
  new Intl.NumberFormat("pl-PL", {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  }).format(v) +
  " " +
  cur;

const fmtDate = (iso: string) =>
  new Intl.DateTimeFormat("pl-PL", {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(new Date(iso));

function LimitBar({
  label,
  used,
  total,
  remaining,
}: {
  label: string;
  used: number;
  total: number;
  // Server-computed, already floored at zero and already incorporating any ADR-058
  // correction — read this rather than recomputing `total - used` locally. Editing the
  // remaining limit itself happens on the Deposits page's cap-usage cards.
  remaining: number;
}) {
  const pct = total > 0 ? Math.min(100, (used / total) * 100) : 0;
  return (
    <div className="space-y-1">
      <div className="flex justify-between text-sm">
        <span className="font-medium">{label}</span>
        <span className="text-muted-foreground">
          {formatPln(remaining)} pozostało z {formatPln(total)}
        </span>
      </div>
      <Progress value={pct} className="h-2" />
    </div>
  );
}

type BudgetStatus = "ok" | "warn" | "error";

function budgetStatus(sum: number, budget: number): BudgetStatus {
  const diff = Math.abs(sum - budget);
  if (diff <= 1) return "ok";
  if (diff <= 10) return "warn";
  return "error";
}

function BudgetCheckRow({
  label,
  sum,
  budget,
}: {
  label: string;
  sum: number;
  budget: number;
}) {
  const status = budgetStatus(sum, budget);
  const diff = sum - budget;
  const icon = status === "ok" ? "✓" : status === "warn" ? "⚠" : "✗";
  const colorClass =
    status === "ok"
      ? "text-green-400"
      : status === "warn"
        ? "text-yellow-400"
        : "text-red-400";

  return (
    <div className="flex items-center justify-between text-sm py-1">
      <span className="font-medium">{label}</span>
      <span className="flex items-center gap-4">
        <span className="text-muted-foreground">
          Suma: <span className="font-mono">{formatPln(sum)}</span>
        </span>
        <span className="text-muted-foreground">
          Budżet: <span className="font-mono">{formatPln(budget)}</span>
        </span>
        <span className={`font-mono ${colorClass}`}>
          {icon}{" "}
          {diff === 0
            ? "OK"
            : `${diff > 0 ? "+" : ""}${formatPln(diff)}`}
        </span>
      </span>
    </div>
  );
}

// Renders a budget-vs-actual comparison when a budget figure is available for this
// portfolio this session, or falls back to a plain sum when it isn't (e.g. a portfolio
// that wasn't "included" this session but still has items from cross-portfolio donor
// activity — ADR-065 — never gets a budget figure, only a sum to review).
function BudgetCheckSection({
  label,
  sum,
  budget,
}: {
  label: string;
  sum: number;
  budget: number | null;
}) {
  if (budget != null) {
    return <BudgetCheckRow label={label} sum={sum} budget={budget} />;
  }
  return (
    <div className="flex items-center justify-between text-sm py-1">
      <span className="font-medium">{label}</span>
      <span className="text-muted-foreground">
        Suma: <span className="font-mono">{formatPln(sum)}</span>
      </span>
    </div>
  );
}

function BudgetCheckCard({
  session,
  emeryturaItems,
  inwestycjeItems,
}: {
  session: PurchaseSessionDetail;
  emeryturaItems: PurchaseSessionDetail["items"];
  inwestycjeItems: PurchaseSessionDetail["items"];
}) {
  const retirementSum = emeryturaItems.reduce(
    (acc, i) => acc + (i.plannedValuePln ?? 0) * (i.isMigrationSell ? -1 : 1),
    0
  );
  const mainSum = inwestycjeItems.reduce(
    (acc, i) => acc + (i.plannedValuePln ?? 0) * (i.isMigrationSell ? -1 : 1),
    0
  );

  // Keyed off actual item presence, not session.portfoliosIncluded: a cross-portfolio
  // donor sell/compensating buy (ADR-065) can legitimately land items in a portfolio the
  // user didn't select this session, and its sum still needs to be reviewable here even
  // though session.retirementBudgetPln/mainBudgetPln stay null for a non-included portfolio.
  const showRetirement = emeryturaItems.length > 0;
  const showMain = inwestycjeItems.length > 0;

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">Weryfikacja budżetu</CardTitle>
      </CardHeader>
      <CardContent className="space-y-1 divide-y divide-border">
        {showRetirement && (
          <BudgetCheckSection
            label="Emerytura"
            sum={retirementSum}
            budget={session.retirementBudgetPln}
          />
        )}
        {showMain && (
          <BudgetCheckSection
            label="Inwestycje"
            sum={mainSum}
            budget={session.mainBudgetPln}
          />
        )}
      </CardContent>
    </Card>
  );
}

function AccountLimitsCard({ title, limits }: { title: string; limits: AccountLimits }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">{title}</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        <LimitBar
          label="IKE"
          used={limits.ikeUsedPln}
          total={limits.ikeAnnualLimitPln}
          remaining={limits.ikeRemainingPln}
        />
        <LimitBar
          label="IKZE"
          used={limits.ikzeUsedPln}
          total={limits.ikzeAnnualLimitPln}
          remaining={limits.ikzeRemainingPln}
        />
      </CardContent>
    </Card>
  );
}

function FxLine({ amount, currency, pln }: { amount: number; currency: string; pln: number }) {
  if (amount <= 0) return null;
  return (
    <dd className="text-muted-foreground">
      {fmtCur(amount, currency)}{" "}
      <span className="text-xs text-muted-foreground/70">(= {formatPln(pln)})</span>
    </dd>
  );
}

function CurrencyBreakdownCard({ breakdown }: { breakdown: CurrencyBreakdown }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">Przelewy do wykonania</CardTitle>
      </CardHeader>
      <CardContent>
        <dl className="grid grid-cols-1 sm:grid-cols-2 gap-4 text-sm">
          <div>
            <dt className="font-semibold mb-1">XTB</dt>
            {breakdown.xtbPln > 0 && (
              <dd className="text-muted-foreground">{fmtCur(breakdown.xtbPln, "PLN")}</dd>
            )}
            <FxLine amount={breakdown.xtbEur} currency="EUR" pln={breakdown.xtbEurPln} />
            <FxLine amount={breakdown.xtbUsd} currency="USD" pln={breakdown.xtbUsdPln} />
          </div>
          <div>
            <dt className="font-semibold mb-1">Binance</dt>
            <FxLine amount={breakdown.binanceEur} currency="EUR" pln={breakdown.binanceEurPln} />
            {breakdown.binanceEur > 0 && (
              <dd className="text-xs text-muted-foreground/70">→ USDC → krypto</dd>
            )}
          </div>
        </dl>
      </CardContent>
    </Card>
  );
}

function initEdits(items: PurchaseSessionDetail["items"]): ItemEdits {
  const out: ItemEdits = {};
  for (const item of items) {
    out[item.id] = {
      actualUnits: item.actualUnits != null ? String(item.actualUnits) : "",
      actualValuePln: item.actualValuePln != null ? String(item.actualValuePln) : "",
    };
  }
  return out;
}

type Props = {
  session: PurchaseSessionDetail;
};

export function ActiveSessionView({ session }: Props) {
  const queryClient = useQueryClient();
  const [edits, setEdits] = useState<ItemEdits>(() => initEdits(session.items));

  const emeryturaItems = session.items.filter((i) => i.portfolio === "RETIREMENT");
  const inwestycjeItems = session.items.filter((i) => i.portfolio === "MAIN");

  // Keyed off actual item presence, not session.portfoliosIncluded: under ADR-065, a
  // MAIN-only session can legitimately contain RETIREMENT-tagged rows (a cross-portfolio
  // donor sell / its compensating buy) and vice versa. Gating on portfoliosIncluded alone
  // would hide exactly the cross-portfolio activity the user needs to review before
  // pressing "Zakupy zrobione".
  const showEmerytura = emeryturaItems.length > 0;
  const showInwestycje = inwestycjeItems.length > 0;

  function handleEdit(id: number, field: "actualUnits" | "actualValuePln", value: string) {
    setEdits((prev) => {
      const existing = prev[id] ?? { actualUnits: "", actualValuePln: "" };
      return { ...prev, [id]: { ...existing, [field]: value } };
    });
  }

  const saveMutation = useMutation({
    mutationFn: () => {
      const items = Object.entries(edits)
        .map(([rawId, vals]) => ({
          id: Number(rawId),
          actualUnits: vals.actualUnits !== "" ? Number(vals.actualUnits) : null,
          actualValuePln: vals.actualValuePln !== "" ? Number(vals.actualValuePln) : null,
        }))
        .filter((item) => item.actualUnits != null || item.actualValuePln != null);
      if (items.length === 0) {
        return Promise.reject(new Error("Brak zmian do zapisania."));
      }
      return updatePurchaseSessionItems(session.id, items);
    },
    onSuccess: (res) => {
      queryClient.setQueryData(queryKeys.purchaseSessions.active(), res);
      setEdits(initEdits(res.data.items));
      toast.success("Zapisano aktualne wartości.");
    },
    onError: (err: unknown) => {
      const msg = err instanceof Error ? err.message : "Nieznany błąd.";
      toast.error("Błąd zapisu", { description: msg });
    },
  });

  const completeMutation = useMutation({
    mutationFn: () => completePurchaseSession(session.id),
    onSuccess: (res) => {
      const { purchasesWritten, depositsCreated } = res.data;
      toast.success("Sesja zakupów zakończona.", {
        description: `Zapisane transakcje: ${purchasesWritten}, utworzone depozyty: ${depositsCreated}.`,
      });
      startTransition(() => {
        void queryClient.invalidateQueries({ queryKey: queryKeys.purchaseSessions.all });
        // deposits.all also covers cap-usage (["deposits", "cap-usage", ...])
        void queryClient.invalidateQueries({ queryKey: queryKeys.deposits.all });
        void queryClient.invalidateQueries({ queryKey: queryKeys.binanceTransfers.all });
        void queryClient.invalidateQueries({ queryKey: queryKeys.positions.all });
        void queryClient.invalidateQueries({ queryKey: queryKeys.xirr.all });
      });
    },
    onError: (err: unknown) => {
      const msg = err instanceof Error ? err.message : "Nieznany błąd.";
      toast.error("Błąd kończenia sesji", { description: msg });
    },
  });

  const deleteMutation = useMutation({
    mutationFn: () => deletePurchaseSession(session.id),
    onSuccess: () => {
      toast.success("Plan zakupów usunięty.");
      startTransition(() => {
        void queryClient.invalidateQueries({ queryKey: queryKeys.purchaseSessions.all });
      });
    },
    onError: (err: unknown) => {
      const msg = err instanceof Error ? err.message : "Nieznany błąd.";
      toast.error("Błąd usuwania planu", { description: msg });
    },
  });

  const anyPending = saveMutation.isPending || completeMutation.isPending || deleteMutation.isPending;

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between flex-wrap gap-3">
        <div>
          <h2 className="text-lg font-semibold">Aktywna sesja zakupów</h2>
          <p className="text-sm text-muted-foreground">
            Zaplanowana: {fmtDate(session.plannedAt)}
            {session.notes && ` — ${session.notes}`}
          </p>
        </div>
        <div className="flex gap-2">
          <AlertDialog>
            <AlertDialogTrigger asChild>
              <Button variant="destructive" disabled={anyPending}>
                {deleteMutation.isPending ? "Usuwam..." : "Usuń plan"}
              </Button>
            </AlertDialogTrigger>
            <AlertDialogContent>
              <AlertDialogHeader>
                <AlertDialogTitle>Usunąć plan zakupów?</AlertDialogTitle>
                <AlertDialogDescription>
                  Plan zostanie trwale usunięty. Żadne pozycje portfela nie zostaną zmienione.
                </AlertDialogDescription>
              </AlertDialogHeader>
              <AlertDialogFooter>
                <AlertDialogCancel>Anuluj</AlertDialogCancel>
                <AlertDialogAction onClick={() => deleteMutation.mutate()}>Usuń</AlertDialogAction>
              </AlertDialogFooter>
            </AlertDialogContent>
          </AlertDialog>
          <Button variant="outline" onClick={() => saveMutation.mutate()} disabled={anyPending}>
            {saveMutation.isPending ? "Zapisuję..." : "Zapisz aktualne"}
          </Button>
          <AlertDialog>
            <AlertDialogTrigger asChild>
              <Button disabled={anyPending}>
                {completeMutation.isPending ? "Kończę..." : "Zakupy zrobione"}
              </Button>
            </AlertDialogTrigger>
            <AlertDialogContent>
              <AlertDialogHeader>
                <AlertDialogTitle>Zakończyć sesję zakupów?</AlertDialogTitle>
                <AlertDialogDescription>
                  Spowoduje to trwałe zapisanie transakcji i aktualizację pozycji portfela.
                  Tej operacji nie można cofnąć.
                </AlertDialogDescription>
              </AlertDialogHeader>
              <AlertDialogFooter>
                <AlertDialogCancel>Anuluj</AlertDialogCancel>
                <AlertDialogAction onClick={() => completeMutation.mutate()}>Potwierdź</AlertDialogAction>
              </AlertDialogFooter>
            </AlertDialogContent>
          </AlertDialog>
        </div>
      </div>

      <div className="grid grid-cols-1 xl:grid-cols-2 gap-6">
        {showEmerytura && (
          <PortfolioItemsTable
            title="Emerytura"
            items={emeryturaItems}
            edits={edits}
            onEdit={handleEdit}
            portfoliosIncluded={session.portfoliosIncluded}
            fxRates={{
              USD: session.currencyBreakdown.usdPlnRate,
              EUR: session.currencyBreakdown.eurPlnRate,
            }}
          />
        )}
        {showInwestycje && (
          <PortfolioItemsTable
            title="Inwestycje"
            items={inwestycjeItems}
            edits={edits}
            onEdit={handleEdit}
            portfoliosIncluded={session.portfoliosIncluded}
            fxRates={{
              USD: session.currencyBreakdown.usdPlnRate,
              EUR: session.currencyBreakdown.eurPlnRate,
            }}
          />
        )}
      </div>

      <BudgetCheckCard
        session={session}
        emeryturaItems={emeryturaItems}
        inwestycjeItems={inwestycjeItems}
      />

      <CurrencyBreakdownCard breakdown={session.currencyBreakdown} />

      <AccountLimitsCard
        title="Limity IKE/IKZE (wspólne)"
        limits={session.retirementLimitsRemaining}
      />
    </div>
  );
}
