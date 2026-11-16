import { create } from "zustand";
import { persist } from "zustand/middleware";
import type { Portfolio } from "../api/index.js";

/**
 * Which of the two portfolios (VARIABLE / FIXED) the whole app is currently
 * scoped to. This is UI-only navigation state (akin to a top-level tab
 * selection), not server data — every data-fetching hook reads it and passes
 * it as the `portfolio` param/path-segment/body-field the backend now
 * requires on every endpoint (see docs/adr/001-portfolio-dimension.md).
 *
 * Persisted to localStorage (mirrors the useAuthStore pattern in lib/auth.ts)
 * so a page refresh doesn't silently snap the user back to VARIABLE.
 */
interface PortfolioState {
  portfolio: Portfolio;
  setPortfolio: (portfolio: Portfolio) => void;
}

export const usePortfolioStore = create<PortfolioState>()(
  persist(
    (set) => ({
      portfolio: "VARIABLE",
      setPortfolio: (portfolio) => set({ portfolio }),
    }),
    { name: "portfolio-c-portfolio" },
  ),
);

export function usePortfolio(): { portfolio: Portfolio; setPortfolio: (portfolio: Portfolio) => void } {
  const portfolio = usePortfolioStore((s) => s.portfolio);
  const setPortfolio = usePortfolioStore((s) => s.setPortfolio);
  return { portfolio, setPortfolio };
}
