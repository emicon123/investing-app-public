import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  useTable,
  tableFeatures,
  rowSortingFeature,
  columnVisibilityFeature,
  createSortedRowModel,
  createColumnHelper,
  sortFn_alphanumeric,
  sortFn_basic,
  sortFn_datetime,
  sortFn_text,
  flexRender,
  type SortingState,
} from "@tanstack/react-table";
import { format } from "date-fns";
import { formatPln } from "@common/app-core";
import { listCryptoTrades, type CryptoTrade } from "../../../api/index.js";
import { queryKeys } from "@/lib/queryKeys";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  Tooltip,
  TooltipTrigger,
  TooltipContent,
  TooltipProvider,
} from "@/components/ui/tooltip";

// Static v9 feature set: client-side sorting only (core row model is automatic).

const features = tableFeatures({
  rowSortingFeature,
  columnVisibilityFeature,
  sortedRowModel: createSortedRowModel(),
  // String-named sort fns (incl. the default 'auto' resolution) must be registered
  // or sorting silently stops working.
  sortFns: {
    alphanumeric: sortFn_alphanumeric,
    basic: sortFn_basic,
    datetime: sortFn_datetime,
    text: sortFn_text,
  },
});

const columnHelper = createColumnHelper<typeof features, CryptoTrade>();

const tradesColumns = columnHelper.columns([
  {
    accessorKey: "tradeDate",
    header: "Data",
    cell: ({ row }) => format(new Date(row.original.tradeDate), "dd MMM yyyy"),
  },
  {
    accessorKey: "ticker",
    header: "Ticker",
    cell: ({ row }) => (
      <span className="font-mono font-semibold">{row.original.ticker}</span>
    ),
  },
  {
    accessorKey: "side",
    header: "Strona",
    cell: ({ row }) => (
      <Badge
        variant={row.original.side === "BUY" ? "default" : "destructive"}
        className={
          row.original.side === "BUY"
            ? "bg-green-600 text-white border-green-600"
            : ""
        }
      >
        {row.original.side}
      </Badge>
    ),
  },
  {
    accessorKey: "quantity",
    header: "Ilość",
    cell: ({ row }) => row.original.quantity.toFixed(8),
  },
  {
    accessorKey: "unitPrice",
    header: "Cena",
    cell: ({ row }) =>
      `${row.original.unitPrice.toLocaleString("pl-PL", { maximumFractionDigits: 4 })} ${row.original.priceCurrency}`,
  },
  {
    accessorKey: "amountPln",
    header: "Kwota PLN",
    cell: ({ row }) => formatPln(row.original.amountPln),
  },
  {
    id: "actions",
    header: "Akcje",
    cell: () => (
      <TooltipProvider>
        <Tooltip>
          <TooltipTrigger asChild>
            <span>
              <Button variant="outline" size="sm" disabled aria-label="Usuń transakcję">
                Usuń
              </Button>
            </span>
          </TooltipTrigger>
          <TooltipContent>Usuń niedostępne</TooltipContent>
        </Tooltip>
      </TooltipProvider>
    ),
  },
]);

export function TradesHistoryTable() {
  const [sorting, setSorting] = useState<SortingState>([
    { id: "tradeDate", desc: true },
  ]);

  const { data, isPending, isError } = useQuery({
    queryKey: queryKeys.cryptoTrades.list(),
    queryFn: () => listCryptoTrades({ size: 100 }),
    staleTime: 5 * 60_000,
  });

  const trades = data?.data ?? [];

  const table = useTable({
    features,
    data: trades,
    columns: tradesColumns,
    state: { sorting },
    onSortingChange: setSorting,
  });

  if (isPending) return <p className="text-muted-foreground py-4">Ładowanie...</p>;
  if (isError) return <p role="alert" className="text-destructive py-4">Błąd ładowania transakcji.</p>;

  return (
    <div className="rounded-md border">
      <Table>
        <TableHeader>
          {table.getHeaderGroups().map((headerGroup) => (
            <TableRow key={headerGroup.id}>
              {headerGroup.headers.map((header) => (
                <TableHead
                  key={header.id}
                  className={["quantity", "unitPrice", "amountPln"].includes(header.column.id) ? "text-right" : undefined}
                >
                  {header.isPlaceholder
                    ? null
                    : flexRender(header.column.columnDef.header, header.getContext())}
                </TableHead>
              ))}
            </TableRow>
          ))}
        </TableHeader>
        <TableBody>
          {table.getRowModel().rows.length === 0 ? (
            <TableRow>
              <TableCell colSpan={tradesColumns.length} className="text-center text-muted-foreground py-8">
                Brak transakcji
              </TableCell>
            </TableRow>
          ) : (
            table.getRowModel().rows.map((row) => (
              <TableRow key={row.id}>
                {row.getVisibleCells().map((cell) => (
                  <TableCell
                    key={cell.id}
                    className={["quantity", "unitPrice", "amountPln"].includes(cell.column.id) ? "text-right tabular-nums" : undefined}
                  >
                    {flexRender(cell.column.columnDef.cell, cell.getContext())}
                  </TableCell>
                ))}
              </TableRow>
            ))
          )}
        </TableBody>
      </Table>
    </div>
  );
}
