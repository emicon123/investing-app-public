import { useTable, tableFeatures, columnVisibilityFeature, createColumnHelper, flexRender } from "@tanstack/react-table";
import { format, parseISO } from "date-fns";
import { pl } from "date-fns/locale";
import type { Deposit } from "../../api/index.js";
import { formatPln } from "@common/app-core";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Badge } from "@/components/ui/badge";

type Props = {
  data: Deposit[];
};

// Only column visibility is registered: the render reads row.getVisibleCells(),
// which v9 scopes to that feature. The core row model itself is automatic.
const features = tableFeatures({ columnVisibilityFeature });

const columnHelper = createColumnHelper<typeof features, Deposit>();

const columns = columnHelper.columns([
  columnHelper.accessor("depositDate", {
    header: "Data",
    cell: (info) => format(parseISO(info.getValue()), "MMM yyyy", { locale: pl }),
  }),
  columnHelper.accessor("accountType", {
    header: "Konto",
    cell: (info) => <Badge variant="secondary">{info.getValue()}</Badge>,
  }),
  columnHelper.accessor("exchange", {
    header: "Giełda",
    cell: (info) => <Badge variant="outline">{info.getValue()}</Badge>,
  }),
  columnHelper.accessor("amount", {
    header: "Kwota",
    cell: (info) => formatPln(info.getValue()),
  }),
]);

export function DepositsTable({ data }: Props) {
  const table = useTable({
    features,
    data,
    columns,
  });

  return (
    <div className="rounded-md border">
      <Table>
        <TableHeader>
          {table.getHeaderGroups().map((headerGroup) => (
            <TableRow key={headerGroup.id}>
              {headerGroup.headers.map((header) => (
                <TableHead key={header.id} className={header.column.id === "amount" ? "text-right" : undefined}>
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
              <TableCell
                colSpan={columns.length}
                className="h-24 text-center text-muted-foreground"
              >
                Brak depozytów.
              </TableCell>
            </TableRow>
          ) : (
            table.getRowModel().rows.map((row) => (
              <TableRow key={row.id}>
                {row.getVisibleCells().map((cell) => (
                  <TableCell key={cell.id} className={cell.column.id === "amount" ? "text-right tabular-nums" : undefined}>
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
