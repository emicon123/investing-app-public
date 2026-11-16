import { useQuery, useQueryClient } from "@tanstack/react-query";
import { listDeposits, createDeposit } from "../../api/index.js";
import { queryKeys } from "@/lib/queryKeys";
import { DepositsTable } from "@/components/tables/DepositsTable";
import { DepositsPage as SharedDepositsPage } from "@common/ui-primitives/DepositsPage";

function useDepositsQuery() {
  const { data, isPending, isError } = useQuery({
    queryKey: queryKeys.deposits.list(),
    queryFn: () => listDeposits({ size: 100 }),
    staleTime: 5 * 60_000,
  });

  return { data: data?.data, isPending, isError };
}

export default function DepositsPage() {
  const queryClient = useQueryClient();

  return (
    <SharedDepositsPage
      title="Depozyty"
      dialogTitle="Dodaj depozyt"
      useDepositsQuery={useDepositsQuery}
      renderTable={(deposits) => <DepositsTable data={deposits} />}
      createDeposit={createDeposit}
      onDepositSaved={() => {
        void queryClient.invalidateQueries({ queryKey: queryKeys.deposits.list() });
      }}
    />
  );
}
