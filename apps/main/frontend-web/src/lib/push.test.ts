import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  currentSubscription,
  disable,
  enable,
  hasKeyMismatch,
  isPushSupported,
  readPermission,
  restore,
  urlBase64ToUint8Array,
} from "./push.js";

// A VAPID public key is a 65-byte uncompressed P-256 point, base64url without padding.
const KEY_A = "BNbxGYNMhEIi9zrneh7mqV4oUanjLUK3m-mYZBc62frMKrEoMk88r3Lk596T0ck9xlT-ZKHIo2Vf9N8u3tlTeLs";
const KEY_B = "BIPUL12DLfytvTajnryr2PRdAgXS3HGKiLqndGcJGabyhHheJYlNGCeXl1dn18gSJ1WAkAPIxr4gK0_dQds4yiI";

function bufferOf(key: string): ArrayBuffer {
  return urlBase64ToUint8Array(key).buffer;
}

interface FakeSub {
  endpoint: string;
  options: { applicationServerKey: ArrayBuffer | null };
  toJSON: () => { endpoint?: string; keys?: Record<string, string> };
  unsubscribe: ReturnType<typeof vi.fn>;
}

function fakeSub(endpoint: string, key: string | null, keys: Record<string, string> | null = { p256dh: "P", auth: "A" }): FakeSub {
  return {
    endpoint,
    options: { applicationServerKey: key === null ? null : bufferOf(key) },
    toJSON: () => ({ endpoint, ...(keys ? { keys } : {}) }),
    unsubscribe: vi.fn(async () => true),
  };
}

interface FakeRegistration {
  pushManager: { getSubscription: ReturnType<typeof vi.fn>; subscribe: ReturnType<typeof vi.fn> };
}

function fakeRegistration(existing: FakeSub | null, next: FakeSub | null = null): FakeRegistration {
  return {
    pushManager: {
      getSubscription: vi.fn(async () => existing),
      subscribe: vi.fn(async () => next),
    },
  };
}

function installServiceWorker(opts: { registered?: FakeRegistration; registerResult?: FakeRegistration; ready?: FakeRegistration }) {
  const sw = {
    getRegistration: vi.fn(async () => opts.registered),
    register: vi.fn(async () => opts.registerResult),
    ready: Promise.resolve(opts.ready ?? opts.registerResult),
  };
  Object.defineProperty(navigator, "serviceWorker", { configurable: true, value: sw });
  return sw;
}

function installNotification(permission: NotificationPermission, requestResult: NotificationPermission = permission) {
  const requestPermission = vi.fn(async () => requestResult);
  vi.stubGlobal("Notification", Object.assign(function Notification() {}, { permission, requestPermission }));
  return requestPermission;
}

beforeEach(() => {
  vi.stubGlobal("PushManager", function PushManager() {});
});

afterEach(() => {
  Reflect.deleteProperty(navigator, "serviceWorker");
  vi.unstubAllGlobals();
});

describe("urlBase64ToUint8Array", () => {
  it("decodes base64url (no padding, - and _) to the 65 raw bytes of a VAPID key", () => {
    const bytes = urlBase64ToUint8Array(KEY_A);
    expect(bytes).toBeInstanceOf(Uint8Array);
    expect(bytes.length).toBe(65);
    expect(bytes[0]).toBe(0x04); // uncompressed point marker
  });

  it("handles the url-safe alphabet and every padding length", () => {
    expect(Array.from(urlBase64ToUint8Array("-_8"))).toEqual([0xfb, 0xff]); // 2 bytes, 1 pad
    expect(Array.from(urlBase64ToUint8Array("_-_-"))).toEqual([0xff, 0xef, 0xfe]); // 3 bytes, 0 pad
    expect(Array.from(urlBase64ToUint8Array("AA"))).toEqual([0]); // 1 byte, 2 pad
  });
});

describe("isPushSupported", () => {
  it("is true only with serviceWorker, PushManager and Notification", () => {
    installServiceWorker({});
    installNotification("default");
    expect(isPushSupported()).toBe(true);
  });

  it("is false without navigator.serviceWorker", () => {
    installNotification("default");
    expect(isPushSupported()).toBe(false);
  });

  it("is false without PushManager", () => {
    installServiceWorker({});
    installNotification("default");
    Reflect.deleteProperty(window, "PushManager");
    expect(isPushSupported()).toBe(false);
  });

  it("is false without Notification (e.g. iOS Safari outside an installed home-screen app)", () => {
    installServiceWorker({});
    Reflect.deleteProperty(window, "Notification");
    expect(isPushSupported()).toBe(false);
  });
});

describe("readPermission", () => {
  it("reports Notification.permission", () => {
    installNotification("denied");
    expect(readPermission()).toBe("denied");
  });
});

describe("currentSubscription", () => {
  it("does not register a worker: with none registered there is no subscription", async () => {
    const sw = installServiceWorker({ registered: undefined });
    expect(await currentSubscription()).toBeNull();
    expect(sw.register).not.toHaveBeenCalled();
    expect(sw.getRegistration).toHaveBeenCalledWith("/");
  });

  it("returns the push subscription of the registered worker", async () => {
    const sub = fakeSub("https://push.example/1", KEY_A);
    installServiceWorker({ registered: fakeRegistration(sub) });
    expect(await currentSubscription()).toBe(sub);
  });

  it("is null when the worker is registered but nothing is subscribed", async () => {
    installServiceWorker({ registered: fakeRegistration(null) });
    expect(await currentSubscription()).toBeNull();
  });
});

describe("hasKeyMismatch (VAPID key rotation, ADR-108 §7)", () => {
  it("is false when the subscription was made with the server's key", () => {
    expect(hasKeyMismatch(fakeSub("e", KEY_A) as unknown as PushSubscription, KEY_A)).toBe(false);
  });

  it("is true when the server key differs", () => {
    expect(hasKeyMismatch(fakeSub("e", KEY_A) as unknown as PushSubscription, KEY_B)).toBe(true);
  });

  it("cannot tell, so does not churn the subscription, when the browser exposes no key", () => {
    expect(hasKeyMismatch(fakeSub("e", null) as unknown as PushSubscription, KEY_B)).toBe(false);
  });
});

describe("enable", () => {
  it("asks for permission, registers /sw.js with scope /, subscribes with the VAPID key and returns the keys", async () => {
    const created = fakeSub("https://push.example/new", KEY_A, { p256dh: "PUB", auth: "SECRET" });
    const registration = fakeRegistration(null, created);
    const sw = installServiceWorker({ registerResult: registration, ready: registration });
    const requestPermission = installNotification("default", "granted");

    const result = await enable(KEY_A);

    expect(requestPermission).toHaveBeenCalledTimes(1);
    expect(sw.register).toHaveBeenCalledWith("/sw.js", { scope: "/" });
    expect(registration.pushManager.subscribe).toHaveBeenCalledTimes(1);
    const opts = registration.pushManager.subscribe.mock.calls[0]?.[0] as { userVisibleOnly: boolean; applicationServerKey: Uint8Array };
    expect(opts.userVisibleOnly).toBe(true);
    expect(Array.from(opts.applicationServerKey)).toEqual(Array.from(urlBase64ToUint8Array(KEY_A)));
    expect(result).toEqual({
      status: "subscribed",
      subscription: { endpoint: "https://push.example/new", keys: { p256dh: "PUB", auth: "SECRET" } },
    });
  });

  it("returns denied and registers nothing when the user refuses", async () => {
    const sw = installServiceWorker({});
    installNotification("default", "denied");
    expect(await enable(KEY_A)).toEqual({ status: "denied" });
    expect(sw.register).not.toHaveBeenCalled();
  });

  it("returns dismissed (not denied) when the prompt is closed without an answer", async () => {
    const sw = installServiceWorker({});
    installNotification("default", "default");
    expect(await enable(KEY_A)).toEqual({ status: "dismissed" });
    expect(sw.register).not.toHaveBeenCalled();
  });

  it("throws when the browser returns a subscription without keys", async () => {
    const registration = fakeRegistration(null, fakeSub("https://push.example/x", KEY_A, null));
    installServiceWorker({ registerResult: registration, ready: registration });
    installNotification("default", "granted");
    await expect(enable(KEY_A)).rejects.toThrow("Przeglądarka zwróciła niepełną subskrypcję");
  });

  it("propagates a subscribe failure (the hook turns it into the error toast)", async () => {
    const registration = fakeRegistration(null);
    registration.pushManager.subscribe.mockRejectedValue(new Error("push service unavailable"));
    installServiceWorker({ registerResult: registration, ready: registration });
    installNotification("default", "granted");
    await expect(enable(KEY_A)).rejects.toThrow("push service unavailable");
  });
});

describe("disable", () => {
  it("unsubscribes the browser and returns the endpoint for the DELETE call", async () => {
    const sub = fakeSub("https://push.example/1", KEY_A);
    installServiceWorker({ registered: fakeRegistration(sub) });
    expect(await disable()).toBe("https://push.example/1");
    expect(sub.unsubscribe).toHaveBeenCalledTimes(1);
  });

  it("returns null when there is nothing to unsubscribe", async () => {
    installServiceWorker({ registered: undefined });
    expect(await disable()).toBeNull();
  });
});

describe("restore (mount in the On state)", () => {
  it("returns null when this browser has no subscription", async () => {
    installServiceWorker({ registered: undefined });
    expect(await restore(KEY_A)).toBeNull();
  });

  it("returns the existing subscription untouched when the key matches", async () => {
    const sub = fakeSub("https://push.example/1", KEY_A, { p256dh: "PUB", auth: "SECRET" });
    const registration = fakeRegistration(sub);
    installServiceWorker({ registered: registration });
    const result = await restore(KEY_A);
    expect(result).toEqual({ subscription: { endpoint: "https://push.example/1", keys: { p256dh: "PUB", auth: "SECRET" } } });
    expect(sub.unsubscribe).not.toHaveBeenCalled();
    expect(registration.pushManager.subscribe).not.toHaveBeenCalled();
  });

  it("unsubscribes and subscribes again with the new key when the key was rotated, reporting the old endpoint", async () => {
    const old = fakeSub("https://push.example/old", KEY_A);
    const fresh = fakeSub("https://push.example/new", KEY_B, { p256dh: "NEWPUB", auth: "NEWAUTH" });
    const registration = fakeRegistration(old, fresh);
    installServiceWorker({ registered: registration, ready: registration });
    const result = await restore(KEY_B);
    expect(old.unsubscribe).toHaveBeenCalledTimes(1);
    expect(registration.pushManager.subscribe).toHaveBeenCalledTimes(1);
    expect(result).toEqual({
      subscription: { endpoint: "https://push.example/new", keys: { p256dh: "NEWPUB", auth: "NEWAUTH" } },
      replacedEndpoint: "https://push.example/old",
    });
  });
});
