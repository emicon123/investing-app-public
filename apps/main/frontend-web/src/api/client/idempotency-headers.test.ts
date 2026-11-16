import { beforeEach, describe, expect, it, vi } from "vitest";

const http = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), delete: vi.fn(), put: vi.fn() }));

vi.mock("@common/app-core", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@common/app-core")>()),
  apiClient: http,
}));

import type { IdempotencyOptions } from "@common/app-core";
import { createBinanceTransfer } from "./binance-transfers.js";
import { createCryptoTrade } from "./crypto-trades.js";
import { createDeposit } from "./deposits.js";
import { createRebalancingTransfer } from "./rebalancing-transfer.js";

// ADR-111: the four money-moving POSTs send `Idempotency-Key` only when the caller passes a key.
const meta = { requestId: "r-1", timestamp: "2026-11-05T08:00:00Z" };
const createdAt = "2026-11-05T08:00:00Z";

interface Case {
  name: string;
  path: string;
  body: unknown;
  response: unknown;
  run: (options?: IdempotencyOptions) => Promise<unknown>;
}

const rebalancingBody = {
  portfolio: "MAIN" as const,
  sold: { ticker: "AAA", accountType: "IKE" as const, units: 1, pricePln: 10 },
  buys: [{ ticker: "BBB", accountType: "IKE" as const, units: 1, pricePln: 10 }],
};
const depositBody = {
  depositDate: "2026-11-01",
  amount: 100,
  currency: "PLN" as const,
  exchange: "XTB" as const,
  accountType: "IKE" as const,
  portfolio: "MAIN" as const,
};
const tradeBody = {
  tradeDate: "2026-11-01",
  ticker: "BTC",
  exchange: "BINANCE" as const,
  accountType: "REGULAR" as const,
  side: "BUY" as const,
  quantity: 1,
  unitPrice: 1,
  priceCurrency: "USD" as const,
  totalAmount: 1,
  amountPln: 4,
};
const binanceBody = { transferDate: "2026-11-01", amountPln: 100 };

const calls: Case[] = [
  {
    name: "createRebalancingTransfer",
    path: "/rebalancing-transfers",
    body: rebalancingBody,
    response: { data: { affectedTickers: ["AAA", "BBB"] }, meta },
    run: (o) => createRebalancingTransfer(rebalancingBody, o),
  },
  {
    name: "createDeposit",
    path: "/deposits",
    body: depositBody,
    response: {
      data: {
        id: 1,
        depositDate: "2026-11-01",
        amount: 100,
        currency: "PLN",
        exchange: "XTB",
        accountType: "IKE",
        portfolio: "MAIN",
        createdAt,
      },
      meta,
    },
    run: (o) => createDeposit(depositBody, o),
  },
  {
    name: "createCryptoTrade",
    path: "/crypto-trades",
    body: tradeBody,
    response: {
      data: {
        id: 1,
        tradeDate: "2026-11-01",
        ticker: "BTC",
        exchange: "BINANCE",
        accountType: "REGULAR",
        side: "BUY",
        quantity: 1,
        unitPrice: 1,
        priceCurrency: "USD",
        totalAmount: 1,
        amountPln: 4,
        source: "MANUAL",
        createdAt,
      },
      meta,
    },
    run: (o) => createCryptoTrade(tradeBody, o),
  },
  {
    name: "createBinanceTransfer",
    path: "/binance-transfers",
    body: binanceBody,
    response: {
      data: { id: 1, transferDate: "2026-11-01", amountPln: 100, source: "MANUAL", createdAt },
      meta,
    },
    run: (o) => createBinanceTransfer(binanceBody, o),
  },
];

beforeEach(() => {
  http.post.mockReset();
});

describe.each(calls)("$name (ADR-111)", ({ path, body, response, run }) => {
  it("sends the Idempotency-Key header when a key is given", async () => {
    http.post.mockResolvedValue({ data: response });
    await run({ idempotencyKey: "key-123" });
    expect(http.post).toHaveBeenCalledWith(path, body, { headers: { "Idempotency-Key": "key-123" } });
  });

  it("sends no Idempotency-Key header without options", async () => {
    http.post.mockResolvedValue({ data: response });
    await run();
    expect(http.post).toHaveBeenCalledWith(path, body, { headers: {} });
  });
});
