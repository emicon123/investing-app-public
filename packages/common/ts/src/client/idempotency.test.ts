import { afterEach, describe, expect, it, vi } from "vitest";
import { IDEMPOTENCY_HEADER, idempotencyHeaders, newIdempotencyKey } from "./idempotency.js";

const UUID_V4 = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("newIdempotencyKey", () => {
  it("returns a v4 UUID when crypto.randomUUID is available", () => {
    expect(typeof crypto.randomUUID).toBe("function");
    expect(newIdempotencyKey()).toMatch(UUID_V4);
  });

  it("uses crypto.randomUUID when present", () => {
    const randomUUID = vi.fn(() => "11111111-2222-4333-8444-555555555555");
    vi.stubGlobal("crypto", { randomUUID, getRandomValues: crypto.getRandomValues.bind(crypto) });
    expect(newIdempotencyKey()).toBe("11111111-2222-4333-8444-555555555555");
    expect(randomUUID).toHaveBeenCalledOnce();
  });

  it("falls back to getRandomValues (plain-HTTP LAN access has no randomUUID)", () => {
    vi.stubGlobal("crypto", { getRandomValues: crypto.getRandomValues.bind(crypto) });
    const key = newIdempotencyKey();
    expect(key).toMatch(UUID_V4);
    expect(newIdempotencyKey()).not.toBe(key);
  });

  it("sets the v4 version and RFC 4122 variant bits on the fallback", () => {
    vi.stubGlobal("crypto", {
      getRandomValues: <T extends ArrayBufferView>(bytes: T): T => {
        new Uint8Array(bytes.buffer).fill(0xff);
        return bytes;
      },
    });
    expect(newIdempotencyKey()).toBe("ffffffff-ffff-4fff-bfff-ffffffffffff");
  });

  it("produces printable ASCII within the 255-byte header limit", () => {
    expect(newIdempotencyKey()).toMatch(/^[\x21-\x7e]{1,255}$/);
  });
});

describe("idempotencyHeaders", () => {
  it("is empty without a key", () => {
    expect(idempotencyHeaders(undefined)).toEqual({});
    expect(idempotencyHeaders()).toEqual({});
  });

  it("carries the key under the Idempotency-Key header", () => {
    expect(IDEMPOTENCY_HEADER).toBe("Idempotency-Key");
    expect(idempotencyHeaders("abc")).toEqual({ "Idempotency-Key": "abc" });
  });
});
