import { describe, expect, it } from "vitest";
import { act, renderHook } from "@testing-library/react";
import { useIdempotencyKey } from "./useIdempotencyKey.js";

describe("useIdempotencyKey", () => {
  it("keeps the same key across re-renders", () => {
    const { result, rerender } = renderHook(() => useIdempotencyKey());
    const first = result.current.key;
    rerender();
    rerender();
    expect(result.current.key).toBe(first);
  });

  it("rotate replaces the key with a fresh one", () => {
    const { result } = renderHook(() => useIdempotencyKey());
    const first = result.current.key;
    act(() => result.current.rotate());
    expect(result.current.key).not.toBe(first);
    expect(result.current.key).toMatch(/^[0-9a-f-]{36}$/);
  });

  it("gives every hook instance its own key", () => {
    const a = renderHook(() => useIdempotencyKey());
    const b = renderHook(() => useIdempotencyKey());
    expect(a.result.current.key).not.toBe(b.result.current.key);
  });

  it("keeps rotate's identity stable so it is safe in effect dependencies", () => {
    const { result, rerender } = renderHook(() => useIdempotencyKey());
    const rotate = result.current.rotate;
    rerender();
    expect(result.current.rotate).toBe(rotate);
  });
});
