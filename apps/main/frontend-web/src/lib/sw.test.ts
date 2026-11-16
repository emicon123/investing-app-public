import { describe, expect, it, vi } from "vitest";
// The worker is a hand-written classic script that Vite copies unhashed to dist/ (ADR-108 §6), so
// the test loads its source and runs it against a stubbed `self` (design doc §9, handoff step F2).
import swSource from "../../public/sw.js?raw";

type Listener = (event: unknown) => unknown;

interface FakeClient {
  url: string;
  focus: ReturnType<typeof vi.fn>;
}

const ORIGIN = "https://app.example.ts.net";

function loadWorker(windowClients: FakeClient[] = []) {
  const listeners = new Map<string, Listener>();
  const showNotification = vi.fn(async () => undefined);
  const matchAll = vi.fn(async () => windowClients);
  const openWindow = vi.fn(async () => undefined);
  const claim = vi.fn(async () => undefined);
  const skipWaiting = vi.fn();
  const addEventListener = vi.fn((type: string, listener: Listener) => {
    listeners.set(type, listener);
  });
  const self = {
    addEventListener,
    skipWaiting,
    registration: { showNotification },
    clients: { matchAll, openWindow, claim },
    location: { origin: ORIGIN },
  };
  new Function("self", "URL", swSource)(self, URL);
  return { listeners, addEventListener, showNotification, matchAll, openWindow, claim, skipWaiting };
}

function pushEvent(payload: unknown, opts: { invalidJson?: boolean; noData?: boolean } = {}) {
  const waitUntil = vi.fn();
  const data = opts.noData
    ? null
    : {
        json: () => {
          if (opts.invalidJson) throw new SyntaxError("Unexpected token");
          return payload;
        },
      };
  return { event: { data, waitUntil }, waitUntil };
}

function clickEvent(data: unknown) {
  const close = vi.fn();
  const waitUntil = vi.fn();
  return { event: { notification: { close, data }, waitUntil }, close, waitUntil };
}

async function settle(waitUntil: ReturnType<typeof vi.fn>): Promise<void> {
  await Promise.all(waitUntil.mock.calls.map((c) => c[0] as Promise<unknown>));
}

describe("sw.js listeners (ADR-108 §6, design doc §9)", () => {
  it("registers exactly install, push and notificationclick, and NO fetch", () => {
    const w = loadWorker();
    const types = w.addEventListener.mock.calls.map((c) => c[0]).sort();
    expect(types).toEqual(["install", "notificationclick", "push"]);
    expect(w.listeners.has("fetch")).toBe(false);
  });

  it("does not take over open pages: no clients.claim()", () => {
    const w = loadWorker();
    expect(w.claim).not.toHaveBeenCalled();
  });

  it("install calls skipWaiting", () => {
    const w = loadWorker();
    w.listeners.get("install")?.({});
    expect(w.skipWaiting).toHaveBeenCalledTimes(1);
  });
});

describe("push handler", () => {
  it("shows title, body, tag, renotify, icon, badge and data.url from the payload", async () => {
    const w = loadWorker();
    const { event, waitUntil } = pushEvent({
      title: "Raport z wiadomościami gotowy",
      body: "Nowe wiadomości: 5 (2026-10-27)",
      url: "/news",
      tag: "news-report",
    });
    w.listeners.get("push")?.(event);
    await settle(waitUntil);
    expect(w.showNotification).toHaveBeenCalledWith("Raport z wiadomościami gotowy", {
      body: "Nowe wiadomości: 5 (2026-10-27)",
      tag: "news-report",
      renotify: true,
      icon: "/icons/icon-192.png",
      badge: "/icons/badge-96.png",
      data: { url: "/news" },
    });
    expect(waitUntil).toHaveBeenCalledTimes(1);
  });

  it.each([
    ["invalid JSON", { invalidJson: true }],
    ["no data", { noData: true }],
  ])("falls back to a generic notification on %s (Chrome needs a visible one)", async (_name, opts) => {
    const w = loadWorker();
    const { event, waitUntil } = pushEvent(null, opts);
    w.listeners.get("push")?.(event);
    await settle(waitUntil);
    expect(w.showNotification).toHaveBeenCalledTimes(1);
    const [title, options] = w.showNotification.mock.calls[0] as unknown as [string, Record<string, unknown>];
    expect(title).toBe("Inwestycje");
    expect(options).toMatchObject({ body: "Nowe powiadomienie", data: { url: "/news" } });
    // renotify without a tag makes showNotification throw, so the fallback must not set it.
    expect(options).not.toHaveProperty("renotify");
  });

  it("falls back to the generic title/body/url when the payload fields are not strings", async () => {
    const w = loadWorker();
    const { event, waitUntil } = pushEvent({ title: 7, body: null, url: { x: 1 }, tag: [] });
    w.listeners.get("push")?.(event);
    await settle(waitUntil);
    const [title, options] = w.showNotification.mock.calls[0] as unknown as [string, Record<string, unknown>];
    expect(title).toBe("Inwestycje");
    expect(options).toMatchObject({ body: "Nowe powiadomienie", data: { url: "/news" } });
    expect(options).not.toHaveProperty("tag");
    expect(options).not.toHaveProperty("renotify");
  });

  it("does not trust a payload url that leaves the origin", async () => {
    const w = loadWorker();
    const { event, waitUntil } = pushEvent({ title: "t", body: "b", url: "//evil.example/x", tag: "x" });
    w.listeners.get("push")?.(event);
    await settle(waitUntil);
    const [, options] = w.showNotification.mock.calls[0] as unknown as [string, { data: { url: string } }];
    expect(options.data.url).toBe("/news");
  });
});

describe("notificationclick handler", () => {
  async function click(data: unknown, clients: FakeClient[] = []) {
    const w = loadWorker(clients);
    const { event, close, waitUntil } = clickEvent(data);
    w.listeners.get("notificationclick")?.(event);
    await settle(waitUntil);
    return { w, close, waitUntil };
  }

  it("closes the notification and opens data.url when no tab is on it", async () => {
    const other: FakeClient = { url: `${ORIGIN}/portfolio`, focus: vi.fn() };
    const { w, close } = await click({ url: "/news" }, [other]);
    expect(close).toHaveBeenCalledTimes(1);
    expect(w.openWindow).toHaveBeenCalledWith("/news");
    expect(other.focus).not.toHaveBeenCalled();
  });

  it("looks at uncontrolled tabs too (the worker never calls clients.claim)", async () => {
    const { w } = await click({ url: "/news" });
    expect(w.matchAll).toHaveBeenCalledWith({ type: "window", includeUncontrolled: true });
  });

  it("focuses a tab of this origin that is already on the path instead of opening one", async () => {
    const onNews: FakeClient = { url: `${ORIGIN}/news?d=2026-10-27`, focus: vi.fn(async () => undefined) };
    const { w } = await click({ url: "/news" }, [{ url: `${ORIGIN}/portfolio`, focus: vi.fn() }, onNews]);
    expect(onNews.focus).toHaveBeenCalledTimes(1);
    expect(w.openWindow).not.toHaveBeenCalled();
  });

  it("ignores a tab on the same path but another origin", async () => {
    const foreign: FakeClient = { url: "https://evil.example/news", focus: vi.fn() };
    const { w } = await click({ url: "/news" }, [foreign]);
    expect(foreign.focus).not.toHaveBeenCalled();
    expect(w.openWindow).toHaveBeenCalledWith("/news");
  });

  it.each([
    ["a protocol-relative url", "//evil.example/steal"],
    ["an absolute https url", "https://evil.example/steal"],
    ["an absolute http url", "http://evil.example"],
    ["a javascript: url", "javascript:alert(1)"],
    ["a backslash host trick", "/\\evil.example"],
    ["a tab-smuggled host", "/\t/evil.example"],
    ["a relative path without a leading slash", "news"],
    ["a non-string", 42],
    ["an empty string", ""],
  ])("falls back to /news for %s", async (_name, url) => {
    const { w } = await click({ url });
    expect(w.openWindow).toHaveBeenCalledTimes(1);
    expect(w.openWindow).toHaveBeenCalledWith("/news");
  });

  it.each([["missing data", undefined], ["null data", null]])("falls back to /news for %s", async (_name, data) => {
    const { w } = await click(data);
    expect(w.openWindow).toHaveBeenCalledWith("/news");
  });

  it("keeps a same-origin deep link, query string included", async () => {
    const { w } = await click({ url: "/portfolio?x=1" });
    expect(w.openWindow).toHaveBeenCalledWith("/portfolio?x=1");
  });
});
