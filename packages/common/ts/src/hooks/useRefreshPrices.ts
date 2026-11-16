import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";

type RefreshStatus = "idle" | "ok" | "warn" | "err";

interface PriceRefreshResult {
  total: number;
  succeeded: number;
  failures: { ticker: string; reason: string }[];
}

/**
 * Byte-identical across investing-app/portfolio-b/portfolio-c except for which generated
 * `refreshPrices` client function each app injects — see each app's
 * `hooks/useRefreshPrices.ts` wrapper.
 */
export function createUseRefreshPrices(refreshPrices: () => Promise<PriceRefreshResult>) {
  return function useRefreshPrices() {
    const queryClient = useQueryClient();
    const [status, setStatus] = useState<RefreshStatus>("idle");

    const mutation = useMutation({
      mutationFn: refreshPrices,
      onSuccess: (data) => {
        if (data.failures.length > 0) {
          setStatus("warn");
          toast.error(`Nie udało się odświeżyć ${data.failures.length} z ${data.total} cen`, {
            description: data.failures.map((f) => `${f.ticker}: ${f.reason}`).join("\n"),
          });
        } else {
          setStatus("ok");
        }
        void queryClient.invalidateQueries();
        setTimeout(() => setStatus("idle"), 4000);
      },
      onError: (err: unknown) => {
        setStatus("err");
        toast.error("Odświeżanie cen nie powiodło się", {
          description: err instanceof Error ? err.message : "Nieznany błąd połączenia",
        });
        setTimeout(() => setStatus("idle"), 4000);
      },
    });

    return {
      refresh: () => mutation.mutate(),
      isRefreshing: mutation.isPending,
      status,
    };
  };
}
