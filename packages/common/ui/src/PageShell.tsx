import type { ReactNode } from "react";
import { Skeleton } from "./skeleton";

export interface PageShellProps {
  isPending: boolean;
  isError: boolean;
  /** aria-label for the loading state — varies per route, no sensible default. */
  loadingLabel: string;
  /** Defaults to a 5-row `space-y-2` skeleton list, matching the most common shape. */
  loadingSkeleton?: ReactNode;
  /** Defaults to the shared copy used verbatim across every route. */
  errorMessage?: ReactNode;
  children: ReactNode;
}

/**
 * Wraps the isPending/isError/children trio repeated across route files.
 * Deliberately does NOT include the route's title/header — that varies too
 * much in layout (plain h1, flex header with action buttons, header + summary
 * bar) across routes/apps to unify without forcing an ill-fitting shape.
 */
export function PageShell({
  isPending,
  isError,
  loadingLabel,
  loadingSkeleton,
  errorMessage,
  children,
}: PageShellProps) {
  if (isPending) {
    return (
      <div
        role="status"
        aria-label={loadingLabel}
        className={loadingSkeleton ? undefined : "space-y-2"}
      >
        {loadingSkeleton ?? (
          <>
            {Array.from({ length: 5 }).map((_, i) => (
              <Skeleton key={i} className="h-10 w-full" />
            ))}
          </>
        )}
      </div>
    );
  }

  if (isError) {
    return (
      <p role="alert" className="text-destructive text-sm">
        {errorMessage ?? "Błąd ładowania danych. Spróbuj odświeżyć stronę."}
      </p>
    );
  }

  return <>{children}</>;
}
