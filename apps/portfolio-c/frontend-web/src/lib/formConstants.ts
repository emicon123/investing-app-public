import { portfolioSchema } from "../api/index.js";
import type { Portfolio } from "../api/index.js";

export const PORTFOLIO_OPTIONS = portfolioSchema.options;

export const PORTFOLIO_LABELS: Record<Portfolio, string> = {
  VARIABLE: "Wspólny",
  FIXED: "Drugi",
};
