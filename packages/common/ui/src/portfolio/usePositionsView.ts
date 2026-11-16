import { useCallback, useDeferredValue, useMemo, useState } from "react";
import {
  FILTER_ALL,
  applyPositionsFilter,
  buildFilterChips,
  buildPositionsModel,
  isFilteringActive,
  type FilterChip,
  type FilteredPositions,
  type PositionsFilter,
  type PositionsModel,
} from "./model/positionsModel.js";
import type { PortfolioAssetLike, PortfolioSubassetLike } from "./types.js";

/**
 * Filter, search and collapse state of the grouped table (design doc §6 state machine).
 *
 * - Browsing (chip "Wszystkie", empty query): groups open per `openGroups`, which starts from
 *   `defaultCollapsedExposures` and then follows the user's toggles.
 * - Filtering (another chip or a query): groups without a match are hidden, matching groups open;
 *   toggling a group writes only `filterOpen`, which every chip/query change resets. Leaving
 *   Filtering therefore restores exactly the browse state.
 * - Expanded assets survive filtering.
 *
 * The query is applied through `useDeferredValue`, so typing stays responsive (the old tables used
 * `startTransition` for the same reason).
 */

export interface PositionsViewOptions {
  /** Exposure values whose group starts collapsed (main MAIN: ["EXPERIMENTS"]). Absent keys are ignored. */
  defaultCollapsedExposures?: readonly string[];
}

export interface PositionsView<TAsset> {
  model: PositionsModel<TAsset>;
  chips: FilterChip[];
  filter: PositionsFilter;
  setFilter: (filter: PositionsFilter) => void;
  query: string;
  setQuery: (query: string) => void;
  clearFilters: () => void;
  filtered: FilteredPositions<TAsset>;
  isGroupOpen: (exposure: string) => boolean;
  toggleGroup: (exposure: string) => void;
  isAssetExpanded: (assetId: number) => boolean;
  toggleAsset: (assetId: number) => void;
}

const NO_COLLAPSED: readonly string[] = [];

export function usePositionsView<TAsset extends PortfolioAssetLike<PortfolioSubassetLike>>(
  assets: readonly TAsset[],
  { defaultCollapsedExposures = NO_COLLAPSED }: PositionsViewOptions = {},
): PositionsView<TAsset> {
  const model = useMemo(() => buildPositionsModel(assets), [assets]);
  const chips = useMemo(() => buildFilterChips(model), [model]);

  const [filter, setFilterState] = useState<PositionsFilter>(FILTER_ALL);
  const [query, setQueryState] = useState("");
  const deferredQuery = useDeferredValue(query);
  const [openGroups, setOpenGroups] = useState<Readonly<Record<string, boolean>>>({});
  const [filterOpen, setFilterOpen] = useState<Readonly<Record<string, boolean>>>({});
  const [expandedAssetIds, setExpandedAssetIds] = useState<ReadonlySet<number>>(() => new Set());

  const filtering = isFilteringActive(filter, deferredQuery);
  const filtered = useMemo(() => applyPositionsFilter(model, filter, deferredQuery), [model, filter, deferredQuery]);

  const isOpenWhileBrowsing = useCallback(
    (exposure: string) => openGroups[exposure] ?? !defaultCollapsedExposures.includes(exposure),
    [openGroups, defaultCollapsedExposures],
  );

  const isGroupOpen = useCallback(
    (exposure: string) => (filtering ? filterOpen[exposure] !== false : isOpenWhileBrowsing(exposure)),
    [filtering, filterOpen, isOpenWhileBrowsing],
  );

  const toggleGroup = useCallback(
    (exposure: string) => {
      if (filtering) {
        setFilterOpen((prev) => ({ ...prev, [exposure]: prev[exposure] === false }));
      } else {
        setOpenGroups((prev) => ({ ...prev, [exposure]: !isOpenWhileBrowsing(exposure) }));
      }
    },
    [filtering, isOpenWhileBrowsing],
  );

  const setFilter = useCallback((next: PositionsFilter) => {
    setFilterState(next);
    setFilterOpen({});
  }, []);

  const setQuery = useCallback((next: string) => {
    setQueryState(next);
    setFilterOpen({});
  }, []);

  const clearFilters = useCallback(() => {
    setFilterState(FILTER_ALL);
    setQueryState("");
    setFilterOpen({});
  }, []);

  const isAssetExpanded = useCallback((assetId: number) => expandedAssetIds.has(assetId), [expandedAssetIds]);

  const toggleAsset = useCallback((assetId: number) => {
    setExpandedAssetIds((prev) => {
      const next = new Set(prev);
      if (!next.delete(assetId)) next.add(assetId);
      return next;
    });
  }, []);

  return {
    model,
    chips,
    filter,
    setFilter,
    query,
    setQuery,
    clearFilters,
    filtered,
    isGroupOpen,
    toggleGroup,
    isAssetExpanded,
    toggleAsset,
  };
}
