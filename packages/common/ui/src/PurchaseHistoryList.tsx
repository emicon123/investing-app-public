import { PurchaseHistoryCard, type PurchaseSessionLike } from "./PurchaseHistoryCard.js";

/**
 * portfolioB/portfolioC only — investing-app has no `PurchaseHistoryList` counterpart.
 * Owns the three UI states + the card list; the only per-app diff (portfolioC's
 * portfolio threading) is absorbed by the injected zero-arg hook
 * (HistoriaDialog `useHistoriaQuery` precedent).
 */
export interface PurchaseHistorySessionLike extends PurchaseSessionLike {
  /** PurchaseSessionLike has no id; the list needs a React key. */
  sessionId: string | number;
}

export interface PurchaseHistoryQueryLike {
  data: { data: { sessions: PurchaseHistorySessionLike[] } } | undefined;
  isPending: boolean;
  isError: boolean;
}

export function PurchaseHistoryList({
  useHistoryQuery,
}: {
  useHistoryQuery: () => PurchaseHistoryQueryLike;
}) {
  const { data, isPending, isError } = useHistoryQuery();

  if (isPending) {
    return <p className="text-sm text-muted-foreground">Ładowanie historii…</p>;
  }

  if (isError) {
    return (
      <p role="alert" className="text-sm text-destructive">
        Nie udało się wczytać historii zakupów.
      </p>
    );
  }

  const sessions = data?.data.sessions ?? [];

  if (sessions.length === 0) {
    return (
      <p className="text-sm text-muted-foreground">
        Brak zakupów. Historia pojawi się po pierwszym zatwierdzonym zakupie.
      </p>
    );
  }

  return (
    <div className="space-y-3">
      {sessions.map((session) => (
        <PurchaseHistoryCard key={session.sessionId} session={session} />
      ))}
    </div>
  );
}
