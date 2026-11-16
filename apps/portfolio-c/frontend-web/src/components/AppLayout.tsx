import { useQuery } from "@tanstack/react-query";
import { getFxRates } from "../api/index.js";
import { AppLayout as SharedAppLayout, type UseFxRatesResult } from "@common/ui-primitives/AppLayout";
import { queryKeys } from "@/lib/queryKeys";
import { useRefreshPrices } from "@/hooks/useRefreshPrices";
import { PortfolioSwitcher } from "@/components/PortfolioSwitcher";

function useFxRates(): UseFxRatesResult {
  const { data } = useQuery({
    queryKey: queryKeys.fxRates.all,
    queryFn: () => getFxRates(),
    staleTime: 5 * 60_000,
    refetchInterval: 15 * 60_000,
  });

  return { data: data?.data };
}

export default function AppLayout() {
  return (
    <SharedAppLayout
      brand={{ label: "PortfolioC" }}
      topBarExtra={<PortfolioSwitcher />}
      useFxRates={useFxRates}
      useRefreshPrices={useRefreshPrices}
    />
  );
}
