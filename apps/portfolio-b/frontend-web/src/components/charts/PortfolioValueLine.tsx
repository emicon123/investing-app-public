import { useQuery } from "@tanstack/react-query";
import { listPositions } from "../../api/index.js";
import {
  PortfolioValueLine as SharedPortfolioValueLine,
  type HistoriaEntryLike,
  type WallPositionLike,
} from "@common/ui-primitives/PortfolioValueLine";
import { queryKeys } from "@/lib/queryKeys";

(globalThis as unknown as Record<string, unknown>).__portfolio_bPortfolioValueLineWrapper = true;

type Props = {
  data: HistoriaEntryLike[];
  height?: number | string;
  positions?: WallPositionLike[];
};

export function PortfolioValueLine({ data, height, positions: positionsProp }: Props) {
  const { data: positionsData } = useQuery({
    queryKey: queryKeys.positions.list(),
    queryFn: () => listPositions({ size: 1000 }),
    staleTime: 60_000,
    enabled: positionsProp == null,
  });
  const positions =
    positionsProp ??
    (positionsData as unknown as { data?: WallPositionLike[] })?.data ??
    undefined;
  return <SharedPortfolioValueLine data={data} height={height} positions={positions} />;
}

export type { HistoriaEntryLike, WallPositionLike };
