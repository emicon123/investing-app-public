import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { act, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

const lib = vi.hoisted(() => ({
  isPushSupported: vi.fn(),
  readPermission: vi.fn(),
  restore: vi.fn(),
  enable: vi.fn(),
  disable: vi.fn(),
}));
const api = vi.hoisted(() => ({
  getPushConfig: vi.fn(),
  upsertPushSubscription: vi.fn(),
  deletePushSubscription: vi.fn(),
}));
const toast = vi.hoisted(() => ({ error: vi.fn(), success: vi.fn() }));

vi.mock("../lib/push.js", () => lib);
vi.mock("../api/index.js", () => api);
vi.mock("sonner", () => ({ toast }));

import { usePushSubscription } from "./usePushSubscription.js";

const KEY = "BNbxGYNMhEIi9zrneh7mqV4oUanjLUK3m-mYZBc62frMKrEoMk88r3Lk596T0ck9xlT-ZKHIo2Vf9N8u3tlTeLs";
const SUB = { endpoint: "https://fcm.googleapis.com/fcm/send/abc", keys: { p256dh: "P", auth: "A" } };
const META = { timestamp: "2026-10-27T06:15:00Z" };

function wrapper({ children }: { children: ReactNode }) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((r) => (resolve = r));
  return { promise, resolve };
}

beforeEach(() => {
  vi.clearAllMocks();
  lib.isPushSupported.mockReturnValue(true);
  lib.readPermission.mockReturnValue("default");
  lib.restore.mockResolvedValue(null);
  api.getPushConfig.mockResolvedValue({ data: { enabled: true, vapidPublicKey: KEY }, meta: META });
  api.upsertPushSubscription.mockResolvedValue({ data: {}, meta: META });
  api.deletePushSubscription.mockResolvedValue(undefined);
});

async function settled() {
  const hook = renderHook(() => usePushSubscription(), { wrapper });
  await waitFor(() => expect(hook.result.current.state.kind).not.toBe("loading"));
  return hook;
}

describe("usePushSubscription — Loading", () => {
  it("starts in loading", () => {
    const { result } = renderHook(() => usePushSubscription(), { wrapper });
    expect(result.current.state.kind).toBe("loading");
  });

  it("goes to hidden when the backend has no VAPID keys", async () => {
    api.getPushConfig.mockResolvedValue({ data: { enabled: false }, meta: META });
    const { result } = await settled();
    expect(result.current.state.kind).toBe("hidden");
    expect(lib.restore).not.toHaveBeenCalled();
  });

  it("goes to hidden, without asking the backend, when the browser lacks the Push API", async () => {
    lib.isPushSupported.mockReturnValue(false);
    const { result } = await settled();
    expect(result.current.state.kind).toBe("hidden");
    expect(api.getPushConfig).not.toHaveBeenCalled();
  });

  it("goes to denied when notifications are blocked for this site", async () => {
    lib.readPermission.mockReturnValue("denied");
    const { result } = await settled();
    expect(result.current.state.kind).toBe("denied");
    expect(lib.restore).not.toHaveBeenCalled();
  });

  it("goes to off when this browser has no subscription", async () => {
    const { result } = await settled();
    expect(result.current.state.kind).toBe("off");
    expect(lib.restore).toHaveBeenCalledWith(KEY);
    expect(api.upsertPushSubscription).not.toHaveBeenCalled();
  });

  it("goes to on and re-POSTs the existing subscription (restores a pruned row)", async () => {
    lib.restore.mockResolvedValue({ subscription: SUB });
    const { result } = await settled();
    expect(result.current.state.kind).toBe("on");
    expect(api.upsertPushSubscription).toHaveBeenCalledWith(SUB);
    expect(api.deletePushSubscription).not.toHaveBeenCalled();
  });

  it("after a key rotation re-POSTs the new subscription and forgets the replaced endpoint", async () => {
    lib.restore.mockResolvedValue({ subscription: SUB, replacedEndpoint: "https://fcm.googleapis.com/fcm/send/old" });
    const { result } = await settled();
    expect(result.current.state.kind).toBe("on");
    expect(api.upsertPushSubscription).toHaveBeenCalledWith(SUB);
    expect(api.deletePushSubscription).toHaveBeenCalledWith("https://fcm.googleapis.com/fcm/send/old");
  });

  it("stays on when the re-POST fails: the browser is still subscribed and the next visit retries", async () => {
    lib.restore.mockResolvedValue({ subscription: SUB });
    api.upsertPushSubscription.mockRejectedValue(new Error("network"));
    const { result } = await settled();
    expect(result.current.state.kind).toBe("on");
  });

  it("goes to off when resubscribing after a key rotation fails", async () => {
    lib.restore.mockRejectedValue(new Error("push service unavailable"));
    const { result } = await settled();
    expect(result.current.state.kind).toBe("off");
  });
});

describe("usePushSubscription — Włącz (Off → Busy → …)", () => {
  it("Off → Busy → On: subscribes the browser and POSTs the subscription", async () => {
    const pending = deferred<{ status: "subscribed"; subscription: typeof SUB }>();
    lib.enable.mockReturnValue(pending.promise);
    const { result } = await settled();

    act(() => result.current.enable());
    await waitFor(() => expect(result.current.state.kind).toBe("busy"));
    expect(lib.enable).toHaveBeenCalledWith(KEY);

    pending.resolve({ status: "subscribed", subscription: SUB });
    await waitFor(() => expect(result.current.state.kind).toBe("on"));
    expect(api.upsertPushSubscription).toHaveBeenCalledWith(SUB);
  });

  it("Busy → Denied when the user refuses the permission prompt", async () => {
    lib.enable.mockResolvedValue({ status: "denied" });
    const { result } = await settled();
    act(() => result.current.enable());
    await waitFor(() => expect(result.current.state.kind).toBe("denied"));
    expect(api.upsertPushSubscription).not.toHaveBeenCalled();
  });

  it("Busy → Off when the prompt is closed without an answer", async () => {
    lib.enable.mockResolvedValue({ status: "dismissed" });
    const { result } = await settled();
    act(() => result.current.enable());
    await waitFor(() => expect(lib.enable).toHaveBeenCalled());
    await waitFor(() => expect(result.current.state.kind).toBe("off"));
    expect(toast.error).not.toHaveBeenCalled();
  });

  it("Busy → Off with an error toast when subscribing fails", async () => {
    lib.enable.mockRejectedValue(new Error("push service unavailable"));
    const { result } = await settled();
    act(() => result.current.enable());
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith("Nie udało się włączyć powiadomień"));
    expect(result.current.state.kind).toBe("off");
  });

  it("Busy → Off with an error toast when the backend rejects the subscription", async () => {
    lib.enable.mockResolvedValue({ status: "subscribed", subscription: SUB });
    api.upsertPushSubscription.mockRejectedValue(new Error("422"));
    const { result } = await settled();
    act(() => result.current.enable());
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith("Nie udało się włączyć powiadomień"));
    expect(result.current.state.kind).toBe("off");
  });
});

describe("usePushSubscription — Wyłącz (On → Busy → Off)", () => {
  beforeEach(() => {
    lib.restore.mockResolvedValue({ subscription: SUB });
  });

  it("unsubscribes the browser and DELETEs its endpoint", async () => {
    const pending = deferred<string | null>();
    lib.disable.mockReturnValue(pending.promise);
    const { result } = await settled();
    expect(result.current.state.kind).toBe("on");

    act(() => result.current.disable());
    await waitFor(() => expect(result.current.state.kind).toBe("busy"));

    pending.resolve(SUB.endpoint);
    await waitFor(() => expect(result.current.state.kind).toBe("off"));
    expect(api.deletePushSubscription).toHaveBeenCalledWith(SUB.endpoint);
  });

  it("skips the DELETE when the browser had no subscription left", async () => {
    lib.disable.mockResolvedValue(null);
    const { result } = await settled();
    act(() => result.current.disable());
    await waitFor(() => expect(result.current.state.kind).toBe("off"));
    expect(api.deletePushSubscription).not.toHaveBeenCalled();
  });

  it("Busy → Off with an error toast when unsubscribing fails", async () => {
    lib.disable.mockRejectedValue(new Error("boom"));
    const { result } = await settled();
    act(() => result.current.disable());
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith("Nie udało się wyłączyć powiadomień"));
    expect(result.current.state.kind).toBe("off");
  });
});
