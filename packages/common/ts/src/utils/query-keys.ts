/**
 * `createQueryKeys()` factory for the portfolioB/portfolioC query-key sets (task 156,
 * Phase A3). Values are byte-identical to the per-app `lib/queryKeys.ts`
 * literals they replace — same arrays, same order, same arity — so no cache
 * invalidation changes. Main's 73-line key set has different key shapes and
 * stays separate (out of scope).
 *
 * `scope` is typed `string`, not either app's `Portfolio` enum — adapters
 * pass their own narrow type through; this package imports no app enum
 * (CreatePositionDialog precedent).
 */

export interface UnscopedQueryKeys {
  positions: {
    all: readonly ["positions"];
    list: () => readonly ["positions", "list"];
  };
  history: {
    all: readonly ["history"];
    list: () => readonly ["history", "list"];
    /** Snapshots from `from` on (the portfolio KPI sparkline, task 170). */
    since: (from: string) => readonly ["history", "since", string];
  };
  deposits: {
    all: readonly ["deposits"];
    list: () => readonly ["deposits", "list"];
    xirr: () => readonly ["deposits", "xirr"];
  };
  purchaseHistory: {
    all: readonly ["purchase-history"];
    list: () => readonly ["purchase-history", "list"];
  };
  xirr: {
    all: readonly ["xirr"];
  };
  settings: {
    all: readonly ["settings"];
  };
  fxRates: {
    all: readonly ["fx-rates"];
  };
  rebalancing: {
    all: readonly ["rebalancing"];
  };
  /** `GET /market-data/prices` (cache-only; "Ceny z …", task 170). */
  prices: {
    all: readonly ["prices"];
  };
}

export interface ScopedQueryKeys {
  positions: {
    all: readonly ["positions"];
    list: (scope: string) => readonly ["positions", "list", string];
  };
  history: {
    all: readonly ["history"];
    list: (scope: string) => readonly ["history", "list", string];
    /** Snapshots from `from` on (the portfolio KPI sparkline, task 170). */
    since: (scope: string, from: string) => readonly ["history", "since", string, string];
  };
  deposits: {
    all: readonly ["deposits"];
    list: (scope: string) => readonly ["deposits", "list", string];
    xirr: (scope: string) => readonly ["deposits", "xirr", string];
  };
  purchaseHistory: {
    all: readonly ["purchase-history"];
    list: (scope: string) => readonly ["purchase-history", string];
  };
  settings: {
    all: readonly ["settings"];
  };
  fxRates: {
    all: readonly ["fx-rates"];
  };
  rebalancing: {
    all: readonly ["rebalancing"];
  };
  /** `GET /market-data/prices` (cache-only; "Ceny z …", task 170). */
  prices: {
    all: readonly ["prices"];
  };
}

export function createQueryKeys(options: { scoped: false }): UnscopedQueryKeys;
export function createQueryKeys(options: { scoped: true }): ScopedQueryKeys;
export function createQueryKeys(
  options: { scoped: boolean },
): UnscopedQueryKeys | ScopedQueryKeys {
  if (options.scoped) {
    return {
      positions: {
        all: ["positions"] as const,
        list: (scope: string) => ["positions", "list", scope] as const,
      },
      history: {
        all: ["history"] as const,
        list: (scope: string) => ["history", "list", scope] as const,
        since: (scope: string, from: string) => ["history", "since", scope, from] as const,
      },
      deposits: {
        all: ["deposits"] as const,
        list: (scope: string) => ["deposits", "list", scope] as const,
        xirr: (scope: string) => ["deposits", "xirr", scope] as const,
      },
      purchaseHistory: {
        all: ["purchase-history"] as const,
        list: (scope: string) => ["purchase-history", scope] as const,
      },
      settings: {
        all: ["settings"] as const,
      },
      fxRates: {
        all: ["fx-rates"] as const,
      },
      rebalancing: {
        all: ["rebalancing"] as const,
      },
      prices: {
        all: ["prices"] as const,
      },
    };
  }
  return {
    positions: {
      all: ["positions"] as const,
      list: () => ["positions", "list"] as const,
    },
    history: {
      all: ["history"] as const,
      list: () => ["history", "list"] as const,
      since: (from: string) => ["history", "since", from] as const,
    },
    deposits: {
      all: ["deposits"] as const,
      list: () => ["deposits", "list"] as const,
      xirr: () => ["deposits", "xirr"] as const,
    },
    purchaseHistory: {
      all: ["purchase-history"] as const,
      list: () => ["purchase-history", "list"] as const,
    },
    xirr: {
      all: ["xirr"] as const,
    },
    settings: {
      all: ["settings"] as const,
    },
    fxRates: {
      all: ["fx-rates"] as const,
    },
    rebalancing: {
      all: ["rebalancing"] as const,
    },
    prices: {
      all: ["prices"] as const,
    },
  };
}
