import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiError, IDEMPOTENCY_IN_PROGRESS_MESSAGE, IDEMPOTENCY_KEY_REUSED_MESSAGE, type IdempotencyOptions } from "@common/app-core";
import { DepositsPage } from "./DepositsPage.js";

type CreateDeposit = (values: unknown, options: IdempotencyOptions) => Promise<unknown>;

function renderPage(createDeposit: CreateDeposit) {
  return render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { mutations: { retry: false } } })}>
      <DepositsPage<string>
        title="Depozyty"
        dialogTitle="Dodaj depozyt"
        useDepositsQuery={() => ({ data: [], isPending: false, isError: false })}
        renderTable={() => null}
        createDeposit={createDeposit}
        onDepositSaved={() => {}}
      />
    </QueryClientProvider>,
  );
}

// The form's FormControl wraps its input in a div that carries the label id, so getByLabelText
// cannot reach the <input>; address the fields by their react-hook-form name instead.
function field(name: string): HTMLInputElement {
  const el = document.querySelector<HTMLInputElement>(`input[name="${name}"]`);
  if (!el) throw new Error(`no input named ${name}`);
  return el;
}

const dialogIsClosed = () => document.querySelector('input[name="amount"]') === null;

function openDialogAndFill() {
  fireEvent.click(screen.getByRole("button", { name: "Dodaj depozyt" }));
  fireEvent.change(field("depositDate"), { target: { value: "2026-11" } });
  fireEvent.change(field("amount"), { target: { value: "100" } });
}

async function submit() {
  fireEvent.click(screen.getByRole("button", { name: "Zapisz" }));
  await waitFor(() => expect(screen.queryByRole("button", { name: "Zapisuję..." })).toBeNull());
}

const keyOf = (create: ReturnType<typeof vi.fn<CreateDeposit>>, call: number) =>
  create.mock.calls[call]?.[1].idempotencyKey;

describe("DepositsPage idempotency key (ADR-111)", () => {
  it("reuses the key when the same form is submitted again after a failure", async () => {
    const create = vi.fn<CreateDeposit>().mockRejectedValue(new ApiError(500, [{ code: "INTERNAL", message: "boom" }], "r"));
    renderPage(create);
    openDialogAndFill();

    await submit();
    await submit();

    expect(create).toHaveBeenCalledTimes(2);
    expect(keyOf(create, 0)).toMatch(/^[0-9a-f-]{36}$/);
    expect(keyOf(create, 1)).toBe(keyOf(create, 0));
  });

  it("tells the user to check the history on IDEMPOTENCY_IN_PROGRESS and starts a new key", async () => {
    const create = vi
      .fn<CreateDeposit>()
      .mockRejectedValue(new ApiError(409, [{ code: "IDEMPOTENCY_IN_PROGRESS", message: "busy" }], "r"));
    renderPage(create);
    openDialogAndFill();

    await submit();
    expect(screen.getByRole("alert").textContent).toBe(IDEMPOTENCY_IN_PROGRESS_MESSAGE);

    await submit();
    expect(keyOf(create, 1)).not.toBe(keyOf(create, 0));
  });

  it("shows the Polish key-reused message and sends the edited form under a new key", async () => {
    const create = vi
      .fn<CreateDeposit>()
      .mockRejectedValueOnce(new ApiError(422, [{ code: "IDEMPOTENCY_KEY_REUSED", message: "different body" }], "r"))
      .mockResolvedValue({});
    renderPage(create);
    openDialogAndFill();

    await submit();
    expect(screen.getByRole("alert").textContent).toBe(IDEMPOTENCY_KEY_REUSED_MESSAGE);

    await submit();
    expect(create).toHaveBeenCalledTimes(2);
    expect(keyOf(create, 1)).not.toBe(keyOf(create, 0));
  });

  it("uses a new key for the next dialog after a success", async () => {
    const create = vi.fn<CreateDeposit>().mockResolvedValue({});
    renderPage(create);

    openDialogAndFill();
    fireEvent.click(screen.getByRole("button", { name: "Zapisz" }));
    await waitFor(() => expect(create).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(dialogIsClosed()).toBe(true));

    openDialogAndFill();
    fireEvent.click(screen.getByRole("button", { name: "Zapisz" }));
    await waitFor(() => expect(create).toHaveBeenCalledTimes(2));

    expect(keyOf(create, 1)).not.toBe(keyOf(create, 0));
  });

  it("uses a new key for the next dialog after cancelling a failed attempt", async () => {
    const create = vi.fn<CreateDeposit>().mockRejectedValue(new ApiError(500, [{ code: "INTERNAL", message: "boom" }], "r"));
    renderPage(create);

    openDialogAndFill();
    await submit();
    fireEvent.click(screen.getByRole("button", { name: "Anuluj" }));
    await waitFor(() => expect(dialogIsClosed()).toBe(true));

    openDialogAndFill();
    await submit();

    expect(keyOf(create, 1)).not.toBe(keyOf(create, 0));
  });
});
