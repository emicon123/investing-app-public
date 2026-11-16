import { describe, expect, it } from "vitest";
import {
  pushConfigResponseSchema,
  pushDeliveryReportResponseSchema,
  pushSubscriptionResponseSchema,
} from "./push.js";

// Real-shaped bodies from docs/openapi/paths/push.yaml (ADR-108 §7).
const meta = { requestId: "550e8400-e29b-41d4-a716-446655440080", timestamp: "2026-10-27T08:00:00Z" };

describe("pushConfigResponseSchema (GET /push/config)", () => {
  it("parses the enabled form with the VAPID public key", () => {
    const parsed = pushConfigResponseSchema.parse({
      data: { enabled: true, vapidPublicKey: "BNbxGYNMhEIi9zrneh7mqV4oUanjLUK3m-mYZBc62frMKrEoMk88r3Lk596T0ck9xlT-ZKHIo2Vf9N8u3tlTeLs" },
      meta,
    });
    expect(parsed.data.enabled).toBe(true);
    expect(parsed.data.vapidPublicKey).toMatch(/^BNbx/);
  });

  it("parses the disabled form, which omits the key", () => {
    const parsed = pushConfigResponseSchema.parse({ data: { enabled: false }, meta });
    expect(parsed.data.enabled).toBe(false);
    expect(parsed.data.vapidPublicKey).toBeUndefined();
  });

  it("rejects a body without `enabled`", () => {
    expect(pushConfigResponseSchema.safeParse({ data: {}, meta }).success).toBe(false);
  });
});

describe("pushSubscriptionResponseSchema (POST /push/subscriptions)", () => {
  it("parses the stored subscription and treats lastSuccessAt as optional", () => {
    const parsed = pushSubscriptionResponseSchema.parse({
      data: { id: 3, deviceLabel: "Android · Chrome", createdAt: "2026-10-27T08:01:00Z", updatedAt: "2026-10-27T08:01:00Z" },
      meta,
    });
    expect(parsed.data.id).toBe(3);
    expect(parsed.data.deviceLabel).toBe("Android · Chrome");
    expect(parsed.data.lastSuccessAt).toBeUndefined();
  });

  it("keeps lastSuccessAt when present", () => {
    const parsed = pushSubscriptionResponseSchema.parse({
      data: {
        id: 3,
        deviceLabel: "Android · Chrome",
        createdAt: "2026-10-27T08:01:00Z",
        updatedAt: "2026-10-27T08:01:00Z",
        lastSuccessAt: "2026-10-28T06:15:02Z",
      },
      meta,
    });
    expect(parsed.data.lastSuccessAt).toBe("2026-10-28T06:15:02Z");
  });

  it("never exposes endpoint or keys: they are not part of the response schema", () => {
    const parsed = pushSubscriptionResponseSchema.parse({
      data: {
        id: 3,
        deviceLabel: "Android · Chrome",
        createdAt: "2026-10-27T08:01:00Z",
        updatedAt: "2026-10-27T08:01:00Z",
        endpoint: "https://fcm.googleapis.com/fcm/send/secret",
      },
      meta,
    });
    expect(Object.keys(parsed.data)).not.toContain("endpoint");
  });
});

describe("pushDeliveryReportResponseSchema (POST /push/test-notifications)", () => {
  it("parses the four counts", () => {
    const parsed = pushDeliveryReportResponseSchema.parse({
      data: { attempted: 2, delivered: 1, pruned: 1, failed: 0 },
      meta,
    });
    expect(parsed.data).toEqual({ attempted: 2, delivered: 1, pruned: 1, failed: 0 });
  });

  it("rejects a negative or missing count", () => {
    expect(pushDeliveryReportResponseSchema.safeParse({ data: { attempted: -1, delivered: 0, pruned: 0, failed: 0 }, meta }).success).toBe(false);
    expect(pushDeliveryReportResponseSchema.safeParse({ data: { attempted: 1, delivered: 1, pruned: 0 }, meta }).success).toBe(false);
  });
});
