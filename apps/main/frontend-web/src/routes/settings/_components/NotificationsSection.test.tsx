import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { PushState } from "../../../hooks/usePushSubscription.js";

const hook = vi.hoisted(() => ({
  state: { kind: "loading" } as PushState,
  enable: vi.fn(),
  disable: vi.fn(),
}));
const api = vi.hoisted(() => ({ sendTestPushNotification: vi.fn() }));
const toast = vi.hoisted(() => ({ success: vi.fn(), info: vi.fn(), error: vi.fn() }));

vi.mock("../../../hooks/usePushSubscription.js", () => ({ usePushSubscription: () => hook }));
vi.mock("../../../api/index.js", () => api);
vi.mock("sonner", () => ({ toast }));

import { NotificationsSection } from "./NotificationsSection.js";

const META = { timestamp: "2026-10-27T06:15:00Z" };
const IOS_HINT = "Na iPhonie działa tylko po dodaniu aplikacji do ekranu głównego.";

function renderSection(state: PushState) {
  hook.state = state;
  return render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { mutations: { retry: false } } })}>
      <NotificationsSection />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
});

describe("NotificationsSection", () => {
  it("renders nothing when push is not configured or not supported (hidden)", () => {
    const { container } = renderSection({ kind: "hidden" });
    expect(container.innerHTML).toBe("");
  });

  it("renders nothing while loading, so an unconfigured backend causes no flash", () => {
    const { container } = renderSection({ kind: "loading" });
    expect(container.innerHTML).toBe("");
  });

  it("off: offers Włącz and the iOS hint, and Włącz calls enable", () => {
    renderSection({ kind: "off" });
    expect(screen.getByRole("heading", { name: "Powiadomienia" })).toBeTruthy();
    expect(screen.getByText(IOS_HINT)).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Wyślij testowe powiadomienie" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Włącz na tym urządzeniu" }));
    expect(hook.enable).toHaveBeenCalledTimes(1);
  });

  it("on: offers Wyłącz and the test button, and Wyłącz calls disable", () => {
    renderSection({ kind: "on" });
    expect(screen.getByText(IOS_HINT)).toBeTruthy();
    expect(screen.getByRole("button", { name: "Wyślij testowe powiadomienie" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Wyłącz na tym urządzeniu" }));
    expect(hook.disable).toHaveBeenCalledTimes(1);
  });

  it("denied: shows the blocked hint and no buttons", () => {
    renderSection({ kind: "denied" });
    expect(screen.getByText("Powiadomienia są zablokowane w ustawieniach przeglądarki dla tej strony.")).toBeTruthy();
    expect(screen.queryByRole("button")).toBeNull();
  });

  it("busy: the only button is disabled", () => {
    renderSection({ kind: "busy" });
    const buttons = screen.getAllByRole("button");
    expect(buttons).toHaveLength(1);
    expect((buttons[0] as HTMLButtonElement).disabled).toBe(true);
  });

  it("the test button calls the API and shows the delivered/attempted counts", async () => {
    api.sendTestPushNotification.mockResolvedValue({
      data: { attempted: 2, delivered: 1, pruned: 1, failed: 0 },
      meta: META,
    });
    renderSection({ kind: "on" });
    fireEvent.click(screen.getByRole("button", { name: "Wyślij testowe powiadomienie" }));
    await waitFor(() => expect(toast.success).toHaveBeenCalledWith("Wysłano: 1/2"));
    expect(api.sendTestPushNotification).toHaveBeenCalledTimes(1);
  });

  it("the test button reports when no device is registered", async () => {
    api.sendTestPushNotification.mockResolvedValue({
      data: { attempted: 0, delivered: 0, pruned: 0, failed: 0 },
      meta: META,
    });
    renderSection({ kind: "on" });
    fireEvent.click(screen.getByRole("button", { name: "Wyślij testowe powiadomienie" }));
    await waitFor(() => expect(toast.info).toHaveBeenCalledWith("Brak zarejestrowanych urządzeń"));
    expect(toast.success).not.toHaveBeenCalled();
  });

  it("the test button shows an error toast when the request fails", async () => {
    api.sendTestPushNotification.mockRejectedValue(new Error("503"));
    renderSection({ kind: "on" });
    fireEvent.click(screen.getByRole("button", { name: "Wyślij testowe powiadomienie" }));
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith("Nie udało się wysłać testowego powiadomienia"));
  });
});
