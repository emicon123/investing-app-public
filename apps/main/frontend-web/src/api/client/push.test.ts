import { beforeEach, describe, expect, it, vi } from "vitest";

const http = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), delete: vi.fn() }));

vi.mock("@common/app-core", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@common/app-core")>()),
  apiClient: http,
}));

import { deletePushSubscription, getPushConfig, sendTestPushNotification, upsertPushSubscription } from "./push.js";

const meta = { requestId: "r-1", timestamp: "2026-10-27T08:00:00Z" };
const ENDPOINT = "https://fcm.googleapis.com/fcm/send/abc";

beforeEach(() => {
  http.get.mockReset();
  http.post.mockReset();
  http.delete.mockReset();
});

describe("push client (ADR-108 §7)", () => {
  it("getPushConfig reads GET /push/config", async () => {
    http.get.mockResolvedValue({ data: { data: { enabled: false }, meta } });
    const res = await getPushConfig();
    expect(http.get).toHaveBeenCalledWith("/push/config");
    expect(res.data.enabled).toBe(false);
  });

  it("upsertPushSubscription POSTs the endpoint and keys in the body", async () => {
    const body = { endpoint: ENDPOINT, keys: { p256dh: "p".repeat(87), auth: "a".repeat(22) } };
    http.post.mockResolvedValue({
      data: { data: { id: 3, deviceLabel: "Android · Chrome", createdAt: "2026-10-27T08:01:00Z", updatedAt: "2026-10-27T08:01:00Z" }, meta },
    });
    const res = await upsertPushSubscription(body);
    expect(http.post).toHaveBeenCalledWith("/push/subscriptions", body);
    expect(res.data.id).toBe(3);
  });

  it("deletePushSubscription sends the endpoint as a JSON body, never in the URL", async () => {
    http.delete.mockResolvedValue({ status: 204, data: "" });
    await deletePushSubscription(ENDPOINT);
    expect(http.delete).toHaveBeenCalledTimes(1);
    expect(http.delete).toHaveBeenCalledWith("/push/subscriptions", { data: { endpoint: ENDPOINT } });
    expect(String(http.delete.mock.calls[0]?.[0])).not.toContain("fcm");
  });

  it("sendTestPushNotification POSTs with no body and returns the counts", async () => {
    http.post.mockResolvedValue({ data: { data: { attempted: 1, delivered: 1, pruned: 0, failed: 0 }, meta } });
    const res = await sendTestPushNotification();
    expect(http.post).toHaveBeenCalledWith("/push/test-notifications");
    expect(res.data).toEqual({ attempted: 1, delivered: 1, pruned: 0, failed: 0 });
  });
});
