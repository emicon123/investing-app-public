import { beforeEach, describe, expect, it, vi } from "vitest";
import { act, fireEvent, render } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { AccountCash } from "../api/index.js";

const api = vi.hoisted(() => ({ getAccountCash: vi.fn(), saveAccountCash: vi.fn() }));
vi.mock("../api/index.js", () => api);

import { AccountCashCard } from "./AccountCashCard.js";

const STORED: AccountCash = {
  ikeTata: 100.5,
  ikeMama: 0,
  ikzeTata: 7,
  ikzeMama: 0,
  updatedAt: "2026-11-01T10:00:00Z",
  updateReason: "MANUAL",
};

const field = (key: string) => document.getElementById(`cash-${key}`) as HTMLInputElement;

function renderCard() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const ui = (
    <QueryClientProvider client={client}>
      <AccountCashCard />
    </QueryClientProvider>
  );
  const view = render(ui);
  return { client, rerender: () => view.rerender(ui) };
}

beforeEach(() => {
  vi.clearAllMocks();
  api.getAccountCash.mockResolvedValue(STORED);
});

describe("AccountCashCard draft", () => {
  it("fills the draft from the stored amounts once loaded, leaving zeros blank", async () => {
    renderCard();

    await vi.waitFor(() => expect(field("ikeTata").value).toBe("100.5"));
    expect(field("ikeMama").value).toBe("");
    expect(field("ikzeTata").value).toBe("7");
  });

  it("keeps what the user typed when the card re-renders without new stored data", async () => {
    const { rerender } = renderCard();
    await vi.waitFor(() => expect(field("ikeTata").value).toBe("100.5"));

    fireEvent.change(field("ikeMama"), { target: { value: "55" } });
    rerender();
    expect(field("ikeMama").value).toBe("55");
  });

  it("re-fills the draft when the stored amounts change (e.g. after a purchase)", async () => {
    const { client } = renderCard();
    await vi.waitFor(() => expect(field("ikeTata").value).toBe("100.5"));
    fireEvent.change(field("ikeTata"), { target: { value: "1" } });

    act(() => {
      client.setQueryData(["accountCash"], { ...STORED, ikeTata: 250, updateReason: "POST_PURCHASE" });
    });

    await vi.waitFor(() => expect(field("ikeTata").value).toBe("250"));
  });
});
