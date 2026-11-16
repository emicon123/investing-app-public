import { beforeEach, describe, expect, it, vi } from "vitest";

const http = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), delete: vi.fn(), put: vi.fn() }));

vi.mock("@common/app-core", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@common/app-core")>()),
  apiClient: http,
}));

import type { IdempotencyOptions } from "@common/app-core";
import { createDeposit } from "./deposits.js";
import { confirmPurchases } from "./rebalancing.js";
import { executePositionTransfer } from "./transfers.js";

// ADR-111: the three money-moving POSTs send `Idempotency-Key` only when the caller passes a key.
const meta = { requestId: "r-1", timestamp: "2026-11-05T08:00:00Z" };
const createdAt = "2026-11-05T08:00:00Z";

const transferBody = { sold: { ticker: "AAA", accountType: "IKE_TATA" as const, units: 1, pricePln: 10 }, buys: [{ ticker: "BBB", accountType: "IKE_TATA" as const, units: 1, pricePln: 10 }] };
const depositBody = { depositDate: "2026-11-01", amount: 100 };
const confirmBody: Parameters<typeof confirmPurchases>[0] = { items: [{ ticker: "AAA", accountAllocations: [{ accountType: "IKE_TATA", units: 1 }] }] };

interface Case {
  name: string;
  path: string;
  body: unknown;
  response: unknown;
  run: (options?: IdempotencyOptions) => Promise<unknown>;
}

const calls: Case[] = [
  {
    name: "executePositionTransfer",
    path: "/position-transfers",
    body: transferBody,
    response: { data: { transferId: "t-1", sold: { ticker: "AAA", accountType: "IKE_TATA", units: 1, pricePln: 10 }, buys: [{ ticker: "BBB", accountType: "IKE_TATA", units: 1, pricePln: 10 }] }, meta },
    run: (o) => executePositionTransfer(transferBody, o),
  },
  {
    name: "createDeposit",
    path: "/deposits",
    body: depositBody,
    response: { data: { id: 1, depositDate: "2026-11-01", amount: 100, createdAt }, meta },
    run: (o) => createDeposit(depositBody, o),
  },
  {
    name: "confirmPurchases",
    path: "/purchases/confirm",
    body: confirmBody,
    response: {},
    run: (o) => confirmPurchases(confirmBody, o),
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
