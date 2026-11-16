// Framework-free Facade over the browser Push API (ADR-108 §6/§8, design doc 172-web-push §3).
// The Settings card talks to `usePushSubscription`, which talks to this module: no component calls
// the Push API directly. Nothing here registers the worker at app boot — only `enable` and
// `restore` (on an explicit Settings visit) touch the registration.

import type { UpsertPushSubscriptionRequest } from "../api/schemas/index.js";

const SW_URL = "/sw.js";
const SW_SCOPE = "/";

export type BrowserSubscription = UpsertPushSubscriptionRequest;

export type EnableResult =
  | { status: "subscribed"; subscription: BrowserSubscription }
  | { status: "denied" }
  | { status: "dismissed" };

export interface RestoreResult {
  subscription: BrowserSubscription;
  /** Set when the VAPID key was rotated: the old endpoint the backend should forget. */
  replacedEndpoint?: string;
}

/** Decodes a base64url string (no padding, `-`/`_` alphabet) such as a VAPID public key. */
export function urlBase64ToUint8Array(base64Url: string): Uint8Array<ArrayBuffer> {
  const padding = "=".repeat((4 - (base64Url.length % 4)) % 4);
  const base64 = (base64Url + padding).replace(/-/g, "+").replace(/_/g, "/");
  const raw = atob(base64);
  const bytes = new Uint8Array(raw.length);
  for (let i = 0; i < raw.length; i++) bytes[i] = raw.charCodeAt(i);
  return bytes;
}

/** False on browsers without Web Push, e.g. iOS Safari outside an installed home-screen app. */
export function isPushSupported(): boolean {
  return "serviceWorker" in navigator && "PushManager" in window && "Notification" in window;
}

export function readPermission(): NotificationPermission {
  return Notification.permission;
}

/** This browser's subscription, without registering a worker when none is registered yet. */
export async function currentSubscription(): Promise<PushSubscription | null> {
  const registration = await navigator.serviceWorker.getRegistration(SW_SCOPE);
  if (!registration) return null;
  return registration.pushManager.getSubscription();
}

/**
 * True when the subscription was made with a different VAPID key (the pair was rotated,
 * ADR-108 §7). A browser that does not expose the key cannot tell, so it reports no mismatch
 * rather than churning the subscription on every visit.
 */
export function hasKeyMismatch(subscription: PushSubscription, vapidPublicKey: string): boolean {
  const current = subscription.options.applicationServerKey;
  if (!current) return false;
  return !bytesEqual(new Uint8Array(current), urlBase64ToUint8Array(vapidPublicKey));
}

/** Asks for permission, registers `/sw.js` (scope `/`) and subscribes this browser. */
export async function enable(vapidPublicKey: string): Promise<EnableResult> {
  const permission = await Notification.requestPermission();
  if (permission === "denied") return { status: "denied" };
  if (permission !== "granted") return { status: "dismissed" };

  await navigator.serviceWorker.register(SW_URL, { scope: SW_SCOPE });
  const registration = await navigator.serviceWorker.ready;
  const subscription = await subscribe(registration, vapidPublicKey);
  return { status: "subscribed", subscription };
}

/** Unsubscribes this browser; returns the endpoint for the backend DELETE, or null if none. */
export async function disable(): Promise<string | null> {
  const subscription = await currentSubscription();
  if (!subscription) return null;
  await subscription.unsubscribe();
  return subscription.endpoint;
}

/**
 * Mount-time check while On: returns the subscription to re-POST, resubscribing first when the
 * VAPID key no longer matches. Null when this browser has no subscription.
 */
export async function restore(vapidPublicKey: string): Promise<RestoreResult | null> {
  const registration = await navigator.serviceWorker.getRegistration(SW_SCOPE);
  if (!registration) return null;
  const existing = await registration.pushManager.getSubscription();
  if (!existing) return null;

  if (!hasKeyMismatch(existing, vapidPublicKey)) return { subscription: toRequest(existing) };

  await existing.unsubscribe();
  const subscription = await subscribe(registration, vapidPublicKey);
  return { subscription, replacedEndpoint: existing.endpoint };
}

async function subscribe(registration: ServiceWorkerRegistration, vapidPublicKey: string): Promise<BrowserSubscription> {
  const created = await registration.pushManager.subscribe({
    userVisibleOnly: true,
    applicationServerKey: urlBase64ToUint8Array(vapidPublicKey),
  });
  return toRequest(created);
}

function toRequest(subscription: PushSubscription): BrowserSubscription {
  const json = subscription.toJSON();
  const p256dh = json.keys?.["p256dh"];
  const auth = json.keys?.["auth"];
  if (!json.endpoint || !p256dh || !auth) {
    throw new Error("Przeglądarka zwróciła niepełną subskrypcję");
  }
  return { endpoint: json.endpoint, keys: { p256dh, auth } };
}

function bytesEqual(a: Uint8Array, b: Uint8Array): boolean {
  return a.length === b.length && a.every((value, i) => value === b[i]);
}
