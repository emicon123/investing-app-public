import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

const state = vi.hoisted(() => ({ portfolio: "VARIABLE" as "VARIABLE" | "FIXED" }));
const api = vi.hoisted(() => ({
  listSettings: vi.fn(),
  calculateRebalancing: vi.fn(),
  confirmPurchases: vi.fn(),
}));
vi.mock("@/lib/portfolio", () => ({ usePortfolio: () => ({ portfolio: state.portfolio }) }));
vi.mock("../api/index.js", () => api);

import { queryKeys } from "@/lib/queryKeys";
import { RebalancingPanel } from "./RebalancingPanel.js";

// Settings already cached — the panel prefills FIXED from them on a portfolio switch.
async function makeClient() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await client.prefetchQuery({ queryKey: queryKeys.settings.all, queryFn: api.listSettings, staleTime: 60_000 });
  return client;
}

function ui(client: QueryClient) {
  return (
    <QueryClientProvider client={client}>
      <RebalancingPanel />
    </QueryClientProvider>
  );
}

const depositInput = () => screen.getByLabelText(/Kwota wpłaty/) as HTMLInputElement;

beforeEach(() => {
  vi.clearAllMocks();
  state.portfolio = "VARIABLE";
  api.listSettings.mockResolvedValue({
    data: [{ key: "fixed_monthly_deposit_default_pln", value: "400" }],
  });
});

describe("RebalancingPanel deposit amount reset", () => {
  it("starts empty for VARIABLE and keeps what the user typed across re-renders", async () => {
    const client = await makeClient();
    const view = render(ui(client));

    fireEvent.change(depositInput(), { target: { value: "1500" } });
    view.rerender(ui(client));

    expect(depositInput().value).toBe("1500");
  });

  it("prefills the stored default when the portfolio switches to FIXED, and clears it again for VARIABLE", async () => {
    const client = await makeClient();
    const view = render(ui(client));

    state.portfolio = "FIXED";
    view.rerender(ui(client));
    expect(depositInput().value).toBe("400");

    fireEvent.change(depositInput(), { target: { value: "999" } });
    state.portfolio = "VARIABLE";
    view.rerender(ui(client));
    expect(depositInput().value).toBe("");
  });
});
