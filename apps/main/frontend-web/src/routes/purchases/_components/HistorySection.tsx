import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { formatPln } from "@common/app-core";
import { listPurchaseSessions } from "../../../api/index.js";
import { queryKeys } from "@/lib/queryKeys";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

const fmtDate = (iso: string) =>
  new Intl.DateTimeFormat("pl-PL", {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(new Date(iso));

export function HistorySection() {
  const [page, setPage] = useState(0);

  const { data, isPending } = useQuery({
    queryKey: queryKeys.purchaseSessions.list(page),
    queryFn: () => listPurchaseSessions(page, 10),
    staleTime: 5 * 60_000,
  });

  if (isPending) {
    return <p className="text-sm text-muted-foreground">Ładowanie historii...</p>;
  }

  const sessions = data?.data ?? [];

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">Purchase history</CardTitle>
      </CardHeader>
      <CardContent className="p-0">
        {sessions.length === 0 ? (
          <p className="p-4 text-sm text-muted-foreground">Brak zakończonych sesji.</p>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Data</TableHead>
                <TableHead>Portfel</TableHead>
                <TableHead className="text-right">Emerytura PLN</TableHead>
                <TableHead className="text-right">Inwestycje PLN</TableHead>
                <TableHead className="text-right">Łącznie PLN</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {sessions.map((s) => (
                <TableRow key={s.id}>
                  <TableCell className="text-sm">{fmtDate(s.plannedAt)}</TableCell>
                  <TableCell>
                    <Badge variant="outline" className="text-xs">
                      {s.portfoliosIncluded}
                    </Badge>
                  </TableCell>
                  <TableCell className="text-right text-sm">
                    {s.retirementBudgetPln != null ? formatPln(s.retirementBudgetPln) : "—"}
                  </TableCell>
                  <TableCell className="text-right text-sm">
                    {s.mainBudgetPln != null ? formatPln(s.mainBudgetPln) : "—"}
                  </TableCell>
                  <TableCell className="text-right font-semibold text-sm">
                    {s.totalActualPln != null ? formatPln(s.totalActualPln) : "—"}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
        {data && data.page.total > 10 && (
          <div className="flex justify-between items-center px-4 py-3 border-t">
            <Button
              variant="outline"
              size="sm"
              onClick={() => setPage((p) => Math.max(0, p - 1))}
              disabled={page === 0}
            >
              Poprzednia
            </Button>
            <span className="text-xs text-muted-foreground">
              Strona {page + 1} z {Math.ceil(data.page.total / 10)}
            </span>
            <Button
              variant="outline"
              size="sm"
              onClick={() => setPage((p) => p + 1)}
              disabled={(page + 1) * 10 >= data.page.total}
            >
              Następna
            </Button>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
