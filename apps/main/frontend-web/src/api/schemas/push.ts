import { z } from "zod";
import { apiResponseSchema } from "@common/app-core";

// Web Push (ADR-108 §7, docs/openapi/components/schemas/push.yaml). The subscription's endpoint
// and keys are a capability: the response DTOs never echo them back.

export const pushConfigSchema = z.object({
  enabled: z.boolean(),
  /** VAPID application server public key (base64url). Omitted when `enabled` is false. */
  vapidPublicKey: z.string().optional(),
});

export const pushSubscriptionKeysSchema = z.object({
  p256dh: z.string(),
  auth: z.string(),
});

export const upsertPushSubscriptionRequestSchema = z.object({
  endpoint: z.string(),
  keys: pushSubscriptionKeysSchema,
});

export const pushSubscriptionInfoSchema = z.object({
  id: z.number().int(),
  deviceLabel: z.string(),
  createdAt: z.string().datetime({ offset: true }),
  updatedAt: z.string().datetime({ offset: true }),
  lastSuccessAt: z.string().datetime({ offset: true }).optional(),
});

/** attempted = delivered + pruned + failed. A push-service failure is a count, never an HTTP error. */
export const pushDeliveryReportSchema = z.object({
  attempted: z.number().int().min(0),
  delivered: z.number().int().min(0),
  pruned: z.number().int().min(0),
  failed: z.number().int().min(0),
});

export const pushConfigResponseSchema = apiResponseSchema(pushConfigSchema);
export const pushSubscriptionResponseSchema = apiResponseSchema(pushSubscriptionInfoSchema);
export const pushDeliveryReportResponseSchema = apiResponseSchema(pushDeliveryReportSchema);

export type PushConfig = z.infer<typeof pushConfigSchema>;
export type UpsertPushSubscriptionRequest = z.infer<typeof upsertPushSubscriptionRequestSchema>;
export type PushSubscriptionInfo = z.infer<typeof pushSubscriptionInfoSchema>;
export type PushDeliveryReport = z.infer<typeof pushDeliveryReportSchema>;
export type PushConfigResponse = z.infer<typeof pushConfigResponseSchema>;
export type PushSubscriptionResponse = z.infer<typeof pushSubscriptionResponseSchema>;
export type PushDeliveryReportResponse = z.infer<typeof pushDeliveryReportResponseSchema>;
