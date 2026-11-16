import { z } from "zod";
import { apiResponseSchema, pagedResponseSchema } from "@common/app-core";

// Use .nullish() below, not .nullable() — backend omits null fields entirely
// (Jackson non_absent), and .nullable() alone rejects a missing key.

export const botCalendarEventSchema = z.object({
  id: z.number(),
  eventTime: z.string(),
  currency: z.string(),
  country: z.string().nullish(),
  eventName: z.string(),
  impact: z.string(),
  forecast: z.string().nullish(),
  previous: z.string().nullish(),
  actual: z.string().nullish(),
  source: z.string(),
  actualSource: z.string().nullish(),
  externalId: z.string(),
  createdAt: z.string(),
});

export const botPlanScenarioSchema = z.object({
  id: z.number(),
  calendarEventId: z.number().nullish(),
  eventName: z.string(),
  eventTime: z.string(),
  scenarioLabel: z.string(),
  triggerCondition: z.string(),
  action: z.string(),
  leverage: z.number().nullish(),
  surpriseMin: z.number().nullish(),
  surpriseMax: z.number().nullish(),
  confidence: z.number().nullish(),
  reasoning: z.string(),
});

export const botDailyPlanSchema = z.object({
  id: z.number(),
  planDate: z.string(),
  status: z.string(),
  summary: z.string().nullish(),
  marketContext: z.record(z.string(), z.unknown()).nullish(),
  model: z.string().nullish(),
  scenarios: z.array(botPlanScenarioSchema),
});

export const botSignalSchema = z.object({
  id: z.number(),
  calendarEventId: z.number().nullish(),
  planScenarioId: z.number().nullish(),
  eventName: z.string(),
  actual: z.string(),
  forecast: z.string().nullish(),
  previous: z.string().nullish(),
  surpriseDesc: z.string().nullish(),
  decision: z.string(),
  executed: z.boolean(),
  rawPayload: z.record(z.string(), z.unknown()).nullish(),
  createdAt: z.string(),
});

export const botTradeSchema = z.object({
  id: z.number(),
  signalId: z.number().nullish(),
  calendarEventId: z.number().nullish(),
  side: z.string(),
  status: z.string(),
  quantity: z.number(),
  leverage: z.number(),
  entryPrice: z.number(),
  exitPrice: z.number().nullish(),
  pnlPln: z.number().nullish(),
  pnlPct: z.number().nullish(),
  okxOrderId: z.string().nullish(),
  okxAlgoId: z.string().nullish(),
  marginAllocatedPln: z.number().nullish(),
  sizingConfidence: z.number().nullish(),
  notes: z.string().nullish(),
  openedAt: z.string(),
  closedAt: z.string().nullish(),
  createdAt: z.string(),
  currentPrice: z.number().nullish(),
  unrealizedPnlPln: z.number().nullish(),
  unrealizedPnlPct: z.number().nullish(),
  heldSeconds: z.number().nullish(),
  timeToHardCloseSeconds: z.number().nullish(),
});

export const botTradeActionSchema = z.object({
  id: z.number(),
  tradeId: z.number(),
  actionType: z.string(),
  price: z.number().nullish(),
  quantity: z.number().nullish(),
  reasoning: z.string().nullish(),
  source: z.string(),
  createdAt: z.string(),
});

export const botTradeTickSchema = z.object({
  id: z.number(),
  tradeId: z.number(),
  price: z.number(),
  volume: z.number().nullish(),
  cvd: z.number().nullish(),
  atr1m: z.number().nullish(),
  obImbalance: z.number().nullish(),
  fundingRate: z.number().nullish(),
  createdAt: z.string(),
});

export const botStatsSchema = z.object({
  totalTrades: z.number(),
  openTrades: z.number(),
  closedTrades: z.number(),
  winRate: z.number().nullish(),
  totalPnlPln: z.number(),
  totalPnlPct: z.number().nullish(),
  avgHoldMinutes: z.number().nullish(),
  bestTrade: botTradeSchema.nullish(),
  worstTrade: botTradeSchema.nullish(),
});

export const botEquityPointSchema = z.object({
  date: z.string(),
  cumulativePnlPln: z.number(),
  dailyPnlPln: z.number(),
});

export const botControlSchema = z.object({
  tradingEnabled: z.boolean(),
  pausedReason: z.string().nullish(),
  updatedAt: z.string(),
});

export const botCalendarListResponseSchema = pagedResponseSchema(botCalendarEventSchema);
export const botPlanResponseSchema = apiResponseSchema(botDailyPlanSchema);
export const botPlanListResponseSchema = pagedResponseSchema(botDailyPlanSchema);
export const botSignalListResponseSchema = pagedResponseSchema(botSignalSchema);
export const botTradeListResponseSchema = pagedResponseSchema(botTradeSchema);
export const botTradeResponseSchema = apiResponseSchema(botTradeSchema);
export const botTradeActionListResponseSchema = apiResponseSchema(z.array(botTradeActionSchema));
export const botTradeTickListResponseSchema = apiResponseSchema(z.array(botTradeTickSchema));
export const botStatsResponseSchema = apiResponseSchema(botStatsSchema);
export const botEquityListResponseSchema = apiResponseSchema(z.array(botEquityPointSchema));
export const botControlResponseSchema = apiResponseSchema(botControlSchema);

export type BotCalendarEvent = z.infer<typeof botCalendarEventSchema>;
export type BotPlanScenario = z.infer<typeof botPlanScenarioSchema>;
export type BotDailyPlan = z.infer<typeof botDailyPlanSchema>;
export type BotSignal = z.infer<typeof botSignalSchema>;
export type BotTrade = z.infer<typeof botTradeSchema>;
export type BotTradeAction = z.infer<typeof botTradeActionSchema>;
export type BotTradeTick = z.infer<typeof botTradeTickSchema>;
export type BotStats = z.infer<typeof botStatsSchema>;
export type BotEquityPoint = z.infer<typeof botEquityPointSchema>;
export type BotControl = z.infer<typeof botControlSchema>;
export type BotCalendarListResponse = z.infer<typeof botCalendarListResponseSchema>;
export type BotPlanResponse = z.infer<typeof botPlanResponseSchema>;
export type BotPlanListResponse = z.infer<typeof botPlanListResponseSchema>;
export type BotSignalListResponse = z.infer<typeof botSignalListResponseSchema>;
export type BotTradeListResponse = z.infer<typeof botTradeListResponseSchema>;
export type BotTradeResponse = z.infer<typeof botTradeResponseSchema>;
export type BotTradeActionListResponse = z.infer<typeof botTradeActionListResponseSchema>;
export type BotTradeTickListResponse = z.infer<typeof botTradeTickListResponseSchema>;
export type BotStatsResponse = z.infer<typeof botStatsResponseSchema>;
export type BotEquityListResponse = z.infer<typeof botEquityListResponseSchema>;
export type BotControlResponse = z.infer<typeof botControlResponseSchema>;
