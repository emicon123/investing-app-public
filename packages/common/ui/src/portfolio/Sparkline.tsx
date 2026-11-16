import { SPARKLINE_HEIGHT, SPARKLINE_WIDTH, type SparklineModel } from "./model/sparklineModel.js";

/**
 * The valuation micro-chart in the first KPI card (design doc §14 D10: no hover layer; the
 * `aria-label` states the range, the full chart is in "Historia").
 */
export function Sparkline({ model }: { model: SparklineModel }) {
  return (
    <div className="relative h-14 max-w-[280px] min-w-0 flex-[1_1_140px]">
      <svg
        viewBox={`0 0 ${SPARKLINE_WIDTH} ${SPARKLINE_HEIGHT}`}
        preserveAspectRatio="none"
        role="img"
        aria-label={model.ariaLabel}
        className="block h-14 w-full overflow-visible"
      >
        <path d={model.area} className="fill-text-2/8" />
        <path
          d={model.line}
          fill="none"
          strokeWidth={1.5}
          strokeLinejoin="round"
          vectorEffect="non-scaling-stroke"
          className="stroke-text-2"
        />
      </svg>
      <span
        aria-hidden="true"
        // The end dot follows the last point's height, which only the model knows.
        style={{ top: `${model.endYPct.toFixed(1)}%` }}
        className="absolute -right-[3px] -mt-[3.5px] size-[7px] rounded-full bg-text-1 shadow-[0_0_0_3px_rgba(231,236,243,0.15)]"
      />
    </div>
  );
}
