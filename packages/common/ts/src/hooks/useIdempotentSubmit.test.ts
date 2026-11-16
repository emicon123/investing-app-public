import { describe, expect, it, vi } from "vitest";
import { act, renderHook } from "@testing-library/react";
import { useIdempotentSubmit } from "./useIdempotentSubmit.js";
import { ApiError } from "../client/errors.js";
import type { IdempotencyOptions } from "../client/idempotency.js";

const inProgress = () => new ApiError(409, [{ code: "IDEMPOTENCY_IN_PROGRESS", message: "busy" }], "r");

describe("useIdempotentSubmit", () => {
  it("passes the current key to the send function", async () => {
    const send = vi.fn(async (_body: string, _options: IdempotencyOptions) => "ok");
    const { result } = renderHook(() => useIdempotentSubmit(send));
    await act(async () => {
      await result.current.submit("body");
    });
    expect(send).toHaveBeenCalledOnce();
    expect(send.mock.calls[0]?.[0]).toBe("body");
    expect(send.mock.calls[0]?.[1].idempotencyKey).toMatch(/^[0-9a-f-]{36}$/);
  });

  it("reuses the key on retry after a failure and rotates it after a success", async () => {
    const keys: (string | undefined)[] = [];
    const send = vi
      .fn<(body: string, options: IdempotencyOptions) => Promise<string>>()
      .mockImplementation(async (_b, o) => {
        keys.push(o.idempotencyKey);
        if (keys.length === 1) throw new ApiError(500, [{ code: "INTERNAL", message: "boom" }], "r");
        return "ok";
      });
    const { result } = renderHook(() => useIdempotentSubmit(send));

    await act(async () => {
      await expect(result.current.submit("b")).rejects.toBeInstanceOf(ApiError);
    });
    await act(async () => {
      await expect(result.current.submit("b")).resolves.toBe("ok");
    });
    await act(async () => {
      await result.current.submit("b");
    });

    expect(keys[1]).toBe(keys[0]); // retry after the failure kept the key
    expect(keys[2]).not.toBe(keys[1]); // the success rotated it
  });

  it("rotates on IDEMPOTENCY_IN_PROGRESS and rethrows the original error", async () => {
    const keys: (string | undefined)[] = [];
    const send = vi.fn(async (_b: string, o: IdempotencyOptions) => {
      keys.push(o.idempotencyKey);
      throw inProgress();
    });
    const { result } = renderHook(() => useIdempotentSubmit(send));

    await act(async () => {
      await expect(result.current.submit("b")).rejects.toMatchObject({ status: 409 });
    });
    await act(async () => {
      await expect(result.current.submit("b")).rejects.toMatchObject({ status: 409 });
    });

    expect(keys[1]).not.toBe(keys[0]);
  });

  it("rotates on IDEMPOTENCY_KEY_REUSED so the edited form's next submit is a new operation", async () => {
    const keys: (string | undefined)[] = [];
    const send = vi.fn(async (_b: string, o: IdempotencyOptions) => {
      keys.push(o.idempotencyKey);
      if (keys.length === 1) {
        throw new ApiError(422, [{ code: "IDEMPOTENCY_KEY_REUSED", message: "different body" }], "r");
      }
      return "ok";
    });
    const { result } = renderHook(() => useIdempotentSubmit(send));

    await act(async () => {
      await expect(result.current.submit("edited")).rejects.toMatchObject({ status: 422 });
    });
    await act(async () => {
      await expect(result.current.submit("edited")).resolves.toBe("ok");
    });

    expect(keys[1]).not.toBe(keys[0]);
  });

  it("keeps the key after a 4xx so a corrected retry replays under the same key", async () => {
    const keys: (string | undefined)[] = [];
    const send = vi.fn(async (_b: string, o: IdempotencyOptions) => {
      keys.push(o.idempotencyKey);
      throw new ApiError(422, [{ code: "VALIDATION_ERROR", message: "bad" }], "r");
    });
    const { result } = renderHook(() => useIdempotentSubmit(send));
    for (let i = 0; i < 2; i++) {
      await act(async () => {
        await expect(result.current.submit("b")).rejects.toBeInstanceOf(ApiError);
      });
    }
    expect(keys[1]).toBe(keys[0]);
  });

  it("rotate() starts a fresh intent (dialog closed)", async () => {
    const keys: (string | undefined)[] = [];
    const send = vi.fn(async (_b: string, o: IdempotencyOptions) => {
      keys.push(o.idempotencyKey);
      throw new Error("net");
    });
    const { result } = renderHook(() => useIdempotentSubmit(send));
    await act(async () => {
      await expect(result.current.submit("b")).rejects.toThrow();
    });
    act(() => result.current.rotate());
    await act(async () => {
      await expect(result.current.submit("b")).rejects.toThrow();
    });
    expect(keys[1]).not.toBe(keys[0]);
  });
});
