import type { PortfolioType } from "../api/index.js";

export const queryKeys = {
  positions: {
    all: ["positions"] as const,
    list: (portfolio: PortfolioType) => ["positions", "list", portfolio] as const,
  },
  history: {
    all: ["history"] as const,
    portfolio: (portfolio: PortfolioType) => ["history", portfolio] as const,
    /** Snapshots from `from` on (the portfolio KPI sparkline, task 170). */
    since: (portfolio: PortfolioType, from: string) => ["history", portfolio, "since", from] as const,
  },
  deposits: {
    all: ["deposits"] as const,
    list: (portfolio?: PortfolioType) => ["deposits", "list", portfolio ?? "ALL"] as const,
    capUsage: (year: number) => ["deposits", "cap-usage", year] as const,
  },
  binanceTransfers: {
    all: ["binance-transfers"] as const,
    list: (year?: number) => ["binance-transfers", "list", year ?? "ALL"] as const,
  },
  xirr: {
    all: ["xirr"] as const,
    portfolio: (portfolio: PortfolioType) => ["xirr", portfolio] as const,
  },
  cryptoTrades: {
    all: ["crypto-trades"] as const,
    list: () => ["crypto-trades", "list"] as const,
    taxSummary: (year: number) => ["crypto-trades", "tax-summary", year] as const,
  },
  purchaseSessions: {
    all: ["purchase-sessions"] as const,
    active: () => ["purchase-sessions", "active"] as const,
    list: (page: number) => ["purchase-sessions", "list", page] as const,
  },
  cashPositions: {
    all: ["cash-positions"] as const,
  },
  settings: {
    all: ["settings"] as const,
  },
  fxRates: {
    all: ["fx-rates"] as const,
  },
  /** `GET /market-data/prices` (cache-only; "Ceny z …", task 170). */
  prices: {
    all: ["prices"] as const,
  },
  macroState: {
    all: ["macro-state"] as const,
    history: ["macro-state", "history"] as const,
    benchmarkHistory: (symbol: string) => ["macro-state", "benchmark-history", symbol] as const,
  },
  linearRegression: {
    params: (params: object) => ["linear-regression", params] as const,
  },
  tradeChart: {
    tickers: () => ["trade-chart", "tickers"] as const,
    chart: (params: object) => ["trade-chart", "chart", params] as const,
  },
  news: {
    all: ["news"] as const,
    list: (date?: string) => ["news", "list", date ?? "LATEST"] as const,
    dates: () => ["news", "dates"] as const,
  },
  /** `GET /push/config` (Settings "Powiadomienia" card, ADR-108). */
  push: {
    config: ["push", "config"] as const,
    /** This browser's subscription state, derived on a Settings visit (no HTTP GET of its own). */
    device: ["push", "device"] as const,
  },
  bot: {
    calendar: (params?: object) => ["bot", "calendar", params ?? {}] as const,
    plans: (params?: object) => ["bot", "plans", params ?? {}] as const,
    planLatest: () => ["bot", "plan-latest"] as const,
    signals: (params?: object) => ["bot", "signals", params ?? {}] as const,
    trades: (params?: object) => ["bot", "trades", params ?? {}] as const,
    openPositions: () => ["bot", "open-positions"] as const,
    stats: () => ["bot", "stats"] as const,
    equity: () => ["bot", "equity"] as const,
    control: () => ["bot", "control"] as const,
  },
};
