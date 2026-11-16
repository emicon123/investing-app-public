import { BarChart2, PiggyBank, Flame, Bitcoin, FileText, Settings, ShoppingCart, LineChart, ScatterChart, Newspaper, Bot } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { getFxRates } from "../api/index.js";
import { AppLayout as SharedAppLayout, type NavSection, type UseFxRatesResult } from "@common/ui-primitives/AppLayout";
import { queryKeys } from "@/lib/queryKeys";
import { useRefreshPrices } from "@/hooks/useRefreshPrices";
import { ROUTES } from "@/lib/routes";

const NAV_SECTIONS: NavSection[] = [
  {
    title: "Portfel",
    items: [
      { label: "Przegląd", to: ROUTES.portfolio, end: true, icon: <BarChart2 className="h-4 w-4" aria-hidden="true" /> },
      { label: "Zakupy", to: ROUTES.purchases, icon: <ShoppingCart className="h-4 w-4" aria-hidden="true" /> },
      { label: "Regresja", to: ROUTES.regression, icon: <LineChart className="h-4 w-4" aria-hidden="true" /> },
      { label: "Historia transakcji", to: ROUTES.tradeHistory, icon: <ScatterChart className="h-4 w-4" aria-hidden="true" /> },
    ],
  },
  {
    title: "Dochód",
    items: [
      { label: "Wpłaty", to: ROUTES.deposits, icon: <PiggyBank className="h-4 w-4" aria-hidden="true" /> },
    ],
  },
  {
    title: "Emerytura",
    items: [
      { label: "Emerytura", to: ROUTES.retirement, icon: <Flame className="h-4 w-4" aria-hidden="true" /> },
    ],
  },
  {
    title: "Krypto",
    items: [
      { label: "Transakcje", to: ROUTES.crypto, icon: <Bitcoin className="h-4 w-4" aria-hidden="true" /> },
      { label: "Podatek PIT-38", to: ROUTES.cryptoTax, icon: <FileText className="h-4 w-4" aria-hidden="true" /> },
    ],
  },
  {
    title: "Wiadomości",
    items: [
      { label: "Wiadomości", to: ROUTES.news, icon: <Newspaper className="h-4 w-4" aria-hidden="true" /> },
    ],
  },
  {
    title: "Bot",
    items: [
      { label: "Bot OKX", to: ROUTES.bot, icon: <Bot className="h-4 w-4" aria-hidden="true" /> },
    ],
  },
  {
    title: "Ustawienia",
    items: [
      { label: "Ustawienia", to: ROUTES.settings, icon: <Settings className="h-4 w-4" aria-hidden="true" /> },
    ],
  },
];

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
      brand={{ label: "Inwestycje", mark: <span className="brand-mark" aria-hidden="true" /> }}
      navSections={NAV_SECTIONS}
      useFxRates={useFxRates}
      useRefreshPrices={useRefreshPrices}
    />
  );
}
