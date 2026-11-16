import { apiClient } from "@common/app-core";
import {
  botCalendarListResponseSchema,
  botPlanResponseSchema,
  botPlanListResponseSchema,
  botSignalListResponseSchema,
  botTradeListResponseSchema,
  botTradeResponseSchema,
  botTradeActionListResponseSchema,
  botTradeTickListResponseSchema,
  botStatsResponseSchema,
  botEquityListResponseSchema,
  botControlResponseSchema,
  type BotCalendarListResponse,
  type BotPlanResponse,
  type BotPlanListResponse,
  type BotSignalListResponse,
  type BotTradeListResponse,
  type BotTradeResponse,
  type BotTradeActionListResponse,
  type BotTradeTickListResponse,
  type BotStatsResponse,
  type BotEquityListResponse,
  type BotControlResponse,
} from "../schemas/bot.js";

export async function listBotCalendar(params?: { from?: string; to?: string; impact?: string; currency?: string; page?: number; size?: number }): Promise<BotCalendarListResponse> {
  const r = await apiClient.get("/bot/calendar", { params });
  return botCalendarListResponseSchema.parse(r.data);
}

export async function listBotPlans(params?: { page?: number; size?: number }): Promise<BotPlanListResponse> {
  const r = await apiClient.get("/bot/plans", { params });
  return botPlanListResponseSchema.parse(r.data);
}

export async function getBotPlanLatest(): Promise<BotPlanResponse> {
  const r = await apiClient.get("/bot/plans/latest");
  return botPlanResponseSchema.parse(r.data);
}

export async function getBotPlanByDate(date: string): Promise<BotPlanResponse> {
  const r = await apiClient.get(`/bot/plans/${date}`);
  return botPlanResponseSchema.parse(r.data);
}

export async function listBotSignals(params?: { eventName?: string; page?: number; size?: number }): Promise<BotSignalListResponse> {
  const r = await apiClient.get("/bot/signals", { params });
  return botSignalListResponseSchema.parse(r.data);
}

export async function listBotTrades(params?: { status?: string; side?: string; page?: number; size?: number }): Promise<BotTradeListResponse> {
  const r = await apiClient.get("/bot/trades", { params });
  return botTradeListResponseSchema.parse(r.data);
}

export async function getBotTrade(id: number): Promise<BotTradeResponse> {
  const r = await apiClient.get(`/bot/trades/${id}`);
  return botTradeResponseSchema.parse(r.data);
}

export async function listBotTradeActions(tradeId: number): Promise<BotTradeActionListResponse> {
  const r = await apiClient.get(`/bot/trades/${tradeId}/actions`);
  return botTradeActionListResponseSchema.parse(r.data);
}

export async function listBotTradeTicks(tradeId: number, params?: { from?: string; to?: string }): Promise<BotTradeTickListResponse> {
  const r = await apiClient.get(`/bot/trades/${tradeId}/ticks`, { params });
  return botTradeTickListResponseSchema.parse(r.data);
}

export async function getBotStats(): Promise<BotStatsResponse> {
  const r = await apiClient.get("/bot/stats");
  return botStatsResponseSchema.parse(r.data);
}

export async function getBotEquity(): Promise<BotEquityListResponse> {
  const r = await apiClient.get("/bot/equity");
  return botEquityListResponseSchema.parse(r.data);
}

export async function getBotOpenPositions(params?: { page?: number; size?: number }): Promise<BotTradeListResponse> {
  const r = await apiClient.get("/bot/positions/open", { params });
  const raw = r.data;
  if (raw.data && Array.isArray(raw.data)) {
    return botTradeListResponseSchema.parse({ data: raw.data, meta: raw.meta, page: { number: 0, size: raw.data.length, total: raw.data.length } });
  }
  return botTradeListResponseSchema.parse(raw);
}

export async function getBotControl(): Promise<BotControlResponse> {
  const r = await apiClient.get("/bot/control");
  return botControlResponseSchema.parse(r.data);
}

export async function updateBotControl(body: { tradingEnabled: boolean; pausedReason?: string }): Promise<BotControlResponse> {
  const r = await apiClient.patch("/bot/control", body);
  return botControlResponseSchema.parse(r.data);
}
