import type { ReactNode } from "react";
import { cn } from "./lib/utils.js";

export interface PageHeaderProps {
  title: string;
  actions?: ReactNode;
  className?: string;
}

/**
 * Shared title+actions row for portfolioB/portfolioC route headers. Deliberately a
 * simpler variant than investing-app's own `PageHeader.tsx` (Prism Ledger
 * redesign, `font-display`/`text-3xl` styling) — portfolioB/portfolioC's plain shadcn
 * theme has no `font-display` token, so this matches their existing
 * `text-2xl font-semibold` sizing instead. Does not include the route's
 * outer `p-6 space-y-6` container — that stays in each route file wrapping
 * `PageHeader` plus the rest of the route content.
 */
export function PageHeader({ title, actions, className }: PageHeaderProps) {
  return (
    <div className={cn("flex items-center justify-between gap-4", className)}>
      <h1 className="text-2xl font-semibold">{title}</h1>
      {actions && <div className="flex gap-2">{actions}</div>}
    </div>
  );
}
