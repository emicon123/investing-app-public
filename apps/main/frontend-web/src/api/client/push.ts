import { apiClient } from "@common/app-core";
import {
  pushConfigResponseSchema,
  pushDeliveryReportResponseSchema,
  pushSubscriptionResponseSchema,
  type PushConfigResponse,
  type PushDeliveryReportResponse,
  type PushSubscriptionResponse,
  type UpsertPushSubscriptionRequest,
} from "../schemas/index.js";

/** Never 503: an unconfigured backend answers `{enabled: false}` (ADR-108 §7). */
export async function getPushConfig(): Promise<PushConfigResponse> {
  const r = await apiClient.get("/push/config");
  return pushConfigResponseSchema.parse(r.data);
}

/** Idempotent upsert on the endpoint; the owner and device label are set by the server. */
export async function upsertPushSubscription(
  body: UpsertPushSubscriptionRequest,
): Promise<PushSubscriptionResponse> {
  const r = await apiClient.post("/push/subscriptions", body);
  return pushSubscriptionResponseSchema.parse(r.data);
}

/**
 * The endpoint is a capability URL, so it travels in the JSON body: URLs end up in nginx access
 * logs (ADR-108 §7). 204, idempotent.
 */
export async function deletePushSubscription(endpoint: string): Promise<void> {
  await apiClient.delete("/push/subscriptions", { data: { endpoint } });
}

/** Sends to the caller's own devices only; 200 with all-zero counts when there are none. */
export async function sendTestPushNotification(): Promise<PushDeliveryReportResponse> {
  const r = await apiClient.post("/push/test-notifications");
  return pushDeliveryReportResponseSchema.parse(r.data);
}
