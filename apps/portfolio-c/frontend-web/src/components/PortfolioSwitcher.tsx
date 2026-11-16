import { startTransition } from "react";
import type { Portfolio } from "../api/index.js";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { PORTFOLIO_OPTIONS, PORTFOLIO_LABELS } from "@/lib/formConstants";
import { usePortfolio } from "@/lib/portfolio";

/**
 * Always-visible portfolio selector — every data-fetching hook in the app
 * scopes its queries to whichever of the two portfolios (VARIABLE / FIXED)
 * is selected here. Lives in the top bar so it's visible on every page.
 */
export function PortfolioSwitcher() {
  const { portfolio, setPortfolio } = usePortfolio();

  return (
    <Tabs
      value={portfolio}
      onValueChange={(value) => startTransition(() => setPortfolio(value as Portfolio))}
    >
      <TabsList aria-label="Wybór portfela">
        {PORTFOLIO_OPTIONS.map((option) => (
          <TabsTrigger key={option} value={option} className="text-xs sm:text-sm">
            {PORTFOLIO_LABELS[option]}
          </TabsTrigger>
        ))}
      </TabsList>
    </Tabs>
  );
}
