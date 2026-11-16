/**
 * History-chart palette. Values live as `--history-*` in
 * `history-chart-tokens.css` so the chart is theme-aware without duplicating
 * oklch literals in TS. Package-private: the CSS vars only make sense with the
 * shared token file, so this is not re-exported from `@common/app-core`.
 */
export const HISTORY_CHART_COLORS = {
  portfolioValue: "var(--history-value)",
  performance: "var(--history-performance)",
  deposits: "var(--history-deposits)",
  inflationAdjusted: "var(--history-inflation)",
  realReturn: "var(--history-real)",
  zero: "var(--history-zero)",
  annualized: "var(--history-annualized)",
  profit: "var(--history-profit)",
  loss: "var(--history-loss)",
  profitText: "var(--history-profit-text)",
  lossText: "var(--history-loss-text)",
  profitFill: "var(--history-profit-fill)",
  lossFill: "var(--history-loss-fill)",

  axisTick: "var(--history-axis-tick)",
  axisLine: "var(--history-axis-line)",
} as const;
