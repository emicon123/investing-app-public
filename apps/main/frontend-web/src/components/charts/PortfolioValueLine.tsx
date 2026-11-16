import { useQuery } from "@tanstack/react-query";
import {
  getMacroStateHistory,
  listPositions,
  type HistoriaEntry,
} from "../../api/index.js";
import { PortfolioValueLine as SharedPortfolioValueLine } from "@common/ui-primitives/PortfolioValueLine";
import { queryKeys } from "@/lib/queryKeys";

type Props = { data: HistoriaEntry[]; height?: number | string };

const STALE_TIME_MS = 5 * 60 * 1000;

/**
 * Main-app adapter: owns the two data sources the shared chart cannot fetch
 * itself (positions for exposure colours, macro history for the blend
 * overlay). The chart is presentation-only.
 */
export function PortfolioValueLine({ data, height }: Props) {
  const positionsQuery = useQuery({
    queryKey: queryKeys.positions.all,
    queryFn: () => listPositions({ size: 1000 }),
    staleTime: STALE_TIME_MS,
  });
  const blendHistoryQuery = useQuery({
    queryKey: queryKeys.macroState.history,
    queryFn: getMacroStateHistory,
    staleTime: STALE_TIME_MS,
  });

  return (
    <SharedPortfolioValueLine
      data={data}
      height={height}
      positions={positionsQuery.data?.data ?? []}
      blendPoints={blendHistoryQuery.data?.data ?? []}
    />
  );
}
