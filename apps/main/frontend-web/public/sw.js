/*
 * Web Push service worker (ADR-108 §6, design doc 172-web-push.md §9).
 *
 * Hand-written on purpose: Vite copies everything in public/ to dist/ unhashed, so this stays at
 * the fixed URL /sw.js that the browser re-checks for updates. It is not bundled.
 *
 * Exactly three listeners: install, push, notificationclick. There is deliberately NO request
 * interception: its root scope also covers /portfolio-b/ and /portfolio-c/ on the same origin, and a worker
 * without such a listener never sees their requests, so it cannot interfere with the sibling
 * apps. It caches nothing and never claims already-open pages.
 */
"use strict";

const FALLBACK_URL = "/news";
const FALLBACK_TITLE = "Inwestycje";
const FALLBACK_BODY = "Nowe powiadomienie";
const ICON = "/icons/icon-192.png";
const BADGE = "/icons/badge-96.png";

self.addEventListener("install", () => {
  self.skipWaiting();
});

self.addEventListener("push", (event) => {
  const payload = readPayload(event);
  const options = {
    body: stringOr(payload.body, FALLBACK_BODY),
    icon: ICON,
    badge: BADGE,
    data: { url: sameOriginPath(payload.url) },
  };
  const tag = stringOr(payload.tag, "");
  if (tag !== "") {
    // A same-tag notification replaces the earlier one. renotify without a tag makes
    // showNotification throw, so both are set together or not at all.
    options.tag = tag;
    options.renotify = true;
  }
  // Chrome requires a visible notification for every push, so a bad payload still shows one.
  event.waitUntil(self.registration.showNotification(stringOr(payload.title, FALLBACK_TITLE), options));
});

self.addEventListener("notificationclick", (event) => {
  event.notification.close();
  const data = event.notification.data;
  event.waitUntil(focusOrOpen(sameOriginPath(data && data.url)));
});

/** The decrypted payload object, or {} when it is missing, not JSON, or not an object. */
function readPayload(event) {
  try {
    const parsed = event.data ? event.data.json() : null;
    return parsed !== null && typeof parsed === "object" ? parsed : {};
  } catch (_err) {
    return {};
  }
}

function stringOr(value, fallback) {
  return typeof value === "string" && value !== "" ? value : fallback;
}

/**
 * The path (+ query + hash) of a same-origin relative url, else FALLBACK_URL. Rejects absolute
 * and protocol-relative urls ("//host", "/\host", "https://host"): resolving against our own
 * origin and comparing the result also catches parser tricks such as embedded tabs.
 */
function sameOriginPath(value) {
  if (typeof value !== "string" || !value.startsWith("/")) return FALLBACK_URL;
  try {
    const resolved = new URL(value, self.location.origin);
    if (resolved.origin !== self.location.origin) return FALLBACK_URL;
    return resolved.pathname + resolved.search + resolved.hash;
  } catch (_err) {
    return FALLBACK_URL;
  }
}

/** Focus a tab of this origin already on the path, else open one. */
async function focusOrOpen(path) {
  const wantedPath = new URL(path, self.location.origin).pathname;
  // includeUncontrolled: this worker never calls clients.claim(), so tabs opened before it
  // activated are not "controlled" yet and matchAll() would otherwise skip them.
  const tabs = await self.clients.matchAll({ type: "window", includeUncontrolled: true });
  const onPath = tabs.find((tab) => {
    const url = new URL(tab.url);
    return url.origin === self.location.origin && url.pathname === wantedPath;
  });
  return onPath ? onPath.focus() : self.clients.openWindow(path);
}
