import { useQuery } from "@tanstack/react-query";
import { getActivePurchaseSession } from "../../api/index.js";
import { queryKeys } from "@/lib/queryKeys";
import { isAxiosNotFound } from "@/lib/queryUtils";
import { PageHeader } from "@/components/PageHeader";
import { ActiveSessionView } from "./_components/ActiveSessionView";
import { NoSessionView } from "./_components/NoSessionView";

export default function PurchasesPage() {
  const { data: activeData, isPending, isError } = useQuery({
    queryKey: queryKeys.purchaseSessions.active(),
    queryFn: async () => {
      try {
        return await getActivePurchaseSession();
      } catch (err: unknown) {
        if (isAxiosNotFound(err)) return null;
        throw err;
      }
    },
    staleTime: 30_000,
    retry: (failureCount, err: unknown) =>
      isAxiosNotFound(err) ? false : failureCount < 2,
  });

  return (
    <div className="p-6 space-y-6">
      <PageHeader eyebrow="Portfel" title="Zakupy" />

      {isPending && (
        <p className="text-muted-foreground text-sm">Sprawdzam aktywną sesję...</p>
      )}

      {isError && (
        <p role="alert" className="text-sm text-destructive">
          Błąd ładowania sesji. Spróbuj odświeżyć stronę.
        </p>
      )}

      {!isPending && !isError && activeData === null && <NoSessionView />}

      {activeData != null && <ActiveSessionView session={activeData.data} />}
    </div>
  );
}
