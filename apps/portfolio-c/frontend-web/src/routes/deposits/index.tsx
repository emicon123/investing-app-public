import { useQuery, useQueryClient } from "@tanstack/react-query";
import { listDeposits, createDeposit } from "../../api/index.js";
import type { DepositFormValues, IdempotencyOptions } from "@common/app-core";
import { queryKeys } from "@/lib/queryKeys";
import { usePortfolio } from "@/lib/portfolio";
import { PORTFOLIO_LABELS } from "@/lib/formConstants";
import { DepositsTable } from "@/components/tables/DepositsTable";
import { DepositsPage as SharedDepositsPage } from "@common/ui-primitives/DepositsPage";

export default function DepositsPage() {
  const { portfolio } = usePortfolio();
  const queryClient = useQueryClient();

  function useDepositsQuery() {
    const { data, isPending, isError } = useQuery({
      queryKey: queryKeys.deposits.list(portfolio),
      queryFn: () => listDeposits({ portfolio, size: 100 }),
      staleTime: 5 * 60_000,
    });

    return { data: data?.data, isPending, isError };
  }

  function handleCreateDeposit(values: DepositFormValues, options: IdempotencyOptions) {
    return createDeposit(
      {
        portfolio,
        depositDate: values.depositDate,
        amount: values.amount,
        comment: values.comment || undefined,
      },
      options,
    );
  }

  return (
    <SharedDepositsPage
      title={`Depozyty — ${PORTFOLIO_LABELS[portfolio]}`}
      dialogTitle={`Dodaj depozyt — ${PORTFOLIO_LABELS[portfolio]}`}
      useDepositsQuery={useDepositsQuery}
      renderTable={(deposits) => <DepositsTable data={deposits} />}
      createDeposit={handleCreateDeposit}
      onDepositSaved={() => {
        void queryClient.invalidateQueries({ queryKey: queryKeys.deposits.list(portfolio) });
      }}
    />
  );
}
